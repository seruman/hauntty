package daemon

import (
	"log/slog"

	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
)

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

	data, err := sess.term.EncodeKey(libghostty.KeyCode(msg.Key), libghostty.Modifier(msg.Mods))
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
