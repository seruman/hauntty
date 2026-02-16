package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var socketPath = sync.OnceValue(func() string {
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		return filepath.Join(xdg, "hauntty", "hauntty.sock")
	}

	return filepath.Join(os.TempDir(), fmt.Sprintf("hauntty-%d", os.Getuid()), "hauntty.sock")
})

func SocketPath() string {
	return socketPath()
}
