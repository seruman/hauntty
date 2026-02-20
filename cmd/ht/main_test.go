package main

import (
	"testing"

	"gotest.tools/v3/assert"
)

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
