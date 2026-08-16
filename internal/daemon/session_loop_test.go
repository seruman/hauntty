package daemon

import (
	"bytes"
	"io"
	"net"
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

type failingRW struct{}

func (failingRW) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (failingRW) Write(_ []byte) (int, error) {
	return 0, io.ErrClosedPipe
}

func (discardRW) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (discardRW) Write(p []byte) (int, error) {
	return len(p), nil
}

func newSessionLoopHarness(t *testing.T) *Session {
	ctx := t.Context()
	rt, err := libghostty.NewRuntime()
	assert.NilError(t, err)
	t.Cleanup(func() {
		assert.NilError(t, rt.Close())
	})

	term, err := newTerminalState(rt, 80, 24, 0)
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
		feedDone:     make(chan struct{}),
		actions:      make(chan sessionAction, 16),
		ptyOut:       make(chan []byte, 64),
		clientReady:  make(chan struct{}, 1),
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
		assert.NilError(t, term.close())
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
	assert.Equal(t, client.id, "1")

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

func TestSessionRemovesClientAfterWriteFailure(t *testing.T) {
	s := newSessionLoopHarness(t)

	closed := make(chan struct{})
	client, err := s.attach(t.Context(), sessionAttachSpec{
		conn: protocol.NewConn(failingRW{}),
		closeConn: func() error {
			close(closed)
			return nil
		},
		size:     termSize{cols: 80, rows: 24},
		version:  "failed-writer",
		readOnly: true,
	})
	assert.NilError(t, err)
	<-closed
	<-client.writeDone

	assert.DeepEqual(t, s.clientInfo(), []protocol.SessionClient{})
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

func TestGatherPTYReads(t *testing.T) {
	saturated := make([][]byte, 70)
	var saturatedData []byte
	for i := range saturated {
		saturated[i] = bytes.Repeat([]byte{byte(i)}, 1024)
		saturatedData = append(saturatedData, saturated[i]...)
	}

	tests := []struct {
		name   string
		chunks [][]byte
		want   [][]byte
	}{
		{
			name:   "interactive chunks remain immediate",
			chunks: [][]byte{[]byte("abc"), []byte("def")},
			want:   [][]byte{[]byte("abc"), []byte("def")},
		},
		{
			name:   "saturated reads are batched",
			chunks: saturated,
			want:   [][]byte{saturatedData[:64*1024], saturatedData[64*1024:]},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reads := make(chan []byte, len(tt.chunks))
			for _, chunk := range tt.chunks {
				reads <- chunk
			}
			close(reads)

			batches := make(chan []byte, len(tt.chunks))
			gatherPTYReads(reads, batches, make(chan struct{}))

			var got [][]byte
			for batch := range batches {
				got = append(got, batch)
			}
			assert.DeepEqual(t, got, tt.want)
		})
	}
}

func TestSessionAcceptsFullPTYBatch(t *testing.T) {
	s := newSessionLoopHarness(t)
	s.ptyOut <- bytes.Repeat([]byte("x"), ptyBatchSize)

	_, err := s.attach(t.Context(), sessionAttachSpec{
		conn:      protocol.NewConn(discardRW{}),
		closeConn: func() error { return nil },
		size:      termSize{cols: 80, rows: 24},
		readOnly:  true,
	})
	assert.NilError(t, err)
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

func TestQueueOutputBackpressuresSlowClients(t *testing.T) {
	msg := &protocol.Output{Data: []byte("hello")}
	fast := &sessionClient{id: "fast", outCh: make(chan protocol.Message, 1)}
	slow := &sessionClient{id: "slow", outCh: make(chan protocol.Message, 1)}
	slow.outCh <- &protocol.Output{Data: []byte("busy")}

	pending := queueOutput([]*sessionClient{fast, slow}, msg)
	assert.DeepEqual(t, []string{pending[0].id}, []string{"slow"})
	assert.DeepEqual(t, <-fast.outCh, msg)
	assert.DeepEqual(t, <-slow.outCh, &protocol.Output{Data: []byte("busy")})

	pending = queueOutput(pending, msg)
	assert.DeepEqual(t, pending, []*sessionClient(nil))
	assert.DeepEqual(t, <-slow.outCh, msg)
}

func TestSessionBackpressurePreservesClientAndOutput(t *testing.T) {
	s := newSessionLoopHarness(t)
	serverConn, clientConn := net.Pipe()
	defer clientConn.Close()
	client := protocol.NewConn(clientConn)

	var closeCount atomic.Int32
	sessionClient, err := s.attach(t.Context(), sessionAttachSpec{
		conn: protocol.NewConn(serverConn),
		closeConn: func() error {
			closeCount.Add(1)
			return serverConn.Close()
		},
		size:     termSize{cols: 80, rows: 24},
		version:  "slow-client",
		readOnly: true,
	})
	assert.NilError(t, err)

	msg, err := client.ReadMessage()
	assert.NilError(t, err)
	assert.DeepEqual(t, msg, &protocol.Attached{
		Name:       "demo",
		PID:        999999999,
		ClientID:   "1",
		Cols:       80,
		Rows:       24,
		ScreenDump: []byte("\x1b[0m\x1b[1;1H"),
		Created:    false,
	})

	outputCount := sessionClientOutBufferSize + 2*cap(s.ptyOut)
	sent := make(chan struct{})
	go func() {
		defer close(sent)
		for range outputCount {
			s.ptyOut <- []byte("x")
		}
	}()

	select {
	case <-sent:
		t.Fatal("PTY producer did not encounter backpressure")
	case <-time.After(150 * time.Millisecond):
	}

	infoCh := make(chan []protocol.SessionClient, 1)
	go func() { infoCh <- s.clientInfo() }()
	select {
	case info := <-infoCh:
		assert.DeepEqual(t, info, []protocol.SessionClient{{ClientID: "1", ReadOnly: true, Version: "slow-client"}})
	case <-time.After(5 * time.Second):
		t.Fatal("session actions blocked behind client output")
	}
	assert.Equal(t, closeCount.Load(), int32(0))

	assert.NilError(t, clientConn.SetReadDeadline(time.Now().Add(5*time.Second)))
	var output []byte
	for len(output) < outputCount {
		msg, err := client.ReadMessage()
		assert.NilError(t, err)
		if m, ok := msg.(*protocol.Output); ok {
			output = append(output, m.Data...)
		}
	}
	assert.DeepEqual(t, output, bytes.Repeat([]byte("x"), outputCount))
	<-sent
	close(s.ptyOut)

	msg, err = client.ReadMessage()
	assert.NilError(t, err)
	assert.DeepEqual(t, msg, &protocol.Exited{ExitCode: 0})
	<-sessionClient.writeDone
	assert.Equal(t, closeCount.Load(), int32(0))
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
