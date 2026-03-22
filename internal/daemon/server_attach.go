package daemon

import (
	"fmt"
	"log/slog"

	"code.selman.me/hauntty/internal/protocol"
)

func (s *Server) handleCreate(conn *protocol.Conn, msg *protocol.Create) {
	name, err := s.reserveSessionName(msg.Name)
	if err != nil {
		writeError(conn, fmt.Errorf("reserve session name: %w", err).Error())
		return
	}

	s.mu.Lock()
	if _, exists := s.sessions[name]; exists {
		s.mu.Unlock()
		writeError(conn, "session already exists")
		return
	}
	s.mu.Unlock()

	if err := s.prepareCreateDeadSession(name, msg.Force); err != nil {
		writeError(conn, err.Error())
		return
	}

	scrollback := msg.Scrollback
	if scrollback == 0 {
		scrollback = s.defaultScrollback
	}

	sess, err := newSession(s.ctx, s.wasmRT, s.resizePolicy, sessionStartSpec{
		name:       name,
		command:    msg.Command,
		env:        msg.Env,
		cwd:        msg.CWD,
		size:       termSize{cols: 80, rows: 24},
		scrollback: scrollback,
	})
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
	if msg.Restore {
		return s.handleAttachRestore(conn, closeConn, msg, clientRev)
	}

	name, err := s.reserveSessionName(msg.Name)
	if err != nil {
		err = fmt.Errorf("reserve session name: %w", err)
		writeError(conn, err.Error())
		return nil, nil, false, err
	}

	size := termSize{cols: msg.Cols, rows: msg.Rows, xpixel: msg.Xpixel, ypixel: msg.Ypixel}

	s.mu.Lock()
	sess := s.sessions[name]
	s.mu.Unlock()

	created := false
	if sess == nil {
		_, exists, err := s.readDeadSession(name)
		switch {
		case err != nil:
			err = fmt.Errorf("load dead session state: %w", err)
			writeError(conn, err.Error())
			return nil, nil, false, err
		case exists:
			writeError(conn, "dead session state exists")
			return nil, nil, false, fmt.Errorf("dead session state exists for %q", name)
		}

		scrollback := msg.Scrollback
		if scrollback == 0 {
			scrollback = s.defaultScrollback
		}

		sess, err = newSession(s.ctx, s.wasmRT, s.resizePolicy, sessionStartSpec{
			name:       name,
			command:    msg.Command,
			env:        msg.Env,
			cwd:        msg.CWD,
			size:       size,
			scrollback: scrollback,
		})
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

	ac, err := sess.attach(s.ctx, sessionAttachSpec{
		conn:      conn,
		closeConn: closeConn,
		size:      size,
		version:   clientRev,
		readOnly:  msg.ReadOnly,
		created:   created,
	})
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

	state, exists, err := s.readDeadSession(name)
	if err != nil {
		err = fmt.Errorf("load saved state: %w", err)
		writeError(conn, err.Error())
		return nil, nil, false, err
	}
	if !exists {
		writeError(conn, "no saved state")
		return nil, nil, false, fmt.Errorf("no saved state for %q", name)
	}

	scrollback := msg.Scrollback
	if scrollback == 0 {
		scrollback = s.defaultScrollback
	}

	size := termSize{cols: msg.Cols, rows: msg.Rows, xpixel: msg.Xpixel, ypixel: msg.Ypixel}

	sess, err := restoreSession(s.ctx, s.wasmRT, state, s.resizePolicy, sessionStartSpec{
		name:       name,
		command:    msg.Command,
		env:        msg.Env,
		cwd:        msg.CWD,
		size:       size,
		scrollback: scrollback,
	})
	if err != nil {
		writeError(conn, err.Error())
		return nil, nil, false, err
	}

	s.mu.Lock()
	if _, exists := s.sessions[name]; exists {
		s.mu.Unlock()
		sess.close(s.ctx)
		writeError(conn, "session already exists")
		return nil, nil, false, fmt.Errorf("session %q created by another client during restore", name)
	}
	s.sessions[name] = sess
	s.mu.Unlock()

	if err := s.removeDeadSession(name); err != nil {
		s.mu.Lock()
		delete(s.sessions, name)
		s.mu.Unlock()
		sess.close(s.ctx)
		err = fmt.Errorf("clean dead session state: %w", err)
		writeError(conn, err.Error())
		return nil, nil, false, err
	}

	s.watchSession(sess)

	ac, err := sess.attach(s.ctx, sessionAttachSpec{
		conn:      conn,
		closeConn: closeConn,
		size:      size,
		version:   clientRev,
		readOnly:  msg.ReadOnly,
	})
	if err != nil {
		s.mu.Lock()
		delete(s.sessions, name)
		s.mu.Unlock()
		sess.close(s.ctx)
		if restoreErr := s.writeDeadSession(name, state); restoreErr != nil {
			err = fmt.Errorf("%w; restore dead session state: %v", err, restoreErr)
		}
		writeError(conn, err.Error())
		return nil, nil, false, err
	}

	return sess, ac, msg.ReadOnly, nil
}

func (s *Server) watchSession(sess *Session) {
	go func() {
		<-sess.done
		if s.persister != nil && s.ctx.Err() == nil {
			if err := s.persister.saveSession(sess.Name, sess); err != nil {
				slog.Warn("persist: save failed on exit", "session", sess.Name, "err", err)
			}
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
