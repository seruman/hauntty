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

	sess, err := newSession(s.ctx, s.wasmRT, s.resizePolicy, sessionStartSpec{
		name:       name,
		command:    msg.Command,
		env:        msg.Env,
		cwd:        msg.CWD,
		size:       termSize{cols: 80, rows: 24},
		scrollback: s.scrollback(msg.Scrollback),
	})
	if err != nil {
		writeError(conn, err.Error())
		return
	}

	if !s.addSession(name, sess) {
		sess.close(s.ctx)
		writeError(conn, "session already exists")
		return
	}

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
			err = fmt.Errorf("%s", deadSessionStateExistsMessage(name))
			writeError(conn, err.Error())
			return nil, nil, false, err
		}

		sess, err = newSession(s.ctx, s.wasmRT, s.resizePolicy, sessionStartSpec{
			name:       name,
			command:    msg.Command,
			env:        msg.Env,
			cwd:        msg.CWD,
			size:       size,
			scrollback: s.scrollback(msg.Scrollback),
		})
		if err != nil {
			writeError(conn, err.Error())
			return nil, nil, false, err
		}

		existing, inserted := s.insertSessionOrExisting(name, sess)
		if !inserted {
			sess.close(s.ctx)
			sess = existing
		} else {
			created = true
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
			s.removeSession(name)
			sess.close(s.ctx)
		}
		writeError(conn, err.Error())
		return nil, nil, false, err
	}

	if created {
		s.watchSession(sess)
	}
	return sess, ac, msg.ReadOnly, nil
}

func (s *Server) handleAttachRestore(conn *protocol.Conn, closeConn func() error, msg *protocol.Attach, clientRev string) (*Session, *sessionClient, bool, error) {
	name := msg.Name
	state, err := s.prepareRestoreDeadSession(name)
	if err != nil {
		writeError(conn, err.Error())
		return nil, nil, false, err
	}

	size := termSize{cols: msg.Cols, rows: msg.Rows, xpixel: msg.Xpixel, ypixel: msg.Ypixel}

	sess, err := restoreSession(s.ctx, s.wasmRT, state, s.resizePolicy, sessionStartSpec{
		name:       name,
		command:    msg.Command,
		env:        msg.Env,
		cwd:        msg.CWD,
		size:       size,
		scrollback: s.scrollback(msg.Scrollback),
	})
	if err != nil {
		writeError(conn, err.Error())
		return nil, nil, false, err
	}

	if !s.insertSession(name, sess) {
		sess.close(s.ctx)
		writeError(conn, "session already exists")
		return nil, nil, false, fmt.Errorf("session %q created by another client during restore", name)
	}

	if err := s.commitRestoreDeadSession(name); err != nil {
		s.removeSession(name)
		sess.close(s.ctx)
		writeError(conn, err.Error())
		return nil, nil, false, err
	}

	ac, err := sess.attach(s.ctx, sessionAttachSpec{
		conn:      conn,
		closeConn: closeConn,
		size:      size,
		version:   clientRev,
		readOnly:  msg.ReadOnly,
	})
	if err != nil {
		s.removeSession(name)
		sess.close(s.ctx)
		err = s.rollbackRestoreDeadSession(name, state, err)
		writeError(conn, err.Error())
		return nil, nil, false, err
	}

	s.watchSession(sess)
	return sess, ac, msg.ReadOnly, nil
}

func (s *Server) scrollback(requested uint32) uint32 {
	if requested == 0 {
		return s.defaultScrollback
	}
	return requested
}

func (s *Server) addSession(name string, sess *Session) bool {
	_, inserted := s.insertSessionOrExisting(name, sess)
	if !inserted {
		return false
	}
	s.watchSession(sess)
	return true
}

func (s *Server) insertSession(name string, sess *Session) bool {
	_, inserted := s.insertSessionOrExisting(name, sess)
	return inserted
}

func (s *Server) insertSessionOrExisting(name string, sess *Session) (*Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, exists := s.sessions[name]; exists {
		return existing, false
	}
	s.sessions[name] = sess
	return nil, true
}

func (s *Server) removeSession(name string) {
	s.mu.Lock()
	delete(s.sessions, name)
	s.mu.Unlock()
}

func (s *Server) watchSession(sess *Session) {
	// Start watching only after the caller has committed the session.
	// Failed create, Attach, and Restore paths close sessions without persistence or auto-exit side effects.
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
