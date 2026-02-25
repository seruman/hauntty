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
		{"Create", &Create{Name: "my-session", Command: []string{"/bin/bash"}, Env: []string{"TERM=xterm-256color", "HOME=/home/user"}, CWD: "/home/user/project", Mode: CreateModeOpenOrCreate}},
		{"Attach", &Attach{Name: "my-session", Cols: 120, Rows: 40, Xpixel: 1920, Ypixel: 1080, ReadOnly: true, ClientTTY: "/dev/ttys003", AttachToken: "tok-1"}},
		{"Input", &Input{Data: []byte("hello world\n")}},
		{"InputEmpty", &Input{Data: []byte{}}},
		{"Resize", &Resize{Cols: 200, Rows: 50, Xpixel: 3200, Ypixel: 1600}},
		{"Detach", &Detach{Name: "sess", TargetClientID: "7", TargetTTY: "/dev/ttys003"}},
		{"DetachSelf", &Detach{}},
		{"List", &List{}},
		{"ListWithClients", &List{IncludeClients: true}},
		{"Kill", &Kill{Name: "doomed-session"}},
		{"Send", &Send{Name: "target", Data: []byte{0x1b, 0x5b, 0x41}}},
		{"Dump", &Dump{Name: "sess", Format: 2}},
		{"OK", &OK{}},
		{"Error", &Error{Message: "session not found"}},
		{"Output", &Output{Data: []byte("\x1b[31mred\x1b[0m")}},
		{"Created", &Created{SessionName: "my-session", PID: 12345, Outcome: CreateOutcomeCreated, AttachToken: "tok-1", AttachTokenExpiresAt: 1700000000000}},
		{"Attached", &Attached{SessionName: "my-session", Cols: 120, Rows: 40, PID: 12345, ClientID: "9", ScreenDump: []byte("screen content here"), CursorRow: 10, CursorCol: 42, IsAlternateScreen: true}},
		{"Sessions", &Sessions{Sessions: []Session{{Name: "s1", State: "running", Cols: 80, Rows: 24, PID: 100, CreatedAt: 1700000000, CWD: "/home/user/src", Clients: []ClientInfo{{ClientID: "1", TTY: "/dev/ttys001", ReadOnly: false, Version: "abc"}}}, {Name: "s2", State: "idle", Cols: 120, Rows: 40, PID: 200, CreatedAt: 1700000001, CWD: "", Clients: []ClientInfo{}}}}},
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
		{"StatusResponse", &StatusResponse{Daemon: DaemonStatus{PID: 12345, Uptime: 7980, SocketPath: "/tmp/hauntty-501/hauntty.sock", RunningCount: 3, DeadCount: 1, Version: "abc123def456"}, Session: &SessionStatus{Name: "curious-fox", State: "running", Cols: 120, Rows: 40, PID: 12389, CWD: "/home/user/project", Clients: []ClientInfo{{ClientID: "2", TTY: "/dev/ttys002", ReadOnly: true, Version: "abc123"}}}}},
		{"StatusResponseNoSession", &StatusResponse{Daemon: DaemonStatus{PID: 99, Uptime: 60, SocketPath: "/tmp/hauntty.sock", RunningCount: 0, DeadCount: 0}, Session: nil}},
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
	var serverRev string
	var serverErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		serverVer, serverRev, serverErr = serverConn.AcceptHandshake()
		if serverErr == nil {
			serverErr = serverConn.AcceptVersion(serverVer, serverRev)
		}
	}()

	accepted, rev, err := clientConn.Handshake(ProtocolVersion, "abc123")
	assert.NilError(t, err)
	assert.Equal(t, accepted, ProtocolVersion)
	assert.Equal(t, rev, "abc123")

	<-done
	assert.NilError(t, serverErr)
	assert.Equal(t, serverVer, ProtocolVersion)
	assert.Equal(t, serverRev, "abc123")
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
		_, _, _ = serverConn.AcceptHandshake()
		serverConn.AcceptVersion(0, "")
	}()

	accepted, _, err := clientConn.Handshake(ProtocolVersion, "abc123")
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
	data := make([]byte, 1<<16)
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
		&Create{Name: "s1", Command: []string{"bash"}, Env: []string{"A=1"}, CWD: "/tmp", Mode: CreateModeOpenOrCreate},
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
