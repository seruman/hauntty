package daemon

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestEncodeDecodeRoundtrip(t *testing.T) {
	saved := time.Unix(1700000000, 0)
	state := &sessionState{
		Cols:     80,
		Rows:     24,
		SavedAt:  saved,
		Snapshot: []byte("snapshot"),
	}

	data, err := encodeState(state)
	assert.NilError(t, err)

	got, err := decodeState(data)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, state)
}

func TestEncodeStateRejectsEmptySnapshot(t *testing.T) {
	state := &sessionState{
		Cols:    120,
		Rows:    40,
		SavedAt: time.Unix(1700000000, 0),
	}

	_, err := encodeState(state)
	assert.Error(t, err, "persist: snapshot is empty")
}

func TestDecodeStateBadMagic(t *testing.T) {
	_, err := decodeState([]byte("NOPE\x01"))
	assert.Equal(t, err.Error(), "persist: bad magic 4e4f5045")
}

func TestDecodeStateTooShort(t *testing.T) {
	_, err := decodeState([]byte("HT"))
	assert.Equal(t, err.Error(), "persist: state file too short")
}

func TestDecodeStateRejectsVersion1(t *testing.T) {
	data := []byte{'H', 'T', 'S', 'T', 1}
	_, err := decodeState(data)
	assert.Equal(t, err.Error(), "persist: unsupported version 1")
}

func TestDecodeStateUnsupportedVersion(t *testing.T) {
	data := []byte{'H', 'T', 'S', 'T', 99}
	_, err := decodeState(data)
	assert.Equal(t, err.Error(), "persist: unsupported version 99")
}

func TestDecodeStateRejectsEmptySnapshot(t *testing.T) {
	state := &sessionState{
		Cols:     80,
		Rows:     24,
		SavedAt:  time.Unix(1700000000, 0),
		Snapshot: []byte("x"),
	}

	data, err := encodeState(state)
	assert.NilError(t, err)
	binary.BigEndian.PutUint32(data[17:21], 0)

	_, err = decodeState(data)
	assert.Error(t, err, "persist: snapshot is empty")
}

func TestDecodeStateRejectsOversizedSnapshot(t *testing.T) {
	state := &sessionState{
		Cols:     80,
		Rows:     24,
		SavedAt:  time.Unix(1700000000, 0),
		Snapshot: []byte("x"),
	}

	data, err := encodeState(state)
	assert.NilError(t, err)
	binary.BigEndian.PutUint32(data[17:21], maxStateSnapshotBytes+1)

	_, err = decodeState(data)
	assert.Error(t, err, "persist: snapshot too large: 134217729 bytes")
}

func TestEncodeStateFormat(t *testing.T) {
	saved := time.Unix(0x65655E40, 0)
	state := &sessionState{
		Cols:     80,
		Rows:     24,
		SavedAt:  saved,
		Snapshot: []byte("AB"),
	}

	data, err := encodeState(state)
	assert.NilError(t, err)

	want := []byte{
		'H', 'T', 'S', 'T', // magic
		2,          // version
		0x00, 0x50, // cols = 80
		0x00, 0x18, // rows = 24
		0, 0, 0, 0, 0x65, 0x65, 0x5E, 0x40, // saved_at
		0, 0, 0, 2, // snapshot_length = 2
		'A', 'B', // snapshot
	}
	assert.DeepEqual(t, data, want)
}

func TestSaveAllWithAggregatesErrors(t *testing.T) {
	p := &persister{sessions: func() map[string]*Session {
		return map[string]*Session{
			"beta":  nil,
			"alpha": nil,
		}
	}}

	err := p.saveAllWith(func(name string, _ *Session) error {
		switch name {
		case "alpha":
			return errors.New("alpha failed")
		case "beta":
			return errors.New("beta failed")
		default:
			return nil
		}
	})

	assert.Error(t, err, "alpha: alpha failed\nbeta: beta failed")
}

func TestSaveAllWithNoErrors(t *testing.T) {
	p := &persister{sessions: func() map[string]*Session {
		return map[string]*Session{"alpha": nil}
	}}

	err := p.saveAllWith(func(_ string, _ *Session) error {
		return nil
	})

	assert.NilError(t, err)
}

func TestCleanStaleTmp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	sessionDir := filepath.Join(dir, "hauntty", "sessions")
	assert.NilError(t, os.MkdirAll(sessionDir, 0o700))

	assert.NilError(t, os.WriteFile(filepath.Join(sessionDir, "foo.state.tmp"), []byte("stale"), 0o600))
	assert.NilError(t, os.WriteFile(filepath.Join(sessionDir, "bar.state"), []byte("keep"), 0o600))

	cleanStaleTmp()

	entries, err := os.ReadDir(sessionDir)
	assert.NilError(t, err)
	got := []string{entries[0].Name()}
	assert.DeepEqual(t, got, []string{"bar.state"})
}

func TestLoadStateMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	_, err := loadState("nonexistent")
	assert.Error(t, err, "open "+filepath.Join(dir, "hauntty", "sessions", "nonexistent.state")+": no such file or directory")
}
