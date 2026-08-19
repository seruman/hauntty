package daemon

import (
	"testing"
	"time"

	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
	"gotest.tools/v3/assert"
)

func TestDumpDeadTerminalStateRestoresSnapshot(t *testing.T) {
	state := snapshotSessionState(t, 20, 5, time.Unix(1700000000, 0), []byte("hello\r\nworld"))

	data, err := dumpDeadTerminalState(state, 0, protocol.DumpPlain)
	assert.NilError(t, err)
	assert.DeepEqual(t, data, []byte("hello\nworld"))
}

func TestRestoreTerminalStateResizesToRequestedSize(t *testing.T) {
	state := snapshotSessionState(t, 20, 5, time.Unix(1700000000, 0), []byte("abcdefghijklmno"))

	term, err := restoreTerminalState(state, termSize{cols: 10, rows: 3}, 0)
	assert.NilError(t, err)
	defer term.close()

	dump, err := term.dumpScreen(terminalFormat{emit: libghostty.FormatterFormatPlain})
	assert.NilError(t, err)
	assert.DeepEqual(t, dump, &screenDump{
		Data:        []byte("abcdefghij\nklmno"),
		CursorRow:   1,
		CursorCol:   5,
		IsAltScreen: false,
	})
}

func TestRestoreTerminalStateCancelsContinuation(t *testing.T) {
	state := snapshotSessionState(t, 20, 5, time.Unix(1700000000, 0), []byte("\x1b[31"))
	term, err := restoreTerminalState(state, termSize{cols: 20, rows: 5}, 0)
	assert.NilError(t, err)
	defer term.close()

	term.feed([]byte("mred"))
	dump, err := term.dumpScreen(terminalFormat{emit: libghostty.FormatterFormatPlain})
	assert.NilError(t, err)
	assert.DeepEqual(t, dump, &screenDump{
		Data:        []byte("mred"),
		CursorRow:   0,
		CursorCol:   4,
		IsAltScreen: false,
	})

	_, err = term.snapshot()
	assert.NilError(t, err)
}

func TestTerminalKeyEventUsesShiftedText(t *testing.T) {
	key, codepoint, text, ok := terminalKeyEvent('1', protocol.KeyMods(libghostty.ModShift))
	assert.Equal(t, ok, true)
	assert.Equal(t, key, libghostty.KeyDigit1)
	assert.Equal(t, codepoint, uint32('1'))
	assert.DeepEqual(t, text, []byte("!"))
}

func TestRestoreTerminalStateExitsAltScreen(t *testing.T) {
	state := snapshotSessionState(t, 20, 5, time.Unix(1700000000, 0), []byte("\x1b[?1049halt\x1b[?1049h"))

	term, err := restoreTerminalState(state, termSize{cols: 20, rows: 5}, 0)
	assert.NilError(t, err)
	defer term.close()

	dump, err := term.dumpScreen(terminalFormatVTFull)
	assert.NilError(t, err)
	assert.DeepEqual(t, dump, &screenDump{
		Data:        []byte("\x1b[1;1H\x1b[0m"),
		CursorRow:   0,
		CursorCol:   0,
		IsAltScreen: false,
	})
}
