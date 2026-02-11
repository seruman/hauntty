package wasm_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/selman/hauntty/wasm"
)

func loadWasm(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../vt/zig-out/bin/hauntty-vt.wasm")
	if err != nil {
		t.Fatalf("failed to read wasm: %v", err)
	}
	return b
}

func newTerminal(t *testing.T, ctx context.Context, rt *wasm.Runtime, cols, rows, scrollback uint32) *wasm.Terminal {
	t.Helper()
	term, err := rt.NewTerminal(ctx, cols, rows, scrollback)
	if err != nil {
		t.Fatalf("NewTerminal(%d,%d,%d): %v", cols, rows, scrollback, err)
	}
	return term
}

func TestBasicFeedAndDump(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	if err := term.Feed(ctx, []byte("Hello, World!\r\n")); err != nil {
		t.Fatalf("Feed: %v", err)
	}

	dump, err := term.DumpScreen(ctx)
	if err != nil {
		t.Fatalf("DumpScreen: %v", err)
	}
	if !strings.Contains(string(dump.VT), "Hello, World!") {
		t.Errorf("dump does not contain expected text:\n%s", dump.VT)
	}
}

func TestResize(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	if err := term.Resize(ctx, 120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	// Feed after resize and verify it still works.
	if err := term.Feed(ctx, []byte("after resize")); err != nil {
		t.Fatalf("Feed after resize: %v", err)
	}

	dump, err := term.DumpScreen(ctx)
	if err != nil {
		t.Fatalf("DumpScreen after resize: %v", err)
	}
	if !strings.Contains(string(dump.VT), "after resize") {
		t.Errorf("dump does not contain expected text after resize:\n%s", dump.VT)
	}
}

func TestCursorPosition(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	// Fresh terminal: cursor at (0, 0).
	dump, err := term.DumpScreen(ctx)
	if err != nil {
		t.Fatalf("DumpScreen: %v", err)
	}
	if dump.CursorRow != 0 || dump.CursorCol != 0 {
		t.Errorf("initial cursor: got (%d,%d), want (0,0)", dump.CursorRow, dump.CursorCol)
	}

	// Feed some text — cursor should move.
	if err := term.Feed(ctx, []byte("ABCDE")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	dump, err = term.DumpScreen(ctx)
	if err != nil {
		t.Fatalf("DumpScreen: %v", err)
	}
	if dump.CursorCol != 5 {
		t.Errorf("cursor col after ABCDE: got %d, want 5", dump.CursorCol)
	}
	if dump.CursorRow != 0 {
		t.Errorf("cursor row after ABCDE: got %d, want 0", dump.CursorRow)
	}
}

func TestReset(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	if err := term.Feed(ctx, []byte("some content")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if err := term.Reset(ctx); err != nil {
		t.Fatalf("Reset: %v", err)
	}

	dump, err := term.DumpScreen(ctx)
	if err != nil {
		t.Fatalf("DumpScreen: %v", err)
	}
	if dump.CursorRow != 0 || dump.CursorCol != 0 {
		t.Errorf("cursor after reset: got (%d,%d), want (0,0)", dump.CursorRow, dump.CursorCol)
	}
}

func TestAltScreen(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term.Close(ctx)

	// Initially not on alt screen.
	dump, err := term.DumpScreen(ctx)
	if err != nil {
		t.Fatalf("DumpScreen: %v", err)
	}
	if dump.IsAltScreen {
		t.Error("expected main screen initially")
	}

	// Enter alt screen.
	if err := term.Feed(ctx, []byte("\x1b[?1049h")); err != nil {
		t.Fatalf("Feed alt screen enter: %v", err)
	}
	dump, err = term.DumpScreen(ctx)
	if err != nil {
		t.Fatalf("DumpScreen: %v", err)
	}
	if !dump.IsAltScreen {
		t.Error("expected alt screen after \\x1b[?1049h")
	}

	// Leave alt screen.
	if err := term.Feed(ctx, []byte("\x1b[?1049l")); err != nil {
		t.Fatalf("Feed alt screen leave: %v", err)
	}
	dump, err = term.DumpScreen(ctx)
	if err != nil {
		t.Fatalf("DumpScreen: %v", err)
	}
	if dump.IsAltScreen {
		t.Error("expected main screen after \\x1b[?1049l")
	}
}

func TestMultipleTerminals(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	term1 := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term1.Close(ctx)

	term2 := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term2.Close(ctx)

	// Feed different content to each.
	if err := term1.Feed(ctx, []byte("terminal one")); err != nil {
		t.Fatalf("Feed term1: %v", err)
	}
	if err := term2.Feed(ctx, []byte("terminal two")); err != nil {
		t.Fatalf("Feed term2: %v", err)
	}

	dump1, err := term1.DumpScreen(ctx)
	if err != nil {
		t.Fatalf("DumpScreen term1: %v", err)
	}
	dump2, err := term2.DumpScreen(ctx)
	if err != nil {
		t.Fatalf("DumpScreen term2: %v", err)
	}

	if !strings.Contains(string(dump1.VT), "terminal one") {
		t.Errorf("term1 dump missing expected text")
	}
	if !strings.Contains(string(dump2.VT), "terminal two") {
		t.Errorf("term2 dump missing expected text")
	}
	if strings.Contains(string(dump1.VT), "terminal two") {
		t.Errorf("term1 dump contains term2 text — instances not isolated")
	}
}

func TestReInit(t *testing.T) {
	ctx := context.Background()
	rt, err := wasm.NewRuntime(ctx, loadWasm(t))
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	defer rt.Close(ctx)

	// Create, use, and close a terminal.
	term := newTerminal(t, ctx, rt, 80, 24, 1000)
	if err := term.Feed(ctx, []byte("first session")); err != nil {
		t.Fatalf("Feed: %v", err)
	}
	if err := term.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Create a new terminal from the same runtime.
	term2 := newTerminal(t, ctx, rt, 80, 24, 1000)
	defer term2.Close(ctx)

	dump, err := term2.DumpScreen(ctx)
	if err != nil {
		t.Fatalf("DumpScreen: %v", err)
	}
	if strings.Contains(string(dump.VT), "first session") {
		t.Error("new terminal should not contain previous session data")
	}
}
