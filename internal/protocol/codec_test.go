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
		{"Create", &Create{
			Name:       "my-session",
			Command:    []string{"/bin/bash", "-l"},
			Env:        []string{"TERM=xterm-256color"},
			CWD:        "/home/user",
			Scrollback: 5000,
			Force:      false,
		}},
		{"CreateForce", &Create{
			Name:       "foo",
			Command:    []string{"sh"},
			Env:        []string{},
			CWD:        "",
			Scrollback: 0,
			Force:      true,
		}},
		{"CreateEmpty", &Create{
			Name:    "",
			Command: []string{},
			Env:     []string{},
		}},
		{"Attach", &Attach{
			Name:       "my-session",
			Command:    []string{"/bin/bash"},
			Env:        []string{"TERM=xterm-256color", "HOME=/home/user"},
			CWD:        "/home/user/project",
			Cols:       120,
			Rows:       40,
			Xpixel:     1920,
			Ypixel:     1080,
			ReadOnly:   false,
			Restore:    false,
			Scrollback: 10000,
		}},
		{"AttachRestore", &Attach{
			Name:    "saved-session",
			Command: []string{},
			Env:     []string{},
			Cols:    80,
			Rows:    24,
			Restore: true,
		}},
		{"AttachReadOnly", &Attach{
			Name:     "s",
			Command:  []string{},
			Env:      []string{},
			Cols:     80,
			Rows:     24,
			ReadOnly: true,
		}},
		{"AttachEmptyCommand", &Attach{
			Name:    "test",
			Command: []string{},
			Env:     []string{},
			Cols:    80,
			Rows:    24,
		}},
		{"Input", &Input{Data: []byte("hello world\n")}},
		{"InputEmpty", &Input{Data: []byte{}}},
		{"Resize", &Resize{Cols: 200, Rows: 50, Xpixel: 3200, Ypixel: 1600}},
		{"Detach", &Detach{}},
		{"List", &List{IncludeClients: false}},
		{"ListWithClients", &List{IncludeClients: true}},
		{"Kill", &Kill{Name: "doomed-session"}},
		{"Send", &Send{Name: "target", Data: []byte{0x1b, 0x5b, 0x41}}},
		{"SendKey", &SendKey{Name: "s", Key: 65, Mods: 3}},
		{"Dump", &Dump{Name: "sess", Format: 2}},
		{"Prune", &Prune{}},
		{"Kick", &Kick{Name: "foo", ClientID: "42"}},
		{"Status", &Status{Name: "my-session"}},
		{"StatusEmpty", &Status{Name: ""}},
		{"OK", &OK{}},
		{"Error", &Error{Message: "session not found"}},
		{"ErrorEmpty", &Error{Message: ""}},
		{"Output", &Output{Data: []byte("\x1b[31mred\x1b[0m")}},
		{"Created", &Created{Name: "my-session", PID: 12345}},
		{"Attached", &Attached{
			Name:       "my-session",
			PID:        12345,
			ClientID:   "7",
			Cols:       120,
			Rows:       40,
			ScreenDump: []byte("screen content here"),
			CursorRow:  10,
			CursorCol:  42,
			AltScreen:  true,
			Created:    false,
		}},
		{"AttachedCreated", &Attached{
			Name:       "new-session",
			PID:        99,
			ClientID:   "1",
			Cols:       80,
			Rows:       24,
			ScreenDump: []byte{},
			CursorRow:  0,
			CursorCol:  0,
			AltScreen:  false,
			Created:    true,
		}},
		{"Sessions", &Sessions{
			Sessions: []Session{
				{Name: "s1", State: "running", Cols: 80, Rows: 24, PID: 100, CreatedAt: 1700000000, CWD: "/home/user/src", Clients: []SessionClient{}},
				{Name: "s2", State: "dead", Cols: 120, Rows: 40, PID: 200, CreatedAt: 1700000001, CWD: "", Clients: []SessionClient{}},
			},
		}},
		{"SessionsWithClients", &Sessions{
			Sessions: []Session{
				{
					Name: "s1", State: "running", Cols: 80, Rows: 24, PID: 100, CreatedAt: 1700000000, CWD: "/tmp",
					Clients: []SessionClient{
						{ClientID: "1", ReadOnly: false, Version: "abc123"},
						{ClientID: "2", ReadOnly: true, Version: "def456"},
					},
				},
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
		{"StatusResponse", &StatusResponse{
			Daemon: DaemonStatus{
				PID:          12345,
				Uptime:       7980,
				SocketPath:   "/tmp/hauntty-501/hauntty.sock",
				RunningCount: 3,
				DeadCount:    1,
				Version:      "abc123def456",
			},
			Session: &SessionStatus{
				Name:  "curious-fox",
				State: "running",
				Cols:  120,
				Rows:  40,
				PID:   12389,
				CWD:   "/home/user/project",
				Clients: []SessionClient{
					{ClientID: "1", ReadOnly: false, Version: "abc123def456"},
					{ClientID: "2", ReadOnly: true, Version: "abc123def456"},
				},
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
		{"StatusResponseNoClients", &StatusResponse{
			Daemon: DaemonStatus{
				PID:     1,
				Uptime:  1,
				Version: "v1",
			},
			Session: &SessionStatus{
				Name:    "s",
				State:   "running",
				Clients: []SessionClient{},
			},
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
		&Create{Name: "s1", Command: []string{"bash"}, Env: []string{"A=1"}, CWD: "/tmp", Scrollback: 1000},
		&Attach{Name: "s1", Command: []string{"bash"}, Env: []string{"A=1"}, CWD: "/tmp", Cols: 80, Rows: 24, Scrollback: 1000},
		&Input{Data: []byte("ls\n")},
		&Output{Data: []byte("file1\nfile2\n")},
		&Resize{Cols: 100, Rows: 50, Xpixel: 1600, Ypixel: 800},
		&Detach{},
		&Kick{Name: "s1", ClientID: "3"},
	}

	for _, msg := range msgs {
		err := c.WriteMessage(msg)
		assert.NilError(t, err)
	}

	for _, orig := range msgs {
		got, err := c.ReadMessage()
		assert.NilError(t, err)
		assert.DeepEqual(t, got, orig)
	}
}
