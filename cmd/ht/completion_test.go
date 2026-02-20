package main

import (
	"testing"

	"gotest.tools/v3/assert"
)

func TestResolveCompletionShell(t *testing.T) {
	t.Run("uses explicit shell", func(t *testing.T) {
		shell, err := resolveCompletionShell("fish")

		assert.NilError(t, err)
		assert.Equal(t, shell, "fish")
	})

	t.Run("detects shell from environment", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/zsh")

		shell, err := resolveCompletionShell("")

		assert.NilError(t, err)
		assert.Equal(t, shell, "zsh")
	})

	t.Run("errors for unsupported shell", func(t *testing.T) {
		t.Setenv("SHELL", "/bin/tcsh")

		_, err := resolveCompletionShell("")

		assert.Error(t, err, "this shell is not supported (tcsh)")
	})
}

func TestCompletionInitCommand(t *testing.T) {
	t.Run("uses source pipe for fish", func(t *testing.T) {
		got := completionInitCommand("ht", "fish")

		assert.Equal(t, got, "ht completion -c fish | source")
	})

	t.Run("uses process substitution for zsh", func(t *testing.T) {
		got := completionInitCommand("ht", "zsh")

		assert.Equal(t, got, "source <(ht completion -c zsh)")
	})
}

func TestCompletionScriptNilNode(t *testing.T) {
	_, err := completionScript("bash", nil)
	assert.Error(t, err, "nil command tree")
}
