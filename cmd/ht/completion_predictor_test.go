package main

import (
	"testing"

	"github.com/posener/complete"
	"gotest.tools/v3/assert"
)

func TestSocketFromCompletionArgs(t *testing.T) {
	t.Setenv("HAUNTTY_SOCKET", "")
	args := complete.Args{All: []string{"--socket", "/tmp/ht.sock", "list"}}
	socket := socketFromCompletionArgs(args)
	assert.Equal(t, socket, "/tmp/ht.sock")
}

func TestSocketFromCompletionArgsEqualsForm(t *testing.T) {
	t.Setenv("HAUNTTY_SOCKET", "")
	args := complete.Args{All: []string{"--socket=/tmp/eq.sock", "list"}}
	socket := socketFromCompletionArgs(args)
	assert.Equal(t, socket, "/tmp/eq.sock")
}

func TestSocketFromCompletionArgsEnv(t *testing.T) {
	t.Setenv("HAUNTTY_SOCKET", "/tmp/env.sock")
	args := complete.Args{All: []string{"list"}}
	socket := socketFromCompletionArgs(args)
	assert.Equal(t, socket, "/tmp/env.sock")
}
