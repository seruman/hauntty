package libghostty_test

import (
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"

	"code.selman.me/hauntty/libghostty"
)

func newTerminal(t *testing.T, rt *libghostty.Runtime, cols, rows, scrollback uint32) *libghostty.Terminal {
	t.Helper()
	term, err := rt.NewTerminal(cols, rows, scrollback)
	assert.NilError(t, err)
	return term
}

func TestBasicFeedAndDump(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	term := newTerminal(t, rt, 80, 24, 1000)
	defer term.Close()

	err = term.Feed([]byte("Hello, World!\r\n"))
	assert.NilError(t, err)

	dump, err := term.DumpScreen(libghostty.DumpVTFull)
	assert.NilError(t, err)
	assert.DeepEqual(t, dump.Data, []byte("Hello, World!\x1b[0m\x1b[2;1H"))
}

func TestResize(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	term := newTerminal(t, rt, 80, 24, 1000)
	defer term.Close()

	err = term.Resize(120, 40)
	assert.NilError(t, err)

	err = term.Feed([]byte("after resize"))
	assert.NilError(t, err)

	dump, err := term.DumpScreen(libghostty.DumpVTFull)
	assert.NilError(t, err)
	assert.DeepEqual(t, dump.Data, []byte("after resize\x1b[0m\x1b[1;13H"))
}

func TestCursorPosition(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	term := newTerminal(t, rt, 80, 24, 1000)
	defer term.Close()

	dump, err := term.DumpScreen(libghostty.DumpVTFull)
	assert.NilError(t, err)
	assert.Equal(t, dump.CursorRow, uint32(0))
	assert.Equal(t, dump.CursorCol, uint32(0))

	err = term.Feed([]byte("ABCDE"))
	assert.NilError(t, err)
	dump, err = term.DumpScreen(libghostty.DumpVTFull)
	assert.NilError(t, err)
	assert.Equal(t, dump.CursorCol, uint32(5))
	assert.Equal(t, dump.CursorRow, uint32(0))
}

func TestAltScreen(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	term := newTerminal(t, rt, 80, 24, 1000)
	defer term.Close()

	dump, err := term.DumpScreen(libghostty.DumpVTFull)
	assert.NilError(t, err)
	assert.Equal(t, dump.IsAltScreen, false)

	err = term.Feed([]byte("\x1b[?1049h"))
	assert.NilError(t, err)
	dump, err = term.DumpScreen(libghostty.DumpVTFull)
	assert.NilError(t, err)
	assert.Equal(t, dump.IsAltScreen, true)

	err = term.Feed([]byte("\x1b[?1049l"))
	assert.NilError(t, err)
	dump, err = term.DumpScreen(libghostty.DumpVTFull)
	assert.NilError(t, err)
	assert.Equal(t, dump.IsAltScreen, false)
}

func TestMultipleTerminals(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	term1 := newTerminal(t, rt, 80, 24, 1000)
	defer term1.Close()

	term2 := newTerminal(t, rt, 80, 24, 1000)
	defer term2.Close()

	err = term1.Feed([]byte("terminal one"))
	assert.NilError(t, err)
	err = term2.Feed([]byte("terminal two"))
	assert.NilError(t, err)

	dump1, err := term1.DumpScreen(libghostty.DumpVTFull)
	assert.NilError(t, err)
	dump2, err := term2.DumpScreen(libghostty.DumpVTFull)
	assert.NilError(t, err)

	assert.DeepEqual(t, dump1.Data, []byte("terminal one\x1b[0m\x1b[1;13H"))
	assert.DeepEqual(t, dump2.Data, []byte("terminal two\x1b[0m\x1b[1;13H"))
}

func TestDumpUnwrap(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	// 20-col terminal: 30 chars will soft-wrap onto two lines.
	term := newTerminal(t, rt, 20, 24, 1000)
	defer term.Close()

	err = term.Feed([]byte("aaaaaaaaaaaaaaaaaaaabbbbbbbbbb"))
	assert.NilError(t, err)

	wrapped, err := term.DumpScreen(libghostty.DumpPlain)
	assert.NilError(t, err)
	assert.DeepEqual(t, wrapped.Data, []byte("aaaaaaaaaaaaaaaaaaaa\nbbbbbbbbbb"))

	unwrapped, err := term.DumpScreen(libghostty.DumpPlain | libghostty.DumpFlagUnwrap)
	assert.NilError(t, err)
	assert.DeepEqual(t, unwrapped.Data, []byte("aaaaaaaaaaaaaaaaaaaabbbbbbbbbb"))
}

