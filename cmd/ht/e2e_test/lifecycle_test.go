package e2e_test

import (
	"testing"
	"time"

	"gotest.tools/v3/icmd"

	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/termtest"
	"code.selman.me/hauntty/libghostty"
)

func TestAttachStartsDaemonWhenNeeded(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	cfg.Daemon.AutoExit = true
	e := setup(t, cfg)

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")

	sh.Type("$HT_BIN attach auto-start\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")

	sh.Type("echo daemon-started\n")
	sh.WaitFor("daemon-started")

	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	sh.WaitFor("$")

	kill := e.run("kill", "auto-start")
	kill.Assert(t, icmd.Expected{ExitCode: 0, Out: "killed session \"auto-start\"\n"})
}

func TestAttachInteractDetachList(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")

	sh.Type("$HT_BIN attach test-session\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")

	sh.Type("echo hello-from-hauntty\n")
	sh.WaitFor("hello-from-hauntty")

	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	sh.WaitFor("$")

	sh.Type("$HT_BIN list\n")
	sh.WaitFor("test-session")
}

func TestReattachSessionContinuity(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")

	sh.Type("$HT_BIN attach continuity\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Type("export PS1='IN> '\n")
	sh.WaitFor("IN>")

	sh.Type("echo marker-one\n")
	sh.WaitFor("marker-one")

	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("$")

	sh.Type("$HT_BIN attach continuity\n")
	sh.WaitFor("IN>")
	sh.WaitFor("marker-one")

	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("$")
}

func TestKillRunningSession(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")

	sh.Type("$HT_BIN attach kill-me\n")
	sh.WaitFor("created session")
	sh.WaitFor("$")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")

	kill := e.run("kill", "kill-me")
	kill.Assert(t, icmd.Expected{ExitCode: 0, Out: "killed session \"kill-me\"\n"})

	sh.Type("$HT_BIN attach kill-me\n")
	sh.WaitFor("created session")
}

func TestAttachWithCommand(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach oneshot -- /bin/sh -c \"printf 'oneshot-ok\\n'\"\n")
	sh.WaitFor("oneshot-ok")
	sh.WaitFor("$")

	listAll := e.run("list", "-a")
	listAll.Assert(t, icmd.Expected{ExitCode: 0})
}

func TestDaemonAutoExit(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	sh.WaitFor("$")
	sh.Type("$HT_BIN attach auto-exit -- /bin/sh -c \"exit 0\"\n")
	sh.WaitFor("$")

	select {
	case <-daemon.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not auto-exit")
	}
}
