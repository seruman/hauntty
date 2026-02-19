package e2e_test

import (
	"os"
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
	e.waitHostPrompt(sh)

	sh.Type("$HT_BIN attach auto-start\n")
	sh.WaitFor("created session")
	e.waitAttachedPrompt(sh)

	sh.Type("echo daemon-started\n")
	sh.WaitFor("daemon-started")

	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	e.waitHostPrompt(sh)

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
	e.waitHostPrompt(sh)

	sh.Type("$HT_BIN attach test-session\n")
	sh.WaitFor("created session")
	e.waitAttachedPrompt(sh)

	sh.Type("echo hello-from-hauntty\n")
	sh.WaitFor("hello-from-hauntty")

	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	e.waitHostPrompt(sh)

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
	e.waitHostPrompt(sh)

	sh.Type("$HT_BIN attach continuity\n")
	sh.WaitFor("created session")
	e.waitAttachedPrompt(sh)
	sh.Type("export PS1='IN> '\n")
	sh.WaitFor("IN>")

	sh.Type("echo marker-one\n")
	sh.WaitFor("marker-one")

	detachOne := icmd.RunCmd(
		icmd.Command(htBin, "detach"),
		icmd.WithEnv(append(os.Environ(), append(e.env(), "HAUNTTY_SESSION=continuity")...)...),
	)
	detachOne.Assert(t, icmd.Success)
	sh.WaitFor("detached")
	e.waitHostPrompt(sh)

	sh.Type("$HT_BIN attach continuity\n")
	sh.WaitFor("IN>")
	sh.WaitFor("marker-one")

	detachTwo := icmd.RunCmd(
		icmd.Command(htBin, "detach"),
		icmd.WithEnv(append(os.Environ(), append(e.env(), "HAUNTTY_SESSION=continuity")...)...),
	)
	detachTwo.Assert(t, icmd.Success)
	sh.WaitFor("detached")
	e.waitHostPrompt(sh)
}

func TestDetachInsideSessionDetachesSingleClient(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh1 := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	e.waitHostPrompt(sh1)
	sh1.Type("$HT_BIN attach shared-detach\n")
	sh1.WaitFor("created session")
	e.waitAttachedPrompt(sh1)

	sh2 := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	e.waitHostPrompt(sh2)
	sh2.Type("$HT_BIN attach shared-detach\n")
	sh2.WaitFor("attached to session")
	e.waitAttachedPrompt(sh2)

	sh1.Type("$HT_BIN detach\n")
	sh1.WaitFor("detached")
	e.waitHostPrompt(sh1)

	sh2.Type("echo still-attached\n")
	sh2.WaitFor("still-attached")

	sh2.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh2.WaitFor("detached")
	e.waitHostPrompt(sh2)
}

func TestKillRunningSession(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	e.waitHostPrompt(sh)

	sh.Type("$HT_BIN attach kill-me\n")
	sh.WaitFor("created session")
	e.waitAttachedPrompt(sh)
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
	e.waitHostPrompt(sh)
	sh.Type("$HT_BIN attach oneshot -- /bin/sh -c \"printf 'oneshot-ok\\n'\"\n")
	sh.WaitFor("oneshot-ok")

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
	e.waitHostPrompt(sh)
	sh.Type("$HT_BIN attach auto-exit -- /bin/sh -c \"exit 0\"\n")
	e.waitHostPrompt(sh)

	select {
	case <-daemon.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("daemon did not auto-exit")
	}
}

func TestNewCreatesSession(t *testing.T) {
	cfg := config.Default()
	cfg.Daemon.AutoExit = true
	e := setup(t, cfg)

	created := e.run("new", "new-session")
	created.Assert(t, icmd.Expected{ExitCode: 0, Out: "created session \"new-session\"\n"})

	sendText := e.run("send", "new-session", "echo created-via-new")
	sendText.Assert(t, icmd.Success)
	sendKey := e.run("send", "new-session", "--key", "enter")
	sendKey.Assert(t, icmd.Success)

	wait := e.run("wait", "new-session", "created-via-new", "-t", "5000")
	wait.Assert(t, icmd.Success)

	kill := e.run("kill", "new-session")
	kill.Assert(t, icmd.Expected{ExitCode: 0, Out: "killed session \"new-session\"\n"})
}

func TestNewWithCommand(t *testing.T) {
	cfg := config.Default()
	cfg.Daemon.AutoExit = true
	e := setup(t, cfg)

	created := e.run("new", "new-command", "--", "/bin/sh", "-c", "printf 'new-command-ok\\n'; sleep 30")
	created.Assert(t, icmd.Expected{ExitCode: 0, Out: "created session \"new-command\"\n"})

	wait := e.run("wait", "new-command", "new-command-ok", "-t", "5000")
	wait.Assert(t, icmd.Success)

	kill := e.run("kill", "new-command")
	kill.Assert(t, icmd.Expected{ExitCode: 0, Out: "killed session \"new-command\"\n"})
}

func TestNewExistingSession(t *testing.T) {
	cfg := config.Default()
	cfg.Daemon.AutoExit = true
	e := setup(t, cfg)

	first := e.run("new", "new-existing")
	first.Assert(t, icmd.Expected{ExitCode: 0, Out: "created session \"new-existing\"\n"})

	sendOne := e.run("send", "new-existing", "echo first-pass")
	sendOne.Assert(t, icmd.Success)
	enterOne := e.run("send", "new-existing", "--key", "enter")
	enterOne.Assert(t, icmd.Success)
	waitOne := e.run("wait", "new-existing", "first-pass", "-t", "5000")
	waitOne.Assert(t, icmd.Success)

	second := e.run("new", "new-existing")
	second.Assert(t, icmd.Expected{ExitCode: 1, Err: "ht: error: session \"new-existing\" already exists\n"})

	sendTwo := e.run("send", "new-existing", "echo second-pass")
	sendTwo.Assert(t, icmd.Success)
	enterTwo := e.run("send", "new-existing", "--key", "enter")
	enterTwo.Assert(t, icmd.Success)
	waitTwo := e.run("wait", "new-existing", "second-pass", "-t", "5000")
	waitTwo.Assert(t, icmd.Success)

	kill := e.run("kill", "new-existing")
	kill.Assert(t, icmd.Expected{ExitCode: 0, Out: "killed session \"new-existing\"\n"})
}
