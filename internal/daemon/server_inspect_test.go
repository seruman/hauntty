package daemon

import (
	"bytes"
	"os"
	"slices"
	"testing"
	"time"

	hauntty "code.selman.me/hauntty"
	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
	"gotest.tools/v3/assert"
)

func TestHandleListIncludesLiveAndDeadSessions(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeDeadSessionState(t, "dead", &sessionState{
		Cols:      100,
		Rows:      40,
		SavedAt:   time.Unix(1700000100, 0),
		CursorRow: 1,
		CursorCol: 1,
		VT:        []byte("saved"),
	})

	live := newSessionLoopHarness(t)
	live.Name = "live"
	_, err := live.attach(t.Context(), sessionAttachSpec{
		conn:      protocol.NewConn(discardRW{}),
		closeConn: func() error { return nil },
		size:      termSize{cols: 80, rows: 24},
		version:   "client-v1",
		readOnly:  true,
	})
	assert.NilError(t, err)

	srv := &Server{
		ctx:       t.Context(),
		sessions:  map[string]*Session{"live": live},
		persister: &persister{},
	}

	var out bytes.Buffer
	srv.handleList(protocol.NewConn(&out), &protocol.List{IncludeClients: true})

	msg := readServerMessage(t, &out)
	got, ok := msg.(*protocol.Sessions)
	assert.Equal(t, ok, true)
	slices.SortFunc(got.Sessions, func(a, b protocol.Session) int {
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	assert.Equal(t, len(got.Sessions), 2)
	assert.Equal(t, got.Sessions[0].Name, "dead")
	assert.Equal(t, got.Sessions[0].State, protocol.SessionStateDead)
	assert.Equal(t, got.Sessions[0].Cols, uint16(100))
	assert.Equal(t, got.Sessions[0].Rows, uint16(40))
	assert.Equal(t, got.Sessions[0].PID, uint32(0))
	assert.Equal(t, got.Sessions[0].CreatedAt, uint32(0))
	assert.Equal(t, got.Sessions[0].SavedAt, uint32(1700000100))
	assert.Equal(t, got.Sessions[0].CWD, "")
	assert.DeepEqual(t, got.Sessions[0].Clients, []protocol.SessionClient{})
	assert.Equal(t, got.Sessions[1].Name, "live")
	assert.Equal(t, got.Sessions[1].State, protocol.SessionStateRunning)
	assert.Equal(t, got.Sessions[1].Cols, uint16(80))
	assert.Equal(t, got.Sessions[1].Rows, uint16(24))
	assert.Equal(t, got.Sessions[1].PID, live.PID)
	assert.Equal(t, got.Sessions[1].CreatedAt, uint32(live.CreatedAt.Unix()))
	assert.Equal(t, got.Sessions[1].SavedAt, uint32(0))
	assert.Equal(t, got.Sessions[1].CWD, "")
	assert.DeepEqual(t, got.Sessions[1].Clients, []protocol.SessionClient{{ClientID: "1", ReadOnly: true, Version: "client-v1"}})
}

func TestHandleDumpReturnsDeadSession(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeDeadSessionState(t, "dead", &sessionState{
		Cols:      80,
		Rows:      24,
		SavedAt:   time.Unix(1700000200, 0),
		CursorRow: 1,
		CursorCol: 1,
		VT:        []byte("hello\nworld\n"),
	})

	cfg := config.Default()
	cfg.Daemon.StatePersistence = true
	srv, err := New(t.Context(), &cfg.Daemon, cfg.Session.ResizePolicy)
	assert.NilError(t, err)
	defer srv.Shutdown()

	expected, exists, err := srv.dumpDeadSession("dead", protocol.DumpPlain)
	assert.NilError(t, err)
	assert.Equal(t, exists, true)

	var out bytes.Buffer
	srv.handleDump(protocol.NewConn(&out), &protocol.Dump{Name: "dead", Format: protocol.DumpPlain})

	msg := readServerMessage(t, &out)
	got, ok := msg.(*protocol.DumpResponse)
	assert.Equal(t, ok, true)
	assert.DeepEqual(t, got, &protocol.DumpResponse{Data: expected})
}

func TestHandleStatusCountsDeadSessionsAndReturnsSession(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	writeDeadSessionState(t, "dead", &sessionState{
		Cols:      90,
		Rows:      30,
		SavedAt:   time.Unix(1700000300, 0),
		CursorRow: 1,
		CursorCol: 1,
		VT:        []byte("saved"),
	})

	live := newSessionLoopHarness(t)
	live.Name = "live"

	srv := &Server{
		ctx:        t.Context(),
		sessions:   map[string]*Session{"live": live},
		persister:  &persister{},
		socketPath: "/tmp/hauntty.sock",
		startedAt:  time.Now(),
	}

	var out bytes.Buffer
	srv.handleStatus(protocol.NewConn(&out), &protocol.Status{Name: "live"})

	msg := readServerMessage(t, &out)
	got, ok := msg.(*protocol.StatusResponse)
	assert.Equal(t, ok, true)
	assert.DeepEqual(t, got, &protocol.StatusResponse{
		Daemon: protocol.DaemonStatus{
			PID:          uint32(os.Getpid()),
			Uptime:       0,
			SocketPath:   "/tmp/hauntty.sock",
			RunningCount: 1,
			DeadCount:    1,
			Version:      hauntty.Version(),
		},
		Session: &protocol.SessionStatus{
			Name:    "live",
			State:   protocol.SessionStateRunning,
			Cols:    80,
			Rows:    24,
			PID:     live.PID,
			CWD:     "",
			Clients: []protocol.SessionClient{},
		},
	})
}

func TestDumpFormatMapping(t *testing.T) {
	tests := []struct {
		name   string
		input  protocol.DumpFormat
		expect libghostty.DumpFormat
	}{
		{"plain", protocol.DumpPlain, libghostty.DumpPlain},
		{"vt", protocol.DumpVT, libghostty.DumpVTSafe},
		{"html", protocol.DumpHTML, libghostty.DumpHTML},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dumpFormat(tt.input)
			assert.Equal(t, got, tt.expect)
		})
	}
}
