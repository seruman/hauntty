package wasm_test

import (
	"context"
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"

	"github.com/selman/hauntty/wasm"
)

func newTerminal(t *testing.T, ctx context.Context, rt *wasm.Runtime, cols, rows, scrollback uint32) *wasm.Terminal {
	t.Helper()
	term, err := rt.NewTerminal(ctx, cols, rows, scrollback)
	assert.NilError(t, err)
	return term
}

func TestBasicFeedAndDump(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx)
	assert.NilError(t, err)
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	err = term.Feed(ctx, []byte("Hello, World!\r\n"))
	assert.NilError(t, err)

	dump, err := term.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)
	assert.DeepEqual(t, dump.VT, []byte("Hello, World!\x1b[0m\x1b[2;1H"))
}

func TestResize(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx)
	assert.NilError(t, err)
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	err = term.Resize(ctx, 120, 40)
	assert.NilError(t, err)

	err = term.Feed(ctx, []byte("after resize"))
	assert.NilError(t, err)

	dump, err := term.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)
	assert.DeepEqual(t, dump.VT, []byte("after resize\x1b[0m\x1b[1;13H"))
}

func TestCursorPosition(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx)
	assert.NilError(t, err)
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	dump, err := term.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)
	assert.Equal(t, dump.CursorRow, uint32(0))
	assert.Equal(t, dump.CursorCol, uint32(0))

	err = term.Feed(ctx, []byte("ABCDE"))
	assert.NilError(t, err)
	dump, err = term.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)
	assert.Equal(t, dump.CursorCol, uint32(5))
	assert.Equal(t, dump.CursorRow, uint32(0))
}

func TestAltScreen(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx)
	assert.NilError(t, err)
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	dump, err := term.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)
	assert.Equal(t, dump.IsAltScreen, false)

	err = term.Feed(ctx, []byte("\x1b[?1049h"))
	assert.NilError(t, err)
	dump, err = term.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)
	assert.Equal(t, dump.IsAltScreen, true)

	err = term.Feed(ctx, []byte("\x1b[?1049l"))
	assert.NilError(t, err)
	dump, err = term.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)
	assert.Equal(t, dump.IsAltScreen, false)
}

func TestMultipleTerminals(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx)
	assert.NilError(t, err)
	defer rt.Close(ctx)

	term1 := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term1.Close(ctx)

	term2 := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term2.Close(ctx)

	err = term1.Feed(ctx, []byte("terminal one"))
	assert.NilError(t, err)
	err = term2.Feed(ctx, []byte("terminal two"))
	assert.NilError(t, err)

	dump1, err := term1.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)
	dump2, err := term2.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)

	assert.DeepEqual(t, dump1.VT, []byte("terminal one\x1b[0m\x1b[1;13H"))
	assert.DeepEqual(t, dump2.VT, []byte("terminal two\x1b[0m\x1b[1;13H"))
}

func TestDumpUnwrap(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx)
	assert.NilError(t, err)
	defer rt.Close(ctx)

	// 20-col terminal: 30 chars will soft-wrap onto two lines.
	term := newTerminal(t, ctx, rt, 20, 24, 1000)
	defer term.Close(ctx)

	err = term.Feed(ctx, []byte("aaaaaaaaaaaaaaaaaaaabbbbbbbbbb"))
	assert.NilError(t, err)

	wrapped, err := term.DumpScreen(ctx, wasm.DumpPlain)
	assert.NilError(t, err)
	assert.DeepEqual(t, wrapped.VT, []byte("aaaaaaaaaaaaaaaaaaaa\nbbbbbbbbbb"))

	unwrapped, err := term.DumpScreen(ctx, wasm.DumpPlain|wasm.DumpFlagUnwrap)
	assert.NilError(t, err)
	assert.DeepEqual(t, unwrapped.VT, []byte("aaaaaaaaaaaaaaaaaaaabbbbbbbbbb"))
}

