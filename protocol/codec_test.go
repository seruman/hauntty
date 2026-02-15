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
			Xpixel:          1920,
			Ypixel:          1080,
			Command:         []string{"/bin/bash"},
			Env:             []string{"TERM=xterm-256color", "HOME=/home/user"},
			ScrollbackLines: 10000,
			CWD:             "/home/user/project",
		}},
		{"AttachEmptyEnv", &Attach{
			Name:    "s",
			Cols:    80,
			Rows:    24,
			Command: []string{"sh"},
			Env:     []string{},
		}},
		{"AttachMultiWordCommand", &Attach{
			Name:    "test",
			Cols:    80,
			Rows:    24,
			Command: []string{"bash", "-c", "echo hello"},
			Env:     []string{},
		}},
		{"AttachEmptyCommand", &Attach{
			Name:    "test",
			Cols:    80,
			Rows:    24,
			Command: []string{},
			Env:     []string{},
		}},
		{"Input", &Input{Data: []byte("hello world\n")}},
		{"InputEmpty", &Input{Data: []byte{}}},
		{"Resize", &Resize{Cols: 200, Rows: 50, Xpixel: 3200, Ypixel: 1600}},
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
				{Name: "s1", State: "running", Cols: 80, Rows: 24, PID: 100, CreatedAt: 1700000000, CWD: "/home/user/src"},
				{Name: "s2", State: "idle", Cols: 120, Rows: 40, PID: 200, CreatedAt: 1700000001, CWD: ""},
			},
		}},
		{"SessionsEmpty", &Sessions{Sessions: []Session{}}},
		{"Exited/0", &Exited{ExitCode: 0}},
		{"Exited/1", &Exited{ExitCode: 1}},
		{"Exited/-1", &Exited{ExitCode: -1}},
		{"Exited/127", &Exited{ExitCode: 127}},
		{"Exited/255", &Exited{ExitCode: 255}},
		{"DumpResponse", &DumpResponse{Data: []byte("dump data")}},
		{"PruneResponse", &PruneResponse{Count: 3}},
		{"ClientsChanged", &ClientsChanged{Count: 2, Cols: 80, Rows: 24}},
		{"ClientsChangedSingle", &ClientsChanged{Count: 1, Cols: 120, Rows: 40}},
		{"Status", &Status{SessionName: "my-session"}},
		{"StatusEmpty", &Status{SessionName: ""}},
		{"StatusResponse", &StatusResponse{
			Daemon: DaemonStatus{
				PID:          12345,
				Uptime:       7980,
				SocketPath:   "/tmp/hauntty-501/hauntty.sock",
				RunningCount: 3,
				DeadCount:    1,
			},
			Session: &SessionStatus{
				Name:        "curious-fox",
				State:       "running",
				Cols:        120,
				Rows:        40,
				PID:         12389,
				CWD:         "/home/user/project",
				ClientCount: 2,
			},
		}},
		{"StatusResponseNoSession", &StatusResponse{
			Daemon: DaemonStatus{
				PID:          99,
				Uptime:       60,
				SocketPath:   "/tmp/hauntty.sock",
				RunningCount: 0,
				DeadCount:    0,
			},
			Session: nil,
		}},
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

func TestHandshake(t *testing.T) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()

	clientConn := NewConn(struct {
		io.Reader
		io.Writer
	}{cr, cw})
	serverConn := NewConn(struct {
		io.Reader
		io.Writer
	}{sr, sw})

	var serverVer uint8
	var serverErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		serverVer, serverErr = serverConn.AcceptHandshake()
		if serverErr == nil {
			serverErr = serverConn.AcceptVersion(serverVer)
		}
	}()

	accepted, err := clientConn.Handshake(ProtocolVersion)
	assert.NilError(t, err)
	assert.Equal(t, accepted, ProtocolVersion)

	<-done
	assert.NilError(t, serverErr)
	assert.Equal(t, serverVer, ProtocolVersion)
}

func TestHandshakeVersionMismatch(t *testing.T) {
	cr, sw := io.Pipe()
	sr, cw := io.Pipe()

	clientConn := NewConn(struct {
		io.Reader
		io.Writer
	}{cr, cw})
	serverConn := NewConn(struct {
		io.Reader
		io.Writer
	}{sr, sw})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = serverConn.AcceptHandshake()
		// Reject: send version 0.
		serverConn.AcceptVersion(0)
	}()

	accepted, err := clientConn.Handshake(ProtocolVersion)
	assert.NilError(t, err)
	assert.Equal(t, accepted, uint8(0))

	<-done
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
	assert.Error(t, err, "unknown message type: 0xff")
}

func TestTruncatedFrame(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	err := enc.WriteU32(100)
	assert.NilError(t, err)
	buf.Write([]byte{0x01, 0x02, 0x03, 0x04, 0x05})

	c := NewConn(&buf)
	_, err = c.ReadMessage()
	assert.Error(t, err, "unexpected EOF")
}

func TestEmptyFrame(t *testing.T) {
	var buf bytes.Buffer
	enc := NewEncoder(&buf)
	err := enc.WriteU32(0)
	assert.NilError(t, err)

	c := NewConn(&buf)
	_, err = c.ReadMessage()
	assert.Error(t, err, "empty message frame")
}

func TestReadMessageEOF(t *testing.T) {
	buf := &bytes.Buffer{}
	c := NewConn(buf)
	_, err := c.ReadMessage()
	assert.Error(t, err, "EOF")
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
		&Attach{Name: "s1", Cols: 80, Rows: 24, Command: []string{"bash"}, Env: []string{"A=1"}, ScrollbackLines: 1000, CWD: "/tmp"},
		&Input{Data: []byte("ls\n")},
		&Output{Data: []byte("file1\nfile2\n")},
		&Resize{Cols: 100, Rows: 50, Xpixel: 1600, Ypixel: 800},
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
