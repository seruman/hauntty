package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	hauntty "code.selman.me/hauntty"
	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
	"golang.org/x/sys/unix"
)

type Server struct {
	socketPath        string
	pidPath           string
	sessions          map[string]*Session
	mu                sync.RWMutex
	wasmRT            *libghostty.Runtime
	ctx               context.Context
	cancel            context.CancelFunc
	listener          net.Listener
	persister         *Persister
	defaultScrollback uint32
	resizePolicy      string
	autoExit          bool
	shutdownOnce      sync.Once
	startedAt         time.Time
}

func New(ctx context.Context, cfg *config.DaemonConfig, resizePolicy string) (*Server, error) {
	rt, err := libghostty.NewRuntime(ctx)
	if err != nil {
		return nil, fmt.Errorf("daemon: init wasm runtime: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	sock := cmp.Or(cfg.SocketPath, config.SocketPath())
	s := &Server{
		socketPath:        sock,
		pidPath:           filepath.Join(filepath.Dir(sock), "hauntty.pid"),
		sessions:          make(map[string]*Session),
		wasmRT:            rt,
		ctx:               ctx,
		cancel:            cancel,
		defaultScrollback: cfg.DefaultScrollback,
		resizePolicy:      resizePolicy,
		autoExit:          cfg.AutoExit,
		startedAt:         time.Now(),
	}

	if cfg.StatePersistence {
		interval := time.Duration(cfg.StatePersistenceInterval) * time.Second
		s.persister = NewPersister(s.liveSessions, interval)
	}

	return s, nil
}

func (s *Server) Listen() error {
	CleanStaleTmp()

	dir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("daemon: create socket dir: %w", err)
	}

	if err := os.Remove(s.socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("remove stale socket", "path", s.socketPath, "err", err)
	}

	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("daemon: listen: %w", err)
	}
	s.listener = ln

	if err := s.writePID(); err != nil {
		ln.Close()
		return err
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case <-sigCh:
			slog.Info("received shutdown signal")
			s.Shutdown()
		case <-s.ctx.Done():
		}
	}()

	if s.persister != nil {
		s.persister.Start()
	}

	slog.Info("daemon listening", "socket", s.socketPath)

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-s.ctx.Done():
				return nil
			default:
				slog.Error("accept error", "err", err)
				continue
			}
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(netConn net.Conn) {
	defer netConn.Close()

	unixConn, ok := netConn.(*net.UnixConn)
	if !ok {
		return
	}
	raw, err := unixConn.SyscallConn()
	if err != nil {
		return
	}
	var peerUID int
	var credErr error
	raw.Control(func(fd uintptr) {
		cred, err := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if err != nil {
			credErr = err
			return
		}
		peerUID = int(cred.Uid)
	})
	if credErr != nil {
		slog.Warn("getpeereid failed", "err", credErr)
		return
	}
	if peerUID != os.Getuid() {
		slog.Warn("rejected connection from different UID", "peer", peerUID)
		return
	}

	conn := protocol.NewConn(netConn)

	clientVer, clientRev, err := conn.AcceptHandshake()
	if err != nil {
		return
	}
	if clientVer != protocol.ProtocolVersion {
		if err := conn.AcceptVersion(0, ""); err != nil {
			slog.Debug("reject handshake", "err", err)
		}
		return
	}
	serverRev := hauntty.Version()
	if clientRev != serverRev {
		slog.Warn("client/server revision differ", "client", clientRev, "server", serverRev)
	}
	if err := conn.AcceptVersion(protocol.ProtocolVersion, serverRev); err != nil {
		return
	}

	// Connection starts in control mode.
	var attached *Session
	var ac *sessionClient
	var readOnly bool

	for {
		msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		// Streaming mode: only Input, Resize, Detach allowed.
		if attached != nil {
			switch m := msg.(type) {
			case *protocol.Input:
				if !readOnly {
					if err := attached.sendInput(m.Data); err != nil {
						slog.Debug("pty write", "session", attached.Name, "err", err)
					}
				}
			case *protocol.Resize:
				if !readOnly {
					attached.resizeClient(ac, m.Cols, m.Rows, m.Xpixel, m.Ypixel)
				}
			case *protocol.Detach:
				attached.detachClient(ac)
				return
			default:
				slog.Debug("control message in streaming mode, closing", "type", fmt.Sprintf("0x%02x", msg.Type()))
				attached.detachClient(ac)
				return
			}
			continue
		}

		// Control mode: handle RPCs.
		switch m := msg.(type) {
		case *protocol.Create:
			s.handleCreate(conn, m)
		case *protocol.Attach:
			sess, client, ro, err := s.handleAttach(conn, netConn.Close, m, clientRev)
			if err != nil {
				slog.Debug("attach error", "err", err)
				// Failed attach stays in control mode unless
				// the error response itself failed to write.
				continue
			}
			attached = sess
			ac = client
			readOnly = ro
		case *protocol.List:
			s.handleList(conn, m)
		case *protocol.Kill:
			s.handleKill(conn, m)
		case *protocol.Send:
			s.handleSend(conn, m)
		case *protocol.SendKey:
			s.handleSendKey(conn, m)
		case *protocol.Dump:
			s.handleDump(conn, m)
		case *protocol.Prune:
			s.handlePrune(conn)
		case *protocol.Status:
			s.handleStatus(conn, m)
		case *protocol.Kick:
			s.handleKick(conn, m)
		default:
			slog.Debug("unknown message in control mode", "type", fmt.Sprintf("0x%02x", msg.Type()))
			return
		}
	}

	if attached != nil {
		attached.detachClient(ac)
	}
}

