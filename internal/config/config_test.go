package config

import (
	"os"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
)

func TestDefaults(t *testing.T) {
	cfg := Default()
	assert.Equal(t, cfg.Daemon.DefaultScrollback, uint32(10000))
	assert.Equal(t, cfg.Daemon.StatePersistence, true)
	assert.Equal(t, cfg.Daemon.StatePersistenceInterval, 30)
	assert.Equal(t, cfg.Client.DetachKeybind, "ctrl+;")
	assert.Equal(t, cfg.Session.DefaultCommand, "")
	assert.DeepEqual(t, cfg.Client.ForwardEnv, []string{"COLORTERM", "GHOSTTY_RESOURCES_DIR", "GHOSTTY_BIN_DIR"})
	assert.Equal(t, cfg.Session.ResizePolicy, ResizePolicySmallest)
}

func TestLoadMissing(t *testing.T) {
	cfg, err := LoadFrom("/nonexistent/path/config.toml")
	assert.NilError(t, err)
	assert.DeepEqual(t, cfg, Default())
}

func TestLoadDefaultCommand(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	err := os.WriteFile(path, []byte(`[session]
default_command = "/bin/zsh"
`), 0o600)
	assert.NilError(t, err)

	cfg, err := LoadFrom(path)
	assert.NilError(t, err)
	assert.Equal(t, cfg.Session.DefaultCommand, "/bin/zsh")
	assert.Equal(t, cfg.Daemon.DefaultScrollback, uint32(10000))
	assert.Equal(t, cfg.Client.DetachKeybind, "ctrl+;")
}

func TestLoadOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	err := os.WriteFile(path, []byte(`[daemon]
default_scrollback = 5000
auto_exit = true

[client]
detach_keybind = "ctrl+q"
forward_env = ["TERM"]

[session]
default_command = "/usr/bin/fish"
`), 0o600)
	assert.NilError(t, err)

	cfg, err := LoadFrom(path)
	assert.NilError(t, err)
	assert.Equal(t, cfg.Daemon.DefaultScrollback, uint32(5000))
	assert.Equal(t, cfg.Daemon.AutoExit, true)
	assert.Equal(t, cfg.Client.DetachKeybind, "ctrl+q")
	assert.DeepEqual(t, cfg.Client.ForwardEnv, []string{"TERM"})
	assert.Equal(t, cfg.Session.DefaultCommand, "/usr/bin/fish")
}

func TestLoadInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	err := os.WriteFile(path, []byte(`not valid toml {{`), 0o600)
	assert.NilError(t, err)

	_, err = LoadFrom(path)
	assert.Error(t, err, "config: parse "+path+": toml: line 1: expected '.' or '=', but got 'v' instead")
}

func TestLoadInvalidResizePolicy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	err := os.WriteFile(path, []byte(`[session]
resize_policy = "bogus"
`), 0o600)
	assert.NilError(t, err)

	_, err = LoadFrom(path)
	assert.Error(t, err, "config: "+path+": invalid resize_policy \"bogus\"")
}

func TestLoadValidResizePolicies(t *testing.T) {
	policies := []ResizePolicy{
		ResizePolicySmallest,
		ResizePolicyLargest,
		ResizePolicyFirst,
		ResizePolicyLast,
	}
	for _, p := range policies {
		t.Run(string(p), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.toml")
			content := `[session]` + "\n" + `resize_policy = "` + string(p) + `"` + "\n"
			err := os.WriteFile(path, []byte(content), 0o600)
			assert.NilError(t, err)

			cfg, err := LoadFrom(path)
			assert.NilError(t, err)
			assert.Equal(t, cfg.Session.ResizePolicy, p)
		})
	}
}
