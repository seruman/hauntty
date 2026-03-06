package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
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

// All fields are owned exclusively by the session's run loop.
type sessionClient struct {
	id        string
	conn      *protocol.Conn
	closeConn func() error
	cols      uint16
	rows      uint16
	xpixel    uint16
	ypixel    uint16
	version   string
	readOnly  bool
	outCh     chan protocol.Message
}

func (c *sessionClient) writeLoop() {
	for msg := range c.outCh {
		if err := c.conn.WriteMessage(msg); err != nil {
			break
		}
	}
}

type attachReq struct {
	conn      *protocol.Conn
	closeConn func() error
	cols      uint16
	rows      uint16
	xpixel    uint16
	ypixel    uint16
	version   string
	readOnly  bool
	created   bool
	result    chan<- attachResp
}

type attachResp struct {
	client *sessionClient
	err    error
}

type detachReq struct {
	client *sessionClient
}

type resizeReq struct {
	client *sessionClient
	cols   uint16
	rows   uint16
	xpixel uint16
	ypixel uint16
}

type kickReq struct {
	clientID string
	result   chan<- bool
}

type clientInfoReq struct {
	result chan<- []protocol.SessionClient
}

type stopReq struct{}

type Session struct {
	Name      string
	PID       uint32
	CreatedAt time.Time

	ptmx    *os.File
	cmd     *exec.Cmd
	term    *libghostty.Terminal
	feedCh  chan *[]byte
	tempDir string

	actions chan any
	ptyOut chan []byte
	done chan struct{}
	exitCode int32

	// sizeVal packs cols|rows as (cols<<16)|rows for lock-free reads.
	sizeVal atomic.Uint32

	resizePolicy string
	ctx          context.Context
}

func (s *Session) size() (uint16, uint16) {
	v := s.sizeVal.Load()
	return uint16(v >> 16), uint16(v)
}

func (s *Session) setSize(cols, rows uint16) {
	s.sizeVal.Store(uint32(cols)<<16 | uint32(rows))
}

func (s *Session) isRunning() bool {
	select {
	case <-s.done:
		return false
	default:
		return true
	}
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
		PID:          uint32(cmd.Process.Pid),
		CreatedAt:    time.Now(),
		ptmx:         ptmx,
		cmd:          cmd,
		term:         term,
		feedCh:       make(chan *[]byte, 64),
		tempDir:      tempDir,
		actions:      make(chan any, 16),
		ptyOut:       make(chan []byte, 64),
		done:         make(chan struct{}),
		resizePolicy: resizePolicy,
		ctx:          ctx,
	}
	s.setSize(cols, rows)

	go s.feedLoop(ctx)
	go s.ptyRead()
	go s.run()
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
		PID:          uint32(cmd.Process.Pid),
		CreatedAt:    time.Now(),
		ptmx:         ptmx,
		cmd:          cmd,
		term:         term,
		feedCh:       make(chan *[]byte, 64),
		tempDir:      tempDir,
		actions:      make(chan any, 16),
		ptyOut:       make(chan []byte, 64),
		done:         make(chan struct{}),
		resizePolicy: resizePolicy,
		ctx:          ctx,
	}
	s.setSize(cols, rows)

	go s.feedLoop(ctx)
	go s.ptyRead()
	go s.run()
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

func (s *Session) ptyRead() {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			select {
			case s.ptyOut <- data:
			case <-s.done:
				return
			}
		}
		if err != nil {
			break
		}
	}

	s.cmd.Wait()
	if ws, ok := s.cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
		s.exitCode = exitCodeFromWaitStatus(ws)
	}
	close(s.ptyOut)
}

