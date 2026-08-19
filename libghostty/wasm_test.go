package libghostty_test

import (
	"errors"
	"testing"

	"code.selman.me/hauntty/libghostty"
	"gotest.tools/v3/assert"
)

func newTerminal(t *testing.T, cols, rows uint16) *libghostty.Terminal {
	t.Helper()

	term, err := libghostty.NewTerminal(
		libghostty.WithSize(cols, rows),
		libghostty.WithMaxScrollbackLines(1000),
		libghostty.WithContinuationMaxBytes(65<<20),
	)
	assert.NilError(t, err)

	t.Cleanup(term.Close)

	return term
}

func formatTerminal(t *testing.T, term *libghostty.Terminal, opts ...libghostty.FormatterOption) []byte {
	t.Helper()

	formatter, err := libghostty.NewFormatter(term, opts...)
	assert.NilError(t, err)
	defer formatter.Close()

	data, err := formatter.Format()
	assert.NilError(t, err)

	return data
}

func TestTerminal(t *testing.T) {
	term := newTerminal(t, 80, 24)

	written, err := term.Write([]byte("hello\r\nworld"))
	assert.NilError(t, err)
	assert.Equal(t, written, len("hello\r\nworld"))

	formatted := formatTerminal(
		t,
		term,
		libghostty.WithFormatterFormat(libghostty.FormatterFormatPlain),
		libghostty.WithFormatterTrim(true),
	)
	assert.DeepEqual(t, formatted, []byte("hello\nworld"))

	cols, err := term.Cols()
	assert.NilError(t, err)
	assert.Equal(t, cols, uint16(80))

	rows, err := term.Rows()
	assert.NilError(t, err)
	assert.Equal(t, rows, uint16(24))

	cursorX, err := term.CursorX()
	assert.NilError(t, err)
	assert.Equal(t, cursorX, uint16(5))

	cursorY, err := term.CursorY()
	assert.NilError(t, err)
	assert.Equal(t, cursorY, uint16(1))

	active, err := term.ActiveScreen()
	assert.NilError(t, err)
	assert.Equal(t, active, libghostty.ScreenPrimary)

	ground, err := term.VTGround()
	assert.NilError(t, err)
	assert.Equal(t, ground, true)

	assert.NilError(t, term.Resize(120, 40, 8, 16))

	cols, err = term.Cols()
	assert.NilError(t, err)
	assert.Equal(t, cols, uint16(120))

	rows, err = term.Rows()
	assert.NilError(t, err)
	assert.Equal(t, rows, uint16(40))

	term.Reset()

	formatted = formatTerminal(
		t,
		term,
		libghostty.WithFormatterFormat(libghostty.FormatterFormatPlain),
		libghostty.WithFormatterTrim(true),
	)
	assert.DeepEqual(t, formatted, []byte{})
}

func TestTerminalRejectsInvalidSize(t *testing.T) {
	term, err := libghostty.NewTerminal(libghostty.WithSize(0, 24))
	assert.Equal(t, term == nil, true)

	ghosttyErr, ok := errors.AsType[*libghostty.Error](err)
	assert.Equal(t, ok, true)
	assert.Equal(t, ghosttyErr.Result, libghostty.ResultInvalidValue)
}

func TestTerminalRejectsOversizedWASMValue(t *testing.T) {
	if uint64(^uint(0)) == uint64(^uint32(0)) {
		t.Skip("uint is 32 bits")
	}

	term := newTerminal(t, 80, 24)
	value := uint64(1) << 32

	err := term.SetContinuationMaxBytes(uint(value))
	ghosttyErr, ok := errors.AsType[*libghostty.Error](err)
	assert.Equal(t, ok, true)
	assert.Equal(t, ghosttyErr.Result, libghostty.ResultLimitExceeded)
}

func TestTerminalPwd(t *testing.T) {
	term := newTerminal(t, 80, 24)
	term.VTWrite([]byte("\x1b]7;file://localhost/tmp/example\x07"))

	pwd, err := term.Pwd()
	assert.NilError(t, err)
	assert.Equal(t, pwd, "file://localhost/tmp/example")
}

func TestFormatterSelection(t *testing.T) {
	term := newTerminal(t, 5, 2)
	term.VTWrite([]byte("one\r\ntwo"))

	start, err := term.GridRef(libghostty.Point{Tag: libghostty.PointTagActive, X: 0, Y: 1})
	assert.NilError(t, err)

	end, err := term.GridRef(libghostty.Point{Tag: libghostty.PointTagActive, X: 4, Y: 1})
	assert.NilError(t, err)

	formatted := formatTerminal(
		t,
		term,
		libghostty.WithFormatterFormat(libghostty.FormatterFormatPlain),
		libghostty.WithFormatterTrim(true),
		libghostty.WithFormatterSelection(&libghostty.Selection{Start: *start, End: *end}),
	)
	assert.DeepEqual(t, formatted, []byte("two"))
}