func (s *Server) handleCreate(conn *protocol.Conn, msg *protocol.Create) {
	name := msg.Name

	s.mu.Lock()
	if name == "" {
		existing := make(map[string]bool, len(s.sessions))
		for k := range s.sessions {
			existing[k] = true
		}
		name = generateUniqueName(existing)
	}

	if _, exists := s.sessions[name]; exists {
		s.mu.Unlock()
		writeError(conn, "session already exists")
		return
	}
	s.mu.Unlock()

	// Check dead state on disk.
	if s.persister != nil {
		if _, err := LoadState(name); err == nil {
			if !msg.Force {
				writeError(conn, "dead session state exists")
				return
			}
			CleanState(name)
		}
	}

	scrollback := msg.Scrollback
	if scrollback == 0 {
		scrollback = s.defaultScrollback
	}

	sess, err := newSession(s.ctx, name, msg.Command, msg.Env, 80, 24, 0, 0, scrollback, s.wasmRT, s.resizePolicy, msg.CWD)
	if err != nil {
		writeError(conn, err.Error())
		return
	}

	s.mu.Lock()
	if _, exists := s.sessions[name]; exists {
		s.mu.Unlock()
		sess.close(s.ctx)
		writeError(conn, "session already exists")
		return
	}
	s.sessions[name] = sess
	s.mu.Unlock()

	s.watchSession(sess)

	if err := conn.WriteMessage(&protocol.Created{Name: name, PID: sess.PID}); err != nil {
		slog.Debug("write created response", "err", err)
	}
}

func (s *Server) handleAttach(conn *protocol.Conn, closeConn func() error, msg *protocol.Attach, clientRev string) (*Session, *sessionClient, bool, error) {
	name := msg.Name

	if msg.Restore {
		return s.handleAttachRestore(conn, closeConn, msg, clientRev)
	}

	s.mu.Lock()
	if name == "" {
		existing := make(map[string]bool, len(s.sessions))
		for k := range s.sessions {
			existing[k] = true
		}
		name = generateUniqueName(existing)
	}
	sess := s.sessions[name]
	s.mu.Unlock()

	created := false
	if sess == nil {
		// Check for dead state on disk.
		if s.persister != nil {
			if _, err := LoadState(name); err == nil {
				writeError(conn, "dead session state exists")
				return nil, nil, false, fmt.Errorf("dead session state exists for %q", name)
			}
		}

		scrollback := msg.Scrollback
		if scrollback == 0 {
			scrollback = s.defaultScrollback
		}

		var err error
		sess, err = newSession(s.ctx, name, msg.Command, msg.Env, msg.Cols, msg.Rows, msg.Xpixel, msg.Ypixel, scrollback, s.wasmRT, s.resizePolicy, msg.CWD)
		if err != nil {
			writeError(conn, err.Error())
			return nil, nil, false, err
		}

		s.mu.Lock()
		if existing, ok := s.sessions[name]; ok {
			s.mu.Unlock()
			sess.close(s.ctx)
			sess = existing
		} else {
			s.sessions[name] = sess
			created = true
			s.mu.Unlock()
			s.watchSession(sess)
		}
	}

	ac, err := sess.attach(s.ctx, conn, closeConn, msg.Cols, msg.Rows, msg.Xpixel, msg.Ypixel, clientRev, msg.ReadOnly, created)
	if err != nil {
		if created {
			s.mu.Lock()
			delete(s.sessions, name)
			s.mu.Unlock()
			sess.close(s.ctx)
		}
		writeError(conn, err.Error())
		return nil, nil, false, err
	}

	return sess, ac, msg.ReadOnly, nil
}

