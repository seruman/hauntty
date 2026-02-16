package client

import "net"

func DaemonRunning(sock string) bool {
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