// run owns client state; connection writes go through per-client outCh.
func (s *Session) run() {
	defer close(s.done)

	var clients []*sessionClient
	var nextClientID uint64

	// pendingFeed holds data waiting to be sent to feedCh. While
	// non-nil, we stop reading ptyOut (backpressure) but keep
	// processing actions so detach/kick/list don't stall.
	var pendingFeed *[]byte

	for {
		// Nil-channel trick: only one of ptyCh/feedSend is active
		// at a time. When pendingFeed is nil, read ptyOut. When
		// non-nil, send to feedCh. Actions are always processed.
		var ptyCh <-chan []byte
		var feedSend chan<- *[]byte
		if pendingFeed != nil {
			feedSend = s.feedCh
		} else {
			ptyCh = s.ptyOut
		}

		select {
		case data, ok := <-ptyCh:
			if !ok {
				close(s.feedCh)
				exitMsg := &protocol.Exited{ExitCode: s.exitCode}
				for _, c := range clients {
					select {
					case c.outCh <- exitMsg:
						close(c.outCh)
					default:
						close(c.outCh)
						c.closeConn()
					}
				}
				return
			}

			msg := &protocol.Output{Data: data}
			before := len(clients)
			clients = broadcastOutput(clients, s.Name, msg)
			if len(clients) != before {
				notifyClientsChanged(clients, s.size)
			}

			bp := feedPool.Get().(*[]byte)
			d := (*bp)[:len(data)]
			copy(d, data)
			*bp = d
			pendingFeed = bp

		case feedSend <- pendingFeed:
			pendingFeed = nil

		case action := <-s.actions:
			switch a := action.(type) {
			case attachReq:
				if !a.readOnly {
					s.resizeForPending(clients, a.cols, a.rows, a.xpixel, a.ypixel)
				}

				dump, err := s.term.DumpScreen(s.ctx, libghostty.DumpVTFull)
				if err != nil {
					a.result <- attachResp{err: err}
					continue
				}

				nextClientID++
				clientID := fmt.Sprintf("%d", nextClientID)
				cols, rows := s.size()

				sc := &sessionClient{
					id:        clientID,
					conn:      a.conn,
					closeConn: a.closeConn,
					cols:      a.cols,
					rows:      a.rows,
					xpixel:    a.xpixel,
					ypixel:    a.ypixel,
					version:   a.version,
					readOnly:  a.readOnly,
					outCh:     make(chan protocol.Message, 64),
				}
				go sc.writeLoop()

				// Attached is the first message on outCh, guaranteed
				// to precede any Output since the client isn't in the
				// clients list yet.
				sc.outCh <- &protocol.Attached{
					Name:       s.Name,
					PID:        s.PID,
					ClientID:   clientID,
					Cols:       cols,
					Rows:       rows,
					ScreenDump: dump.VT,
					CursorRow:  dump.CursorRow,
					CursorCol:  dump.CursorCol,
					AltScreen:  dump.IsAltScreen,
					Created:    a.created,
				}

				clients = append(clients, sc)
				if !a.readOnly {
					s.arbitrateResize(clients)
				}
				notifyClientsChanged(clients, s.size)

				a.result <- attachResp{client: sc}

			case detachReq:
				before := len(clients)
				clients = removeClient(clients, a.client)
				if len(clients) == before {
					continue // already removed (e.g., kicked)
				}
				close(a.client.outCh)
				s.arbitrateResize(clients)
				notifyClientsChanged(clients, s.size)

			case kickReq:
				var target *sessionClient
				for _, c := range clients {
					if c.id == a.clientID {
						target = c
						break
					}
				}
				if target == nil {
					a.result <- false
					continue
				}
				clients = removeClient(clients, target)
				close(target.outCh)
				target.closeConn()
				s.arbitrateResize(clients)
				notifyClientsChanged(clients, s.size)
				a.result <- true

			case resizeReq:
				a.client.cols = a.cols
				a.client.rows = a.rows
				a.client.xpixel = a.xpixel
				a.client.ypixel = a.ypixel
				s.arbitrateResize(clients)

			case clientInfoReq:
				info := make([]protocol.SessionClient, len(clients))
				for i, c := range clients {
					info[i] = protocol.SessionClient{
						ClientID: c.id,
						ReadOnly: c.readOnly,
						Version:  c.version,
					}
				}
				a.result <- info

			case stopReq:
				// Force-close: disconnect all clients, close feedCh, return.
				// Clients see connection close (EOF), not Exited — this is
				// the kill/shutdown path.
				if pendingFeed != nil {
					feedPool.Put(pendingFeed)
					pendingFeed = nil
				}
				close(s.feedCh)
				for _, c := range clients {
					close(c.outCh)
					c.closeConn()
				}
				return
			}
		}
	}
}

func removeClient(clients []*sessionClient, target *sessionClient) []*sessionClient {
	for i, c := range clients {
		if c == target {
			return append(clients[:i], clients[i+1:]...)
		}
	}
	return clients
}

func broadcastOutput(clients []*sessionClient, name string, msg *protocol.Output) []*sessionClient {
	i := 0
	for _, c := range clients {
		select {
		case c.outCh <- msg:
			clients[i] = c
			i++
		default:
			slog.Debug("evicting slow client", "session", name)
			close(c.outCh)
			c.closeConn()
		}
	}
	return clients[:i]
}

func notifyClientsChanged(clients []*sessionClient, sizeFn func() (uint16, uint16)) {
	cols, rows := sizeFn()
	msg := &protocol.ClientsChanged{
		Count: uint16(len(clients)),
		Cols:  cols,
		Rows:  rows,
	}
	for _, c := range clients {
		select {
		case c.outCh <- msg:
		default:
		}
	}
}