func (s *Server) handleAttachRestore(conn *protocol.Conn, closeConn func() error, msg *protocol.Attach, clientRev string) (*Session, *sessionClient, bool, error) {
	name := msg.Name
	if name == "" {
		writeError(conn, "name required for restore")
		return nil, nil, false, fmt.Errorf("name required for restore")
	}

	if s.persister == nil {
		writeError(conn, "persistence is disabled")
		return nil, nil, false, fmt.Errorf("persistence is disabled")
	}

	s.mu.RLock()
	_, running := s.sessions[name]
	s.mu.RUnlock()

	if running {
		writeError(conn, "session is running")
		return nil, nil, false, fmt.Errorf("session %q is running", name)
	}

	state, err := LoadState(name)
	if err != nil {
		writeError(conn, "no saved state")
		return nil, nil, false, fmt.Errorf("no saved state for %q", name)
	}

	scrollback := msg.Scrollback
	if scrollback == 0 {
		scrollback = s.defaultScrollback
	}

	sess, err := restoreSession(s.ctx, name, msg.Command, msg.Env, msg.Cols, msg.Rows, msg.Xpixel, msg.Ypixel, scrollback, s.wasmRT, state, s.resizePolicy, msg.CWD)
	if err != nil {
		writeError(conn, err.Error())
		return nil, nil, false, err
	}

	CleanState(name)

	s.mu.Lock()
	if _, exists := s.sessions[name]; exists {
		s.mu.Unlock()
		sess.close(s.ctx)
		writeError(conn, "session already exists")
		return nil, nil, false, fmt.Errorf("session %q created by another client during restore", name)
	}
	s.sessions[name] = sess
	s.mu.Unlock()

	s.watchSession(sess)

	ac, err := sess.attach(s.ctx, conn, closeConn, msg.Cols, msg.Rows, msg.Xpixel, msg.Ypixel, clientRev, msg.ReadOnly, true)
	if err != nil {
		s.mu.Lock()
		delete(s.sessions, name)
		s.mu.Unlock()
		sess.close(s.ctx)
		writeError(conn, err.Error())
		return nil, nil, false, err
	}

	return sess, ac, msg.ReadOnly, nil
}

func (s *Server) handleKick(conn *protocol.Conn, msg *protocol.Kick) {
	if msg.Name == "" || msg.ClientID == "" {
		writeError(conn, "name and client ID required")
		return
	}

	s.mu.RLock()
	sess, ok := s.sessions[msg.Name]
	s.mu.RUnlock()

	if !ok {
		writeError(conn, "session not found")
		return
	}

	if !sess.kickClient(msg.ClientID) {
		writeError(conn, "client not found")
		return
	}

	if err := conn.WriteMessage(&protocol.OK{}); err != nil {
		slog.Debug("write ok response", "err", err)
	}
}

