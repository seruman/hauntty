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

var feedPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

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
	feedCh      chan *[]byte
	done        chan struct{}
	exitCode    int32
	tempDir     string
}

func resolveCommand(command []string, env []string) []string {
	if len(command) > 0 {
		return command
	}
	for _, e := range env {
		if len(e) > 6 && e[:6] == "SHELL=" {
			return []string{e[6:]}
		}
	}
	if shell := os.Getenv("SHELL"); shell != "" {
		return []string{shell}
	}
	return []string{"/bin/sh"}
}

func newSession(ctx context.Context, name string, command []string, env []string, cols, rows uint16, scrollback uint32, wasmRT *wasm.Runtime) (*Session, error) {
	command = resolveCommand(command, env)

	shellArgs, shellEnv, tempDir, err := SetupShellEnv(command, env, name)
	if err != nil {
		slog.Warn("shell integration setup failed, continuing without it", "err", err)
		shellArgs = command
		shellEnv = env
	}

	cmd := exec.Command(shellArgs[0], shellArgs[1:]...)
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
		if cerr := ptmx.Close(); cerr != nil {
			slog.Warn("close pty on cleanup", "err", cerr)
		}
		if kerr := cmd.Process.Kill(); kerr != nil {
			slog.Warn("kill process on cleanup", "err", kerr)
		}
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
		feedCh:    make(chan *[]byte, 64),
		done:      make(chan struct{}),
		tempDir:   tempDir,
	}

	go s.feedLoop(ctx)
	go s.readLoop(ctx)
	return s, nil
}

func restoreSession(ctx context.Context, name string, command []string, env []string, cols, rows uint16, scrollback uint32, wasmRT *wasm.Runtime, state *SessionState) (*Session, error) {
	term, err := wasmRT.NewTerminal(ctx, uint32(state.Cols), uint32(state.Rows), scrollback)
	if err != nil {
		return nil, err
	}

	if len(state.VT) > 0 {
		if err := term.Feed(ctx, state.VT); err != nil {
			term.Close(ctx)
			return nil, err
		}
	}
	if state.IsAltScreen {
		if err := term.Feed(ctx, []byte("\x1b[?1049l")); err != nil {
			term.Close(ctx)
			return nil, err
		}
	}
	// DECSTR: clear modes left by the dead process.
	if err := term.Feed(ctx, []byte("\x1b[!p")); err != nil {
		term.Close(ctx)
		return nil, err
	}

	command = resolveCommand(command, env)

	shellArgs, shellEnv, tempDir, err := SetupShellEnv(command, env, name)
	if err != nil {
		slog.Warn("shell integration setup failed, continuing without it", "err", err)
		shellArgs = command
		shellEnv = env
	}

	cmd := exec.Command(shellArgs[0], shellArgs[1:]...)
	cmd.Env = shellEnv

	ws := &pty.Winsize{Rows: rows, Cols: cols}
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		term.Close(ctx)
		if tempDir != "" {
			os.RemoveAll(tempDir)
		}
		return nil, err
	}

	if state.Cols != cols || state.Rows != rows {
		if err := term.Resize(ctx, uint32(cols), uint32(rows)); err != nil {
			slog.Warn("wasm resize on restore", "session", name, "err", err)
		}
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
		feedCh:    make(chan *[]byte, 64),
		done:      make(chan struct{}),
		tempDir:   tempDir,
	}

	go s.feedLoop(ctx)
	go s.readLoop(ctx)
	return s, nil
}

func (s *Session) feedLoop(ctx context.Context) {
	for bp := range s.feedCh {
		if err := s.term.Feed(ctx, *bp); err != nil {
			slog.Debug("wasm feed error", "session", s.Name, "err", err)
		}
		*bp = (*bp)[:cap(*bp)]
		feedPool.Put(bp)
	}
}

// Backpressure: readLoop blocks on both client writes (kernel socket
// buffer) and WASM feeds (channel send), which blocks PTY reads,
// which makes the child process block on write.
func (s *Session) readLoop(ctx context.Context) {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			// WriteMessage copies internally, so buf[:n] is safe to reuse.
			s.clientMu.Lock()
			if s.client != nil {
				if werr := s.client.WriteMessage(&protocol.Output{Data: buf[:n]}); werr != nil {
					slog.Debug("client write error", "session", s.Name, "err", werr)
				}
			}
			s.clientMu.Unlock()

			bp := feedPool.Get().(*[]byte)
			data := (*bp)[:n]
			copy(data, buf[:n])
			*bp = data
			s.feedCh <- bp
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
		if err := s.client.WriteMessage(&protocol.Exited{ExitCode: s.exitCode}); err != nil {
			slog.Debug("write exited notification", "session", s.Name, "err", err)
		}
	}
	s.clientMu.Unlock()
}

func (s *Session) attach(conn *protocol.Conn, closeConn func() error, cols, rows uint16) error {
	s.disconnectClient()

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

	s.clientMu.Lock()
	s.client = conn
	s.clientClose = closeConn
	s.clientMu.Unlock()

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
	if rerr := s.term.Resize(context.Background(), uint32(cols), uint32(rows)); rerr != nil {
		slog.Warn("wasm resize", "session", s.Name, "err", rerr)
	}
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
