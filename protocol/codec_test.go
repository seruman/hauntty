package protocol

import (
	"bytes"
	"io"
	"testing"
)

func roundTrip(t *testing.T, msg Message) Message {
	t.Helper()
	var buf bytes.Buffer
	c := NewConn(&buf)
	if err := c.WriteMessage(msg); err != nil {
		t.Fatalf("WriteMessage(%T): %v", msg, err)
	}
	got, err := c.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage(%T): %v", msg, err)
	}
	if got.Type() != msg.Type() {
		t.Fatalf("type mismatch: got 0x%02x, want 0x%02x", got.Type(), msg.Type())
	}
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
	if got.Name != orig.Name || got.Cols != orig.Cols || got.Rows != orig.Rows ||
		got.Command != orig.Command || got.ScrollbackLines != orig.ScrollbackLines {
		t.Errorf("Attach mismatch: got %+v, want %+v", got, orig)
	}
	if len(got.Env) != len(orig.Env) {
		t.Fatalf("Env length: got %d, want %d", len(got.Env), len(orig.Env))
	}
	for i := range orig.Env {
		if got.Env[i] != orig.Env[i] {
			t.Errorf("Env[%d]: got %q, want %q", i, got.Env[i], orig.Env[i])
		}
	}
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
	if len(got.Env) != 0 {
		t.Errorf("expected empty env, got %v", got.Env)
	}
}

func TestRoundTripInput(t *testing.T) {
	orig := &Input{Data: []byte("hello world\n")}
	got := roundTrip(t, orig).(*Input)
	if !bytes.Equal(got.Data, orig.Data) {
		t.Errorf("Input.Data: got %q, want %q", got.Data, orig.Data)
	}
}

func TestRoundTripInputEmpty(t *testing.T) {
	orig := &Input{Data: []byte{}}
	got := roundTrip(t, orig).(*Input)
	if len(got.Data) != 0 {
		t.Errorf("expected empty data, got %q", got.Data)
	}
}

func TestRoundTripResize(t *testing.T) {
	orig := &Resize{Cols: 200, Rows: 50}
	got := roundTrip(t, orig).(*Resize)
	if got.Cols != orig.Cols || got.Rows != orig.Rows {
		t.Errorf("Resize: got %+v, want %+v", got, orig)
	}
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
	if got.Name != orig.Name {
		t.Errorf("Kill.Name: got %q, want %q", got.Name, orig.Name)
	}
}

func TestRoundTripSend(t *testing.T) {
	orig := &Send{Name: "target", Data: []byte{0x1b, 0x5b, 0x41}}
	got := roundTrip(t, orig).(*Send)
	if got.Name != orig.Name || !bytes.Equal(got.Data, orig.Data) {
		t.Errorf("Send: got %+v, want %+v", got, orig)
	}
}

func TestRoundTripDump(t *testing.T) {
	orig := &Dump{Name: "sess", Format: 2}
	got := roundTrip(t, orig).(*Dump)
	if got.Name != orig.Name || got.Format != orig.Format {
		t.Errorf("Dump: got %+v, want %+v", got, orig)
	}
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
	if got.SessionName != orig.SessionName || got.Cols != orig.Cols ||
		got.Rows != orig.Rows || got.PID != orig.PID || got.Created != orig.Created {
		t.Errorf("OK: got %+v, want %+v", got, orig)
	}
}

func TestRoundTripError(t *testing.T) {
	orig := &Error{Code: 404, Message: "session not found"}
	got := roundTrip(t, orig).(*Error)
	if got.Code != orig.Code || got.Message != orig.Message {
		t.Errorf("Error: got %+v, want %+v", got, orig)
	}
}

func TestRoundTripErrorEmptyMessage(t *testing.T) {
	orig := &Error{Code: 500, Message: ""}
	got := roundTrip(t, orig).(*Error)
	if got.Code != orig.Code || got.Message != "" {
		t.Errorf("Error empty msg: got %+v, want %+v", got, orig)
	}
}

func TestRoundTripOutput(t *testing.T) {
	orig := &Output{Data: []byte("\x1b[31mred\x1b[0m")}
	got := roundTrip(t, orig).(*Output)
	if !bytes.Equal(got.Data, orig.Data) {
		t.Errorf("Output.Data: got %q, want %q", got.Data, orig.Data)
	}
}

func TestRoundTripState(t *testing.T) {
	orig := &State{
		ScreenDump:        []byte("screen content here"),
		CursorRow:         10,
		CursorCol:         42,
		IsAlternateScreen: true,
	}
	got := roundTrip(t, orig).(*State)
	if !bytes.Equal(got.ScreenDump, orig.ScreenDump) || got.CursorRow != orig.CursorRow ||
		got.CursorCol != orig.CursorCol || got.IsAlternateScreen != orig.IsAlternateScreen {
		t.Errorf("State: got %+v, want %+v", got, orig)
	}
}

func TestRoundTripSessions(t *testing.T) {
	orig := &Sessions{
		Sessions: []Session{
			{Name: "s1", State: "running", Cols: 80, Rows: 24, PID: 100, CreatedAt: 1700000000},
			{Name: "s2", State: "idle", Cols: 120, Rows: 40, PID: 200, CreatedAt: 1700000001},
		},
	}
	got := roundTrip(t, orig).(*Sessions)
	if len(got.Sessions) != len(orig.Sessions) {
		t.Fatalf("Sessions length: got %d, want %d", len(got.Sessions), len(orig.Sessions))
	}
	for i, s := range orig.Sessions {
		g := got.Sessions[i]
		if g != s {
			t.Errorf("Sessions[%d]: got %+v, want %+v", i, g, s)
		}
	}
}

