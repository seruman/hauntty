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

func isConnClosed(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return opErr.Err.Error() == "use of closed network connection"
	}
	return false
}

var envVarsToForward = []string{
	"SHELL",
	"GHOSTTY_RESOURCES_DIR",
	"TERM",
	"COLORTERM",
	"GHOSTTY_BIN_DIR",
}

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

// Checks both raw Ctrl-] (0x1d) and kitty keyboard CSI u (\x1b[93;5u).
func detachIndex(data []byte) int {
	if i := bytes.IndexByte(data, 0x1d); i >= 0 {
		return i
	}
	return bytes.Index(data, detachSeq)
}

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

	// Save host terminal DEC private mode state (XTSAVE) and push kitty
	// keyboard level so we can restore exactly on detach, rather than
	// blindly resetting modes the host shell may have had enabled.
	if _, err := os.Stdout.Write([]byte(
		"\x1b[?1000;1002;1003;1006;2004;1004;1049;2048;2026;25s" +
			"\x1b[>0u")); err != nil {
		return fmt.Errorf("save terminal state: %w", err)
	}

	if _, err := os.Stdout.Write([]byte("\x1b[2J\x1b[H")); err != nil {
		return fmt.Errorf("clear screen: %w", err)
	}

	// Peek at the first message: if STATE, write the screen dump for
	// reattach restore. Otherwise fall through to the main loop.
	firstMsg, err := c.ReadMessage()
	if err != nil {
		return fmt.Errorf("read initial message: %w", err)
	}
	if state, isState := firstMsg.(*protocol.State); isState {
		if _, err := os.Stdout.Write(state.ScreenDump); err != nil {
			return fmt.Errorf("write state dump: %w", err)
		}
		firstMsg = nil
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("set raw mode: %w", err)
	}
	defer term.Restore(fd, oldState)

	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, unix.SIGWINCH)
	defer signal.Stop(sigwinch)

	var (
		exitCode int
		mu       sync.Mutex
		done     = make(chan struct{})
	)

	go func() {
		for {
			select {
			case <-sigwinch:
				w, h, err := term.GetSize(fd)
				if err != nil {
					continue
				}
				mu.Lock()
				werr := c.WriteMessage(&protocol.Resize{Cols: uint16(w), Rows: uint16(h)})
				mu.Unlock()
				if werr != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				data := buf[:n]
				if i := detachIndex(data); i >= 0 {
					if i > 0 {
						mu.Lock()
						werr := c.WriteMessage(&protocol.Input{Data: data[:i]})
						mu.Unlock()
						if werr != nil {
							return
						}
					}
					mu.Lock()
					_ = c.Detach()
					mu.Unlock()
					return
				}
				mu.Lock()
				werr := c.WriteMessage(&protocol.Input{Data: data})
				mu.Unlock()
				if werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

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
				// Restore host terminal DEC private modes (XTRESTORE) and
				// pop kitty keyboard level to pre-attach state.
				os.Stdout.Write([]byte(
					"\x1b[?1000;1002;1003;1006;2004;1004;1049;2048;2026;25r" +
						"\x1b[<u" +
						"\x1b[0m\x1b[2J\x1b[H"))
				fmt.Fprintf(os.Stderr, "[hauntty] detached\n")
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
