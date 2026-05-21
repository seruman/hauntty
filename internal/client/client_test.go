package client

import (
	"net"
	"os"
	"testing"

	"code.selman.me/hauntty/internal/protocol"
	"gotest.tools/v3/assert"
)

func TestCreateSessionSendsCreateRequest(t *testing.T) {
	f, err := os.CreateTemp("/tmp", "htsock-*")
	assert.NilError(t, err)
	sock := f.Name()
	assert.NilError(t, f.Close())
	assert.NilError(t, os.Remove(sock))
	defer os.Remove(sock)

	ln, err := net.Listen("unix", sock)
	assert.NilError(t, err)
	defer ln.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()

		pc := protocol.NewConn(conn)
		clientVer, clientRev, err := pc.AcceptHandshake()
		if err != nil {
			done <- err
			return
		}
		if clientVer != protocol.ProtocolVersion || clientRev == "" {
			done <- os.ErrInvalid
			return
		}
		if err := pc.WriteVersionReply(clientVer, clientRev); err != nil {
			done <- err
			return
		}
		msg, err := pc.ReadMessage()
		if err != nil {
			done <- err
			return
		}
		create, ok := msg.(*protocol.Create)
		if !ok {
			done <- os.ErrInvalid
			return
		}
		assert.DeepEqual(t, create, &protocol.Create{
			Name:       "demo",
			Command:    []string{"/bin/sh", "-lc", "echo hi"},
			Env:        []string{"TERM=xterm-256color"},
			CWD:        "/tmp/demo",
			Scrollback: 99,
			Force:      true,
		})
		done <- pc.WriteMessage(&protocol.Created{Name: "demo", PID: 42})
	}()

	c, err := Connect(sock)
	assert.NilError(t, err)
	created, err := c.CreateSession(CreateSessionOpts{
		Name:       "demo",
		Command:    []string{"/bin/sh", "-lc", "echo hi"},
		Env:        []string{"TERM=xterm-256color"},
		CWD:        "/tmp/demo",
		Scrollback: 99,
		Force:      true,
	})
	assert.NilError(t, err)
	assert.DeepEqual(t, created, &CreatedSession{Name: "demo", PID: 42})
	assert.NilError(t, c.Close())
	assert.NilError(t, <-done)
}

func TestStatusFromProtocol(t *testing.T) {
	status := statusFromProtocol(&protocol.StatusResponse{
		Daemon: protocol.DaemonStatus{
			PID:          42,
			Uptime:       99,
			SocketPath:   "/tmp/hauntty.sock",
			RunningCount: 2,
			DeadCount:    1,
			Version:      "v1",
		},
		Session: &protocol.SessionStatus{
			Name:  "demo",
			State: protocol.SessionStateRunning,
			Cols:  80,
			Rows:  24,
			PID:   100,
			CWD:   "/tmp/demo",
			Clients: []protocol.SessionClient{
				{ClientID: "1", ReadOnly: true, Version: "client-v1"},
			},
		},
	})

	assert.DeepEqual(t, status, &Status{
		Daemon: DaemonStatus{
			PID:          42,
			Uptime:       99,
			SocketPath:   "/tmp/hauntty.sock",
			RunningCount: 2,
			DeadCount:    1,
			Version:      "v1",
		},
		Session: &SessionStatus{
			Name:  "demo",
			State: SessionStateRunning,
			Cols:  80,
			Rows:  24,
			PID:   100,
			CWD:   "/tmp/demo",
			Clients: []SessionClient{
				{ClientID: "1", ReadOnly: true, Version: "client-v1"},
			},
		},
	})
}

func TestConnectAcceptsRevisionMismatchWhenProtocolMatches(t *testing.T) {
	f, err := os.CreateTemp("/tmp", "htsock-*")
	assert.NilError(t, err)
	sock := f.Name()
	assert.NilError(t, f.Close())
	assert.NilError(t, os.Remove(sock))
	defer os.Remove(sock)

	ln, err := net.Listen("unix", sock)
	assert.NilError(t, err)
	defer ln.Close()

	done := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()

		pc := protocol.NewConn(conn)
		clientVer, _, err := pc.AcceptHandshake()
		if err != nil {
			done <- err
			return
		}
		err = pc.WriteVersionReply(clientVer, "different-revision")
		done <- err
	}()

	c, err := Connect(sock)
	assert.NilError(t, err)
	assert.NilError(t, c.Close())
	assert.NilError(t, <-done)
}
