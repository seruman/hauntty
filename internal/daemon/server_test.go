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
