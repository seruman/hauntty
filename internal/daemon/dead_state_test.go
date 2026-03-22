package daemon

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestReserveSessionName_ProvidedName(t *testing.T) {
	srv := &Server{
		sessions:  make(map[string]*Session),
		persister: &persister{},
	}

	got, err := srv.reserveSessionName("my-session")
	assert.NilError(t, err)
	assert.Equal(t, got, "my-session")
}

func TestReserveSessionName_GeneratesName(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	oldAdjectives := adjectives
	oldNouns := nouns
	adjectives = []string{"alpha"}
	nouns = []string{"beta"}
	defer func() {
		adjectives = oldAdjectives
		nouns = oldNouns
	}()

	srv := &Server{
		sessions:  make(map[string]*Session),
		persister: &persister{},
	}

	got, err := srv.reserveSessionName("")
	assert.NilError(t, err)
	assert.Equal(t, got, "alpha-beta")
}

func TestLiveSessionNames(t *testing.T) {
	srv := &Server{
		sessions: map[string]*Session{
			"alpha": {},
			"beta":  {},
		},
		mu: sync.RWMutex{},
	}

	got := srv.liveSessionNames()
	assert.DeepEqual(t, got, map[string]bool{"alpha": true, "beta": true})
}

func TestLiveSessionNames_Empty(t *testing.T) {
	srv := &Server{
		sessions: make(map[string]*Session),
	}

	got := srv.liveSessionNames()
	assert.DeepEqual(t, got, map[string]bool{})
}

func TestDeadSessionNames(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	writeDeadSessionState(t, "dead-one", &sessionState{
		Cols: 80, Rows: 24, CursorRow: 0, CursorCol: 0,
		SavedAt: time.Unix(1700000000, 0), VT: []byte("x"),
	})
	writeDeadSessionState(t, "dead-two", &sessionState{
		Cols: 80, Rows: 24, CursorRow: 0, CursorCol: 0,
		SavedAt: time.Unix(1700000000, 0), VT: []byte("y"),
	})

	srv := &Server{
		sessions:  map[string]*Session{"live": {}},
		persister: &persister{},
	}

	got, err := srv.deadSessionNames()
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []string{"dead-one", "dead-two"})
}

func TestDeadSessionNames_ExcludesLive(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	writeDeadSessionState(t, "sess", &sessionState{
		Cols: 80, Rows: 24, CursorRow: 0, CursorCol: 0,
		SavedAt: time.Unix(1700000000, 0), VT: []byte("x"),
	})

	srv := &Server{
		sessions:  map[string]*Session{"sess": {}},
		persister: &persister{},
	}

	got, err := srv.deadSessionNames()
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []string(nil))
}

func TestDeadSessionNames_NilPersister(t *testing.T) {
	srv := &Server{
		sessions:  make(map[string]*Session),
		persister: nil,
	}

	got, err := srv.deadSessionNames()
	assert.NilError(t, err)
	assert.DeepEqual(t, got, []string(nil))
}

func TestPrepareCreateDeadSession_NoForceErrorsWhenStateExists(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	writeDeadSessionState(t, "existing", &sessionState{
		Cols: 80, Rows: 24, CursorRow: 0, CursorCol: 0,
		SavedAt: time.Unix(1700000000, 0), VT: []byte("x"),
	})

	srv := &Server{
		sessions:  make(map[string]*Session),
		persister: &persister{},
	}

	err := srv.prepareCreateDeadSession("existing", false)
	assert.Error(t, err, "dead session state exists")
}

func TestPrepareCreateDeadSession_NoForceNoState(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	srv := &Server{
		sessions:  make(map[string]*Session),
		persister: &persister{},
	}

	err := srv.prepareCreateDeadSession("nonexistent", false)
	assert.NilError(t, err)
}

func TestPrepareCreateDeadSession_ForceRemovesState(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	writeDeadSessionState(t, "existing", &sessionState{
		Cols: 80, Rows: 24, CursorRow: 0, CursorCol: 0,
		SavedAt: time.Unix(1700000000, 0), VT: []byte("x"),
	})

	srv := &Server{
		sessions:  make(map[string]*Session),
		persister: &persister{},
	}

	err := srv.prepareCreateDeadSession("existing", true)
	assert.NilError(t, err)

	statePath := filepath.Join(tmp, "hauntty", "sessions", "existing.state")
	_, statErr := os.Stat(statePath)
	assert.Error(t, statErr, "stat "+statePath+": no such file or directory")
}

func TestPruneDeadSessions(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_STATE_HOME", tmp)

	writeDeadSessionState(t, "dead-a", &sessionState{
		Cols: 80, Rows: 24, CursorRow: 0, CursorCol: 0,
		SavedAt: time.Unix(1700000000, 0), VT: []byte("a"),
	})
	writeDeadSessionState(t, "dead-b", &sessionState{
		Cols: 80, Rows: 24, CursorRow: 0, CursorCol: 0,
		SavedAt: time.Unix(1700000000, 0), VT: []byte("b"),
	})

	srv := &Server{
		sessions:  make(map[string]*Session),
		persister: &persister{},
	}

	count, err := srv.pruneDeadSessions()
	assert.NilError(t, err)
	assert.Equal(t, count, uint32(2))

	remaining, err := srv.deadSessionNames()
	assert.NilError(t, err)
	assert.DeepEqual(t, remaining, []string(nil))
}

func TestPruneDeadSessions_Empty(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	srv := &Server{
		sessions:  make(map[string]*Session),
		persister: &persister{},
	}

	count, err := srv.pruneDeadSessions()
	assert.NilError(t, err)
	assert.Equal(t, count, uint32(0))
}
