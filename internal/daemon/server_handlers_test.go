package daemon

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
	"gotest.tools/v3/assert"
)

func snapshotSessionState(t *testing.T, cols, rows uint16, savedAt time.Time, input []byte) *sessionState {
	t.Helper()

	term, err := libghostty.NewTerminal(
		libghostty.WithSize(cols, rows),
		libghostty.WithMaxScrollbackLines(2000),
		libghostty.WithContinuationMaxBytes(continuationMaxBytes),
	)
	assert.NilError(t, err)
	defer term.Close()
	term.VTWrite(input)
	snapshot, err := term.Snapshot()
	assert.NilError(t, err)
	return &sessionState{Cols: cols, Rows: rows, SavedAt: savedAt, Snapshot: snapshot}
}

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