func (s *Session) attach(ctx context.Context, conn *protocol.Conn, closeConn func() error, cols, rows, xpixel, ypixel uint16, version string, readOnly, created bool) (*sessionClient, error) {
	ch := make(chan attachResp, 1)
	req := attachReq{
		conn:      conn,
		closeConn: closeConn,
		cols:      cols,
		rows:      rows,
		xpixel:    xpixel,
		ypixel:    ypixel,
		version:   version,
		readOnly:  readOnly,
		created:   created,
		result:    ch,
	}
	select {
	case s.actions <- req:
	case <-s.done:
		return nil, fmt.Errorf("session closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	// Don't select on ctx.Done() here — if the action was accepted,
	// we must wait for the result to avoid orphaning the client.
	select {
	case resp := <-ch:
		return resp.client, resp.err
	case <-s.done:
		return nil, fmt.Errorf("session closed")
	}
}

func (s *Session) detachClient(sc *sessionClient) {
	select {
	case s.actions <- detachReq{client: sc}:
	case <-s.done:
	}
}

func (s *Session) kickClient(clientID string) bool {
	ch := make(chan bool, 1)
	select {
	case s.actions <- kickReq{clientID: clientID, result: ch}:
	case <-s.done:
		return false
	}
	select {
	case found := <-ch:
		return found
	case <-s.done:
		return false
	}
}

func (s *Session) resizeClient(sc *sessionClient, cols, rows, xpixel, ypixel uint16) {
	select {
	case s.actions <- resizeReq{client: sc, cols: cols, rows: rows, xpixel: xpixel, ypixel: ypixel}:
	case <-s.done:
	}
}

func (s *Session) clientInfo() []protocol.SessionClient {
	ch := make(chan []protocol.SessionClient, 1)
	select {
	case s.actions <- clientInfoReq{result: ch}:
	case <-s.done:
		return nil
	}
	select {
	case info := <-ch:
		return info
	case <-s.done:
		return nil
	}
}

func (s *Session) sendInput(data []byte) error {
	_, err := s.ptmx.Write(data)
	return err
}

func (s *Session) dumpScreen(ctx context.Context, format libghostty.DumpFormat) (*libghostty.ScreenDump, error) {
	return s.term.DumpScreen(ctx, format)
}

func (s *Session) kill() {
	syscall.Kill(-int(s.PID), syscall.SIGHUP)
}

func (s *Session) close(ctx context.Context) {
	select {
	case s.actions <- stopReq{}:
	case <-s.done:
	}

	s.kill()
	s.ptmx.Close() // unblock ptyRead if blocked on Read
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		slog.Warn("child ignored SIGHUP, sending SIGKILL", "session", s.Name)
		syscall.Kill(-int(s.PID), syscall.SIGKILL)
		<-s.done
	}
	s.term.Close(ctx)
	if s.tempDir != "" {
		os.RemoveAll(s.tempDir)
	}
}

func (s *Session) resize(cols, rows, xpixel, ypixel uint16) {
	s.setSize(cols, rows)

	if err := pty.Setsize(s.ptmx, &pty.Winsize{Rows: rows, Cols: cols, X: xpixel, Y: ypixel}); err != nil {
		slog.Warn("pty setsize", "session", s.Name, "err", err)
	}
	syscall.Kill(-int(s.PID), syscall.SIGWINCH)
	if err := s.term.Resize(s.ctx, uint32(cols), uint32(rows)); err != nil {
		slog.Warn("wasm resize", "session", s.Name, "err", err)
	}
}

func collectClientDims(clients []*sessionClient) []clientDims {
	dims := make([]clientDims, 0, len(clients))
	for _, c := range clients {
		if c.readOnly {
			continue
		}
		dims = append(dims, clientDims{c.cols, c.rows, c.xpixel, c.ypixel})
	}
	return dims
}

func (s *Session) arbitrateResize(clients []*sessionClient) {
	dims := collectClientDims(clients)
	if len(dims) == 0 {
		return
	}
	cols, rows, xpixel, ypixel := applyResizePolicy(s.resizePolicy, dims)
	curCols, curRows := s.size()
	if cols != curCols || rows != curRows {
		s.resize(cols, rows, xpixel, ypixel)
	}
}

func (s *Session) resizeForPending(clients []*sessionClient, cols, rows, xpixel, ypixel uint16) {
	dims := append(collectClientDims(clients), clientDims{cols, rows, xpixel, ypixel})
	targetCols, targetRows, targetXpixel, targetYpixel := applyResizePolicy(s.resizePolicy, dims)
	curCols, curRows := s.size()
	if targetCols != curCols || targetRows != curRows {
		s.resize(targetCols, targetRows, targetXpixel, targetYpixel)
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

func exitCodeFromWaitStatus(ws syscall.WaitStatus) int32 {
	if ws.Exited() {
		return int32(ws.ExitStatus())
	}
	if ws.Signaled() {
		return int32(128 + ws.Signal())
	}
	return 1
}
