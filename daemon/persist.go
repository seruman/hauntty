package daemon

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/selman/hauntty/protocol"
	"github.com/selman/hauntty/wasm"
)

// State file format: [HTST magic 4B][version u8][cols u16][rows u16]
// [cursor_row u32][cursor_col u32][is_alt_screen u8][saved_at u64]
// [vt_data_length u32][vt_data...]
var stateMagic = [4]byte{'H', 'T', 'S', 'T'}

const stateVersion = 1

type SessionState struct {
	Cols        uint16
	Rows        uint16
	CursorRow   uint32
	CursorCol   uint32
	IsAltScreen bool
	SavedAt     time.Time
	VT          []byte
}

type Persister struct {
	sessions func() map[string]*Session
	dir      string
	interval time.Duration
	ctx      context.Context
	cancel   context.CancelFunc
}

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

func (p *Persister) Start() {
	go p.loop()
}

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

func (p *Persister) SaveSession(name string, s *Session) error {
	dump, err := s.dumpScreen(p.ctx, wasm.DumpVTFull)
	if err != nil {
		return fmt.Errorf("persist: dump screen: %w", err)
	}

	state := &SessionState{
		Cols:        s.Cols,
		Rows:        s.Rows,
		CursorRow:   dump.CursorRow,
		CursorCol:   dump.CursorCol,
		IsAltScreen: dump.IsAltScreen,
		SavedAt:     time.Now(),
		VT:          dump.VT,
	}

	data, err := encodeState(state)
	if err != nil {
		return fmt.Errorf("persist: encode state: %w", err)
	}

	if err := os.MkdirAll(p.dir, 0o700); err != nil {
		return fmt.Errorf("persist: create dir: %w", err)
	}

	path := filepath.Join(p.dir, name+".state")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("persist: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("persist: rename: %w", err)
	}
	return nil
}

func encodeState(s *SessionState) ([]byte, error) {
	var buf bytes.Buffer
	enc := protocol.NewEncoder(&buf)

	buf.Write(stateMagic[:])
	if err := enc.WriteU8(stateVersion); err != nil {
		return nil, err
	}
	if err := enc.WriteU16(s.Cols); err != nil {
		return nil, err
	}
	if err := enc.WriteU16(s.Rows); err != nil {
		return nil, err
	}
	if err := enc.WriteU32(s.CursorRow); err != nil {
		return nil, err
	}
	if err := enc.WriteU32(s.CursorCol); err != nil {
		return nil, err
	}
	if err := enc.WriteBool(s.IsAltScreen); err != nil {
		return nil, err
	}
	if err := enc.WriteU64(uint64(s.SavedAt.Unix())); err != nil {
		return nil, err
	}
	if err := enc.WriteBytes(s.VT); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func LoadState(name string) (*SessionState, error) {
	path := filepath.Join(stateDir(), name+".state")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return decodeState(data)
}

func decodeState(data []byte) (*SessionState, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("persist: state file too short")
	}
	if !bytes.Equal(data[:4], stateMagic[:]) {
		return nil, fmt.Errorf("persist: bad magic %x", data[:4])
	}

	dec := protocol.NewDecoder(bytes.NewReader(data[4:]))

	version, err := dec.ReadU8()
	if err != nil {
		return nil, fmt.Errorf("persist: read version: %w", err)
	}
	if version != stateVersion {
		return nil, fmt.Errorf("persist: unsupported version %d", version)
	}

	cols, err := dec.ReadU16()
	if err != nil {
		return nil, fmt.Errorf("persist: read cols: %w", err)
	}
	rows, err := dec.ReadU16()
	if err != nil {
		return nil, fmt.Errorf("persist: read rows: %w", err)
	}
	cursorRow, err := dec.ReadU32()
	if err != nil {
		return nil, fmt.Errorf("persist: read cursor_row: %w", err)
	}
	cursorCol, err := dec.ReadU32()
	if err != nil {
		return nil, fmt.Errorf("persist: read cursor_col: %w", err)
	}
	isAlt, err := dec.ReadBool()
	if err != nil {
		return nil, fmt.Errorf("persist: read is_alt_screen: %w", err)
	}
	savedAtUnix, err := dec.ReadU64()
	if err != nil {
		return nil, fmt.Errorf("persist: read saved_at: %w", err)
	}
	vt, err := dec.ReadBytes()
	if err != nil {
		return nil, fmt.Errorf("persist: read vt_data: %w", err)
	}

	return &SessionState{
		Cols:        cols,
		Rows:        rows,
		CursorRow:   cursorRow,
		CursorCol:   cursorCol,
		IsAltScreen: isAlt,
		SavedAt:     time.Unix(int64(savedAtUnix), 0),
		VT:          vt,
	}, nil
}

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

func CleanState(name string) error {
	path := filepath.Join(stateDir(), name+".state")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// CleanStaleTmp removes leftover .state.tmp files from interrupted writes.
func CleanStaleTmp() {
	dir := stateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".state.tmp") {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

func stateDir() string {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "hauntty", "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("cannot determine home directory, using temp dir for state", "err", err)
		home = os.TempDir()
	}
	return filepath.Join(home, ".local", "state", "hauntty", "sessions")
}
