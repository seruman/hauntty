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
	"sync/atomic"
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
	deadSessionTTL    time.Duration
	attachLeaseTTL    time.Duration
	deadTimers        map[string]*time.Timer
	deadTimerAt       map[string]time.Time
	attachLeases      map[string]*attachLease
	clientIDCounter   atomic.Uint64
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
		deadSessionTTL:    time.Duration(cfg.DeadSessionTTLSeconds) * time.Second,
		attachLeaseTTL:    5 * time.Second,
		deadTimers:        make(map[string]*time.Timer),
		deadTimerAt:       make(map[string]time.Time),
		attachLeases:      make(map[string]*attachLease),
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

	var attached *Session
	var ac *attachedClient

	for {
		msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		switch m := msg.(type) {
		case *protocol.Create:
			s.handleCreate(conn, m)
		case *protocol.Attach:
			attached, ac, err = s.handleAttach(conn, netConn.Close, m, clientRev)
			if err != nil {
				slog.Debug("attach error", "err", err)
				return
			}
		case *protocol.Input:
			if attached != nil {
				attached.sendInputFrom(ac, m.Data)
			}
		case *protocol.Resize:
			if ac != nil && !ac.readOnly {
				ac.cols = m.Cols
				ac.rows = m.Rows
				ac.xpixel = m.Xpixel
				ac.ypixel = m.Ypixel
				attached.arbitrateResize()
			}
		case *protocol.Detach:
			if m.Name != "" {
				s.mu.RLock()
				target := s.sessions[m.Name]
				s.mu.RUnlock()
				if target == nil {
					_ = conn.WriteMessage(&protocol.Error{Message: "session not found"})
					continue
				}
				if m.TargetClientID != "" {
					if !target.disconnectClientByID(m.TargetClientID) {
						_ = conn.WriteMessage(&protocol.Error{Message: "client not found"})
						continue
					}
				} else if m.TargetTTY != "" {
					if !target.disconnectClientByTTY(m.TargetTTY) {
						_ = conn.WriteMessage(&protocol.Error{Message: "client not found"})
						continue
					}
				} else {
					if !target.disconnectActiveClient() {
						_ = conn.WriteMessage(&protocol.Error{Message: "client not found"})
						continue
					}
				}
				_ = conn.WriteMessage(&protocol.OK{})
			} else if attached != nil {
				attached.detachClient(ac)
				attached = nil
				ac = nil
			}
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
		}
	}

	if attached != nil {
		attached.detachClient(ac)
	}
}

func (s *Server) ensureSession(name string, command []string, env []string, cwd string) (*Session, protocol.CreateOutcome, error) {
	if name == "" {
		s.mu.RLock()
		existing := make(map[string]bool, len(s.sessions))
		for k := range s.sessions {
			existing[k] = true
		}
		s.mu.RUnlock()
		name = generateUniqueName(existing)
	}

	s.mu.RLock()
	sess := s.sessions[name]
	s.mu.RUnlock()
	if sess != nil {
		s.stopDeadTimer(name)
		return sess, protocol.CreateOutcomeExisting, nil
	}

	const defaultCols uint16 = 80
	const defaultRows uint16 = 24

	var err error
	scrollback := s.defaultScrollback
	if state, serr := LoadState(name); serr == nil {
		slog.Info("restoring dead session", "session", name)
		sess, err = restoreSession(s.ctx, name, command, env, defaultCols, defaultRows, 0, 0, scrollback, s.wasmRT, state, s.resizePolicy, cwd)
		if err == nil {
			CleanState(name)
		}
	}
	if sess == nil {
		sess, err = newSession(s.ctx, name, command, env, defaultCols, defaultRows, 0, 0, scrollback, s.wasmRT, s.resizePolicy, cwd)
	}
	if err != nil {
		return nil, 0, err
	}

	s.mu.Lock()
	if existing, ok := s.sessions[name]; ok {
		s.mu.Unlock()
		sess.close(s.ctx)
		s.stopDeadTimer(name)
		return existing, protocol.CreateOutcomeExisting, nil
	}
	s.sessions[name] = sess
	go s.watchSession(sess)
	s.mu.Unlock()

	return sess, protocol.CreateOutcomeCreated, nil
}

