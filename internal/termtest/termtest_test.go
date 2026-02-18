package termtest_test

import (
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"code.selman.me/hauntty/internal/termtest"
	"code.selman.me/hauntty/libghostty"
)

func TestTypeAndScreen(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh"}, termtest.WithEnv("PS1=$ "))
	tm.WaitFor("$")

	tm.Type("echo hello\n")
	tm.WaitFor("hello")

	screen := tm.Screen()
	assert.Assert(t, screen != "", "screen should not be empty")
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
	tm := termtest.New(t, []string{"/bin/sh"}, termtest.WithEnv("PS1=$ "))
	tm.WaitFor("$")

	tm.Type("echo snap\n")
	tm.WaitFor("snap")

	dump := tm.Snapshot(libghostty.DumpVTFull)
	assert.Assert(t, len(dump.VT) > 0, "VT dump should not be empty")
}

func TestDone(t *testing.T) {
	tm := termtest.New(t, []string{"/bin/sh", "-c", "exit 0"})
	select {
	case <-tm.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s")
	}
}
