package e2e_test

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/fs"
	"gotest.tools/v3/icmd"

	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/termtest"
	"code.selman.me/hauntty/libghostty"
)

func TestInitConfig(t *testing.T) {
	dir := fs.NewDir(t, "init-config")
	cfgHome := dir.Join("config")
	expectedPath := filepath.Join(cfgHome, "hauntty", "config.toml")

	first := icmd.RunCmd(icmd.Command(htBin, "init"), icmd.WithEnv(
		"XDG_CONFIG_HOME="+cfgHome,
	))
	first.Assert(t, icmd.Expected{
		ExitCode: 0,
		Out:      "created " + expectedPath + "\n",
	})

	second := icmd.RunCmd(icmd.Command(htBin, "init"), icmd.WithEnv(
		"XDG_CONFIG_HOME="+cfgHome,
	))
	second.Assert(t, icmd.Expected{
		ExitCode: 1,
		Err:      "ht: error: config already exists: " + expectedPath + "\n",
	})
}

func TestPruneDeadSessions(t *testing.T) {
	e := setup(t, nil)

	daemon := e.term([]string{htBin, "daemon"})
	daemon.WaitFor("daemon listening")

	stateDir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "hauntty", "sessions")
	assert.NilError(t, os.MkdirAll(stateDir, 0o755))
	assert.NilError(t, os.WriteFile(filepath.Join(stateDir, "dead-prune.state"), []byte("fake"), 0o644))

	pruneOne := e.run("prune")
	pruneOne.Assert(t, icmd.Expected{
		ExitCode: 0,
		Out:      "pruned 1 dead session(s)\n",
	})

	pruneTwo := e.run("prune")
	pruneTwo.Assert(t, icmd.Expected{
		ExitCode: 0,
		Out:      "no dead sessions to prune\n",
	})

	assert.Equal(t, pruneTwo.ExitCode, 0)
}

func TestListCorruptDeadSessionStateReportsLoadError(t *testing.T) {
	e := setup(t, nil)

	daemon := e.term([]string{htBin, "daemon"})
	daemon.WaitFor("daemon listening")

	assert.NilError(t, os.MkdirAll(filepath.Dir(e.statePath("broken-list")), 0o700))
	assert.NilError(t, os.WriteFile(e.statePath("broken-list"), []byte("NOPE\x01"), 0o600))

	list := e.run("list", "-a")
	list.Assert(t, icmd.Expected{
		ExitCode: 1,
		Err:      "list: list dead sessions: load dead session state \"broken-list\": persist: bad magic 4e4f5045",
	})
}

func TestDumpCorruptDeadSessionStateReportsLoadError(t *testing.T) {
	e := setup(t, nil)

	daemon := e.term([]string{htBin, "daemon"})
	daemon.WaitFor("daemon listening")

	assert.NilError(t, os.MkdirAll(filepath.Dir(e.statePath("broken-dump")), 0o700))
	assert.NilError(t, os.WriteFile(e.statePath("broken-dump"), []byte("NOPE\x01"), 0o600))

	dump := e.run("dump", "broken-dump")
	dump.Assert(t, icmd.Expected{
		ExitCode: 1,
		Err:      "dump: load dead session state: persist: bad magic 4e4f5045",
	})
}

func TestNewCorruptDeadSessionStateReportsLoadError(t *testing.T) {
	cfg := config.Default()
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon"})
	daemon.WaitFor("daemon listening")

	assert.NilError(t, os.MkdirAll(filepath.Dir(e.statePath("broken-new")), 0o700))
	assert.NilError(t, os.WriteFile(e.statePath("broken-new"), []byte("NOPE\x01"), 0o600))

	created := e.run("new", "broken-new")
	created.Assert(t, icmd.Expected{
		ExitCode: 1,
		Err:      "create: load dead session state: persist: bad magic 4e4f5045",
	})
}

func TestDetachKeybind(t *testing.T) {
	cfg := config.Default()
	cfg.Client.DetachKeybind = "ctrl+\\"
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon", "--auto-exit"})
	daemon.WaitFor("daemon listening")

	sh := e.term([]string{"/bin/sh"}, termtest.WithEnv("PS1=$ ", "SHELL=/bin/sh"))
	e.waitHostPrompt(sh)
	sh.Type("$HT_BIN attach keybind-session\n")
	sh.WaitFor("created session")
	e.waitAttachedPrompt(sh)
	sh.Key(libghostty.KeyCode('\\'), libghostty.ModCtrl)
	sh.WaitFor("detached")
	e.waitHostPrompt(sh)
}