func (s *Server) handleCreate(conn *protocol.Conn, msg *protocol.Create) {
	sess, outcome, err := s.ensureSession(msg.Name, msg.Command, msg.Env, msg.CWD)
	if err != nil {
		if werr := conn.WriteMessage(&protocol.Error{Message: err.Error()}); werr != nil {
			slog.Debug("write create error response", "err", werr)
		}
		return
	}

	if msg.Mode == protocol.CreateModeRequireNew && outcome == protocol.CreateOutcomeExisting {
		if err := conn.WriteMessage(&protocol.Error{Message: fmt.Sprintf("session %q already exists", sess.Name)}); err != nil {
			slog.Debug("write create error response", "err", err)
		}
		return
	}

	token, tokenExpires := s.issueAttachLease(sess.Name)
	if err := conn.WriteMessage(&protocol.Created{SessionName: sess.Name, PID: sess.PID, Outcome: outcome, AttachToken: token, AttachTokenExpiresAt: tokenExpires}); err != nil {
		slog.Debug("write created response", "err", err)
	}
}

func (s *Server) handleAttach(conn *protocol.Conn, closeConn func() error, msg *protocol.Attach, clientRev string) (*Session, *attachedClient, error) {
	if msg.AttachToken != "" {
		if !s.consumeAttachLease(msg.Name, msg.AttachToken) {
			if err := conn.WriteMessage(&protocol.Error{Message: "invalid or expired attach token"}); err != nil {
				slog.Debug("write attach error response", "err", err)
			}
			return nil, nil, fmt.Errorf("invalid or expired attach token")
		}
	}

	s.mu.RLock()
	sess := s.sessions[msg.Name]
	s.mu.RUnlock()
	if sess == nil {
		if err := conn.WriteMessage(&protocol.Error{Message: "session not found"}); err != nil {
			slog.Debug("write attach error response", "err", err)
		}
		return nil, nil, fmt.Errorf("session not found")
	}

	clientID := s.nextClientID()
	attached, ac, err := sess.attach(s.ctx, conn, closeConn, msg.Cols, msg.Rows, msg.Xpixel, msg.Ypixel, clientRev, msg.ReadOnly, msg.ClientTTY, clientID)
	if err != nil {
		if werr := conn.WriteMessage(&protocol.Error{Message: err.Error()}); werr != nil {
			slog.Debug("write attach error response", "err", werr)
		}
		return nil, nil, err
	}
	if err := conn.WriteMessage(attached); err != nil {
		return nil, nil, err
	}
	if !sess.isRunning() {
		if err := conn.WriteMessage(&protocol.Exited{ExitCode: sess.exitCode}); err != nil {
			slog.Debug("write exited response", "err", err)
		}
		return nil, nil, nil
	}
	return sess, ac, nil
}

func (s *Server) nextClientID() string {
	id := s.clientIDCounter.Add(1)
	return strconv.FormatUint(id, 10)
}

func (s *Server) stopDeadTimer(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.deadTimers[name]
	if t == nil {
		return
	}
	delete(s.deadTimers, name)
	delete(s.deadTimerAt, name)
	t.Stop()
}

func (s *Server) watchSession(sess *Session) {
	<-sess.done

	s.mu.Lock()
	if s.deadSessionTTL <= 0 {
		delete(s.sessions, sess.Name)
		empty := s.runningCountLocked() == 0
		s.mu.Unlock()
		if s.autoExit && empty {
			slog.Info("auto-exit: last session ended, shutting down")
			s.Shutdown()
		}
		return
	}

	target := time.Now().Add(s.deadSessionTTL)
	leaseUntil := s.maxLeaseExpiryLocked(sess.Name)
	if leaseUntil.After(target) {
		target = leaseUntil
	}
	s.scheduleDeadTimerLocked(sess.Name, target)
	empty := s.runningCountLocked() == 0
	s.mu.Unlock()

	if s.autoExit && empty {
		slog.Info("auto-exit: last session ended, shutting down")
		s.Shutdown()
	}
}

func (s *Server) runningCountLocked() int {
	count := 0
	for _, sess := range s.sessions {
		if sess.isRunning() {
			count++
		}
	}
	return count
}

