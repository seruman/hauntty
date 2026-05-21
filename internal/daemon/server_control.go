package daemon

import "code.selman.me/hauntty/internal/protocol"

func (s *Server) handleKick(conn *protocol.Conn, msg *protocol.Kick) {
	if msg.Name == "" || msg.ClientID == "" {
		writeError(conn, "name and client ID required")
		return
	}

	sess, ok := s.liveSession(msg.Name)
	if !ok {
		writeError(conn, "session not found")
		return
	}

	if !sess.kickClient(msg.ClientID) {
		writeError(conn, "client not found")
		return
	}

	writeOK(conn)
}

func (s *Server) handleKill(conn *protocol.Conn, msg *protocol.Kill) {
	sess, ok := s.liveSession(msg.Name)
	if !ok {
		writeError(conn, "session not found")
		return
	}

	sess.kill()
	writeOK(conn)
}

func (s *Server) handleSend(conn *protocol.Conn, msg *protocol.Send) {
	sess, ok := s.liveSession(msg.Name)
	if !ok {
		writeError(conn, "session not found")
		return
	}

	if err := sess.sendInput(msg.Data); err != nil {
		writeError(conn, err.Error())
		return
	}
	writeOK(conn)
}

func (s *Server) handleSendKey(conn *protocol.Conn, msg *protocol.SendKey) {
	sess, ok := s.liveSession(msg.Name)
	if !ok {
		writeError(conn, "session not found")
		return
	}

	data, err := sess.term.encodeClientKey(msg.Key, msg.Mods)
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

	writeOK(conn)
}
