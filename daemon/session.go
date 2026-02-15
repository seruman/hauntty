package daemon

import (
	"context"
	"log/slog"
	"math"
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

type attachedClient struct {
	conn   *protocol.Conn
	close  func() error
	cols   uint16
	rows   uint16
	xpixel uint16
	ypixel uint16
	outCh  chan []byte
	done   chan struct{}
}

func (ac *attachedClient) writeLoop() {
	for data := range ac.outCh {
		if err := ac.conn.WriteMessage(&protocol.Output{Data: data}); err != nil {
			break
		}
	}
	close(ac.done)
}

type Session struct {
	Name      string
	Cols      uint16
	Rows      uint16
	PID       uint32
	CreatedAt time.Time

	mu           sync.Mutex
	ptmx         *os.File
	cmd          *exec.Cmd
	term         *wasm.Terminal
	clients      []*attachedClient
	clientMu     sync.Mutex
	feedCh       chan *[]byte
	done         chan struct{}
	exitCode     int32
	tempDir      string
	resizePolicy string
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

func newSession(ctx context.Context, name string, command []string, env []string, cols, rows, xpixel, ypixel uint16, scrollback uint32, wasmRT *wasm.Runtime, resizePolicy string) (*Session, error) {
	env = mergeEnv(os.Environ(), env)
	command = resolveCommand(command, env)

	shellArgs, shellEnv, tempDir, err := SetupShellEnv(command, env, name)
	if err != nil {
		slog.Warn("shell integration setup failed, continuing without it", "err", err)
		shellArgs = command
		shellEnv = env
	}

	cmd := exec.Command(shellArgs[0], shellArgs[1:]...)
	cmd.Env = shellEnv

	ws := &pty.Winsize{Rows: rows, Cols: cols, X: xpixel, Y: ypixel}
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
		Name:         name,
		Cols:         cols,
		Rows:         rows,
		PID:          uint32(cmd.Process.Pid),
		CreatedAt:    time.Now(),
		ptmx:         ptmx,
		cmd:          cmd,
		term:         term,
		feedCh:       make(chan *[]byte, 64),
		done:         make(chan struct{}),
		tempDir:      tempDir,
		resizePolicy: resizePolicy,
	}

	go s.feedLoop(ctx)
	go s.readLoop(ctx)
	return s, nil
}

func restoreSession(ctx context.Context, name string, command []string, env []string, cols, rows, xpixel, ypixel uint16, scrollback uint32, wasmRT *wasm.Runtime, state *SessionState, resizePolicy string) (*Session, error) {
	env = mergeEnv(os.Environ(), env)

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

	ws := &pty.Winsize{Rows: rows, Cols: cols, X: xpixel, Y: ypixel}
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
		Name:         name,
		Cols:         cols,
		Rows:         rows,
		PID:          uint32(cmd.Process.Pid),
		CreatedAt:    time.Now(),
		ptmx:         ptmx,
		cmd:          cmd,
		term:         term,
		feedCh:       make(chan *[]byte, 64),
		done:         make(chan struct{}),
		tempDir:      tempDir,
		resizePolicy: resizePolicy,
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

func (s *Session) readLoop(ctx context.Context) {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			s.clientMu.Lock()
			s.broadcastOutput(data)
			s.clientMu.Unlock()

			bp := feedPool.Get().(*[]byte)
			feedData := (*bp)[:n]
			copy(feedData, buf[:n])
			*bp = feedData
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

	exitMsg := &protocol.Exited{ExitCode: s.exitCode}
	s.clientMu.Lock()
	clients := s.clients
	s.clients = nil
	s.clientMu.Unlock()
	for _, ac := range clients {
		close(ac.outCh)
		<-ac.done
		ac.conn.WriteMessage(exitMsg)
	}
}

func (s *Session) attach(ctx context.Context, conn *protocol.Conn, closeConn func() error, cols, rows, xpixel, ypixel uint16) (*attachedClient, error) {
	dump, err := s.term.DumpScreen(ctx, wasm.DumpVTFull)
	if err != nil {
		return nil, err
	}
	err = conn.WriteMessage(&protocol.State{
		ScreenDump:        dump.VT,
		CursorRow:         dump.CursorRow,
		CursorCol:         dump.CursorCol,
		IsAlternateScreen: dump.IsAltScreen,
	})
	if err != nil {
		return nil, err
	}

	ac := &attachedClient{
		conn:   conn,
		close:  closeConn,
		cols:   cols,
		rows:   rows,
		xpixel: xpixel,
		ypixel: ypixel,
		outCh:  make(chan []byte, 64),
		done:   make(chan struct{}),
	}
	go ac.writeLoop()

	s.addClient(ac)
	s.arbitrateResize()

	return ac, nil
}

// detachClient removes a specific client without closing its net.Conn.
func (s *Session) detachClient(ac *attachedClient) {
	s.removeClient(ac)
	s.arbitrateResize()
}

// disconnectAllClients removes all clients and closes their connections.
func (s *Session) disconnectAllClients() {
	s.clientMu.Lock()
	clients := s.clients
	s.clients = nil
	s.clientMu.Unlock()
	for _, ac := range clients {
		close(ac.outCh)
		<-ac.done
		ac.close()
	}
}

func (s *Session) addClient(ac *attachedClient) {
	s.clientMu.Lock()
	s.clients = append(s.clients, ac)
	count := uint16(len(s.clients))
	s.clientMu.Unlock()
	s.broadcastClientsChanged(count)
}

func (s *Session) removeClient(ac *attachedClient) {
	s.clientMu.Lock()
	found := false
	for i, c := range s.clients {
		if c == ac {
			s.clients = append(s.clients[:i], s.clients[i+1:]...)
			found = true
			break
		}
	}
	count := uint16(len(s.clients))
	s.clientMu.Unlock()
	if !found {
		return
	}
	close(ac.outCh)
	<-ac.done
	s.broadcastClientsChanged(count)
}

// broadcastOutput sends output data to all clients' outCh channels.
// Evicts clients whose channels are full. Must be called with clientMu held.
func (s *Session) broadcastOutput(data []byte) {
	var evict []*attachedClient
	for _, ac := range s.clients {
		select {
		case ac.outCh <- data:
		default:
			evict = append(evict, ac)
		}
	}
	for _, ac := range evict {
		for i, c := range s.clients {
			if c == ac {
				s.clients = append(s.clients[:i], s.clients[i+1:]...)
				break
			}
		}
		slog.Debug("evicting slow client", "session", s.Name)
		close(ac.outCh)
		go func(ac *attachedClient) {
			<-ac.done
			ac.close()
		}(ac)
	}
}

func (s *Session) broadcastClientsChanged(count uint16) {
	cols, rows := s.size()
	s.clientMu.Lock()
	msg := &protocol.ClientsChanged{
		Count: count,
		Cols:  cols,
		Rows:  rows,
	}
	for _, ac := range s.clients {
		ac.conn.WriteMessage(msg)
	}
	s.clientMu.Unlock()
}

func (s *Session) kill() {
	syscall.Kill(-int(s.PID), syscall.SIGHUP)
}

func (s *Session) sendInput(data []byte) error {
	_, err := s.ptmx.Write(data)
	return err
}

func (s *Session) size() (uint16, uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Cols, s.Rows
}

func (s *Session) resize(ctx context.Context, cols, rows, xpixel, ypixel uint16) error {
	s.mu.Lock()
	s.Cols = cols
	s.Rows = rows
	s.mu.Unlock()

	err := pty.Setsize(s.ptmx, &pty.Winsize{Rows: rows, Cols: cols, X: xpixel, Y: ypixel})
	if err != nil {
		return err
	}
	syscall.Kill(-int(s.PID), syscall.SIGWINCH)
	if rerr := s.term.Resize(ctx, uint32(cols), uint32(rows)); rerr != nil {
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

func (s *Session) arbitrateResize() {
	s.clientMu.Lock()
	if len(s.clients) == 0 {
		s.clientMu.Unlock()
		return
	}
	var cols, rows, xpixel, ypixel uint16
	switch s.resizePolicy {
	case "largest":
		for _, c := range s.clients {
			cols = max(cols, c.cols)
			rows = max(rows, c.rows)
			xpixel = max(xpixel, c.xpixel)
			ypixel = max(ypixel, c.ypixel)
		}
	case "last":
		last := s.clients[len(s.clients)-1]
		cols, rows = last.cols, last.rows
		xpixel, ypixel = last.xpixel, last.ypixel
	case "first":
		first := s.clients[0]
		cols, rows = first.cols, first.rows
		xpixel, ypixel = first.xpixel, first.ypixel
	default: // "smallest"
		cols, rows = math.MaxUint16, math.MaxUint16
		xpixel, ypixel = math.MaxUint16, math.MaxUint16
		for _, c := range s.clients {
			cols = min(cols, c.cols)
			rows = min(rows, c.rows)
			xpixel = min(xpixel, c.xpixel)
			ypixel = min(ypixel, c.ypixel)
		}
	}
	s.clientMu.Unlock()

	curCols, curRows := s.size()
	if cols != curCols || rows != curRows {
		s.resize(context.Background(), cols, rows, xpixel, ypixel)
	}
}

func (s *Session) close(ctx context.Context) {
	s.disconnectAllClients()
	s.kill()
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		slog.Warn("child ignored SIGHUP, sending SIGKILL", "session", s.Name)
		syscall.Kill(-int(s.PID), syscall.SIGKILL)
		<-s.done
	}
	s.ptmx.Close()
	s.term.Close(ctx)
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}
