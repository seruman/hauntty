package termtest_test

import (
	"os"
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"code.selman.me/hauntty/internal/termtest"
	"code.selman.me/hauntty/libghostty"
)

func TestTypeAndScreen(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh"},
		termtest.WithEnv("PS1=$ "),
		termtest.WithTimeout(2*time.Second),
	)
	tm.WaitFor("$", termtest.WaitInterval(20*time.Millisecond))

	tm.Type("echo hello\n")
	tm.WaitFor("hello")

	assert.Equal(t, tm.Screen(), "$ echo hello\nhello\n$")
	assert.DeepEqual(t, tm.ScreenVT(), []byte("$ echo hello\r\nhello\r\n$ \x1b[0m\x1b[3;3H"))
	assert.Equal(t, tm.PromptVisible(), true)
	assert.Equal(t, tm.PromptVisibleMatch(func(line string) bool { return line == "$" || line == "$ " }), true)
}

func TestKey(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh"}, termtest.WithEnv("PS1=$ "))
	tm.WaitFor("$")

	tm.Type("echo test")
	tm.Key(libghostty.KeyEnter, 0)
	tm.WaitFor("test")

	assert.Equal(t, tm.Screen(), "$ echo test\ntest\n$")
}

func TestResize(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh"},
		termtest.WithEnv("PS1=$ "),
		termtest.WithSize(40, 10),
	)
	tm.WaitFor("$")

	tm.Resize(120, 40)
	tm.Type("echo resized\n")
	tm.WaitFor("resized")

	assert.Equal(t, tm.Screen(), "$ echo resized\nresized\n$")
}

func TestSnapshot(t *testing.T) {
	dir := t.TempDir()
	marker := dir + "/marker"
	assert.NilError(t, os.WriteFile(marker, []byte("x"), 0o644))

	tm := termtest.New(t, []string{"/bin/sh"},
		termtest.WithEnv("PS1=$ "),
		termtest.WithDir(dir),
		termtest.WithScrollback(200),
	)
	tm.WaitFor("$")

	tm.Type("ls marker\n")
	tm.WaitFor("marker")

	tm.Type("echo snap\n")
	tm.WaitFor("snap")

	got := tm.Snapshot(libghostty.DumpVTFull)
	assert.DeepEqual(t, got, &libghostty.ScreenDump{
		Data:        []byte("$ ls marker\r\nmarker\r\n$ echo snap\r\nsnap\r\n$ \x1b[0m\x1b[5;3H"),
		CursorRow:   4,
		CursorCol:   2,
		IsAltScreen: false,
	})
}

func TestWaitRowContains(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh"}, termtest.WithEnv("PS1=$ "))
	tm.WaitFor("$")

	dump := tm.Snapshot(libghostty.DumpPlain)
	assert.DeepEqual(t, dump, &libghostty.ScreenDump{
		Data:        []byte("$"),
		CursorRow:   0,
		CursorCol:   2,
		IsAltScreen: false,
	})

	row := int(dump.CursorRow)
	tm.WaitRowContains(row, "$")
	assert.Equal(t, tm.RowContains(row, "$"), true)
	tm.WaitCursorRowContains("$")
	assert.Equal(t, tm.CursorRowContains("$"), true)
	tm.WaitPrompt()
	tm.WaitPromptMatch(func(line string) bool { return line == "$" || line == "$ " })
}

func TestWaitStable(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh"}, termtest.WithEnv("PS1=$ "))
	tm.WaitFor("$")
	tm.WaitStable(150 * time.Millisecond)
	assert.Equal(t, tm.Screen(), "$")
}

func TestDone(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh", "-c", "exit 0"})
	select {
	case <-tm.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s")
	}
}
