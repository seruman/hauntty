package client

import (
	"net"
	"os"
	"testing"

	"code.selman.me/hauntty/internal/protocol"
	"gotest.tools/v3/assert"
)

func TestProbeDaemonReturnsFalseForMissingSocket(t *testing.T) {
	f, err := os.CreateTemp("/tmp", "htsock-*")
	assert.NilError(t, err)
	sock := f.Name()
	assert.NilError(t, f.Close())
	assert.NilError(t, os.Remove(sock))
	defer os.Remove(sock)

	running, err := ProbeDaemon(sock)

	assert.Equal(t, running, false)
	assert.NilError(t, err)
}

func TestProbeDaemonAcceptsHandshakeServer(t *testing.T) {
	sock, ln := newProbeListener(t)
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
		done <- pc.WriteVersionReply(clientVer, "server-revision")
	}()

	running, err := ProbeDaemon(sock)

	assert.Equal(t, running, true)
	assert.NilError(t, err)
	assert.NilError(t, <-done)
}

func TestProbeDaemonRejectsVersionMismatch(t *testing.T) {
	sock, ln := newProbeListener(t)
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
		_, _, err = pc.AcceptHandshake()
		if err != nil {
			done <- err
			return
		}
		done <- pc.WriteVersionReply(0, "")
	}()

	running, err := ProbeDaemon(sock)

	assert.Equal(t, running, false)
	assert.Error(t, err, "protocol version mismatch: server accepted 0, expected 8")
	assert.NilError(t, <-done)
}

func newProbeListener(t *testing.T) (string, net.Listener) {
	t.Helper()

	f, err := os.CreateTemp("/tmp", "htsock-*")
	assert.NilError(t, err)
	sock := f.Name()
	assert.NilError(t, f.Close())
	assert.NilError(t, os.Remove(sock))

	ln, err := net.Listen("unix", sock)
	assert.NilError(t, err)
	return sock, ln
}