func TestDumpScrollback(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx)
	assert.NilError(t, err)
	defer rt.Close(ctx)

	// 80x5 terminal: 10 lines will push lines 1-6 into scrollback.
	term := newTerminal(t, ctx, rt, 80, 5, 100)
	defer term.Close(ctx)

	for i := 1; i <= 10; i++ {
		err = term.Feed(ctx, fmt.Appendf(nil, "line %d\r\n", i))
		assert.NilError(t, err)
	}

	t.Run("visible only", func(t *testing.T) {
		dump, err := term.DumpScreen(ctx, wasm.DumpPlain)
		assert.NilError(t, err)
		assert.DeepEqual(t, dump.VT, []byte("line 7\nline 8\nline 9\nline 10"))
	})

	t.Run("with scrollback", func(t *testing.T) {
		dump, err := term.DumpScreen(ctx, wasm.DumpPlain|wasm.DumpFlagScrollback)
		assert.NilError(t, err)
		assert.DeepEqual(t, dump.VT, []byte("line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10"))
	})
}

func TestDumpHTML(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx)
	assert.NilError(t, err)
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	err = term.Feed(ctx, []byte("Hello"))
	assert.NilError(t, err)

	dump, err := term.DumpScreen(ctx, wasm.DumpHTML)
	assert.NilError(t, err)
	golden.AssertBytes(t, dump.VT, "dump_html.golden")
}

func TestEncodeKey(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx)
	assert.NilError(t, err)
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	t.Run("plain letter", func(t *testing.T) {
		data, err := term.EncodeKey(ctx, uint32('a'), 0)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("a"))
	})

	t.Run("ctrl+c", func(t *testing.T) {
		data, err := term.EncodeKey(ctx, uint32('c'), wasm.ModCtrl)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x03"))
	})

	t.Run("enter", func(t *testing.T) {
		data, err := term.EncodeKey(ctx, wasm.KeyEnter, 0)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\r"))
	})

	t.Run("escape", func(t *testing.T) {
		data, err := term.EncodeKey(ctx, wasm.KeyEscape, 0)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x1b"))
	})

	t.Run("arrow up", func(t *testing.T) {
		data, err := term.EncodeKey(ctx, wasm.KeyUp, 0)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x1b[A"))
	})

	t.Run("ctrl+shift+up", func(t *testing.T) {
		data, err := term.EncodeKey(ctx, wasm.KeyUp, wasm.ModCtrl|wasm.ModShift)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x1b[1;6A"))
	})

	t.Run("alt+a", func(t *testing.T) {
		data, err := term.EncodeKey(ctx, uint32('a'), wasm.ModAlt)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x1ba"))
	})

	t.Run("f1", func(t *testing.T) {
		data, err := term.EncodeKey(ctx, wasm.KeyF1, 0)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x1bOP"))
	})
}

func TestEncodeKeyKittyMode(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx)
	assert.NilError(t, err)
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	// Push kitty keyboard mode (disambiguate).
	err = term.Feed(ctx, []byte("\x1b[>1u"))
	assert.NilError(t, err)

	t.Run("ctrl+c in kitty mode", func(t *testing.T) {
		data, err := term.EncodeKey(ctx, uint32('c'), wasm.ModCtrl)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x1b[99;5u"))
	})

	t.Run("enter in kitty mode", func(t *testing.T) {
		data, err := term.EncodeKey(ctx, wasm.KeyEnter, 0)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\r"))
	})
}

func TestGetPwd(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx)
	assert.NilError(t, err)
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	assert.Equal(t, term.GetPwd(ctx), "")

	err = term.Feed(ctx, []byte("\x1b]7;file:///tmp/example\x1b\\"))
	assert.NilError(t, err)
	assert.Equal(t, term.GetPwd(ctx), "/tmp/example")

	err = term.Feed(ctx, []byte("\x1b]7;file:///home/user/src\x07"))
	assert.NilError(t, err)
	assert.Equal(t, term.GetPwd(ctx), "/home/user/src")

	err = term.Feed(ctx, []byte("\x1b]7;kitty-shell-cwd://myhost/var/log\x07"))
	assert.NilError(t, err)
	assert.Equal(t, term.GetPwd(ctx), "/var/log")
}

func TestReInit(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx)
	assert.NilError(t, err)
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	err = term.Feed(ctx, []byte("first session"))
	assert.NilError(t, err)
	err = term.Close(ctx)
	assert.NilError(t, err)

	term2 := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term2.Close(ctx)

	dump, err := term2.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)
	assert.DeepEqual(t, dump.VT, []byte("\x1b[0m\x1b[1;1H"))
}
