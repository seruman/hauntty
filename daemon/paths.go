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

func SocketPath() string {
	return filepath.Join(socketDir(), "hauntty.sock")
}

func PIDPath() string {
	return filepath.Join(socketDir(), "hauntty.pid")
}
