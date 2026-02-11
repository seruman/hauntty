package client

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"

	"github.com/selman/hauntty/protocol"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// isConnClosed returns true if err indicates a closed network connection.
func isConnClosed(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return opErr.Err.Error() == "use of closed network connection"
	}
	return false
}

// envVarsToForward lists environment variables forwarded from the client
// to the daemon on session creation.
var envVarsToForward = []string{
	"SHELL",
	"GHOSTTY_RESOURCES_DIR",
	"TERM",
	"COLORTERM",
	"GHOSTTY_BIN_DIR",
}

// collectEnv gathers environment variables to forward to the daemon.
func collectEnv() []string {
	var env []string
	for _, key := range envVarsToForward {
		if val, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+val)
		}
	}
	return env
}

// detachSeq is the CSI u encoding of Ctrl-] (\x1b[93;5u).
var detachSeq = []byte("\x1b[93;5u")

// detachIndex returns the index of the first detach trigger in data,
// checking for both the raw byte 0x1d and the kitty keyboard CSI u
// sequence \x1b[93;5u. Returns -1 if not found.
func detachIndex(data []byte) int {
	if i := bytes.IndexByte(data, 0x1d); i >= 0 {
		return i
	}
	return bytes.Index(data, detachSeq)
}

// RunAttach performs the interactive attach loop: connects to (or creates)
// a session, puts the terminal into raw mode, and proxies I/O until detach
// or the child process exits.
func (c *Client) RunAttach(name string, command string) error {
	fd := int(os.Stdin.Fd())

	cols, rows, err := term.GetSize(fd)
	if err != nil {
		return fmt.Errorf("get terminal size: %w", err)
	}

	env := collectEnv()

	ok, err := c.Attach(name, uint16(cols), uint16(rows), command, env, 10000)
	if err != nil {
		return err
	}

	if ok.Created {
		fmt.Fprintf(os.Stderr, "[hauntty] created session %q (pid %d)\n", ok.SessionName, ok.PID)
	} else {
		fmt.Fprintf(os.Stderr, "[hauntty] attached to session %q (pid %d)\n", ok.SessionName, ok.PID)
	}

	// Clear screen before rendering session content so it doesn't mix
	// with whatever was previously on the host terminal.
	os.Stdout.Write([]byte("\x1b[2J\x1b[H"))

	// Check for initial STATE message (screen restore on reattach).
	// We peek at the first message; if it's STATE, write the screen dump.
	// Otherwise, we handle it in the main loop.
	firstMsg, err := c.ReadMessage()
	if err != nil {
		return fmt.Errorf("read initial message: %w", err)
	}
	if state, isState := firstMsg.(*protocol.State); isState {
		os.Stdout.Write(state.ScreenDump)
		firstMsg = nil
	}

	// Put terminal into raw mode.
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("set raw mode: %w", err)
	}
	defer term.Restore(fd, oldState)

	// Handle SIGWINCH for terminal resize.
	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, unix.SIGWINCH)
	defer signal.Stop(sigwinch)

	var (
		exitCode int
		mu       sync.Mutex
		done     = make(chan struct{})
	)

	// Goroutine: watch for resize signals.
	go func() {
		for {
			select {
			case <-sigwinch:
				w, h, err := term.GetSize(fd)
				if err != nil {
					continue
				}
				mu.Lock()
				c.WriteMessage(&protocol.Resize{Cols: uint16(w), Rows: uint16(h)})
				mu.Unlock()
			case <-done:
				return
			}
		}
	}()

	// Goroutine: read from stdin and send to daemon.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				data := buf[:n]
				// Scan for detach keybind: Ctrl-] as raw byte (0x1d)
				// or kitty keyboard protocol CSI u sequence (\x1b[93;5u).
				if i := detachIndex(data); i >= 0 {
					if i > 0 {
						mu.Lock()
						c.WriteMessage(&protocol.Input{Data: data[:i]})
						mu.Unlock()
					}
					mu.Lock()
					c.Detach()
					mu.Unlock()
					return
				}
				mu.Lock()
				c.WriteMessage(&protocol.Input{Data: data})
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	// Main loop: read messages from daemon.
	handleMsg := func(msg protocol.Message) bool {
		switch m := msg.(type) {
		case *protocol.Output:
			os.Stdout.Write(m.Data)
		case *protocol.Exited:
			exitCode = int(m.ExitCode)
			return true
		case *protocol.Error:
			term.Restore(fd, oldState)
			fmt.Fprintf(os.Stderr, "[hauntty] error: %s\n", m.Message)
			exitCode = 1
			return true
		}
		return false
	}

	// Handle the first message if it wasn't a STATE.
	if firstMsg != nil {
		if handleMsg(firstMsg) {
			close(done)
			term.Restore(fd, oldState)
			os.Exit(exitCode)
		}
	}

	for {
		msg, err := c.ReadMessage()
		if err != nil {
			close(done)
			if err == io.EOF || isConnClosed(err) {
				term.Restore(fd, oldState)
				// Reset terminal modes that may have been set by the inner
				// session (mouse, bracketed paste, focus events, alt screen,
				// cursor visibility) so they don't leak into the host terminal.
				os.Stdout.Write([]byte(
					"\x1b[?1000l\x1b[?1002l\x1b[?1003l\x1b[?1006l" +
						"\x1b[?2004l\x1b[?1004l\x1b[?1049l" +
						"\x1b[?25h\x1b[0m"))
				fmt.Fprintf(os.Stderr, "\n[hauntty] detached\n")
				return nil
			}
			term.Restore(fd, oldState)
			return fmt.Errorf("read message: %w", err)
		}
		if handleMsg(msg) {
			close(done)
			term.Restore(fd, oldState)
			os.Exit(exitCode)
		}
	}
}
