package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/protocol"
	"github.com/BurntSushi/toml"
	"github.com/alecthomas/kong"
	"gotest.tools/v3/assert"
)

func TestMain(m *testing.M) {
	if os.Getenv("HAUNTTY_TEST_DAEMON_HELPER") == "1" && len(os.Args) > 1 && os.Args[1] == "daemon" {
		os.Exit(23)
	}
	os.Exit(m.Run())
}

func TestConfigCommandHelp(t *testing.T) {
	var cli CLI
	parser, err := kong.New(&cli)
	assert.NilError(t, err)

	var configNode *kong.Node
	var walk func(node *kong.Node)
	walk = func(node *kong.Node) {
		if node.Name == "config" {
			configNode = node
			return
		}
		for _, child := range node.Children {
			if configNode != nil {
				return
			}
			walk(child)
		}
	}
	walk(parser.Model.Node)

	if configNode == nil {
		t.Fatal("config command not found")
	}
	assert.Equal(t, configNode.Help, "Print current configuration.")
}

func TestWaitCmdReportsConnectError(t *testing.T) {
	sock := t.TempDir()
	cfg := config.Default()
	cfg.Daemon.SocketPath = sock

	cmd := WaitCmd{Name: "demo", Pattern: "ready"}
	err := cmd.Run(cfg)

	var exitErr *commandExitError
	matched := errors.As(err, &exitErr)
	assert.Equal(t, matched, true)
	assert.Equal(t, exitErr.code, 2)
	assert.Equal(t, exitErr.stderr, "error: connect to daemon: dial unix "+sock+": connect: socket operation on non-socket\n")
}

func TestDaemonCmdValidate(t *testing.T) {
	t.Run("rejects log file without detach", func(t *testing.T) {
		cmd := DaemonCmd{LogFile: "/tmp/daemon.log"}

		err := cmd.validate()

		assert.Error(t, err, "--log-file requires --detach")
	})

	t.Run("allows log file with detach", func(t *testing.T) {
		cmd := DaemonCmd{Detach: true, LogFile: "/tmp/daemon.log"}

		err := cmd.validate()

		assert.NilError(t, err)
	})
}

func TestDaemonStartupTimeoutError(t *testing.T) {
	t.Run("includes log path when available", func(t *testing.T) {
		err := daemonStartupTimeoutError("/tmp/hauntty.sock", "/tmp/hauntty-server.log")

		assert.Error(t, err, "timed out waiting for daemon at /tmp/hauntty.sock (see /tmp/hauntty-server.log)")
	})

	t.Run("omits empty log path", func(t *testing.T) {
		err := daemonStartupTimeoutError("/tmp/hauntty.sock", "")

		assert.Error(t, err, "timed out waiting for daemon at /tmp/hauntty.sock")
	})
}

func TestOpenDaemonLogFile(t *testing.T) {
	t.Run("uses provided log path", func(t *testing.T) {
		sock := filepath.Join(t.TempDir(), "hauntty.sock")
		logPath := filepath.Join(t.TempDir(), "logs", "daemon.log")

		f, err := openDaemonLogFile(sock, logPath)
		assert.NilError(t, err)
		defer f.Close()

		assert.Equal(t, f.Name(), logPath)
	})
}

func TestDaemonStartArgs(t *testing.T) {
	t.Run("includes auto-exit and socket", func(t *testing.T) {
		args := daemonStartArgs("/tmp/hauntty.sock", true)

		assert.DeepEqual(t, args, []string{"daemon", "--auto-exit", "--socket", "/tmp/hauntty.sock"})
	})

	t.Run("omits optional flags", func(t *testing.T) {
		args := daemonStartArgs("", false)

		assert.DeepEqual(t, args, []string{"daemon"})
	})
}

func TestEnsureDaemonDetachedReportsEarlyChildExit(t *testing.T) {
	t.Setenv("HAUNTTY_TEST_DAEMON_HELPER", "1")

	dir, err := os.MkdirTemp("/tmp", "htd-*")
	assert.NilError(t, err)
	defer os.RemoveAll(dir)

	sock := filepath.Join(dir, "hauntty.sock")
	logPath := filepath.Join(dir, "daemon.log")

	err = ensureDaemonDetached(sock, false, logPath)

	assert.Error(t, err, "daemon failed before ready at "+sock+": daemon exited before ready with status 23 (see "+logPath+")")
}

