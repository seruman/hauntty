package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/selman/hauntty/config"
	"github.com/selman/hauntty/namegen"
	"github.com/selman/hauntty/protocol"
	"github.com/selman/hauntty/wasm"
	"golang.org/x/sys/unix"
)

type Server struct {
	socketPath        string
	pidPath           string
	sessions          map[string]*Session
	mu                sync.RWMutex
	wasmRT            *wasm.Runtime
	ctx               context.Context
	cancel            context.CancelFunc
	listener          net.Listener
	persister         *Persister
	defaultScrollback uint32
	autoExit          bool
}

func New(ctx context.Context, cfg *config.DaemonConfig) (*Server, error) {
	rt, err := wasm.NewRuntime(ctx)
	if err != nil {
		return nil, fmt.Errorf("daemon: init wasm runtime: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	s := &Server{
		socketPath:        SocketPath(),
		pidPath:           PIDPath(),
		sessions:          make(map[string]*Session),
		wasmRT:            rt,
		ctx:               ctx,
		cancel:            cancel,
		defaultScrollback: cfg.DefaultScrollback,
		autoExit:          cfg.AutoExit,
	}

	if cfg.StatePersistence {
		interval := time.Duration(cfg.StatePersistenceInterval) * time.Second
		s.persister = NewPersister(s.liveSessions, interval)
	}

	return s, nil
}

func (s *Server) Listen() error {
	CleanStaleTmp()

	dir := socketDir()
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

	clientVer, err := conn.AcceptHandshake()
	if err != nil {
		return
	}
	if clientVer != protocol.ProtocolVersion {
		if err := conn.AcceptVersion(0); err != nil {
			slog.Debug("reject handshake", "err", err)
		}
		return
	}
	if err := conn.AcceptVersion(protocol.ProtocolVersion); err != nil {
		return
	}

	// Attached session for this connection (set by handleAttach).
	var attached *Session

	for {
		msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

		switch m := msg.(type) {
		case *protocol.Attach:
			attached, err = s.handleAttach(conn, netConn.Close, m)
			if err != nil {
				slog.Debug("attach error", "err", err)
				return
			}
		case *protocol.Input:
			if attached != nil {
				attached.sendInput(m.Data)
			}
		case *protocol.Resize:
			if attached != nil {
				attached.resize(m.Cols, m.Rows)
			}
		case *protocol.Detach:
			target := attached
			if m.Name != "" {
				s.mu.RLock()
				target = s.sessions[m.Name]
				s.mu.RUnlock()
			}
			if target != nil {
				target.disconnectClient()
				attached = nil
			}
		case *protocol.List:
			s.handleList(conn)
		case *protocol.Kill:
			s.handleKill(conn, m)
		case *protocol.Send:
			s.handleSend(conn, m)
		case *protocol.Dump:
			s.handleDump(conn, m)
		case *protocol.Prune:
			s.handlePrune(conn)
		}
	}

	// Connection closed — detach if attached.
	if attached != nil {
		attached.detach()
	}
}

func (s *Server) handleAttach(conn *protocol.Conn, closeConn func() error, msg *protocol.Attach) (*Session, error) {
	name := msg.Name
	if name == "" {
		existing := make(map[string]bool, len(s.sessions))
		for k := range s.sessions {
			existing[k] = true
		}
		name = namegen.GenerateUnique(existing)
	}

	s.mu.Lock()
	sess, exists := s.sessions[name]
	if !exists {
		scrollback := msg.ScrollbackLines
		if scrollback == 0 {
			scrollback = s.defaultScrollback
		}

		// Check for a persisted dead session to restore.
		var err error
		if state, serr := LoadState(name); serr == nil {
			slog.Info("restoring dead session", "session", name)
			sess, err = restoreSession(s.ctx, name, msg.Command, msg.Env, msg.Cols, msg.Rows, scrollback, s.wasmRT, state)
			if err == nil {
				CleanState(name)
			}
		}
		if sess == nil {
			sess, err = newSession(s.ctx, name, msg.Command, msg.Env, msg.Cols, msg.Rows, scrollback, s.wasmRT)
		}
		if err != nil {
			s.mu.Unlock()
			if werr := conn.WriteMessage(&protocol.Error{Code: 1, Message: err.Error()}); werr != nil {
				slog.Debug("write error response", "err", werr)
			}
			return nil, err
		}
		s.sessions[name] = sess

		go func() {
			<-sess.done
			s.mu.Lock()
			delete(s.sessions, name)
			empty := len(s.sessions) == 0
			s.mu.Unlock()
			CleanState(name)
			if s.autoExit && empty {
				slog.Info("auto-exit: last session ended, shutting down")
				s.Shutdown()
			}
		}()
	}
	s.mu.Unlock()

	// Send OK before state dump.
	err := conn.WriteMessage(&protocol.OK{
		SessionName: sess.Name,
		Cols:        sess.Cols,
		Rows:        sess.Rows,
		PID:         sess.PID,
		Created:     !exists,
	})
	if err != nil {
		return nil, err
	}

	if err := sess.attach(conn, closeConn, msg.Cols, msg.Rows); err != nil {
		if werr := conn.WriteMessage(&protocol.Error{Code: 2, Message: err.Error()}); werr != nil {
			slog.Debug("write error response", "err", werr)
		}
		return nil, err
	}

	return sess, nil
}

func (s *Server) handleList(conn *protocol.Conn) {
	s.mu.RLock()
	running := make(map[string]bool, len(s.sessions))
	sessions := make([]protocol.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		running[sess.Name] = true
		state := "running"
		if !sess.isRunning() {
			state = "dead"
		}
		sessions = append(sessions, protocol.Session{
			Name:      sess.Name,
			State:     state,
			Cols:      sess.Cols,
			Rows:      sess.Rows,
			PID:       sess.PID,
			CreatedAt: uint32(sess.CreatedAt.Unix()),
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
		if err := conn.WriteMessage(&protocol.Error{Code: 3, Message: "session not found"}); err != nil {
			slog.Debug("write error response", "err", err)
		}
		return
	}

	sess.kill()
	if err := conn.WriteMessage(&protocol.OK{SessionName: msg.Name}); err != nil {
		slog.Debug("write ok response", "err", err)
	}
}

func (s *Server) handleSend(conn *protocol.Conn, msg *protocol.Send) {
	s.mu.RLock()
	sess, ok := s.sessions[msg.Name]
	s.mu.RUnlock()

	if !ok {
		if err := conn.WriteMessage(&protocol.Error{Code: 3, Message: "session not found"}); err != nil {
			slog.Debug("write error response", "err", err)
		}
		return
	}

	if err := sess.sendInput(msg.Data); err != nil {
		if werr := conn.WriteMessage(&protocol.Error{Code: 4, Message: err.Error()}); werr != nil {
			slog.Debug("write error response", "err", werr)
		}
		return
	}
	if err := conn.WriteMessage(&protocol.OK{SessionName: msg.Name}); err != nil {
		slog.Debug("write ok response", "err", err)
	}
}

func (s *Server) handleDump(conn *protocol.Conn, msg *protocol.Dump) {
	s.mu.RLock()
	sess, ok := s.sessions[msg.Name]
	s.mu.RUnlock()

	if !ok {
		if err := conn.WriteMessage(&protocol.Error{Code: 3, Message: "session not found"}); err != nil {
			slog.Debug("write error response", "err", err)
		}
		return
	}

	// Map protocol format (0=plain, 1=vt, 2=html) to WASM dump format.
	var wasmFmt uint32
	switch msg.Format {
	case 1:
		wasmFmt = wasm.DumpVTSafe // VT with SGR reset, no palette/mode corruption.
	default:
		wasmFmt = wasm.DumpPlain
	}

	dump, err := sess.dumpScreen(s.ctx, wasmFmt)
	if err != nil {
		if werr := conn.WriteMessage(&protocol.Error{Code: 5, Message: err.Error()}); werr != nil {
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

func (s *Server) Shutdown() {
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

	s.wasmRT.Close(context.Background())

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
