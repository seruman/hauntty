package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"time"

	hauntty "code.selman.me/hauntty"
	"code.selman.me/hauntty/internal/protocol"
)

func (s *Server) handleList(conn *protocol.Conn, msg *protocol.List) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	type liveSessionSnapshot struct {
		sess *Session
		row  protocol.Session
	}

	s.mu.RLock()
	snapshots := make([]liveSessionSnapshot, 0, len(s.sessions))
	for _, sess := range s.sessions {
		state := protocol.SessionStateRunning
		if !sess.isRunning() {
			state = protocol.SessionStateDead
		}
		cols, rows := sess.size()
		snapshots = append(snapshots, liveSessionSnapshot{
			sess: sess,
			row: protocol.Session{
				Name:      sess.Name,
				State:     state,
				Cols:      cols,
				Rows:      rows,
				PID:       sess.PID,
				CreatedAt: uint32(sess.CreatedAt.Unix()),
			},
		})
	}
	s.mu.RUnlock()

	sessions := make([]protocol.Session, 0, len(snapshots))
	for _, snapshot := range snapshots {
		cwd, err := sessionCWD(ctx, snapshot.sess)
		if err != nil {
			slog.Debug("list session cwd", "err", err)
		}
		snapshot.row.CWD = cwd
		if msg.IncludeClients {
			snapshot.row.Clients = snapshot.sess.clientInfo()
		}
		sessions = append(sessions, snapshot.row)
	}

	dead, err := s.deadSessionRows()
	if err != nil {
		writeError(conn, fmt.Errorf("list dead sessions: %w", err).Error())
		return
	}
	sessions = append(sessions, dead...)

	if err := conn.WriteMessage(&protocol.Sessions{Sessions: sessions}); err != nil {
		slog.Debug("write sessions response", "err", err)
	}
}

func (s *Server) handleDump(conn *protocol.Conn, msg *protocol.Dump) {
	sess, ok := s.liveSession(msg.Name)
	if ok {
		dump, err := sess.dumpScreen(s.ctx, terminalDumpFormat(msg.Format))
		if err != nil {
			writeError(conn, err.Error())
			return
		}
		if err := conn.WriteMessage(&protocol.DumpResponse{Data: dump.Data}); err != nil {
			slog.Debug("write dump response", "err", err)
		}
		return
	}

	data, exists, err := s.dumpDeadSession(msg.Name, msg.Format)
	if err != nil {
		writeError(conn, fmt.Errorf("load dead session state: %w", err).Error())
		return
	}
	if exists {
		if err := conn.WriteMessage(&protocol.DumpResponse{Data: data}); err != nil {
			slog.Debug("write dump response", "err", err)
		}
		return
	}

	writeError(conn, "session not found")
}

func (s *Server) handlePrune(conn *protocol.Conn) {
	count, err := s.pruneDeadSessions()
	if err != nil {
		writeError(conn, err.Error())
		return
	}

	if err := conn.WriteMessage(&protocol.PruneResponse{Count: count}); err != nil {
		slog.Debug("write prune response", "err", err)
	}
}

func (s *Server) handleStatus(conn *protocol.Conn, msg *protocol.Status) {
	runningCount, deadCount, ss := s.statusSnapshot(msg.Name)

	dead, err := s.deadSessionNames()
	if err != nil {
		writeError(conn, fmt.Errorf("list dead sessions: %w", err).Error())
		return
	}
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

func (s *Server) statusSnapshot(sessionName string) (uint32, uint32, *protocol.SessionStatus) {
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()

	s.mu.RLock()
	var runningCount uint32
	var deadCount uint32
	for _, sess := range s.sessions {
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
		return runningCount, deadCount, nil
	}

	state := protocol.SessionStateRunning
	if !sess.isRunning() {
		state = protocol.SessionStateDead
	}

	cols, rows := sess.size()
	cwd, err := sessionCWD(ctx, sess)
	if err != nil {
		slog.Debug("status session cwd", "err", err)
	}
	ss := &protocol.SessionStatus{
		Name:    sess.Name,
		State:   state,
		Cols:    cols,
		Rows:    rows,
		PID:     sess.PID,
		CWD:     cwd,
		Clients: sess.clientInfo(),
	}
	return runningCount, deadCount, ss
}

func sessionCWD(ctx context.Context, sess *Session) (string, error) {
	cwd, ok, err := sess.term.cwd()
	if err != nil {
		return "", fmt.Errorf("lookup cwd for %s: %w", sess.Name, err)
	}
	if !ok {
		return "", nil
	}
	return cwd, nil
}
