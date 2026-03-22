package daemon

import (
	"os"
	"path/filepath"
	"testing"

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
