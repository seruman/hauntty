package protocol

import (
	"fmt"
	"os"
	"path/filepath"
)

func socketDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "hauntty")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("hauntty-%d", os.Getuid()))
}

func SocketPath() string {
	return filepath.Join(socketDir(), "hauntty.sock")
}

func PIDPath() string {
	return filepath.Join(socketDir(), "hauntty.pid")
}

func LogPath(pid int) string {
	return filepath.Join(socketDir(), fmt.Sprintf("hauntty-server-%d.log", pid))
}
