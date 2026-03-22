package daemon

import (
	"errors"
	"fmt"
	"os"

	"code.selman.me/hauntty/internal/protocol"
)

func (s *Server) reserveSessionName(name string) (string, error) {
	if name != "" {
		return name, nil
	}

	reserved := s.liveSessionNames()
	if s.persister != nil {
		dead, err := listDeadSessions(reserved)
		if err != nil {
			return "", err
		}
		for _, name := range dead {
			reserved[name] = true
		}
	}

	return generateUniqueName(reserved), nil
}

func (s *Server) liveSessionNames() map[string]bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	running := make(map[string]bool, len(s.sessions))
	for name := range s.sessions {
		running[name] = true
	}
	return running
}

func (s *Server) deadSessionNames() ([]string, error) {
	if s.persister == nil {
		return nil, nil
	}
	return listDeadSessions(s.liveSessionNames())
}

func (s *Server) deadSessionRows() ([]protocol.Session, error) {
	dead, err := s.deadSessionNames()
	if err != nil {
		return nil, err
	}

	rows := make([]protocol.Session, 0, len(dead))
	for _, name := range dead {
		state, exists, err := s.readDeadSession(name)
		if err != nil {
			return nil, fmt.Errorf("load dead session state %q: %w", name, err)
		}
		if !exists {
			continue
		}
		rows = append(rows, protocol.Session{
			Name:    name,
			State:   protocol.SessionStateDead,
			Cols:    state.Cols,
			Rows:    state.Rows,
			SavedAt: uint32(state.SavedAt.Unix()),
		})
	}
	return rows, nil
}

func (s *Server) dumpDeadSession(name string, format protocol.DumpFormat) ([]byte, bool, error) {
	state, exists, err := s.readDeadSession(name)
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, nil
	}

	data, err := dumpDeadState(s.ctx, s.wasmRT, state, s.defaultScrollback, format)
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (s *Server) pruneDeadSessions() (uint32, error) {
	dead, err := s.deadSessionNames()
	if err != nil {
		return 0, err
	}

	var count uint32
	for _, name := range dead {
		if err := s.removeDeadSession(name); err != nil {
			return 0, fmt.Errorf("prune dead session %q: %w", name, err)
		}
		count++
	}
	return count, nil
}

func (s *Server) readDeadSession(name string) (*sessionState, bool, error) {
	if s.persister == nil {
		return nil, false, nil
	}

	state, err := loadState(name)
	if err == nil {
		return state, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, err
}

func (s *Server) removeDeadSession(name string) error {
	if s.persister == nil {
		return nil
	}
	return cleanState(name)
}

func (s *Server) writeDeadSession(name string, state *sessionState) error {
	if s.persister == nil {
		return nil
	}
	return writeState(name, state)
}

func (s *Server) prepareCreateDeadSession(name string, force bool) error {
	_, exists, err := s.readDeadSession(name)
	if err != nil && !force {
		return fmt.Errorf("load dead session state: %w", err)
	}
	if exists && !force {
		return fmt.Errorf("dead session state exists")
	}
	if !force {
		return nil
	}
	if err := s.removeDeadSession(name); err != nil {
		return fmt.Errorf("clean dead session state: %w", err)
	}
	return nil
}
