package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestEncodeDecodeRoundtrip(t *testing.T) {
	saved := time.Unix(1700000000, 0)
	state := &SessionState{
		Cols:        80,
		Rows:        24,
		CursorRow:   5,
		CursorCol:   10,
		IsAltScreen: false,
		SavedAt:     saved,
		VT:          []byte("Hello\x1b[0m\x1b[2;1H"),
	}

	data, err := encodeState(state)
	assert.NilError(t, err)

	got, err := decodeState(data)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, state)
}

func TestEncodeDecodeAltScreen(t *testing.T) {
	saved := time.Unix(1700000000, 0)
	state := &SessionState{
		Cols:        120,
		Rows:        40,
		CursorRow:   0,
		CursorCol:   0,
		IsAltScreen: true,
		SavedAt:     saved,
		VT:          []byte{},
	}

	data, err := encodeState(state)
	assert.NilError(t, err)

	got, err := decodeState(data)
	assert.NilError(t, err)
	assert.DeepEqual(t, got, state)
}

func TestDecodeStateBadMagic(t *testing.T) {
	_, err := decodeState([]byte("NOPE\x01"))
	assert.Equal(t, err.Error(), "persist: bad magic 4e4f5045")
}

func TestDecodeStateTooShort(t *testing.T) {
	_, err := decodeState([]byte("HT"))
	assert.Equal(t, err.Error(), "persist: state file too short")
}

func TestDecodeStateUnsupportedVersion(t *testing.T) {
	data := []byte{'H', 'T', 'S', 'T', 99}
	_, err := decodeState(data)
	assert.Equal(t, err.Error(), "persist: unsupported version 99")
}

func TestEncodeStateFormat(t *testing.T) {
	saved := time.Unix(0x65655E40, 0)
	state := &SessionState{
		Cols:        80,
		Rows:        24,
		CursorRow:   0,
		CursorCol:   5,
		IsAltScreen: false,
		SavedAt:     saved,
		VT:          []byte("AB"),
	}

	data, err := encodeState(state)
	assert.NilError(t, err)

	expected := []byte{
		'H', 'T', 'S', 'T', // magic
		1,          // version
		0x00, 0x50, // cols = 80
		0x00, 0x18, // rows = 24
		0, 0, 0, 0, // cursor_row = 0
		0, 0, 0, 5, // cursor_col = 5
		0,                                  // is_alt_screen = false
		0, 0, 0, 0, 0x65, 0x65, 0x5E, 0x40, // saved_at
		0, 0, 0, 2, // vt_data_length = 2
		'A', 'B', // vt_data
	}
	assert.DeepEqual(t, data, expected)
}

func TestCleanStaleTmp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)

	sessionDir := filepath.Join(dir, "hauntty", "sessions")
	assert.NilError(t, os.MkdirAll(sessionDir, 0o700))

	assert.NilError(t, os.WriteFile(filepath.Join(sessionDir, "foo.state.tmp"), []byte("stale"), 0o600))
	assert.NilError(t, os.WriteFile(filepath.Join(sessionDir, "bar.state"), []byte("keep"), 0o600))

	CleanStaleTmp()

	entries, err := os.ReadDir(sessionDir)
	assert.NilError(t, err)
	assert.Equal(t, len(entries), 1)
	assert.Equal(t, entries[0].Name(), "bar.state")
}

func TestLoadStateMissing(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	_, err := LoadState("nonexistent")
	assert.Assert(t, os.IsNotExist(err))
}
