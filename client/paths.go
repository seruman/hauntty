package client

import (
	"net"

	"code.selman.me/hauntty/config"
)

func DaemonRunning(socketPath string) bool {
	conn, err := net.Dial("unix", config.SocketPathFrom(socketPath))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
