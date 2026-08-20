package daemon

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"code.selman.me/hauntty/internal/config"
	"gotest.tools/v3/assert"
)

func TestReserveSessionNameIncludesDeadSessions(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	oldAdjectives := adjectives
	oldNouns := nouns
	adjectives = []string{"alpha"}
	nouns = []string{"beta", "gamma"}
	defer func() {
		adjectives = oldAdjectives
		nouns = oldNouns
	}()

	sessionDir := filepath.Join(os.Getenv("XDG_STATE_HOME"), "hauntty", "sessions")
	assert.NilError(t, os.MkdirAll(sessionDir, 0o700))
	assert.NilError(t, os.WriteFile(filepath.Join(sessionDir, "alpha-beta.state"), []byte("saved"), 0o600))

	srv := &Server{sessions: make(map[string]*Session), persister: &persister{}}
	name, err := srv.reserveSessionName("")
	assert.NilError(t, err)
	assert.Equal(t, name, "alpha-gamma")
}

func TestShutdownBeforeListenPreservesExistingSocket(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "ht-shutdown-")
	assert.NilError(t, err)
	t.Cleanup(func() { assert.NilError(t, os.RemoveAll(dir)) })

	socketPath := filepath.Join(dir, "hauntty.sock")
	listener, err := net.Listen("unix", socketPath)
	assert.NilError(t, err)
	t.Cleanup(func() { assert.NilError(t, listener.Close()) })

	cfg := config.Default()
	cfg.Daemon.SocketPath = socketPath
	srv, err := New(t.Context(), &cfg.Daemon, cfg.Session.ResizePolicy)
	assert.NilError(t, err)

	srv.Shutdown()

	conn, err := net.Dial("unix", socketPath)
	assert.NilError(t, err)
	assert.NilError(t, conn.Close())
}

func TestAcquireLockPreventsSecondDaemon(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "hauntty.lock")

	s1 := &Server{lockPath: lockPath}
	assert.NilError(t, s1.acquireLock())
	t.Cleanup(s1.releaseLock)

	s2 := &Server{lockPath: lockPath}
	err := s2.acquireLock()
	assert.Error(t, err, "daemon already running")
}

func TestReleaseLockAllowsNextDaemon(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "hauntty.lock")

	s1 := &Server{lockPath: lockPath}
	assert.NilError(t, s1.acquireLock())
	s1.releaseLock()

	s2 := &Server{lockPath: lockPath}
	assert.NilError(t, s2.acquireLock())
	t.Cleanup(s2.releaseLock)
}
