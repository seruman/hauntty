package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/selman/hauntty/protocol"
	"github.com/selman/hauntty/wasm"
)

// Session represents a single terminal session managed by the daemon.
type Session struct {
	Name      string
	Cols      uint16
	Rows      uint16
	PID       uint32
	CreatedAt time.Time

	mu          sync.Mutex
	ptmx        *os.File
	cmd         *exec.Cmd
	term        *wasm.Terminal
	client      *protocol.Conn
	clientClose func() error // closes the underlying net.Conn of the attached client
	clientMu    sync.Mutex
	feedCh      chan []byte
	done        chan struct{}
	exitCode int32
	tempDir  string
}

func newSession(ctx context.Context, name, command string, env []string, cols, rows uint16, scrollback uint32, wasmRT *wasm.Runtime) (*Session, error) {
	if command == "" {
		// Prefer SHELL from the client's forwarded env over the daemon's own env.
		for _, e := range env {
			if len(e) > 6 && e[:6] == "SHELL=" {
				command = e[6:]
				break
			}
		}
		if command == "" {
			command = os.Getenv("SHELL")
		}
		if command == "" {
			command = "/bin/sh"
		}
	}

	// Apply shell integration (sets HAUNTTY_SESSION, ZDOTDIR, etc.).
	shellCmd, shellEnv, tempDir, err := SetupShellEnv(command, env, name)
	if err != nil {
		slog.Warn("shell integration setup failed, continuing without it", "err", err)
		shellCmd = command
		shellEnv = env
	}

	cmd := exec.Command(shellCmd)
	cmd.Env = shellEnv

	ws := &pty.Winsize{Rows: rows, Cols: cols}
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		return nil, err
	}

	term, err := wasmRT.NewTerminal(ctx, uint32(cols), uint32(rows), scrollback)
	if err != nil {
		ptmx.Close()
		cmd.Process.Kill()
		cmd.Wait()
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		return nil, err
	}

	s := &Session{
		Name:      name,
		Cols:      cols,
		Rows:      rows,
		PID:       uint32(cmd.Process.Pid),
		CreatedAt: time.Now(),
		ptmx:      ptmx,
		cmd:       cmd,
		term:      term,
		feedCh:    make(chan []byte, 64),
		done:      make(chan struct{}),
		tempDir:   tempDir,
	}

	go s.feedLoop(ctx)
	go s.readLoop(ctx)
	return s, nil
}

// feedLoop consumes PTY output from feedCh and feeds it to the WASM terminal.
// Runs in its own goroutine so WASM processing never blocks the client path.
func (s *Session) feedLoop(ctx context.Context) {
	for data := range s.feedCh {
		if err := s.term.Feed(ctx, data); err != nil {
			slog.Debug("wasm feed error", "session", s.Name, "err", err)
		}
	}
}

// readLoop reads PTY output, forwards it to the attached client and queues it
// for WASM state machine processing. The client write uses buf[:n] directly
// (WriteMessage copies internally), avoiding an allocation on the hot path.
// Backpressure: WriteMessage calls net.Conn.Write synchronously. When the
// kernel socket send buffer is full, Write blocks, which blocks readLoop,
// which blocks PTY reads, which makes the child process block on write —
// natural end-to-end flow control with no extra code needed.
func (s *Session) readLoop(ctx context.Context) {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			// Client write: pass buf slice directly (WriteMessage copies internally).
			s.clientMu.Lock()
			if s.client != nil {
				if werr := s.client.WriteMessage(&protocol.Output{Data: buf[:n]}); werr != nil {
					slog.Debug("client write error", "session", s.Name, "err", werr)
				}
			}
			s.clientMu.Unlock()

			// Async WASM feed: copy needed since feedLoop consumes later.
			data := make([]byte, n)
			copy(data, buf[:n])
			select {
			case s.feedCh <- data:
			default:
				slog.Debug("wasm feed channel full, dropping", "session", s.Name)
			}
		}
		if err != nil {
			break
		}
	}
	close(s.feedCh)

	s.cmd.Wait()
	if ws, ok := s.cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
		s.exitCode = int32(ws.ExitStatus())
	}
	close(s.done)

	s.clientMu.Lock()
	if s.client != nil {
		s.client.WriteMessage(&protocol.Exited{ExitCode: s.exitCode})
	}
	s.clientMu.Unlock()
}

func (s *Session) attach(conn *protocol.Conn, closeConn func() error, cols, rows uint16) error {
	// Disconnect any existing client (closes their connection).
	s.disconnectClient()

	// Send state dump to the new client (full VT for state restoration).
	dump, err := s.term.DumpScreen(context.Background(), wasm.DumpVTFull)
	if err != nil {
		return err
	}
	err = conn.WriteMessage(&protocol.State{
		ScreenDump:        dump.VT,
		CursorRow:         dump.CursorRow,
		CursorCol:         dump.CursorCol,
		IsAlternateScreen: dump.IsAltScreen,
	})
	if err != nil {
		return err
	}

	// Set the new client.
	s.clientMu.Lock()
	s.client = conn
	s.clientClose = closeConn
	s.clientMu.Unlock()

	// Resize if client dimensions differ.
	if cols != s.Cols || rows != s.Rows {
		s.resize(cols, rows)
	}

	return nil
}

// detach clears the attached client without closing the connection.
// Used when the same connection sends a DETACH message.
func (s *Session) detach() {
	s.clientMu.Lock()
	s.client = nil
	s.clientClose = nil
	s.clientMu.Unlock()
}

// disconnectClient clears the attached client AND closes the underlying
// connection so the remote attach process unblocks. Used by "ht detach".
func (s *Session) disconnectClient() {
	s.clientMu.Lock()
	closeFn := s.clientClose
	s.client = nil
	s.clientClose = nil
	s.clientMu.Unlock()
	if closeFn != nil {
		closeFn()
	}
}

func (s *Session) kill() {
	syscall.Kill(-int(s.PID), syscall.SIGHUP)
}

func (s *Session) sendInput(data []byte) error {
	_, err := s.ptmx.Write(data)
	return err
}

func (s *Session) resize(cols, rows uint16) error {
	s.mu.Lock()
	s.Cols = cols
	s.Rows = rows
	s.mu.Unlock()

	err := pty.Setsize(s.ptmx, &pty.Winsize{Rows: rows, Cols: cols})
	if err != nil {
		return err
	}
	syscall.Kill(-int(s.PID), syscall.SIGWINCH)
	s.term.Resize(context.Background(), uint32(cols), uint32(rows))
	return nil
}

func (s *Session) dumpScreen(ctx context.Context, format uint32) (*wasm.ScreenDump, error) {
	return s.term.DumpScreen(ctx, format)
}

func (s *Session) isRunning() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
}

func (s *Session) close(ctx context.Context) {
	s.kill()
	<-s.done
	s.ptmx.Close()
	s.term.Close(ctx)
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}
