package daemon

import (
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

func TestIssueAttachLeaseConsumeOnce(t *testing.T) {
	s := &Server{
		attachLeaseTTL: 50 * time.Millisecond,
		deadSessionTTL: time.Second,
		sessions:       map[string]*Session{"s": {done: make(chan struct{})}},
		deadTimers:     make(map[string]*time.Timer),
		deadTimerAt:    make(map[string]time.Time),
		attachLeases:   make(map[string]*attachLease),
	}

	token, _ := s.issueAttachLease("s")

	ok := s.consumeAttachLease("s", token)
	assert.Equal(t, ok, true)

	ok = s.consumeAttachLease("s", token)
	assert.Equal(t, ok, false)
}

func TestExtendDeadTimerUsesLeaseExpiry(t *testing.T) {
	done := make(chan struct{})
	close(done)

	now := time.Now()
	leaseExpiry := now.Add(120 * time.Millisecond)

	s := &Server{
		attachLeaseTTL: time.Second,
		deadSessionTTL: 40 * time.Millisecond,
		sessions:       map[string]*Session{"s": {done: done}},
		deadTimers:     make(map[string]*time.Timer),
		deadTimerAt:    make(map[string]time.Time),
		attachLeases: map[string]*attachLease{
			"tok": {session: "s", expires: leaseExpiry},
		},
	}

	s.mu.Lock()
	s.extendDeadTimerLocked("s")
	after := s.deadTimerAt["s"]
	s.mu.Unlock()

	assert.Equal(t, after.UnixMilli(), leaseExpiry.UnixMilli())

	s.mu.Lock()
	for _, timer := range s.deadTimers {
		timer.Stop()
	}
	s.mu.Unlock()
}
