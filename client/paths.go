package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func socketDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "hauntty")
	}
	tmpdir := os.TempDir()
	return filepath.Join(tmpdir, fmt.Sprintf("hauntty-%d", os.Getuid()))
}

func SocketPath() string {
	return filepath.Join(socketDir(), "hauntty.sock")
}

func PIDPath() string {
	return filepath.Join(socketDir(), "hauntty.pid")
}

func DaemonRunning() bool {
	data, err := os.ReadFile(PIDPath())
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
