package protocol

import (
	"bytes"
	"testing"

	"gotest.tools/v3/assert"
)

func roundTrip(t *testing.T, msg Message) Message {
	t.Helper()
	var buf bytes.Buffer
	c := NewConn(&buf)
	err := c.WriteMessage(msg)
	assert.NilError(t, err)
	got, err := c.ReadMessage()
	assert.NilError(t, err)
	return got
}
