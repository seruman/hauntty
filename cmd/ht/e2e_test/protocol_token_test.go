package e2e_test

import (
	"testing"
	"time"

	"gotest.tools/v3/assert"

	"code.selman.me/hauntty/client"
	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/protocol"
)

func TestCreateAttachTokenSurvivesDeadTTLWindow(t *testing.T) {
	cfg := config.Default()
	cfg.Daemon.DeadSessionTTLSeconds = 1
	e := setup(t, cfg)

	daemon := e.term([]string{htBin, "daemon"})
	daemon.WaitFor("daemon listening")

	c, err := client.Connect(e.sock)
	assert.NilError(t, err)
	defer c.Close()

	created, err := c.Create("token-window", []string{"/bin/sh", "-c", "exit 0"}, []string{}, "/", protocol.CreateModeOpenOrCreate)
	assert.NilError(t, err)
	assert.Equal(t, created.Outcome, protocol.CreateOutcomeCreated)

	time.Sleep(2 * time.Second)

	attached, err := c.Attach(created.SessionName, 80, 24, 0, 0, false, "", created.AttachToken)
	assert.NilError(t, err)
	assert.Equal(t, attached.SessionName, created.SessionName)
}
