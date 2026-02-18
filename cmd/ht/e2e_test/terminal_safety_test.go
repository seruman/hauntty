package e2e_test

import (
	"fmt"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/icmd"

	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/termtest"
	"code.selman.me/hauntty/libghostty"
)

func TestDetachFromAltScreen(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach alt-one\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Type("printf '\\033[?1049hALT-SCREEN\\n'\n")
	sh.WaitFor("ALT-SCREEN")

	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	sh.WaitFor("$")
	sh.Type("echo shell-usable\n")
	sh.WaitFor("shell-usable")
}

func TestReattachAfterAltScreenDetach(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach alt-two\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Type("export PS1='ALT2> '\n")
	sh.WaitFor("ALT2>")
	sh.Type("printf '\\033[?1049hALT-REATTACH\\n'\n")
	sh.WaitFor("ALT-REATTACH")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("$")

	sh.Type("$HT_BIN attach alt-two\n")
	sh.WaitFor("ALT2>")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
}

func TestDetachAltScreenPrimaryScreenContent(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach alt-three\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Type("echo primary-marker\n")
	sh.WaitFor("primary-marker")
	sh.Type("printf '\\033[?1049hALT\\n'\n")
	sh.WaitFor("ALT")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")

	result := e.run("dump", "alt-three", "--format", "plain")
	result.Assert(t, icmd.Success)
	assert.Assert(t, result.Stdout() != "")
}

func TestAltScreenDetachReattachCycles(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	for i := 0; i < 3; i++ {
		sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
		sh.WaitFor("$")
		name := fmt.Sprintf("alt-cycle-%d", i)
		sh.Type(fmt.Sprintf("$HT_BIN attach %s\n", name))
		sh.WaitFor("created session")
		sh.WaitFor("$")
		sh.Type(fmt.Sprintf("printf '\\033[?1049hCYCLE-%d\\n'\n", i))
		sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
		sh.WaitFor("detached")
		sh.Type(fmt.Sprintf("if [ -z \"$HAUNTTY_SESSION\" ]; then echo OUT-%d; else echo IN-%d; fi\n", i, i))
		sh.WaitFor(fmt.Sprintf("OUT-%d", i))
	}
}

func TestDetachHostCursor(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach cursor-session\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Type("printf '\\033[?25l'\n")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	sh.Type("echo cursor-ok\n")
	sh.WaitFor("cursor-ok")
}

func TestDetachHostMouseModes(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach mouse-session\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Type("printf '\\033[?1000h\\033[?1002h\\033[?1003h\\033[?1006h'\n")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	sh.Type("echo mouse-ok\n")
	sh.WaitFor("mouse-ok")
}

func TestDetachHostBracketedPaste(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach paste-session\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Type("printf '\\033[?2004h'\n")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	sh.Type("echo paste-ok\n")
	sh.WaitFor("paste-ok")
}

func TestDetachHostKeyboardModes(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach keymode-session\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Type("printf '\\033[>1u'\n")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	sh.Type("echo keyboard-ok\n")
	sh.WaitFor("keyboard-ok")
	sh.Key(libghostty.KeyUp, 0)
	sh.Key(libghostty.KeyEnter, 0)
	sh.WaitFor("keyboard-ok")
}

func TestDetachHostStyling(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach style-session\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Type("printf '\\033[7m\\033[1mstyle\\033[0m\\n'\n")
	sh.WaitFor("style")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	sh.Type("echo style-ok\n")
	sh.WaitFor("style-ok")
}

func TestDetachAltScreenHostUsability(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach usability-session\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Type("printf '\\033[?1049husability\\n'\n")
	sh.WaitFor("usability")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	sh.Type("echo usability-ok\n")
	sh.WaitFor("usability-ok")
}

func TestDetachAltScreenNoDestructiveClear(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("echo host-before\n")
	sh.WaitFor("host-before")
	sh.Type("$HT_BIN attach clear-session\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Type("printf '\\033[?1049hclear-test\\n'\n")
	sh.WaitFor("clear-test")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	sh.WaitFor("host-before")
}

func TestDetachHostModeLeakCycles(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	for i := 0; i < 5; i++ {
		sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
		sh.WaitFor("$")
		name := fmt.Sprintf("leak-cycle-%d", i)
		sh.Type(fmt.Sprintf("$HT_BIN attach %s\n", name))
		sh.WaitFor("created session")
		sh.WaitFor("$")
		sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
		sh.WaitFor("detached")
		sh.Type(fmt.Sprintf("if [ -z \"$HAUNTTY_SESSION\" ]; then echo OUT-LEAK-%d; else echo IN-LEAK-%d; fi\n", i, i))
		sh.WaitFor(fmt.Sprintf("OUT-LEAK-%d", i))
	}
}

func TestDetachFailurePathHostCleanup(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach fail-cleanup\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")

	kill := e.run("kill", "fail-cleanup")
	kill.Assert(t, icmd.Expected{ExitCode: 0, Out: "killed session \"fail-cleanup\"\n"})

	sh.WaitFor("$")
	sh.Type("echo failure-cleanup-ok\n")
	sh.WaitFor("failure-cleanup-ok")
}

func TestDetachAfterInteractiveWork(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach suspend-session\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Type("echo suspend-resume\n")
	sh.WaitFor("suspend-resume")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	sh.Type("echo suspend-resume-ok\n")
	sh.WaitFor("suspend-resume-ok")
}

func TestDetachHostResize(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"), termtest.WithSize(80, 24))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach resize-session\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	sh.Resize(120, 40)
	sh.Type("echo resize-ok\n")
	sh.WaitFor("resize-ok")
}
