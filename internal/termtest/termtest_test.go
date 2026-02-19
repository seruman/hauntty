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

	screen := tm.Screen()
	assert.Assert(t, screen != "", "screen should not be empty")

	vt := tm.ScreenVT()
	assert.Assert(t, len(vt) > 0, "screen vt should not be empty")

	assert.Assert(t, tm.PromptVisible())
	assert.Assert(t, tm.PromptVisibleMatch(func(line string) bool { return line == "$" || line == "$ " }))
}

func TestKey(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh"}, termtest.WithEnv("PS1=$ "))
	tm.WaitFor("$")

	tm.Type("echo test")
	tm.Key(libghostty.KeyEnter, 0)
	tm.WaitFor("test")
}

func TestResize(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh"},
		termtest.WithEnv("PS1=$ "),
		termtest.WithSize(40, 10),
	)
	tm.WaitFor("$")

	tm.Resize(120, 40)
	// After resize, shell should still be responsive.
	tm.Type("echo resized\n")
	tm.WaitFor("resized")
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

	dump := tm.Snapshot(libghostty.DumpVTFull)
	assert.Assert(t, len(dump.VT) > 0, "VT dump should not be empty")
}

func TestWaitRowContains(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh"}, termtest.WithEnv("PS1=$ "))
	tm.WaitFor("$")

	dump := tm.Snapshot(libghostty.DumpPlain)
	row := int(dump.CursorRow)
	tm.WaitRowContains(row, "$")
	assert.Assert(t, tm.RowContains(row, "$"))
	tm.WaitCursorRowContains("$")
	assert.Assert(t, tm.CursorRowContains("$"))
	tm.WaitPrompt()
	tm.WaitPromptMatch(func(line string) bool { return line == "$" || line == "$ " })
}

func TestWaitStable(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh"}, termtest.WithEnv("PS1=$ "))
	tm.WaitFor("$")
	tm.WaitStable(150 * time.Millisecond)
}

func TestDone(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh", "-c", "exit 0"})
	select {
	case <-tm.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s")
	}
}
