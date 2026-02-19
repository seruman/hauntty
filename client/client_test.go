package client

import (
	"net"
	"os"
	"testing"

	"code.selman.me/hauntty/internal/protocol"
	"gotest.tools/v3/assert"
)

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
		err = pc.AcceptVersion(clientVer, "different-revision")
		done <- err
	}()

	c, err := Connect(sock)
	assert.NilError(t, err)
	assert.NilError(t, c.Close())
	assert.NilError(t, <-done)
}
