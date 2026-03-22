package main

import (
	"testing"

	"code.selman.me/hauntty/internal/protocol"
	"gotest.tools/v3/assert"
)

func TestCompletionDynamicTopics(t *testing.T) {
	topics := completionDynamicTopics()

	assert.DeepEqual(t, topics, map[string]string{
		"attach":  "live_sessions",
		"dump":    "dumpable_sessions",
		"kill":    "live_sessions",
		"kick":    "live_sessions",
		"restore": "dead_sessions",
		"send":    "live_sessions",
		"status":  "sessions",
		"wait":    "dumpable_sessions",
	})
}

func TestCompletionTopicNames(t *testing.T) {
	sessions := []protocol.Session{
		{Name: "live-a", State: protocol.SessionStateRunning},
		{Name: "dead-a", State: protocol.SessionStateDead},
		{Name: "live-b", State: protocol.SessionStateRunning},
	}

	t.Run("sessions", func(t *testing.T) {
		names := completionTopicNames("sessions", sessions)

		assert.DeepEqual(t, names, []string{"live-a", "dead-a", "live-b"})
	})

	t.Run("live sessions", func(t *testing.T) {
		names := completionTopicNames("live_sessions", sessions)

		assert.DeepEqual(t, names, []string{"live-a", "live-b"})
	})

	t.Run("dead sessions", func(t *testing.T) {
		names := completionTopicNames("dead_sessions", sessions)

		assert.DeepEqual(t, names, []string{"dead-a"})
	})

	t.Run("dumpable sessions", func(t *testing.T) {
		names := completionTopicNames("dumpable_sessions", sessions)

		assert.DeepEqual(t, names, []string{"live-a", "dead-a", "live-b"})
	})

	t.Run("unknown topic", func(t *testing.T) {
		names := completionTopicNames("unknown", sessions)

		assert.DeepEqual(t, names, []string{})
	})
}
