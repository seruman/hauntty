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

	"code.selman.me/hauntty/internal/protocol"
	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

func isConnClosed(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && errors.Is(opErr.Err, net.ErrClosed)
}

func findDetach(data []byte, dk DetachKey) int {
	if dk.rawByte != 0 {
		if i := bytes.IndexByte(data, dk.rawByte); i >= 0 {
			return i
		}
	}
	return bytes.Index(data, dk.csiSeq)
}

type AttachMetadata struct {
	Cols   uint16
	Rows   uint16
	Xpixel uint16
	Ypixel uint16
	Env    []string
	CWD    string
}

type AttachMetadataFunc func(fd int) (AttachMetadata, error)

// AttachOpts configures an interactive attach session.
type AttachOpts struct {
	Name      string
	Command   []string
	DetachKey DetachKey
	Metadata  AttachMetadataFunc
	ReadOnly  bool
	Restore   bool
}

func (c *Client) RunAttach(opts AttachOpts) error {
	fd := int(os.Stdin.Fd())

	req, err := attachRequestFromOpts(fd, opts)
	if err != nil {
		return err
	}

	attached, err := c.attach(req)
	if err != nil {
		return err
	}

	writeAttachStatus(attached, opts.ReadOnly)

	if err := pushKittyKeyboard(); err != nil {
		return err
	}

	if err := writeReattachScreenDump(fd, req.Rows, attached); err != nil {
		return err
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("set raw mode: %w", err)
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	// Drain terminal responses from the kitty keyboard push and any
	// mode-setting sequences in the state dump.
	drainStdin(fd, 50*time.Millisecond)

	var (
		mu   sync.Mutex
		done = make(chan struct{})
	)

	if !opts.ReadOnly {
		c.forwardHostResize(fd, &mu, done)
	}

	c.forwardHostInput(&mu, opts)

	if err := writeCreatedCommandDump(attached, opts.Command); err != nil {
		return err
	}

	for {
		msg, err := c.conn.ReadMessage()
		if err != nil {
			close(done)
			if err == io.EOF || isConnClosed(err) {
				restoreHostTerminal(fd, oldState, "[hauntty] detached\n")
				return nil
			}
			_ = term.Restore(fd, oldState)
			return fmt.Errorf("read message: %w", err)
		}
		if err := handleAttachMessage(fd, oldState, msg); err != nil {
			close(done)
			return err
		}
	}
}

func attachRequestFromOpts(fd int, opts AttachOpts) (*protocol.Attach, error) {
	if opts.Metadata == nil {
		return nil, fmt.Errorf("attach metadata function is required")
	}
	metadata, err := opts.Metadata(fd)
	if err != nil {
		return nil, err
	}
	return &protocol.Attach{
		Name:       opts.Name,
		Command:    opts.Command,
		Cols:       metadata.Cols,
		Rows:       metadata.Rows,
		Xpixel:     metadata.Xpixel,
		Ypixel:     metadata.Ypixel,
		Env:        metadata.Env,
		CWD:        metadata.CWD,
		Scrollback: 0,
		ReadOnly:   opts.ReadOnly,
		Restore:    opts.Restore,
	}, nil
}

func writeAttachStatus(attached *protocol.Attached, readOnly bool) {
	if attached.Created {
		fmt.Fprintf(os.Stderr, "[hauntty] created session %q (pid %d)\n", attached.Name, attached.PID)
	} else if readOnly {
		fmt.Fprintf(os.Stderr, "[hauntty] attached read-only to session %q (pid %d)\n", attached.Name, attached.PID)
	} else {
		fmt.Fprintf(os.Stderr, "[hauntty] attached to session %q (pid %d)\n", attached.Name, attached.PID)
	}
}

func pushKittyKeyboard() error {
	// Push kitty keyboard level to isolate inner session keyboard
	// modes from the host terminal.
	if _, err := os.Stdout.Write([]byte("\x1b[>0u")); err != nil {
		return fmt.Errorf("push kitty keyboard: %w", err)
	}
	return nil
}

func writeReattachScreenDump(fd int, rows uint16, attached *protocol.Attached) error {
	if attached.Created || len(attached.ScreenDump) == 0 {
		return nil
	}

	// Reattach: preserve visible content in scrollback, then
	// clear for the dump. Query cursor row via DSR so we
	// scroll exactly the content-bearing rows — no blank
	// line gap. Then EL-clear every row (ED touches
	// scrollback in Ghostty, EL does not).
	// Cooked mode so OPOST translates \n in the dump to \r\n.
	cursorRow := int(rows) // fallback: scroll everything
	if tmpState, rerr := term.MakeRaw(fd); rerr == nil {
		os.Stdout.Write([]byte("\x1b[6n"))
		cursorRow = readCursorRow(fd, int(rows))
		_ = term.Restore(fd, tmpState)
	}
	scroll := append([]byte("\x1b[999;1H"), bytes.Repeat([]byte{'\n'}, cursorRow)...)
	os.Stdout.Write(scroll)
	var clear bytes.Buffer
	for row := 1; row <= int(rows); row++ {
		fmt.Fprintf(&clear, "\x1b[%d;1H\x1b[2K", row)
	}
	clear.WriteString("\x1b[H")
	os.Stdout.Write(clear.Bytes())

	if _, err := os.Stdout.Write(attached.ScreenDump); err != nil {
		return fmt.Errorf("write state dump: %w", err)
	}
	return nil
}

func (c *Client) forwardHostResize(fd int, mu *sync.Mutex, done <-chan struct{}) {
	sigwinch := make(chan os.Signal, 1)
	signal.Notify(sigwinch, unix.SIGWINCH)

	go func() {
		defer signal.Stop(sigwinch)
		for {
			select {
			case <-sigwinch:
				ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
				if err != nil {
					continue
				}
				mu.Lock()
				werr := c.conn.WriteMessage(&protocol.Resize{
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
}

func (c *Client) forwardHostInput(mu *sync.Mutex, opts AttachOpts) {
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				data := buf[:n]
				if i := findDetach(data, opts.DetachKey); i >= 0 {
					if !opts.ReadOnly && i > 0 {
						mu.Lock()
						werr := c.conn.WriteMessage(&protocol.Input{Data: data[:i]})
						mu.Unlock()
						if werr != nil {
							return
						}
					}
					mu.Lock()
					_ = c.Detach()
					c.Close()
					mu.Unlock()
					return
				}
				if !opts.ReadOnly {
					mu.Lock()
					werr := c.conn.WriteMessage(&protocol.Input{Data: data})
					mu.Unlock()
					if werr != nil {
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()
}

func writeCreatedCommandDump(attached *protocol.Attached, command []string) error {
	if !attached.Created || len(command) == 0 || len(attached.ScreenDump) == 0 {
		return nil
	}
	if _, err := os.Stdout.Write(attached.ScreenDump); err != nil {
		return fmt.Errorf("write state dump: %w", err)
	}
	return nil
}

func handleAttachMessage(fd int, oldState *term.State, msg protocol.Message) error {
	switch m := msg.(type) {
	case *protocol.Output:
		os.Stdout.Write(m.Data)
	case *protocol.Exited:
		restoreHostTerminal(fd, oldState, "[hauntty] session exited\n")
		return &ExitError{Code: int(m.ExitCode)}
	case *protocol.Error:
		_ = term.Restore(fd, oldState)
		fmt.Fprintf(os.Stderr, "[hauntty] error: %s\n", m.Message)
		return &ExitError{Code: 1}
	case *protocol.ClientsChanged:
		_ = m // informational, no action needed
	}
	return nil
}

func restoreHostTerminal(fd int, oldState *term.State, message string) {
	// Use 1047 (not 1049) to exit alt screen: 1047 just switches the
	// buffer without restoring the saved cursor, so session content on
	// the primary screen stays intact. No-op when already on primary.
	//
	// Reset modes, show cursor, pop kitty keyboard, reset SGR, erase
	// from cursor to end of screen. Session content above the cursor is
	// preserved.
	os.Stdout.Write([]byte(
		"\x1b[?1047;1;1000;1002;1003;1006;1004;2004;2048;2026l" +
			"\x1b[?25h" +
			"\x1b[<u" +
			"\x1b[0m" +
			"\x1b[J"))
	drainStdin(fd, 20*time.Millisecond)
	_ = term.Restore(fd, oldState)
	if message != "" {
		fmt.Fprint(os.Stderr, message)
	}
}

// readCursorRow reads the DSR response (\x1b[{row};{col}R) from fd
// and returns the cursor row. Must be called in raw mode after sending
// \x1b[6n. Returns fallback if the response cannot be parsed.
func readCursorRow(fd int, fallback int) int {
	var buf [32]byte
	var n int
	deadline := time.Now().Add(100 * time.Millisecond)
	for n < len(buf) && time.Now().Before(deadline) {
		remaining := time.Until(deadline)
		tv := unix.NsecToTimeval(remaining.Nanoseconds())
		var fds unix.FdSet
		fds.Set(fd)
		ready, _ := unix.Select(fd+1, &fds, nil, nil, &tv)
		if ready <= 0 {
			break
		}
		nr, _ := unix.Read(fd, buf[n:])
		if nr > 0 {
			n += nr
			if buf[n-1] == 'R' {
				break
			}
		}
	}
	// Parse \x1b[{row};{col}R — use last '[' to skip any
	// preceding terminal responses (e.g. focus events).
	resp := buf[:n]
	start := bytes.LastIndexByte(resp, '[')
	if start < 0 {
		return fallback
	}
	resp = resp[start+1:]
	before, _, ok := bytes.Cut(resp, []byte{';'})
	if !ok {
		return fallback
	}
	row := 0
	for _, b := range before {
		if b < '0' || b > '9' {
			return fallback
		}
		row = row*10 + int(b-'0')
	}
	if row <= 0 {
		return fallback
	}
	return row
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
		_, _ = unix.Read(fd, buf)
	}
}
