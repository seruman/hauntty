package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"
)

type attachLease struct {
	session string
	expires time.Time
	timer   *time.Timer
}

func (s *Server) issueAttachLease(session string) (string, uint64) {
	token := make([]byte, 16)
	tokenStr := ""
	if _, err := rand.Read(token); err != nil {
		id := s.clientIDCounter.Add(1)
		tokenStr = strconv.FormatUint(id, 16)
	} else {
		tokenStr = hex.EncodeToString(token)
	}
	expires := time.Now().Add(s.attachLeaseTTL)

	s.mu.Lock()
	lease := &attachLease{session: session, expires: expires}
	lease.timer = time.AfterFunc(s.attachLeaseTTL, func() {
		s.expireAttachLease(tokenStr)
	})
	s.attachLeases[tokenStr] = lease
	s.extendDeadTimerLocked(session)
	s.mu.Unlock()

	return tokenStr, uint64(expires.UnixMilli())
}

func (s *Server) expireAttachLease(token string) {
	s.mu.Lock()
	lease := s.attachLeases[token]
	if lease == nil {
		s.mu.Unlock()
		return
	}
	delete(s.attachLeases, token)
	s.extendDeadTimerLocked(lease.session)
	s.mu.Unlock()
}

func (s *Server) consumeAttachLease(session, token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	lease := s.attachLeases[token]
	if lease == nil {
		return false
	}
	if lease.session != session || time.Now().After(lease.expires) {
		delete(s.attachLeases, token)
		if lease.timer != nil {
			lease.timer.Stop()
		}
		return false
	}
	delete(s.attachLeases, token)
	if lease.timer != nil {
		lease.timer.Stop()
	}
	s.extendDeadTimerLocked(session)
	return true
}

func (s *Server) maxLeaseExpiryLocked(session string) time.Time {
	var until time.Time
	for _, lease := range s.attachLeases {
		if lease.session != session {
			continue
		}
		if lease.expires.After(until) {
			until = lease.expires
		}
	}
	return until
}

func (s *Server) scheduleDeadTimerLocked(session string, when time.Time) {
	timer := s.deadTimers[session]
	if timer != nil {
		timer.Stop()
	}
	delay := max(time.Until(when), 0)
	s.deadTimers[session] = time.AfterFunc(delay, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.deadTimers, session)
		delete(s.deadTimerAt, session)
		delete(s.sessions, session)
	})
	s.deadTimerAt[session] = when
}

func (s *Server) extendDeadTimerLocked(session string) {
	sess := s.sessions[session]
	if sess == nil || sess.isRunning() {
		return
	}
	target := time.Now().Add(s.deadSessionTTL)
	leaseUntil := s.maxLeaseExpiryLocked(session)
	if leaseUntil.After(target) {
		target = leaseUntil
	}
	current, ok := s.deadTimerAt[session]
	if ok && !target.After(current) {
		return
	}
	s.scheduleDeadTimerLocked(session, target)
}
