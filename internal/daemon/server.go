package daemon

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
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
	lockPath          string
	sessions          map[string]*Session
	mu                sync.RWMutex
	wasmRT            *libghostty.Runtime
	ctx               context.Context
	cancel            context.CancelFunc
	listener          net.Listener
	lockFile          *os.File
	persister         *persister
	defaultScrollback uint32
	resizePolicy      config.ResizePolicy
	autoExit          bool
	shutdownOnce      sync.Once
	startedAt         time.Time
}

func New(ctx context.Context, cfg *config.DaemonConfig, resizePolicy config.ResizePolicy) (*Server, error) {
	if cfg.StatePersistence && cfg.StatePersistenceInterval <= 0 {
		return nil, fmt.Errorf("daemon: state_persistence_interval must be > 0 when state persistence is enabled")
	}

	rt, err := libghostty.NewRuntime(ctx)
	if err != nil {
		return nil, fmt.Errorf("daemon: init wasm runtime: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)
	sock := cmp.Or(cfg.SocketPath, config.SocketPath())
	s := &Server{
		socketPath:        sock,
		pidPath:           filepath.Join(filepath.Dir(sock), "hauntty.pid"),
		lockPath:          filepath.Join(filepath.Dir(sock), "hauntty.lock"),
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
		s.persister = newPersister(s.liveSessions, interval)
	}

	return s, nil
}

func (s *Server) Listen() error {
	cleanStaleTmp()

	dir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("daemon: create socket dir: %w", err)
	}

	if err := s.acquireLock(); err != nil {
		return err
	}
	lockHeld := true
	defer func() {
		if lockHeld {
			s.releaseLock()
		}
	}()

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
		s.persister.start()
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

func (s *Server) acquireLock() error {
	f, err := os.OpenFile(s.lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("daemon: open lock file: %w", err)
	}

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return fmt.Errorf("daemon already running")
		}
		return fmt.Errorf("daemon: acquire lock: %w", err)
	}

	s.lockFile = f
	return nil
}

func (s *Server) releaseLock() {
	if s.lockFile == nil {
		return
	}
	_ = unix.Flock(int(s.lockFile.Fd()), unix.LOCK_UN)
	_ = s.lockFile.Close()
	s.lockFile = nil
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
	_ = raw.Control(func(fd uintptr) {
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
		if err := conn.WriteVersionReply(0, ""); err != nil {
			slog.Debug("reject handshake", "err", err)
		}
		return
	}
	serverRev := hauntty.Version()
	if clientRev != serverRev {
		slog.Warn("client/server revision differ", "client", clientRev, "server", serverRev)
	}
	if err := conn.WriteVersionReply(protocol.ProtocolVersion, serverRev); err != nil {
		return
	}

	var attached *Session
	var ac *sessionClient
	var readOnly bool

	for {
		msg, err := conn.ReadMessage()
		if err != nil {
			break
		}

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
					attached.resizeClient(ac, termSize{cols: m.Cols, rows: m.Rows, xpixel: m.Xpixel, ypixel: m.Ypixel})
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

		switch m := msg.(type) {
		case *protocol.Create:
			s.handleCreate(conn, m)
		case *protocol.Attach:
			sess, client, ro, err := s.handleAttach(conn, netConn.Close, m, clientRev)
			if err != nil {
				slog.Debug("attach error", "err", err)
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

func (s *Server) Shutdown() {
	s.shutdownOnce.Do(s.shutdown)
}

func (s *Server) shutdown() {
	if s.persister != nil {
		s.persister.stop()
		if err := s.persister.saveAll(); err != nil {
			slog.Warn("persist: shutdown save failed", "err", err)
		}
	}

	s.cancel()

	if s.listener != nil {
		s.listener.Close()
	}
	s.releaseLock()

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
