package client

import (
	"errors"
	"net"
	"os"
	"syscall"
)

func ProbeDaemon(sock string) (bool, error) {
	c, err := Connect(sock)
	if err != nil {
		if DaemonUnavailable(err) {
			return false, nil
		}
		return false, err
	}
	defer c.Close()
	return true, nil
}

func DaemonUnavailable(err error) bool {
	var opErr *net.OpError
	if !errors.As(err, &opErr) {
		return false
	}

	if sysErr, ok := errors.AsType[*os.SyscallError](opErr.Err); ok {
		err = sysErr.Err
	} else {
		err = opErr.Err
	}

	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}
