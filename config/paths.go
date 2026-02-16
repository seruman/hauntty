package config

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
