package wasm_test

import (
	"context"
	"os"
	"testing"

	"gotest.tools/v3/assert"

	"github.com/selman/hauntty/wasm"
)

func loadWasm(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../vt/zig-out/bin/hauntty-vt.wasm")
	assert.NilError(t, err)
	return b
}

func newTerminal(t *testing.T, ctx context.Context, rt *wasm.Runtime, cols, rows, scrollback uint32) *wasm.Terminal {
	t.Helper()
	term, err := rt.NewTerminal(ctx, cols, rows, scrollback)
	assert.NilError(t, err)
	return term
}

func TestBasicFeedAndDump(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
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
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
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
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
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

func TestReset(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
	assert.NilError(t, err)
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	err = term.Feed(ctx, []byte("some content"))
	assert.NilError(t, err)
	err = term.Reset(ctx)
	assert.NilError(t, err)

	dump, err := term.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)
	assert.Equal(t, dump.CursorRow, uint32(0))
	assert.Equal(t, dump.CursorCol, uint32(0))
}

func TestAltScreen(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
	assert.NilError(t, err)
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	dump, err := term.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)
	assert.Assert(t, !dump.IsAltScreen)

	err = term.Feed(ctx, []byte("\x1b[?1049h"))
	assert.NilError(t, err)
	dump, err = term.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)
	assert.Assert(t, dump.IsAltScreen)

	err = term.Feed(ctx, []byte("\x1b[?1049l"))
	assert.NilError(t, err)
	dump, err = term.DumpScreen(ctx, wasm.DumpVTFull)
	assert.NilError(t, err)
	assert.Assert(t, !dump.IsAltScreen)
}

func TestMultipleTerminals(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
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

func TestReInit(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
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
