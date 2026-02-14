package client

import (
	"net"

	"github.com/selman/hauntty/protocol"
)

func DaemonRunning() bool {
	conn, err := net.Dial("unix", protocol.SocketPath())
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