func (s *Server) handleList(conn *protocol.Conn, msg *protocol.List) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	type sessRef struct {
		sess *Session
		idx  int
	}
	s.mu.RLock()
	running := make(map[string]bool, len(s.sessions))
	sessions := make([]protocol.Session, 0, len(s.sessions))
	var refs []sessRef
	for _, sess := range s.sessions {
		running[sess.Name] = true
		state := "running"
		if !sess.isRunning() {
			state = "dead"
		}
		cols, rows := sess.size()
		ps := protocol.Session{
			Name:      sess.Name,
			State:     state,
			Cols:      cols,
			Rows:      rows,
			PID:       sess.PID,
			CreatedAt: uint32(sess.CreatedAt.Unix()),
			CWD:       sess.term.GetPwd(ctx),
		}
		if msg.IncludeClients {
			refs = append(refs, sessRef{sess: sess, idx: len(sessions)})
		}
		sessions = append(sessions, ps)
	}
	s.mu.RUnlock()

	for _, r := range refs {
		sessions[r.idx].Clients = r.sess.clientInfo()
	}

	dead, _ := ListDeadSessions(running)
	for _, name := range dead {
		ps := protocol.Session{Name: name, State: "dead"}
		if state, err := LoadState(name); err == nil {
			ps.Cols = state.Cols
			ps.Rows = state.Rows
			ps.CreatedAt = uint32(state.SavedAt.Unix())
		}
		sessions = append(sessions, ps)
	}

	if err := conn.WriteMessage(&protocol.Sessions{Sessions: sessions}); err != nil {
		slog.Debug("write sessions response", "err", err)
	}
}

func (s *Server) handleKill(conn *protocol.Conn, msg *protocol.Kill) {
	s.mu.RLock()
	sess, ok := s.sessions[msg.Name]
	s.mu.RUnlock()

	if !ok {
		writeError(conn, "session not found")
		return
	}

	sess.kill()
	if err := conn.WriteMessage(&protocol.OK{}); err != nil {
		slog.Debug("write ok response", "err", err)
	}
}

func (s *Server) handleSend(conn *protocol.Conn, msg *protocol.Send) {
	s.mu.RLock()
	sess, ok := s.sessions[msg.Name]
	s.mu.RUnlock()

	if !ok {
		writeError(conn, "session not found")
		return
	}

	if err := sess.sendInput(msg.Data); err != nil {
		writeError(conn, err.Error())
		return
	}
	if err := conn.WriteMessage(&protocol.OK{}); err != nil {
		slog.Debug("write ok response", "err", err)
	}
}

func (s *Server) handleSendKey(conn *protocol.Conn, msg *protocol.SendKey) {
	s.mu.RLock()
	sess, ok := s.sessions[msg.Name]
	s.mu.RUnlock()

	if !ok {
		writeError(conn, "session not found")
		return
	}

	data, err := sess.term.EncodeKey(s.ctx, libghostty.KeyCode(msg.Key), libghostty.Modifier(msg.Mods))
	if err != nil {
		writeError(conn, err.Error())
		return
	}

	if len(data) > 0 {
		if err := sess.sendInput(data); err != nil {
			writeError(conn, err.Error())
			return
		}
	}

	if err := conn.WriteMessage(&protocol.OK{}); err != nil {
		slog.Debug("write ok response", "err", err)
	}
}

func (s *Server) handleDump(conn *protocol.Conn, msg *protocol.Dump) {
	s.mu.RLock()
	sess, ok := s.sessions[msg.Name]
	s.mu.RUnlock()

	if ok {
		flags := libghostty.DumpFormat(msg.Format) & ^libghostty.DumpFormatMask
		var wasmFmt libghostty.DumpFormat
		switch msg.Format & protocol.DumpFormatMask {
		case protocol.DumpVT:
			wasmFmt = libghostty.DumpVTSafe
		case protocol.DumpHTML:
			wasmFmt = libghostty.DumpHTML
		default:
			wasmFmt = libghostty.DumpPlain
		}
		wasmFmt |= flags

		dump, err := sess.dumpScreen(s.ctx, wasmFmt)
		if err != nil {
			writeError(conn, err.Error())
			return
		}
		if err := conn.WriteMessage(&protocol.DumpResponse{Data: dump.VT}); err != nil {
			slog.Debug("write dump response", "err", err)
		}
		return
	}

	// Fall back to disk state for dead sessions.
	if state, err := LoadState(msg.Name); err == nil {
		if err := conn.WriteMessage(&protocol.DumpResponse{Data: state.VT}); err != nil {
			slog.Debug("write dump response", "err", err)
		}
		return
	}

	writeError(conn, "session not found")
}

