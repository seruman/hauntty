package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Daemon  DaemonConfig  `toml:"daemon"`
	Client  ClientConfig  `toml:"client"`
	Session SessionConfig `toml:"session"`
}

type DaemonConfig struct {
	SocketPath               string `toml:"socket_path"`
	AutoExit                 bool   `toml:"auto_exit"`
	DefaultScrollback        uint32 `toml:"default_scrollback"`
	StatePersistence         bool   `toml:"state_persistence"`
	StatePersistenceInterval int    `toml:"state_persistence_interval"`
}

type ClientConfig struct {
	DetachKeybind string `toml:"detach_keybind"`
}

type SessionConfig struct {
	DefaultCommand string   `toml:"default_command"`
	ForwardEnv     []string `toml:"forward_env"`
	ResizePolicy   string   `toml:"resize_policy"`
}

func Default() *Config {
	return &Config{
		Daemon: DaemonConfig{
			DefaultScrollback:        10000,
			StatePersistence:         true,
			StatePersistenceInterval: 30,
		},
		Client: ClientConfig{
			DetachKeybind: "ctrl+;",
		},
		Session: SessionConfig{
			ForwardEnv:   []string{"COLORTERM", "GHOSTTY_RESOURCES_DIR", "GHOSTTY_BIN_DIR"},
			ResizePolicy: "smallest",
		},
	}
}

func Load() (*Config, error) {
	path, err := DefaultPath()
	if err != nil {
		return Default(), nil
	}
	return LoadFrom(path)
}

func LoadFrom(path string) (*Config, error) {
	cfg := Default()

	_, err := toml.DecodeFile(path, cfg)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	return cfg, nil
}

func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "hauntty", "config.toml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "hauntty", "config.toml"), nil
}
