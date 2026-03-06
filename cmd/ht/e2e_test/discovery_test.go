package e2e_test

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"code.selman.me/hauntty/client"
	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/internal/termtest"
	"code.selman.me/hauntty/libghostty"
	"gotest.tools/v3/assert"
	"gotest.tools/v3/icmd"
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

	c, err := client.Connect(e.sock)
	assert.NilError(t, err)
	defer c.Close()

	sessions, err := c.List(false)
	assert.NilError(t, err)

	rowsByName := make(map[string]protocol.Session, len(sessions.Sessions))
	for _, s := range sessions.Sessions {
		rowsByName[s.Name] = s
	}

	splitCols := regexp.MustCompile(`\s{2,}`)
	parseRows := func(out string) [][]string {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		rows := make([][]string, len(lines))
		for i, line := range lines {
			rows[i] = splitCols.Split(strings.TrimRight(line, " "), -1)
			if len(rows[i]) == 5 {
				rows[i] = append(rows[i][:3], append([]string{""}, rows[i][3:]...)...)
			}
		}
		return rows
	}

	home := ""
	if h, err := os.UserHomeDir(); err == nil {
		home = h
	} else {
		t.Logf("resolve home dir: %v", err)
	}
	formatRow := func(s protocol.Session) []string {
		cwd := s.CWD
		if home != "" && strings.HasPrefix(cwd, home) {
			cwd = "~" + cwd[len(home):]
		}
		pid := "-"
		if s.PID != 0 {
			pid = strconv.FormatUint(uint64(s.PID), 10)
		}
		created := "-"
		if s.CreatedAt != 0 {
			created = time.Unix(int64(s.CreatedAt), 0).Format("2006-01-02 15:04:05")
		}
		return []string{
			s.Name,
			s.State,
			fmt.Sprintf("%dx%d", s.Cols, s.Rows),
			cwd,
			pid,
			created,
		}
	}

	list := e.run("list")
	list.Assert(t, icmd.Expected{ExitCode: 0})
	assert.DeepEqual(t, parseRows(list.Stdout()), [][]string{
		{"NAME", "STATE", "SIZE", "CWD", "PID", "CREATED"},
		formatRow(rowsByName["alive"]),
	})

	listAll := e.run("list", "-a")
	listAll.Assert(t, icmd.Expected{ExitCode: 0})
	assert.DeepEqual(t, parseRows(listAll.Stdout()), [][]string{
		{"NAME", "STATE", "SIZE", "CWD", "PID", "CREATED"},
		formatRow(rowsByName["alive"]),
		formatRow(rowsByName["dead"]),
	})
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
