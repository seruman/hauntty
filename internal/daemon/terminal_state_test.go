package daemon

import (
	"testing"
	"time"

	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
	"gotest.tools/v3/assert"
)

func TestDumpDeadTerminalStateReplaysVT(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	state := &sessionState{
		Cols:    20,
		Rows:    5,
		SavedAt: time.Unix(1700000000, 0),
		VT:      []byte("hello\r\nworld"),
	}

	data, err := dumpDeadTerminalState(rt, state, 0, protocol.DumpPlain)
	assert.NilError(t, err)
	assert.DeepEqual(t, data, []byte("hello\nworld"))
}

func TestRestoreTerminalStateResizesToRequestedSize(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	state := &sessionState{
		Cols:    20,
		Rows:    5,
		SavedAt: time.Unix(1700000000, 0),
		VT:      []byte("hello"),
	}

	term, err := restoreTerminalState(rt, state, termSize{cols: 10, rows: 3}, 0)
	assert.NilError(t, err)
	defer term.close()

	dump, err := term.dumpScreen(libghostty.DumpPlain)
	assert.NilError(t, err)
	assert.DeepEqual(t, dump, &libghostty.ScreenDump{
		Data:        []byte("hello"),
		CursorRow:   0,
		CursorCol:   5,
		IsAltScreen: false,
	})
}

func TestRestoreTerminalStateExitsAltScreen(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	state := &sessionState{
		Cols:        20,
		Rows:        5,
		IsAltScreen: true,
		SavedAt:     time.Unix(1700000000, 0),
		VT:          []byte("\x1b[?1049halt\x1b[?1049h"),
	}

	term, err := restoreTerminalState(rt, state, termSize{cols: 20, rows: 5}, 0)
	assert.NilError(t, err)
	defer term.close()

	dump, err := term.dumpScreen(libghostty.DumpVTFull)
	assert.NilError(t, err)
	assert.DeepEqual(t, dump, &libghostty.ScreenDump{
		Data:        []byte("\x1b[0m\x1b[1;1H"),
		CursorRow:   0,
		CursorCol:   0,
		IsAltScreen: false,
	})
}
