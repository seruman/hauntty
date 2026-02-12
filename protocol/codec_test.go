package protocol

import (
	"bytes"
	"io"
	"testing"

	"gotest.tools/v3/assert"
)

func TestRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
	}{
		{"Attach", &Attach{
			Name:            "my-session",
			Cols:            120,
			Rows:            40,
			Command:         "/bin/bash",
			Env:             []string{"TERM=xterm-256color", "HOME=/home/user"},
			ScrollbackLines: 10000,
		}},
		{"AttachEmptyEnv", &Attach{
			Name:    "s",
			Cols:    80,
			Rows:    24,
			Command: "sh",
			Env:     []string{},
		}},
		{"Input", &Input{Data: []byte("hello world\n")}},
		{"InputEmpty", &Input{Data: []byte{}}},
		{"Resize", &Resize{Cols: 200, Rows: 50}},
		{"Detach", &Detach{}},
		{"List", &List{}},
		{"Kill", &Kill{Name: "doomed-session"}},
		{"Send", &Send{Name: "target", Data: []byte{0x1b, 0x5b, 0x41}}},
		{"Dump", &Dump{Name: "sess", Format: 2}},
		{"OK", &OK{
			SessionName: "my-session",
			Cols:        120,
			Rows:        40,
			PID:         12345,
			Created:     true,
		}},
		{"Error", &Error{Code: 404, Message: "session not found"}},
		{"ErrorEmptyMessage", &Error{Code: 500, Message: ""}},
		{"Output", &Output{Data: []byte("\x1b[31mred\x1b[0m")}},
		{"State", &State{
			ScreenDump:        []byte("screen content here"),
			CursorRow:         10,
			CursorCol:         42,
			IsAlternateScreen: true,
		}},
		{"Sessions", &Sessions{
			Sessions: []Session{
				{Name: "s1", State: "running", Cols: 80, Rows: 24, PID: 100, CreatedAt: 1700000000},
				{Name: "s2", State: "idle", Cols: 120, Rows: 40, PID: 200, CreatedAt: 1700000001},
			},
		}},
		{"SessionsEmpty", &Sessions{Sessions: []Session{}}},
		{"Exited/0", &Exited{ExitCode: 0}},
		{"Exited/1", &Exited{ExitCode: 1}},
		{"Exited/-1", &Exited{ExitCode: -1}},
		{"Exited/127", &Exited{ExitCode: 127}},
		{"Exited/255", &Exited{ExitCode: 255}},
		{"DumpResponse", &DumpResponse{Data: []byte("dump data")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			c := NewConn(&buf)
			err := c.WriteMessage(tt.msg)
			assert.NilError(t, err)
			got, err := c.ReadMessage()
			assert.NilError(t, err)
			assert.Equal(t, got.Type(), tt.msg.Type())
			assert.DeepEqual(t, got, tt.msg)
		})
	}
}

func TestHandshakeSuccess(t *testing.T) {
	clientBuf := &bytes.Buffer{}
	serverBuf := &bytes.Buffer{}

	// Client writes version to clientBuf.
	client := NewConn(clientBuf)
	enc := NewEncoder(clientBuf)
	err := enc.WriteU8(ProtocolVersion)
	assert.NilError(t, err)

	// Server reads from clientBuf.
	_ = client // suppress unused
	server := NewConn(clientBuf)
	version, err := server.AcceptHandshake()
	assert.NilError(t, err)
	assert.Equal(t, version, ProtocolVersion)

	// Server writes accepted version to serverBuf.
	serverConn := NewConn(serverBuf)
	err = serverConn.AcceptVersion(version)
	assert.NilError(t, err)

	// Client reads accepted version from serverBuf.
	clientConn := NewConn(serverBuf)
	dec := NewDecoder(serverBuf)
	_ = clientConn
	accepted, err := dec.ReadU8()
	assert.NilError(t, err)
	assert.Equal(t, accepted, ProtocolVersion)
}

func TestHandshakeRoundTrip(t *testing.T) {
	wire := &bytes.Buffer{}

	// Client side: send version.
	clientConn := NewConn(wire)
	enc := NewEncoder(wire)
	err := enc.WriteU8(ProtocolVersion)
	assert.NilError(t, err)

	// Server side: read version.
	_ = clientConn
	serverConn := NewConn(wire)
	version, err := serverConn.AcceptHandshake()
	assert.NilError(t, err)
	assert.Equal(t, version, ProtocolVersion)

	// Server writes accepted version back to wire.
	err = serverConn.AcceptVersion(version)
	assert.NilError(t, err)

	// Client reads accepted version.
	dec := NewDecoder(wire)
	accepted, err := dec.ReadU8()
	assert.NilError(t, err)
	assert.Equal(t, accepted, ProtocolVersion)
}

func TestUnknownMessageType(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	err := enc.WriteU32(1)
	assert.NilError(t, err)
	err = enc.WriteU8(0xFF)
	assert.NilError(t, err)

	c := NewConn(&buf)
	_, err = c.ReadMessage()
	assert.Assert(t, err != nil)
}

func TestTruncatedFrame(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	err := enc.WriteU32(100)
	assert.NilError(t, err)
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04, 0x05})

	c := NewConn(&buf)
	_, err = c.ReadMessage()
	assert.Assert(t, err != nil)
}

func TestEmptyFrame(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	err := enc.WriteU32(0)
	assert.NilError(t, err)

	c := NewConn(&buf)
	_, err = c.ReadMessage()
	assert.Assert(t, err != nil)
}

func TestReadMessageEOF(t *testing.T) {
	buf := &bytes.Buffer{}
	c := NewConn(buf)
	_, err := c.ReadMessage()
	assert.Assert(t, err == io.EOF || err == io.ErrUnexpectedEOF)
}

func TestLargePayload(t *testing.T) {
	data := make([]byte, 1<<16) // 64KB
	for i := range data {
		data[i] = byte(i % 256)
	}
	orig := &Output{Data: data}

	var buf bytes.Buffer
	c := NewConn(&buf)
	err := c.WriteMessage(orig)
	assert.NilError(t, err)
	got, err := c.ReadMessage()
	assert.NilError(t, err)
	assert.Equal(t, got.Type(), orig.Type())
	assert.DeepEqual(t, got, Message(orig))
}

func TestMultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	c := NewConn(&buf)

	msgs := []Message{
		&Attach{Name: "s1", Cols: 80, Rows: 24, Command: "bash", Env: []string{"A=1"}, ScrollbackLines: 1000},
		&Input{Data: []byte("ls\n")},
		&Output{Data: []byte("file1\nfile2\n")},
		&Resize{Cols: 100, Rows: 50},
		&Detach{},
	}

	for _, msg := range msgs {
		err := c.WriteMessage(msg)
		assert.NilError(t, err)
	}

	for i, orig := range msgs {
		got, err := c.ReadMessage()
		assert.NilError(t, err)
		assert.Equal(t, got.Type(), orig.Type(), "message %d", i)
	}
}
