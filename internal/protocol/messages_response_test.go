package protocol

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestResponseMessageTypes(t *testing.T) {
	tests := []struct {
		name    string
		message Message
		want    MessageType
	}{
		{"OK", &OK{}, TypeOK},
		{"Error", &Error{}, TypeError},
		{"Output", &Output{}, TypeOutput},
		{"Attached", &Attached{}, TypeAttached},
		{"Sessions", &Sessions{}, TypeSessions},
		{"Exited", &Exited{}, TypeExited},
		{"DumpResponse", &DumpResponse{}, TypeDumpResponse},
		{"PruneResponse", &PruneResponse{}, TypePruneResponse},
		{"ClientsChanged", &ClientsChanged{}, TypeClientsChanged},
		{"StatusResponse", &StatusResponse{}, TypeStatusResponse},
		{"Created", &Created{}, TypeCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.message.Type(), tt.want)
		})
	}
}

func TestCreatedEncodeDecode(t *testing.T) {
	msg := &Created{
		Name: "new-session",
		PID:  12345,
	}
	got := roundTrip(t, msg).(*Created)
	assert.Equal(t, got.Name, msg.Name)
	assert.Equal(t, got.PID, msg.PID)
}

func TestAttachedEncodeDecode(t *testing.T) {
	msg := &Attached{
		Name:       "session-1",
		PID:        42,
		ClientID:   "client-abc",
		Cols:       80,
		Rows:       24,
		ScreenDump: []byte("hello world"),
		CursorRow:  5,
		CursorCol:  10,
		AltScreen:  false,
		Created:    true,
	}
	got := roundTrip(t, msg).(*Attached)
	assert.Equal(t, got.Name, msg.Name)
	assert.Equal(t, got.PID, msg.PID)
	assert.Equal(t, got.ClientID, msg.ClientID)
	assert.Equal(t, got.Cols, msg.Cols)
	assert.Equal(t, got.Rows, msg.Rows)
	assert.DeepEqual(t, got.ScreenDump, msg.ScreenDump)
	assert.Equal(t, got.CursorRow, msg.CursorRow)
	assert.Equal(t, got.CursorCol, msg.CursorCol)
	assert.Equal(t, got.AltScreen, msg.AltScreen)
	assert.Equal(t, got.Created, msg.Created)
}

func TestSessionsEncodeDecode(t *testing.T) {
	msg := &Sessions{
		Sessions: []Session{
			{
				Name:      "s1",
				State:     SessionStateRunning,
				Cols:      80,
				Rows:      24,
				PID:       100,
				CreatedAt: 1000,
				SavedAt:   0,
				CWD:       "/home/user",
				Clients: []SessionClient{
					{ClientID: "c1", ReadOnly: false, Version: "abc123"},
				},
			},
			{
				Name:      "s2",
				State:     SessionStateDead,
				Cols:      0,
				Rows:      0,
				PID:       0,
				CreatedAt: 900,
				SavedAt:   950,
				CWD:       "/tmp",
				Clients:   []SessionClient{},
			},
		},
	}
	got := roundTrip(t, msg).(*Sessions)
	assert.Equal(t, len(got.Sessions), 2)
	assert.Equal(t, got.Sessions[0].Name, "s1")
	assert.Equal(t, got.Sessions[0].State, SessionStateRunning)
	assert.Equal(t, got.Sessions[0].PID, uint32(100))
	assert.Equal(t, len(got.Sessions[0].Clients), 1)
	assert.Equal(t, got.Sessions[0].Clients[0].ClientID, "c1")
	assert.Equal(t, got.Sessions[1].Name, "s2")
	assert.Equal(t, got.Sessions[1].State, SessionStateDead)
}

func TestClientsChangedEncodeDecode(t *testing.T) {
	msg := &ClientsChanged{
		Count: 3,
		Cols:  120,
		Rows:  40,
	}
	got := roundTrip(t, msg).(*ClientsChanged)
	assert.Equal(t, got.Count, msg.Count)
	assert.Equal(t, got.Cols, msg.Cols)
	assert.Equal(t, got.Rows, msg.Rows)
}

func TestStatusResponseEncodeDecode(t *testing.T) {
	msg := &StatusResponse{
		Daemon: DaemonStatus{
			PID:          1234,
			Uptime:       3600,
			SocketPath:   "/tmp/hauntty.sock",
			RunningCount: 2,
			DeadCount:    1,
			Version:      "abc123",
		},
		Session: &SessionStatus{
			Name:  "s1",
			State: SessionStateRunning,
			Cols:  80,
			Rows:  24,
			PID:   5678,
			CWD:   "/home/user",
			Clients: []SessionClient{
				{ClientID: "c1", ReadOnly: false, Version: "abc123"},
			},
		},
	}
	got := roundTrip(t, msg).(*StatusResponse)
	assert.Equal(t, got.Daemon.PID, msg.Daemon.PID)
	assert.Equal(t, got.Daemon.Uptime, msg.Daemon.Uptime)
	assert.Equal(t, got.Daemon.SocketPath, msg.Daemon.SocketPath)
	assert.Equal(t, got.Session.Name, msg.Session.Name)
	assert.Equal(t, got.Session.PID, msg.Session.PID)
	assert.Equal(t, len(got.Session.Clients), 1)
}

func TestStatusResponseNilSession(t *testing.T) {
	msg := &StatusResponse{
		Daemon: DaemonStatus{
			PID:    999,
			Uptime: 60,
		},
	}
	got := roundTrip(t, msg).(*StatusResponse)
	assert.Equal(t, got.Daemon.PID, uint32(999))
	assert.Assert(t, got.Session == nil)
}
