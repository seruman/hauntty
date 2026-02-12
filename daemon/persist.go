package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/selman/hauntty/wasm"
)

// Persister periodically saves terminal state for all sessions.
type Persister struct {
	sessions func() map[string]*Session
	dir      string
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewPersister creates a new Persister that periodically dumps session state.
func NewPersister(sessions func() map[string]*Session, interval time.Duration) *Persister {
	ctx, cancel := context.WithCancel(context.Background())
	return &Persister{
		sessions: sessions,
		dir:      stateDir(),
		interval: interval,
		ctx:      ctx,
		cancel:   cancel,
	}
}

// Start begins the periodic save loop in a goroutine.
func (p *Persister) Start() {
	go p.loop()
}

// Stop cancels the periodic save loop.
func (p *Persister) Stop() {
	p.cancel()
}

func (p *Persister) loop() {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.saveAll()
		}
	}
}

func (p *Persister) saveAll() {
	sessions := p.sessions()
	for name, s := range sessions {
		if err := p.SaveSession(name, s); err != nil {
			slog.Debug("persist: save failed", "session", name, "err", err)
		}
	}
}

// SaveSession dumps a single session's screen state to disk.
func (p *Persister) SaveSession(name string, s *Session) error {
	dump, err := s.dumpScreen(p.ctx, wasm.DumpVTFull)
	if err != nil {
		return fmt.Errorf("persist: dump screen: %w", err)
	}

	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return fmt.Errorf("persist: create dir: %w", err)
	}

	path := filepath.Join(p.dir, name+".state")
	return os.WriteFile(path, dump.VT, 0o600)
}

// LoadState reads a persisted state file for the given session name.
func LoadState(name string) ([]byte, error) {
	path := filepath.Join(stateDir(), name+".state")
	return os.ReadFile(path)
}

// ListDeadSessions returns names of sessions that have state files but no running session.
func ListDeadSessions(running map[string]bool) ([]string, error) {
	dir := stateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var dead []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".state") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".state")
		if !running[name] {
			dead = append(dead, name)
		}
	}
	return dead, nil
}

// CleanState removes a session's state file.
func CleanState(name string) error {
	path := filepath.Join(stateDir(), name+".state")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func stateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "hauntty", "sessions")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "hauntty", "sessions")
}
