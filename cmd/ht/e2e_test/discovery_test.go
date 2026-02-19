package e2e_test

import (
	"os"
	"testing"

	"gotest.tools/v3/icmd"

	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/termtest"
	"code.selman.me/hauntty/libghostty"
)

func TestListSessionsFiltering(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	alive := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	e.waitHostPrompt(alive)
	alive.Type("$HT_BIN attach alive\n")
	alive.WaitFor("created session")
	e.waitAttachedPrompt(alive)
	alive.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	alive.WaitFor("detached")

	oneshootShell := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	e.waitHostPrompt(oneshootShell)
	oneshootShell.Type("$HT_BIN attach dead -- /bin/sh -c \"exit 0\"\n")
	oneshootShell.WaitFor("created session")

	list := e.run("list")
	list.Assert(t, icmd.Expected{ExitCode: 0})

	listAll := e.run("list", "-a")
	listAll.Assert(t, icmd.Expected{ExitCode: 0})
}

func TestStatusSessionContext(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	outside := e.run("status")
	outside.Assert(t, icmd.Expected{ExitCode: 0})

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	e.waitHostPrompt(sh)
	sh.Type("$HT_BIN attach status-session\n")
	sh.WaitFor("created session")
	e.waitAttachedPrompt(sh)
	sh.Type("$HT_BIN status\n")
	sh.WaitFor("session:  status-session")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
}

func TestDetachOutsideSession(t *testing.T) {
	e := setup(t, nil)

	result := e.run("detach")
	result.Assert(t, icmd.Expected{
		ExitCode: 1,
		Err:      "ht: error: not inside a hauntty session\n",
	})
}

func TestAttachInsideSession(t *testing.T) {
	e := setup(t, nil)

	cmd := icmd.Command(htBin, "attach", "nested")
	result := icmd.RunCmd(cmd, icmd.WithEnv(append(os.Environ(), append(e.env(), "HAUNTTY_SESSION=outer")...)...))
	result.Assert(t, icmd.Expected{
		ExitCode: 1,
		Err:      "ht: error: already inside session \"outer\", nested sessions are not supported\n",
	})
}

func TestCommandsWithoutDaemon(t *testing.T) {
	e := setup(t, nil)

	errText := "ht: error: connect to daemon: dial unix " + e.sock + ": connect: no such file or directory\n"

	list := e.run("list")
	list.Assert(t, icmd.Expected{ExitCode: 1, Err: errText})

	status := e.run("status")
	status.Assert(t, icmd.Expected{ExitCode: 1, Err: errText})
}
