package client

import (
	"net"

	"code.selman.me/hauntty/protocol"
)

func DaemonRunning(socketPath string) bool {
	conn, err := net.Dial("unix", protocol.SocketPathFrom(socketPath))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
