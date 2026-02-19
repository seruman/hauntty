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

	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
	"github.com/creack/pty"
)

var feedPool = sync.Pool{
	New: func() any {
		b := make([]byte, 32*1024)
		return &b
	},
}

type attachedClient struct {
	conn    *protocol.Conn
	close   func() error
	cols    uint16
	rows    uint16
	xpixel  uint16
	ypixel  uint16
	version string
	outCh   chan []byte
	done    chan struct{}
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
	term         *libghostty.Terminal
	clients      []*attachedClient
	activeClient *attachedClient
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

func newSession(ctx context.Context, name string, command []string, env []string, cols, rows, xpixel, ypixel uint16, scrollback uint32, wasmRT *libghostty.Runtime, resizePolicy string, cwd string) (*Session, error) {
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
	if cwd != "" {
		cmd.Dir = cwd
	}

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

func restoreSession(ctx context.Context, name string, command []string, env []string, cols, rows, xpixel, ypixel uint16, scrollback uint32, wasmRT *libghostty.Runtime, state *SessionState, resizePolicy string, cwd string) (*Session, error) {
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
	if cwd != "" {
		cmd.Dir = cwd
	}

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

func (s *Session) attach(ctx context.Context, conn *protocol.Conn, closeConn func() error, cols, rows, xpixel, ypixel uint16, version string) (*attachedClient, error) {
	// Resize before dumping so the screen dump reflects the dimensions
	// that will be in effect once this client is added.
	s.resizeForPendingClient(ctx, cols, rows, xpixel, ypixel)

	dump, err := s.term.DumpScreen(ctx, libghostty.DumpVTFull)
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
		conn:    conn,
		close:   closeConn,
		cols:    cols,
		rows:    rows,
		xpixel:  xpixel,
		ypixel:  ypixel,
		version: version,
		outCh:   make(chan []byte, 64),
		done:    make(chan struct{}),
	}
	go ac.writeLoop()

	s.addClient(ac)
	s.arbitrateResize()

	return ac, nil
}

// detachClient removes a specific client without closing its net.Conn.
func (s *Session) detachClient(ac *attachedClient) {
	if !s.removeClient(ac) {
		return
	}
	s.arbitrateResize()
}

func (s *Session) disconnectActiveClient() {
	s.clientMu.Lock()
	ac := s.activeClient
	if ac == nil && len(s.clients) == 1 {
		ac = s.clients[0]
	}
	s.clientMu.Unlock()
	if ac == nil {
		return
	}
	if !s.removeClient(ac) {
		return
	}
	s.arbitrateResize()
	_ = ac.close()
}

// disconnectAllClients removes all clients and closes their connections.
func (s *Session) disconnectAllClients() {
	s.clientMu.Lock()
	clients := s.clients
	s.clients = nil
	s.activeClient = nil
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

func (s *Session) removeClient(ac *attachedClient) bool {
	s.clientMu.Lock()
	found := false
	for i, c := range s.clients {
		if c == ac {
			s.clients = append(s.clients[:i], s.clients[i+1:]...)
			found = true
			break
		}
	}
	if s.activeClient == ac {
		s.activeClient = nil
	}
	count := uint16(len(s.clients))
	s.clientMu.Unlock()
	if !found {
		return false
	}
	close(ac.outCh)
	<-ac.done
	s.broadcastClientsChanged(count)
	return true
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
		if s.activeClient == ac {
			s.activeClient = nil
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
	msg := &protocol.ClientsChanged{
		Count: count,
		Cols:  cols,
		Rows:  rows,
	}

	s.clientMu.Lock()
	clients := append([]*attachedClient(nil), s.clients...)
	s.clientMu.Unlock()

	for _, ac := range clients {
		_ = ac.conn.WriteMessage(msg)
	}
}

func (s *Session) kill() {
	syscall.Kill(-int(s.PID), syscall.SIGHUP)
}

func (s *Session) sendInput(data []byte) error {
	_, err := s.ptmx.Write(data)
	return err
}

func (s *Session) sendInputFrom(ac *attachedClient, data []byte) error {
	s.clientMu.Lock()
	s.activeClient = ac
	s.clientMu.Unlock()
	return s.sendInput(data)
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

func (s *Session) dumpScreen(ctx context.Context, format libghostty.DumpFormat) (*libghostty.ScreenDump, error) {
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

type clientDims struct {
	cols, rows, xpixel, ypixel uint16
}

func applyResizePolicy(policy string, dims []clientDims) (cols, rows, xpixel, ypixel uint16) {
	switch policy {
	case "largest":
		for _, d := range dims {
			cols = max(cols, d.cols)
			rows = max(rows, d.rows)
			xpixel = max(xpixel, d.xpixel)
			ypixel = max(ypixel, d.ypixel)
		}
	case "last":
		d := dims[len(dims)-1]
		cols, rows = d.cols, d.rows
		xpixel, ypixel = d.xpixel, d.ypixel
	case "first":
		d := dims[0]
		cols, rows = d.cols, d.rows
		xpixel, ypixel = d.xpixel, d.ypixel
	default: // "smallest"
		cols, rows = math.MaxUint16, math.MaxUint16
		xpixel, ypixel = math.MaxUint16, math.MaxUint16
		for _, d := range dims {
			cols = min(cols, d.cols)
			rows = min(rows, d.rows)
			xpixel = min(xpixel, d.xpixel)
			ypixel = min(ypixel, d.ypixel)
		}
	}
	return
}

func (s *Session) collectClientDims() []clientDims {
	dims := make([]clientDims, len(s.clients))
	for i, c := range s.clients {
		dims[i] = clientDims{c.cols, c.rows, c.xpixel, c.ypixel}
	}
	return dims
}

func (s *Session) arbitrateResize() {
	s.clientMu.Lock()
	if len(s.clients) == 0 {
		s.clientMu.Unlock()
		return
	}
	dims := s.collectClientDims()
	s.clientMu.Unlock()

	cols, rows, xpixel, ypixel := applyResizePolicy(s.resizePolicy, dims)
	curCols, curRows := s.size()
	if cols != curCols || rows != curRows {
		s.resize(context.Background(), cols, rows, xpixel, ypixel)
	}
}

// resizeForPendingClient computes and applies the resize that would
// result from adding a client with the given dimensions, without
// actually adding it to the client list. This is used before dumping
// the screen on attach so the dump reflects the correct size.
func (s *Session) resizeForPendingClient(ctx context.Context, cols, rows, xpixel, ypixel uint16) {
	s.clientMu.Lock()
	dims := append(s.collectClientDims(), clientDims{cols, rows, xpixel, ypixel})
	s.clientMu.Unlock()

	targetCols, targetRows, targetXpixel, targetYpixel := applyResizePolicy(s.resizePolicy, dims)
	curCols, curRows := s.size()
	if targetCols != curCols || targetRows != curRows {
		s.resize(ctx, targetCols, targetRows, targetXpixel, targetYpixel)
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
