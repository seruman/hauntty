package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func TestSetupShellEnvZsh(t *testing.T) {
	resourcesDir := t.TempDir()
	realZdotdir := t.TempDir()
	env := []string{
		"HOME=/home/tester",
		"ZDOTDIR=" + realZdotdir,
		"GHOSTTY_RESOURCES_DIR=" + resourcesDir,
	}

	gotCommand, gotEnv, tempDir, err := prepareShellLaunch([]string{"/bin/zsh"}, env, "demo-session")
	assert.NilError(t, err)
	if tempDir != "" {
		defer os.RemoveAll(tempDir)
	}

	assert.DeepEqual(t, gotCommand, []string{"/bin/zsh"})
	assert.DeepEqual(t, gotEnv, []string{
		"HOME=/home/tester",
		"ZDOTDIR=" + tempDir,
		"GHOSTTY_RESOURCES_DIR=" + resourcesDir,
		"HAUNTTY_SESSION=demo-session",
	})

	contents, err := os.ReadFile(filepath.Join(tempDir, ".zshenv"))
	assert.NilError(t, err)

	integrationScript := filepath.Join(resourcesDir, "shell-integration", "zsh", "ghostty-integration")
	userZshenv := filepath.Join(realZdotdir, ".zshenv")
	assert.Equal(t, string(contents), fmt.Sprintf("export ZDOTDIR=%q\nsource %q\n[[ -f %q ]] && source %q\n", realZdotdir, integrationScript, userZshenv, userZshenv))
}

func TestSetupShellEnvBash(t *testing.T) {
	resourcesDir := t.TempDir()
	env := []string{
		"HOME=/home/tester",
		"GHOSTTY_RESOURCES_DIR=" + resourcesDir,
	}

	gotCommand, gotEnv, tempDir, err := prepareShellLaunch([]string{"/bin/bash"}, env, "demo-session")
	assert.NilError(t, err)

	assert.DeepEqual(t, gotCommand, []string{"/bin/bash"})
	assert.Equal(t, tempDir, "")
	assert.DeepEqual(t, gotEnv, []string{
		"HOME=/home/tester",
		"GHOSTTY_RESOURCES_DIR=" + resourcesDir,
		"HAUNTTY_SESSION=demo-session",
		"GHOSTTY_BASH_INJECT=1",
		"ENV=" + filepath.Join(resourcesDir, "shell-integration", "bash", "ghostty.bash"),
		"BASH_ENV=" + filepath.Join(resourcesDir, "shell-integration", "bash", "ghostty.bash"),
	})
}

func TestSetupShellEnvFish(t *testing.T) {
	resourcesDir := t.TempDir()
	env := []string{
		"HOME=/home/tester",
		"GHOSTTY_RESOURCES_DIR=" + resourcesDir,
		"XDG_DATA_DIRS=/opt/share:/usr/share",
	}

	gotCommand, gotEnv, tempDir, err := prepareShellLaunch([]string{"/usr/bin/fish"}, env, "demo-session")
	assert.NilError(t, err)

	assert.DeepEqual(t, gotCommand, []string{"/usr/bin/fish"})
	assert.Equal(t, tempDir, "")
	assert.DeepEqual(t, gotEnv, []string{
		"HOME=/home/tester",
		"GHOSTTY_RESOURCES_DIR=" + resourcesDir,
		"XDG_DATA_DIRS=" + filepath.Join(resourcesDir, "shell-integration", "fish") + ":/opt/share:/usr/share",
		"HAUNTTY_SESSION=demo-session",
	})
}

func TestDetectShell(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"/bin/zsh", "zsh"},
		{"/usr/bin/bash", "bash"},
		{"/usr/local/bin/fish", "fish"},
		{"-zsh", "zsh"},
		{"/bin/sh", ""},
		{"/usr/bin/dash", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, detectShell(tt.input), tt.want)
		})
	}
}
