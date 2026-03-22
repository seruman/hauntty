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
	message := &Created{
		Name: "new-session",
		PID:  12345,
	}

	got := roundTrip(t, message).(*Created)
	assert.DeepEqual(t, got, message)
}

func TestAttachedEncodeDecode(t *testing.T) {
	message := &Attached{
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

	got := roundTrip(t, message).(*Attached)
	assert.DeepEqual(t, got, message)
}

func TestSessionsEncodeDecode(t *testing.T) {
	message := &Sessions{
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

	got := roundTrip(t, message).(*Sessions)
	assert.DeepEqual(t, got, message)
}

func TestClientsChangedEncodeDecode(t *testing.T) {
	message := &ClientsChanged{
		Count: 3,
		Cols:  120,
		Rows:  40,
	}

	got := roundTrip(t, message).(*ClientsChanged)
	assert.DeepEqual(t, got, message)
}

func TestStatusResponseEncodeDecode(t *testing.T) {
	message := &StatusResponse{
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

	got := roundTrip(t, message).(*StatusResponse)
	assert.DeepEqual(t, got, message)
}

func TestStatusResponseNilSession(t *testing.T) {
	message := &StatusResponse{
		Daemon: DaemonStatus{
			PID:    999,
			Uptime: 60,
		},
	}

	got := roundTrip(t, message).(*StatusResponse)
	assert.DeepEqual(t, got, message)
}
