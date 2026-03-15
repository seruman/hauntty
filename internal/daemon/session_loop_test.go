package daemon

import (
	"io"
	"sync/atomic"
	"testing"
	"time"

	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/protocol"
	"code.selman.me/hauntty/libghostty"
	"github.com/creack/pty"
	"gotest.tools/v3/assert"
)

type discardRW struct{}

func (discardRW) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (discardRW) Write(p []byte) (int, error) {
	return len(p), nil
}

func newSessionLoopHarness(t *testing.T) *Session {
	ctx := t.Context()
	rt, err := libghostty.NewRuntime(ctx)
	assert.NilError(t, err)
	t.Cleanup(func() {
		assert.NilError(t, rt.Close())
	})

	term, err := rt.NewTerminal(ctx, 80, 24, 0)
	assert.NilError(t, err)

	ptmx, tty, err := pty.Open()
	assert.NilError(t, err)

	s := &Session{
		Name:         "demo",
		PID:          999999999,
		CreatedAt:    time.Unix(1700000000, 0),
		ptmx:         ptmx,
		term:         term,
		feedCh:       make(chan feedItem, 64),
		actions:      make(chan sessionAction, 16),
		ptyOut:       make(chan []byte, 64),
		done:         make(chan struct{}),
		resizePolicy: config.ResizePolicySmallest,
		ctx:          ctx,
	}
	s.setSize(80, 24)

	go s.feedLoop(ctx)
	go s.run()

	t.Cleanup(func() {
		select {
		case s.actions <- stopReq{}:
		case <-s.done:
		}
		<-s.done
		assert.NilError(t, ptmx.Close())
		assert.NilError(t, tty.Close())
		assert.NilError(t, term.Close(ctx))
	})

	return s
}

func TestSessionClientInfoTracksAttachAndDetach(t *testing.T) {
	s := newSessionLoopHarness(t)

	client, err := s.attach(t.Context(), sessionAttachSpec{
		conn:      protocol.NewConn(discardRW{}),
		closeConn: func() error { return nil },
		size:      termSize{cols: 80, rows: 24},
		version:   "client-v1",
		readOnly:  true,
	})
	assert.NilError(t, err)
	assert.Equal(t, client == nil, false)

	info := s.clientInfo()
	assert.DeepEqual(t, info, []protocol.SessionClient{{ClientID: "1", ReadOnly: true, Version: "client-v1"}})

	s.detachClient(client)

	info = s.clientInfo()
	assert.DeepEqual(t, info, []protocol.SessionClient{})
}

func TestSessionKickClientClosesConnectionAndRemovesClient(t *testing.T) {
	s := newSessionLoopHarness(t)

	var closeCount atomic.Int32
	_, err := s.attach(t.Context(), sessionAttachSpec{
		conn: protocol.NewConn(discardRW{}),
		closeConn: func() error {
			closeCount.Add(1)
			return nil
		},
		size:     termSize{cols: 80, rows: 24},
		version:  "client-v1",
		readOnly: true,
		created:  false,
	})
	assert.NilError(t, err)

	kicked := s.kickClient("1")
	assert.Equal(t, kicked, true)
	assert.Equal(t, closeCount.Load(), int32(1))
	assert.DeepEqual(t, s.clientInfo(), []protocol.SessionClient{})

	kicked = s.kickClient("missing")
	assert.Equal(t, kicked, false)
}

func TestSessionWritableAttachResizesAndReadOnlyAttachDoesNot(t *testing.T) {
	s := newSessionLoopHarness(t)

	_, err := s.attach(t.Context(), sessionAttachSpec{
		conn:      protocol.NewConn(discardRW{}),
		closeConn: func() error { return nil },
		size:      termSize{cols: 100, rows: 40, xpixel: 900, ypixel: 700},
		version:   "writer",
	})
	assert.NilError(t, err)

	cols, rows := s.size()
	assert.Equal(t, cols, uint16(100))
	assert.Equal(t, rows, uint16(40))

	_, err = s.attach(t.Context(), sessionAttachSpec{
		conn:      protocol.NewConn(discardRW{}),
		closeConn: func() error { return nil },
		size:      termSize{cols: 120, rows: 50, xpixel: 1200, ypixel: 900},
		version:   "reader",
		readOnly:  true,
	})
	assert.NilError(t, err)

	cols, rows = s.size()
	assert.Equal(t, cols, uint16(100))
	assert.Equal(t, rows, uint16(40))
}

