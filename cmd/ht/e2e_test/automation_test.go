package e2e_test

import (
	"testing"
	"time"

	"gotest.tools/v3/golden"
	"gotest.tools/v3/icmd"

	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/termtest"
	"code.selman.me/hauntty/libghostty"
)

func TestSendTextAndKey(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	e.waitHostPrompt(sh)
	sh.Type("$HT_BIN attach send-session\n")
	sh.WaitFor("created session")
	e.waitAttachedPrompt(sh)
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")

	sendText := e.run("send", "send-session", "echo send-ok")
	sendText.Assert(t, icmd.Success)
	sendKey := e.run("send", "send-session", "--key", "enter")
	sendKey.Assert(t, icmd.Success)

	wait := e.run("wait", "send-session", "send-ok")
	wait.Assert(t, icmd.Success)

	dump := e.run("dump", "send-session", "--format", "plain")
	dump.Assert(t, icmd.Success)
}

func TestWaitSessionOutput(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	e.waitHostPrompt(sh)
	sh.Type("$HT_BIN attach wait-session\n")
	sh.WaitFor("created session")
	e.waitAttachedPrompt(sh)
	sh.Type("echo ready-for-wait\n")
	sh.WaitFor("ready-for-wait")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")

	success := e.run("wait", "wait-session", "ready-for-wait", "-t", "5000")
	success.Assert(t, icmd.Success)

	timeout := e.run("wait", "wait-session", "definitely-not-present", "-t", "100")
	timeout.Assert(t, icmd.Expected{ExitCode: 1, Err: "timeout waiting for \"definitely-not-present\"\n"})
}

func TestWaitRegex(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	e.waitHostPrompt(sh)
	sh.Type("$HT_BIN attach regex-session\n")
	sh.WaitFor("created session")
	e.waitAttachedPrompt(sh)
	sh.Type("echo value-42\n")
	sh.WaitFor("value-42")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")

	ok := e.run("wait", "regex-session", "value-[0-9]+", "-e", "-t", "5000")
	ok.Assert(t, icmd.Success)

	bad := e.run("wait", "regex-session", "[", "-e")
	bad.Assert(t, icmd.Expected{ExitCode: 1, Err: "ht: error: invalid regex: error parsing regexp: missing closing ]: `[`\n"})
}

func TestDumpPlain(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	cfg.Client.ForwardEnv = []string{"PS1"}
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	e.waitHostPrompt(sh)

	sh.Type("$HT_BIN attach dump-session\n")
	sh.WaitFor("created session")
	e.waitAttachedPrompt(sh)

	sh.Type("printf 'alpha\\nbeta\\n'\n")
	sh.WaitFor("alpha")
	sh.WaitFor("beta")

	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	e.waitHostPrompt(sh)

	result := e.run("dump", "dump-session", "--format", "plain")
	result.Assert(t, icmd.Success)
	golden.Assert(t, result.Stdout(), "dump_plain.golden")
}

func TestDumpFormats(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	e.waitHostPrompt(sh)
	sh.Type("$HT_BIN attach dump-formats\n")
	sh.WaitFor("created session")
	e.waitAttachedPrompt(sh)
	sh.Type("printf 'fmt-line-1\\nfmt-line-2\\n'\n")
	sh.WaitFor("fmt-line-1")
	sh.WaitFor("fmt-line-2")
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	sh.WaitFor("detached")

	vt := e.run("dump", "dump-formats", "--format", "vt")
	vt.Assert(t, icmd.Success)
	golden.Assert(t, vt.Stdout(), "dump_vt.golden")

	html := e.run("dump", "dump-formats", "--format", "html")
	html.Assert(t, icmd.Success)
	golden.Assert(t, html.Stdout(), "dump_html.golden")
}

func TestDumpDeadSessionPreservesFormats(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	e.waitHostPrompt(sh)
	sh.Type("$HT_BIN attach dead-dump-formats -- /bin/sh -c \"printf '\\033[31mred\\033[0m\\nplain\\n'; sleep 30\"\n")
	sh.WaitFor("red")
	sh.WaitFor("plain")
	sh.WaitStable(250*time.Millisecond, termtest.WaitTimeout(2*time.Second))
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	e.waitHostPrompt(sh)

	kill := e.run("kill", "dead-dump-formats")
	kill.Assert(t, icmd.Expected{ExitCode: 0, Out: "killed session \"dead-dump-formats\"\n"})

	deadPlain := e.waitForCommandSuccess("dump", "dead-dump-formats", "--format", "plain")
	deadPlain.Assert(t, icmd.Success)
	golden.Assert(t, deadPlain.Stdout(), "dump_dead_plain.golden")

	deadHTML := e.run("dump", "dead-dump-formats", "--format", "html")
	deadHTML.Assert(t, icmd.Success)
	golden.Assert(t, deadHTML.Stdout(), "dump_dead_html.golden")
}

func TestDumpDeadSessionPreservesJoinFlag(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+]"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon"})
	daemon.WaitFor("daemon listening")

	sh := e.term(
		[]string{"/bin/sh"},
		termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"),
		termtest.WithSize(20, 24),
	)
	e.waitHostPrompt(sh)
	sh.Type("$HT_BIN attach dead-dump-join -- /bin/sh -c \"printf 'aaaaaaaaaaaaaaaaaaaabbbbbbbbbb\\n'; sleep 30\"\n")
	sh.WaitFor("bbbb")
	sh.WaitStable(250*time.Millisecond, termtest.WaitTimeout(2*time.Second))
	sh.Key(libghostty.KeyCode(']'), libghostty.ModCtrl)
	e.waitHostPrompt(sh)

	kill := e.run("kill", "dead-dump-join")
	kill.Assert(t, icmd.Expected{ExitCode: 0, Out: "killed session \"dead-dump-join\"\n"})

	deadPlain := e.waitForCommandSuccess("dump", "dead-dump-join", "--format", "plain")
	deadPlain.Assert(t, icmd.Success)
	golden.Assert(t, deadPlain.Stdout(), "dump_dead_wrap_plain.golden")

	deadJoin := e.run("dump", "dead-dump-join", "--format", "plain", "-J")
	deadJoin.Assert(t, icmd.Success)
	golden.Assert(t, deadJoin.Stdout(), "dump_dead_wrap_join.golden")
}
