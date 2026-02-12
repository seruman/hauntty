package protocol

import (
	"bytes"
	"io"
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
	assert.Equal(t, got.Type(), msg.Type())
	return got
}

func TestRoundTripAttach(t *testing.T) {
	orig := &Attach{
		Name:            "my-session",
		Cols:            120,
		Rows:            40,
		Command:         "/bin/bash",
		Env:             []string{"TERM=xterm-256color", "HOME=/home/user"},
		ScrollbackLines: 10000,
	}
	got := roundTrip(t, orig).(*Attach)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripAttachEmptyEnv(t *testing.T) {
	orig := &Attach{
		Name:    "s",
		Cols:    80,
		Rows:    24,
		Command: "sh",
		Env:     []string{},
	}
	got := roundTrip(t, orig).(*Attach)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripInput(t *testing.T) {
	orig := &Input{Data: []byte("hello world\n")}
	got := roundTrip(t, orig).(*Input)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripInputEmpty(t *testing.T) {
	orig := &Input{Data: []byte{}}
	got := roundTrip(t, orig).(*Input)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripResize(t *testing.T) {
	orig := &Resize{Cols: 200, Rows: 50}
	got := roundTrip(t, orig).(*Resize)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripDetach(t *testing.T) {
	roundTrip(t, &Detach{})
}

func TestRoundTripList(t *testing.T) {
	roundTrip(t, &List{})
}

func TestRoundTripKill(t *testing.T) {
	orig := &Kill{Name: "doomed-session"}
	got := roundTrip(t, orig).(*Kill)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripSend(t *testing.T) {
	orig := &Send{Name: "target", Data: []byte{0x1b, 0x5b, 0x41}}
	got := roundTrip(t, orig).(*Send)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripDump(t *testing.T) {
	orig := &Dump{Name: "sess", Format: 2}
	got := roundTrip(t, orig).(*Dump)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripOK(t *testing.T) {
	orig := &OK{
		SessionName: "my-session",
		Cols:        120,
		Rows:        40,
		PID:         12345,
		Created:     true,
	}
	got := roundTrip(t, orig).(*OK)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripError(t *testing.T) {
	orig := &Error{Code: 404, Message: "session not found"}
	got := roundTrip(t, orig).(*Error)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripErrorEmptyMessage(t *testing.T) {
	orig := &Error{Code: 500, Message: ""}
	got := roundTrip(t, orig).(*Error)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripOutput(t *testing.T) {
	orig := &Output{Data: []byte("\x1b[31mred\x1b[0m")}
	got := roundTrip(t, orig).(*Output)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripState(t *testing.T) {
	orig := &State{
		ScreenDump:        []byte("screen content here"),
		CursorRow:         10,
		CursorCol:         42,
		IsAlternateScreen: true,
	}
	got := roundTrip(t, orig).(*State)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripSessions(t *testing.T) {
	orig := &Sessions{
		Sessions: []Session{
			{Name: "s1", State: "running", Cols: 80, Rows: 24, PID: 100, CreatedAt: 1700000000},
			{Name: "s2", State: "idle", Cols: 120, Rows: 40, PID: 200, CreatedAt: 1700000001},
		},
	}
	got := roundTrip(t, orig).(*Sessions)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripSessionsEmpty(t *testing.T) {
	orig := &Sessions{Sessions: []Session{}}
	got := roundTrip(t, orig).(*Sessions)
	assert.DeepEqual(t, got, orig)
}

func TestRoundTripExited(t *testing.T) {
	for _, code := range []int32{0, 1, -1, 127, 255} {
		orig := &Exited{ExitCode: code}
		got := roundTrip(t, orig).(*Exited)
		assert.DeepEqual(t, got, orig)
	}
}

func TestRoundTripDumpResponse(t *testing.T) {
	orig := &DumpResponse{Data: []byte("dump data")}
	got := roundTrip(t, orig).(*DumpResponse)
	assert.DeepEqual(t, got, orig)
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
	got := roundTrip(t, orig).(*Output)
	assert.DeepEqual(t, got, orig)
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
