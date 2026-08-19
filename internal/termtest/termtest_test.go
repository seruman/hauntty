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
	tm := termtest.New(
		t, []string{"/bin/sh"},
		termtest.WithEnv("PS1=$ "),
		termtest.WithTimeout(2*time.Second),
	)
	tm.WaitFor("$", termtest.WaitInterval(20*time.Millisecond))

	tm.Type("echo hello\n")
	tm.WaitFor("hello")

	screen := tm.Screen()
	assert.Equal(t, screen, "$ echo hello\nhello\n$")

	screenVT := tm.ScreenVT()
	assert.DeepEqual(t, screenVT, []byte("$ echo hello\r\nhello\r\n$ \x1b[3;3H\x1b[0m"))

	promptVisible := tm.PromptVisible()
	assert.Equal(t, promptVisible, true)

	promptMatches := tm.PromptVisibleMatch(func(line string) bool {
		return line == "$" || line == "$ "
	})
	assert.Equal(t, promptMatches, true)
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
	tm := termtest.New(
		t, []string{"/bin/sh"},
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

	tm := termtest.New(
		t, []string{"/bin/sh"},
		termtest.WithEnv("PS1=$ "),
		termtest.WithDir(dir),
		termtest.WithScrollback(200),
	)
	tm.WaitFor("$")

	tm.Type("ls marker\n")
	tm.WaitFor("marker")

	tm.Type("echo snap\n")
	tm.WaitFor("snap")

	got := tm.Snapshot(termtest.DumpVTFull)
	assert.DeepEqual(t, got, &termtest.ScreenDump{
		Data:        []byte("$ ls marker\r\nmarker\r\n$ echo snap\r\nsnap\r\n$ \x1b[5;3H\x1b[0m"),
		CursorRow:   4,
		CursorCol:   2,
		IsAltScreen: false,
	})
}

func TestWaitRowContains(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh"}, termtest.WithEnv("PS1=$ "))
	tm.WaitFor("$")

	dump := tm.Snapshot(termtest.DumpPlain)
	assert.DeepEqual(t, dump, &termtest.ScreenDump{
		Data:        []byte("$"),
		CursorRow:   0,
		CursorCol:   2,
		IsAltScreen: false,
	})

	row := int(dump.CursorRow)
	tm.WaitRowContains(row, "$")

	rowContains := tm.RowContains(row, "$")
	assert.Equal(t, rowContains, true)

	tm.WaitCursorRowContains("$")

	cursorRowContains := tm.CursorRowContains("$")
	assert.Equal(t, cursorRowContains, true)

	tm.WaitPrompt()
	tm.WaitPromptMatch(func(line string) bool { return line == "$" || line == "$ " })
}

func TestWaitStable(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh"}, termtest.WithEnv("PS1=$ "))
	tm.WaitFor("$")
	tm.WaitStable(150 * time.Millisecond)

	screen := tm.Screen()
	assert.Equal(t, screen, "$")
}

func TestDone(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh", "-c", "exit 0"})

	select {
	case <-tm.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s")
	}
}
