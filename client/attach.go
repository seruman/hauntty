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
	"time"

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

var alwaysForwardEnv = []string{
	"TERM",
	"SHELL",
}

func collectEnv(extra []string) []string {
	var env []string
	for _, key := range alwaysForwardEnv {
		if val, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+val)
		}
	}
	for _, key := range extra {
		if val, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+val)
		}
	}
	return env
}

func findDetach(data []byte, dk DetachKey) int {
	if i := bytes.IndexByte(data, dk.rawByte); i >= 0 {
		return i
	}
	return bytes.Index(data, dk.csiSeq)
}

func (c *Client) RunAttach(name string, command []string, dk DetachKey, forwardEnv []string) error {
	fd := int(os.Stdin.Fd())

	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil {
		return fmt.Errorf("get terminal size: %w", err)
	}

	env := collectEnv(forwardEnv)

	cwd, _ := os.Getwd()

	ok, err := c.Attach(name, uint16(ws.Col), uint16(ws.Row), ws.Xpixel, ws.Ypixel, command, env, 10000, cwd)
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

	// Discard stale terminal responses (e.g. kitty keyboard query
	// replies) that accumulated in stdin while connecting. These
	// arrive because the inner shell's queries pass through to
	// the host terminal, whose responses sit in the kernel buffer
	// until we start reading.
	drainStdin(fd, 10*time.Millisecond)

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
				ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
				if err != nil {
					continue
				}
				mu.Lock()
				werr := c.WriteMessage(&protocol.Resize{
					Cols:   uint16(ws.Col),
					Rows:   uint16(ws.Row),
					Xpixel: ws.Xpixel,
					Ypixel: ws.Ypixel,
				})
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
				if i := findDetach(data, dk); i >= 0 {
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
		case *protocol.ClientsChanged:
			_ = m // informational, no action needed
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
				// All restore work happens in raw mode so we can
				// drain terminal responses before the host shell
				// sees them.

				// Disable event-generating modes.
				os.Stdout.Write([]byte("\x1b[?1004;1000;1002;1003;1006;2048l"))
				drainStdin(fd, 20*time.Millisecond)

				// Restore host terminal state: XTRESTORE (DEC private
				// modes), pop kitty keyboard, reset scroll region,
				// reset SGR, clear screen.
				os.Stdout.Write([]byte(
					"\x1b[?1000;1002;1003;1006;2004;1004;1049;2048;2026;25r" +
						"\x1b[<u" +
						"\x1b[r" +
						"\x1b[0m\x1b[2J\x1b[H"))

				// Drain responses from restored modes (focus events,
				// color reports, geometry replies, etc.).
				drainStdin(fd, 20*time.Millisecond)
				term.Restore(fd, oldState)
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

// drainStdin reads and discards any pending bytes on fd for the given
// duration. Uses unix.Select for timed polling because os.Stdin.Read
// has no timeout mechanism on tty fds. Must be called in raw mode.
func drainStdin(fd int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 256)
	for time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		tv := unix.NsecToTimeval(remaining.Nanoseconds())
		var fds unix.FdSet
		fds.Set(fd)
		n, _ := unix.Select(fd+1, &fds, nil, nil, &tv)
		if n <= 0 {
			return
		}
		unix.Read(fd, buf)
	}
}