func TestRoundTripSessionsEmpty(t *testing.T) {
	orig := &Sessions{Sessions: []Session{}}
	got := roundTrip(t, orig).(*Sessions)
	if len(got.Sessions) != 0 {
		t.Errorf("expected empty sessions, got %d", len(got.Sessions))
	}
}

func TestRoundTripExited(t *testing.T) {
	for _, code := range []int32{0, 1, -1, 127, 255} {
		orig := &Exited{ExitCode: code}
		got := roundTrip(t, orig).(*Exited)
		if got.ExitCode != orig.ExitCode {
			t.Errorf("Exited.ExitCode: got %d, want %d", got.ExitCode, orig.ExitCode)
		}
	}
}

func TestRoundTripDumpResponse(t *testing.T) {
	orig := &DumpResponse{Data: []byte("dump data")}
	got := roundTrip(t, orig).(*DumpResponse)
	if !bytes.Equal(got.Data, orig.Data) {
		t.Errorf("DumpResponse.Data: got %q, want %q", got.Data, orig.Data)
	}
}

func TestHandshakeSuccess(t *testing.T) {
	clientBuf := &bytes.Buffer{}
	serverBuf := &bytes.Buffer{}

	// Client writes version to clientBuf.
	client := NewConn(clientBuf)
	enc := NewEncoder(clientBuf)
	if err := enc.WriteU8(ProtocolVersion); err != nil {
		t.Fatal(err)
	}

	// Server reads from clientBuf.
	_ = client // suppress unused
	server := NewConn(clientBuf)
	version, err := server.AcceptHandshake()
	if err != nil {
		t.Fatal(err)
	}
	if version != ProtocolVersion {
		t.Errorf("version: got %d, want %d", version, ProtocolVersion)
	}

	// Server writes accepted version to serverBuf.
	serverConn := NewConn(serverBuf)
	if err := serverConn.AcceptVersion(version); err != nil {
		t.Fatal(err)
	}

	// Client reads accepted version from serverBuf.
	clientConn := NewConn(serverBuf)
	dec := NewDecoder(serverBuf)
	_ = clientConn
	accepted, err := dec.ReadU8()
	if err != nil {
		t.Fatal(err)
	}
	if accepted != ProtocolVersion {
		t.Errorf("accepted version: got %d, want %d", accepted, ProtocolVersion)
	}
}

func TestHandshakeRoundTrip(t *testing.T) {
	// Simulate full handshake using a pipe-like approach with two buffers.
	// Client sends version, server reads it, server sends accepted, client reads it.
	wire := &bytes.Buffer{}

	// Client side: send version.
	clientConn := NewConn(wire)
	enc := NewEncoder(wire)
	if err := enc.WriteU8(ProtocolVersion); err != nil {
		t.Fatal(err)
	}

	// Server side: read version.
	_ = clientConn
	serverConn := NewConn(wire)
	version, err := serverConn.AcceptHandshake()
	if err != nil {
		t.Fatal(err)
	}
	if version != ProtocolVersion {
		t.Fatalf("server got version %d, want %d", version, ProtocolVersion)
	}

	// Server writes accepted version back to wire.
	if err := serverConn.AcceptVersion(version); err != nil {
		t.Fatal(err)
	}

	// Client reads accepted version.
	dec := NewDecoder(wire)
	accepted, err := dec.ReadU8()
	if err != nil {
		t.Fatal(err)
	}
	if accepted != ProtocolVersion {
		t.Errorf("client got accepted version %d, want %d", accepted, ProtocolVersion)
	}
}

func TestUnknownMessageType(t *testing.T) {
	// Write a frame with an unknown message type.
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	// Length: 1 byte (just the type byte, no payload).
	if err := enc.WriteU32(1); err != nil {
		t.Fatal(err)
	}
	if err := enc.WriteU8(0xFF); err != nil {
		t.Fatal(err)
	}

	c := NewConn(&buf)
	_, err := c.ReadMessage()
	if err == nil {
		t.Fatal("expected error for unknown message type")
	}
}

func TestTruncatedFrame(t *testing.T) {
	// Write a length header claiming 100 bytes but provide only 5.
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteU32(100); err != nil {
		t.Fatal(err)
	}
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04, 0x05})

	c := NewConn(&buf)
	_, err := c.ReadMessage()
	if err == nil {
		t.Fatal("expected error for truncated frame")
	}
}

func TestEmptyFrame(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	if err := enc.WriteU32(0); err != nil {
		t.Fatal(err)
	}

	c := NewConn(&buf)
	_, err := c.ReadMessage()
	if err == nil {
		t.Fatal("expected error for empty frame")
	}
}

func TestReadMessageEOF(t *testing.T) {
	buf := &bytes.Buffer{}
	c := NewConn(buf)
	_, err := c.ReadMessage()
	if err != io.EOF && err != io.ErrUnexpectedEOF {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestLargePayload(t *testing.T) {
	data := make([]byte, 1<<16) // 64KB
	for i := range data {
		data[i] = byte(i % 256)
	}
	orig := &Output{Data: data}
	got := roundTrip(t, orig).(*Output)
	if !bytes.Equal(got.Data, orig.Data) {
		t.Error("large payload round-trip failed")
	}
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
		if err := c.WriteMessage(msg); err != nil {
			t.Fatalf("WriteMessage(%T): %v", msg, err)
		}
	}

	for i, orig := range msgs {
		got, err := c.ReadMessage()
		if err != nil {
			t.Fatalf("ReadMessage %d: %v", i, err)
		}
		if got.Type() != orig.Type() {
			t.Errorf("message %d type: got 0x%02x, want 0x%02x", i, got.Type(), orig.Type())
		}
	}
}