func TestFormatterBuf(t *testing.T) {
	term := newTerminal(t, 80, 24)
	term.VTWrite([]byte("hello"))

	formatter, err := libghostty.NewFormatter(
		term,
		libghostty.WithFormatterTrim(true),
		libghostty.WithFormatterExtraTabstops(false),
	)
	assert.NilError(t, err)
	defer formatter.Close()

	required, err := formatter.FormatBuf(nil)
	ghosttyErr, ok := errors.AsType[*libghostty.Error](err)
	assert.Equal(t, ok, true)
	assert.Equal(t, ghosttyErr.Result, libghostty.ResultOutOfSpace)
	assert.Equal(t, required, 5)

	buf := make([]byte, required)
	written, err := formatter.FormatBuf(buf)
	assert.NilError(t, err)
	assert.Equal(t, written, 5)
	assert.DeepEqual(t, buf, []byte("hello"))

	formatted, err := formatter.FormatString()
	assert.NilError(t, err)
	assert.Equal(t, formatted, "hello")
}

func TestKeyEncoder(t *testing.T) {
	term := newTerminal(t, 80, 24)

	encoder, err := libghostty.NewKeyEncoder()
	assert.NilError(t, err)
	defer encoder.Close()

	event, err := libghostty.NewKeyEvent()
	assert.NilError(t, err)
	defer event.Close()

	encoder.SetOptBool(libghostty.KeyEncoderOptCursorKeyApplication, false)
	encoder.SetOptKittyFlags(libghostty.KittyKeyDisabled)
	encoder.SetOptOptionAsAlt(libghostty.OptionAsAltFalse)

	event.SetAction(libghostty.KeyActionPress)
	event.SetKey(libghostty.KeyC)
	event.SetMods(libghostty.ModCtrl)
	event.SetUnshiftedCodepoint('c')

	assert.Equal(t, event.Action(), libghostty.KeyActionPress)
	assert.Equal(t, event.Key(), libghostty.KeyC)
	assert.Equal(t, event.Mods(), libghostty.ModCtrl)
	assert.Equal(t, event.ConsumedMods(), libghostty.Mods(0))
	assert.Equal(t, event.Composing(), false)
	assert.Equal(t, event.UnshiftedCodepoint(), 'c')
	assert.Equal(t, event.UTF8(), "")

	encoder.SetOptFromTerminal(term)
	data, err := encoder.Encode(event)
	assert.NilError(t, err)
	assert.DeepEqual(t, data, []byte{3})

	term.VTWrite([]byte("\x1b[>1u"))
	event.SetKey(libghostty.KeyArrowUp)
	event.SetMods(libghostty.ModCtrl | libghostty.ModShift)
	event.SetUnshiftedCodepoint(0)
	encoder.SetOptFromTerminal(term)

	data, err = encoder.Encode(event)
	assert.NilError(t, err)
	assert.DeepEqual(t, data, []byte("\x1b[1;6A"))
}

func TestSnapshotRetainsUnfinishedContinuation(t *testing.T) {
	term := newTerminal(t, 80, 24)
	term.VTWrite([]byte("before\x1b[31"))

	ground, err := term.VTGround()
	assert.NilError(t, err)
	assert.Equal(t, ground, false)

	snapshot, err := term.Snapshot()
	assert.NilError(t, err)

	decoder, err := libghostty.NewSnapshotDecoderBytes(snapshot)
	assert.NilError(t, err)
	defer decoder.Close()

	assert.NilError(t, decoder.SetMaxContinuationBytes(65<<20))
	assert.NilError(t, decoder.SetRetainContinuation(true))

	restored, err := decoder.Decode()
	assert.NilError(t, err)
	defer restored.Close()

	resnapshot, err := restored.Snapshot()
	assert.NilError(t, err)
	assert.DeepEqual(t, resnapshot, snapshot)

	copyDecoder, err := libghostty.NewSnapshotDecoderBytesCopy(snapshot)
	assert.NilError(t, err)

	copyRestored, err := copyDecoder.Decode()
	assert.NilError(t, err)

	copyRestored.Close()
	copyDecoder.Close()

	restored.VTWrite([]byte("mred"))

	formatted := formatTerminal(
		t,
		restored,
		libghostty.WithFormatterFormat(libghostty.FormatterFormatPlain),
		libghostty.WithFormatterTrim(true),
	)
	assert.DeepEqual(t, formatted, []byte("beforered"))
}