func (s *Server) handlePrune(conn *protocol.Conn) {
	s.mu.RLock()
	running := make(map[string]bool, len(s.sessions))
	for name := range s.sessions {
		running[name] = true
	}
	s.mu.RUnlock()

	dead, _ := ListDeadSessions(running)
	var count uint32
	for _, name := range dead {
		if err := CleanState(name); err == nil {
			count++
		}
	}

	if err := conn.WriteMessage(&protocol.PruneResponse{Count: count}); err != nil {
		slog.Debug("write prune response", "err", err)
	}
}

func (s *Server) handleStatus(conn *protocol.Conn, msg *protocol.Status) {
	running, runningCount, deadCount, ss := s.statusSnapshot(msg.Name)

	dead, _ := ListDeadSessions(running)
	deadCount += uint32(len(dead))

	uptime := max(time.Since(s.startedAt), 0)
	uptimeSec := min(int64(uptime/time.Second), math.MaxUint32)

	resp := &protocol.StatusResponse{
		Daemon: protocol.DaemonStatus{
			PID:          uint32(os.Getpid()),
			Uptime:       uint32(uptimeSec),
			SocketPath:   s.socketPath,
			RunningCount: runningCount,
			DeadCount:    deadCount,
			Version:      hauntty.Version(),
		},
		Session: ss,
	}
	if err := conn.WriteMessage(resp); err != nil {
		slog.Debug("write status response", "err", err)
	}
}

func (s *Server) statusSnapshot(sessionName string) (map[string]bool, uint32, uint32, *protocol.SessionStatus) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	s.mu.RLock()
	running := make(map[string]bool, len(s.sessions))
	var runningCount uint32
	var deadCount uint32
	for _, sess := range s.sessions {
		running[sess.Name] = true
		if sess.isRunning() {
			runningCount++
		} else {
			deadCount++
		}
	}

	var sess *Session
	if sessionName != "" {
		sess = s.sessions[sessionName]
	}
	s.mu.RUnlock()

	if sess == nil {
		return running, runningCount, deadCount, nil
	}

	state := "running"
	if !sess.isRunning() {
		state = "dead"
	}

	cols, rows := sess.size()
	ss := &protocol.SessionStatus{
		Name:    sess.Name,
		State:   state,
		Cols:    cols,
		Rows:    rows,
		PID:     sess.PID,
		CWD:     sess.term.GetPwd(ctx),
		Clients: sess.clientInfo(),
	}
	return running, runningCount, deadCount, ss
}

// watchSession starts a goroutine that waits for session exit, removes it
// from the sessions map, persists state, and triggers auto-exit if needed.
func (s *Server) watchSession(sess *Session) {
	go func() {
		<-sess.done
		// Persist before removing from map so a concurrent Create with the
		// same name cannot race: the name is still "taken" during persist.
		// Skip during shutdown — saveAll() already ran, and the terminal
		// may be closed by sess.close() racing with us.
		if s.persister != nil && s.ctx.Err() == nil {
			s.persister.SaveSession(sess.Name, sess)
		}
		s.mu.Lock()
		delete(s.sessions, sess.Name)
		empty := len(s.sessions) == 0
		s.mu.Unlock()
		if s.autoExit && empty {
			slog.Info("auto-exit: last session ended, shutting down")
			s.Shutdown()
		}
	}()
}

func (s *Server) Shutdown() {
	s.shutdownOnce.Do(s.shutdown)
}

func (s *Server) shutdown() {
	if s.persister != nil {
		s.persister.Stop()
		s.persister.saveAll()
	}

	s.cancel()

	if s.listener != nil {
		s.listener.Close()
	}

	s.mu.Lock()
	for _, sess := range s.sessions {
		sess.close(context.Background())
	}
	s.sessions = make(map[string]*Session)
	s.mu.Unlock()

	s.wasmRT.Close()

	if err := os.Remove(s.socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("remove socket", "path", s.socketPath, "err", err)
	}
	if err := os.Remove(s.pidPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("remove pid file", "path", s.pidPath, "err", err)
	}
}

func (s *Server) writePID() error {
	return os.WriteFile(s.pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600)
}

func (s *Server) liveSessions() map[string]*Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := make(map[string]*Session, len(s.sessions))
	maps.Copy(m, s.sessions)
	return m
}

func writeError(conn *protocol.Conn, message string) {
	if err := conn.WriteMessage(&protocol.Error{Message: message}); err != nil {
		slog.Debug("write error response", "err", err)
	}
}
