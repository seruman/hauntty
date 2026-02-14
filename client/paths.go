package client

import (
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/selman/hauntty/protocol"
)

func DaemonRunning() bool {
	data, err := os.ReadFile(protocol.PIDPath())
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks if the process exists without actually sending a signal.
	return proc.Signal(syscall.Signal(0)) == nil
}
