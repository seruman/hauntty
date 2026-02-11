package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

func socketDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "hauntty")
	}
	tmpdir := os.TempDir()
	return filepath.Join(tmpdir, fmt.Sprintf("hauntty-%d", os.Getuid()))
}

// SocketPath returns the path to the daemon Unix socket.
func SocketPath() string {
	return filepath.Join(socketDir(), "hauntty.sock")
}

// PIDPath returns the path to the daemon PID file.
func PIDPath() string {
	return filepath.Join(socketDir(), "hauntty.pid")
}