func (s *Server) handleList(conn *protocol.Conn, req *protocol.List) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	s.mu.RLock()
	running := make(map[string]bool, len(s.sessions))
	sessions := make([]protocol.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		running[sess.Name] = true
		state := "running"
		if !sess.isRunning() {
			state = "dead"
		}
		cols, rows := sess.size()
		clients := []protocol.ClientInfo(nil)
		if req.IncludeClients {
			sess.clientMu.Lock()
			clients = make([]protocol.ClientInfo, 0, len(sess.clients))
			for _, ac := range sess.clients {
				clients = append(clients, protocol.ClientInfo{ClientID: ac.id, TTY: ac.tty, ReadOnly: ac.readOnly, Version: ac.version})
			}
			sess.clientMu.Unlock()
		}
		sessions = append(sessions, protocol.Session{
			Name:      sess.Name,
			State:     state,
			Cols:      cols,
			Rows:      rows,
			PID:       sess.PID,
			CreatedAt: uint32(sess.CreatedAt.Unix()),
			CWD:       sess.term.GetPwd(ctx),
			Clients:   clients,
		})
	}
	s.mu.RUnlock()

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
		if err := conn.WriteMessage(&protocol.Error{Message: "session not found"}); err != nil {
			slog.Debug("write error response", "err", err)
		}
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
		if err := conn.WriteMessage(&protocol.Error{Message: "session not found"}); err != nil {
			slog.Debug("write error response", "err", err)
		}
		return
	}

	if err := sess.sendInput(msg.Data); err != nil {
		if werr := conn.WriteMessage(&protocol.Error{Message: err.Error()}); werr != nil {
			slog.Debug("write error response", "err", werr)
		}
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
		if err := conn.WriteMessage(&protocol.Error{Message: "session not found"}); err != nil {
			slog.Debug("write error response", "err", err)
		}
		return
	}

	data, err := sess.term.EncodeKey(s.ctx, libghostty.KeyCode(msg.KeyCode), libghostty.Modifier(msg.Mods))
	if err != nil {
		if werr := conn.WriteMessage(&protocol.Error{Message: err.Error()}); werr != nil {
			slog.Debug("write error response", "err", werr)
		}
		return
	}

	if len(data) > 0 {
		if err := sess.sendInput(data); err != nil {
			if werr := conn.WriteMessage(&protocol.Error{Message: err.Error()}); werr != nil {
				slog.Debug("write error response", "err", werr)
			}
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

	if !ok {
		if err := conn.WriteMessage(&protocol.Error{Message: "session not found"}); err != nil {
			slog.Debug("write error response", "err", err)
		}
		return
	}

	// Map protocol format to WASM format, preserving flag bits.
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
		if werr := conn.WriteMessage(&protocol.Error{Message: err.Error()}); werr != nil {
			slog.Debug("write error response", "err", werr)
		}
		return
	}

	if err := conn.WriteMessage(&protocol.DumpResponse{Data: dump.VT}); err != nil {
		slog.Debug("write dump response", "err", err)
	}
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
	running, runningCount, deadCount, ss := s.statusSnapshot(msg.SessionName)

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
	defer s.mu.RUnlock()

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

	if sessionName == "" {
		return running, runningCount, deadCount, nil
	}
	sess, ok := s.sessions[sessionName]
	if !ok {
		return running, runningCount, deadCount, nil
	}

	state := "running"
	if !sess.isRunning() {
		state = "dead"
	}

	sess.clientMu.Lock()
	clients := make([]protocol.ClientInfo, 0, len(sess.clients))
	for _, ac := range sess.clients {
		clients = append(clients, protocol.ClientInfo{ClientID: ac.id, TTY: ac.tty, ReadOnly: ac.readOnly, Version: ac.version})
	}
	sess.clientMu.Unlock()

	cols, rows := sess.size()
	ss := &protocol.SessionStatus{
		Name:    sess.Name,
		State:   state,
		Cols:    cols,
		Rows:    rows,
		PID:     sess.PID,
		CWD:     sess.term.GetPwd(ctx),
		Clients: clients,
	}
	return running, runningCount, deadCount, ss
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
	for _, t := range s.deadTimers {
		t.Stop()
	}
	s.deadTimers = make(map[string]*time.Timer)
	s.deadTimerAt = make(map[string]time.Time)
	for _, lease := range s.attachLeases {
		if lease.timer != nil {
			lease.timer.Stop()
		}
	}
	s.attachLeases = make(map[string]*attachLease)
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