func TestSessionListRows(t *testing.T) {
	sessions := []protocol.Session{
		{
			Name:      "live",
			State:     protocol.SessionStateRunning,
			Cols:      80,
			Rows:      24,
			CWD:       "/home/alice/src/project",
			PID:       42,
			CreatedAt: 1700000000,
		},
		{
			Name:    "dead",
			State:   protocol.SessionStateDead,
			Cols:    100,
			Rows:    40,
			CWD:     "/tmp/dead",
			SavedAt: 1700000100,
		},
	}

	rows := sessionListRows(sessions, false, "/home/alice")
	assert.DeepEqual(t, rows, [][]string{
		{"NAME", "STATE", "SIZE", "CWD", "PID", "CREATED", "SAVED"},
		{"live", "running", "80x24", "~/src/project", "42", time.Unix(1700000000, 0).Format("2006-01-02 15:04:05"), "-"},
	})

	rows = sessionListRows(sessions, true, "/home/alice")
	assert.DeepEqual(t, rows, [][]string{
		{"NAME", "STATE", "SIZE", "CWD", "PID", "CREATED", "SAVED"},
		{"live", "running", "80x24", "~/src/project", "42", time.Unix(1700000000, 0).Format("2006-01-02 15:04:05"), "-"},
		{"dead", "dead", "100x40", "/tmp/dead", "-", "-", time.Unix(1700000100, 0).Format("2006-01-02 15:04:05")},
	})
}

func TestDumpRequestFormat(t *testing.T) {
	format := dumpRequestFormat("plain", false, false)
	assert.Equal(t, format, protocol.DumpFormat(0))

	format = dumpRequestFormat("vt", true, false)
	assert.Equal(t, format, protocol.DumpVT|protocol.DumpFlagUnwrap)

	format = dumpRequestFormat("html", true, true)
	assert.Equal(t, format, protocol.DumpHTML|protocol.DumpFlagUnwrap|protocol.DumpFlagScrollback)
}

func TestCompileWaitMatcher(t *testing.T) {
	match, err := compileWaitMatcher("ready", false)
	assert.NilError(t, err)
	assert.Equal(t, match("daemon ready"), true)
	assert.Equal(t, match("daemon waiting"), false)

	match, err = compileWaitMatcher("^ready-[0-9]+$", true)
	assert.NilError(t, err)
	assert.Equal(t, match("ready-42"), true)
	assert.Equal(t, match("ready-now"), false)

	_, err = compileWaitMatcher("(", true)
	assert.Error(t, err, "invalid regex: error parsing regexp: missing closing ): `(`")
}

func TestWaitContent(t *testing.T) {
	content := "alpha\nbeta\ngamma"

	assert.Equal(t, waitContent(content, -1), "alpha\nbeta\ngamma")
	assert.Equal(t, waitContent(content, 0), "alpha")
	assert.Equal(t, waitContent(content, 1), "beta")
	assert.Equal(t, waitContent(content, 9), "")
}

func TestInitCmdCreatesConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cmd := InitCmd{}
	err := cmd.Run(config.Default())
	assert.NilError(t, err)

	path := filepath.Join(dir, "hauntty", "config.toml")
	_, err = os.Stat(path)
	assert.NilError(t, err)

	cfg, err := config.LoadFrom(path)
	assert.NilError(t, err)
	assert.DeepEqual(t, cfg, config.Default())
}

func TestInitCmdErrorWhenExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path := filepath.Join(dir, "hauntty", "config.toml")
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	assert.NilError(t, err)
	err = os.WriteFile(path, []byte{}, 0o600)
	assert.NilError(t, err)

	cmd := InitCmd{}
	err = cmd.Run(config.Default())
	assert.Error(t, err, "config already exists: "+path)
}

func TestConfigCmdWritesToml(t *testing.T) {
	cfg := config.Default()

	r, w, err := os.Pipe()
	assert.NilError(t, err)

	oldStdout := os.Stdout
	os.Stdout = w

	cmd := ConfigCmd{}
	runErr := cmd.Run(cfg)

	w.Close()
	os.Stdout = oldStdout

	assert.NilError(t, runErr)

	var got bytes.Buffer
	_, err = io.Copy(&got, r)
	assert.NilError(t, err)
	r.Close()

	var want bytes.Buffer
	err = toml.NewEncoder(&want).Encode(cfg)
	assert.NilError(t, err)

	assert.Equal(t, got.String(), want.String())
}