func TestDumpScrollback(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	// 80x5 terminal: 10 lines will push lines 1-6 into scrollback.
	term := newTerminal(t, rt, 80, 5, 100)
	defer term.Close()

	for i := 1; i <= 10; i++ {
		err = term.Feed(fmt.Appendf(nil, "line %d\r\n", i))
		assert.NilError(t, err)
	}

	t.Run("visible only", func(t *testing.T) {
		dump, err := term.DumpScreen(libghostty.DumpPlain)
		assert.NilError(t, err)
		assert.DeepEqual(t, dump.Data, []byte("line 7\nline 8\nline 9\nline 10"))
	})

	t.Run("with scrollback", func(t *testing.T) {
		dump, err := term.DumpScreen(libghostty.DumpPlain | libghostty.DumpFlagScrollback)
		assert.NilError(t, err)
		assert.DeepEqual(t, dump.Data, []byte("line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10"))
	})
}

func TestDumpHTML(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	term := newTerminal(t, rt, 80, 24, 1000)
	defer term.Close()

	err = term.Feed([]byte("Hello"))
	assert.NilError(t, err)

	dump, err := term.DumpScreen(libghostty.DumpHTML)
	assert.NilError(t, err)
	golden.AssertBytes(t, dump.Data, "dump_html.golden")
}

func TestEncodeKey(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	term := newTerminal(t, rt, 80, 24, 1000)
	defer term.Close()

	t.Run("plain letter", func(t *testing.T) {
		data, err := term.EncodeKey(libghostty.KeyCode('a'), 0)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("a"))
	})

	t.Run("ctrl+c", func(t *testing.T) {
		data, err := term.EncodeKey(libghostty.KeyCode('c'), libghostty.ModCtrl)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x03"))
	})

	t.Run("enter", func(t *testing.T) {
		data, err := term.EncodeKey(libghostty.KeyEnter, 0)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\r"))
	})

	t.Run("escape", func(t *testing.T) {
		data, err := term.EncodeKey(libghostty.KeyEscape, 0)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x1b"))
	})

	t.Run("arrow up", func(t *testing.T) {
		data, err := term.EncodeKey(libghostty.KeyUp, 0)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x1b[A"))
	})

	t.Run("ctrl+shift+up", func(t *testing.T) {
		data, err := term.EncodeKey(libghostty.KeyUp, libghostty.ModCtrl|libghostty.ModShift)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x1b[1;6A"))
	})

	t.Run("alt+a", func(t *testing.T) {
		data, err := term.EncodeKey(libghostty.KeyCode('a'), libghostty.ModAlt)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x1ba"))
	})

	t.Run("f1", func(t *testing.T) {
		data, err := term.EncodeKey(libghostty.KeyF1, 0)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x1bOP"))
	})
}

func TestEncodeKeyKittyMode(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	term := newTerminal(t, rt, 80, 24, 1000)
	defer term.Close()

	// Push kitty keyboard mode (disambiguate).
	err = term.Feed([]byte("\x1b[>1u"))
	assert.NilError(t, err)

	t.Run("ctrl+c in kitty mode", func(t *testing.T) {
		data, err := term.EncodeKey(libghostty.KeyCode('c'), libghostty.ModCtrl)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\x1b[99;5u"))
	})

	t.Run("enter in kitty mode", func(t *testing.T) {
		data, err := term.EncodeKey(libghostty.KeyEnter, 0)
		assert.NilError(t, err)
		assert.DeepEqual(t, data, []byte("\r"))
	})
}

func TestGetCwd(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	term := newTerminal(t, rt, 80, 24, 1000)
	defer term.Close()

	pwd, ok, err := term.GetCwd()
	assert.NilError(t, err)
	assert.Equal(t, ok, false)
	assert.Equal(t, pwd, "")

	err = term.Feed([]byte("\x1b]7;file:///tmp/example\x1b\\"))
	assert.NilError(t, err)
	pwd, ok, err = term.GetCwd()
	assert.NilError(t, err)
	assert.Equal(t, ok, true)
	assert.Equal(t, pwd, "/tmp/example")

	err = term.Feed([]byte("\x1b]7;file:///home/user/src\x07"))
	assert.NilError(t, err)
	pwd, ok, err = term.GetCwd()
	assert.NilError(t, err)
	assert.Equal(t, ok, true)
	assert.Equal(t, pwd, "/home/user/src")

	err = term.Feed([]byte("\x1b]7;kitty-shell-cwd://myhost/var/log\x07"))
	assert.NilError(t, err)
	pwd, ok, err = term.GetCwd()
	assert.NilError(t, err)
	assert.Equal(t, ok, true)
	assert.Equal(t, pwd, "/var/log")
}

func TestReInit(t *testing.T) {
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	defer rt.Close()

	term := newTerminal(t, rt, 80, 24, 1000)
	err = term.Feed([]byte("first session"))
	assert.NilError(t, err)
	err = term.Close()
	assert.NilError(t, err)

	term2 := newTerminal(t, rt, 80, 24, 1000)
	defer term2.Close()

	dump, err := term2.DumpScreen(libghostty.DumpVTFull)
	assert.NilError(t, err)
	assert.DeepEqual(t, dump.Data, []byte("\x1b[0m\x1b[1;1H"))
}
