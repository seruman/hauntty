package daemon

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"code.selman.me/hauntty/internal/protocol"
	"gotest.tools/v3/assert"
)

func writeDeadSessionState(t *testing.T, name string, state *sessionState) {
	t.Helper()

	data, err := encodeState(state)
	assert.NilError(t, err)

	sessionDir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "hauntty", "sessions")
	assert.NilError(t, os.MkdirAll(sessionDir, 0o700))
	assert.NilError(t, os.WriteFile(filepath.Join(sessionDir, name+".state"), data, 0o600))
}

func readServerMessage(t *testing.T, out *bytes.Buffer) protocol.Message {
	t.Helper()

	msg, err := protocol.NewConn(bytes.NewBuffer(out.Bytes())).ReadMessage()
	assert.NilError(t, err)
	return msg
}