func TestSessionResizeClientIgnoresReadOnlyClients(t *testing.T) {
	s := newSessionLoopHarness(t)

	writable, err := s.attach(t.Context(), sessionAttachSpec{
		conn:      protocol.NewConn(discardRW{}),
		closeConn: func() error { return nil },
		size:      termSize{cols: 80, rows: 24},
		version:   "writer",
	})
	assert.NilError(t, err)

	readOnly, err := s.attach(t.Context(), sessionAttachSpec{
		conn:      protocol.NewConn(discardRW{}),
		closeConn: func() error { return nil },
		size:      termSize{cols: 120, rows: 50},
		version:   "reader",
		readOnly:  true,
	})
	assert.NilError(t, err)

	s.resizeClient(readOnly, termSize{cols: 140, rows: 60})
	_ = s.clientInfo()

	cols, rows := s.size()
	assert.Equal(t, cols, uint16(80))
	assert.Equal(t, rows, uint16(24))

	s.resizeClient(writable, termSize{cols: 90, rows: 30})
	_ = s.clientInfo()

	cols, rows = s.size()
	assert.Equal(t, cols, uint16(90))
	assert.Equal(t, rows, uint16(30))
}

func TestSessionStopClosesClients(t *testing.T) {
	s := newSessionLoopHarness(t)

	var closeCount atomic.Int32
	_, err := s.attach(t.Context(), sessionAttachSpec{
		conn: protocol.NewConn(discardRW{}),
		closeConn: func() error {
			closeCount.Add(1)
			return nil
		},
		size:    termSize{cols: 80, rows: 24},
		version: "client-1",
	})
	assert.NilError(t, err)

	_, err = s.attach(t.Context(), sessionAttachSpec{
		conn: protocol.NewConn(discardRW{}),
		closeConn: func() error {
			closeCount.Add(1)
			return nil
		},
		size:    termSize{cols: 100, rows: 40},
		version: "client-2",
	})
	assert.NilError(t, err)

	select {
	case s.actions <- stopReq{}:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out sending stop request")
	}

	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for session shutdown")
	}

	assert.Equal(t, closeCount.Load(), int32(2))
}

func TestBroadcastOutputEvictsSlowClients(t *testing.T) {
	msg := &protocol.Output{Data: []byte("hello")}

	var fastClosed atomic.Int32
	fast := &sessionClient{
		id: "fast",
		closeConn: func() error {
			fastClosed.Add(1)
			return nil
		},
		outCh: make(chan protocol.Message, 1),
	}

	var slowClosed atomic.Int32
	slow := &sessionClient{
		id: "slow",
		closeConn: func() error {
			slowClosed.Add(1)
			return nil
		},
		outCh: make(chan protocol.Message, 1),
	}
	slow.outCh <- &protocol.Output{Data: []byte("busy")}

	clients := broadcastOutput([]*sessionClient{fast, slow}, "demo", msg)
	assert.Equal(t, len(clients), 1)
	assert.Assert(t, clients[0] == fast)
	assert.Equal(t, fastClosed.Load(), int32(0))
	assert.Equal(t, slowClosed.Load(), int32(1))

	got := <-fast.outCh
	assert.DeepEqual(t, got, msg)
}

func TestNotifyClientsChangedSkipsBlockedClients(t *testing.T) {
	ready := &sessionClient{outCh: make(chan protocol.Message, 1)}
	blocked := &sessionClient{outCh: make(chan protocol.Message, 1)}
	blocked.outCh <- &protocol.Output{Data: []byte("busy")}

	notifyClientsChanged([]*sessionClient{ready, blocked}, func() (uint16, uint16) {
		return 90, 30
	})

	got := <-ready.outCh
	assert.DeepEqual(t, got, &protocol.ClientsChanged{Count: 2, Cols: 90, Rows: 30})

	stillBlocked := <-blocked.outCh
	assert.DeepEqual(t, stillBlocked, &protocol.Output{Data: []byte("busy")})
}
