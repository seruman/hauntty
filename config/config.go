package config

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config is the top-level hauntty configuration.
type Config struct {
	Daemon  DaemonConfig  `toml:"daemon"`
	Client  ClientConfig  `toml:"client"`
	Session SessionConfig `toml:"session"`
}

// DaemonConfig holds daemon-related settings.
type DaemonConfig struct {
	SocketPath               string `toml:"socket_path"`
	AutoExit                 bool   `toml:"auto_exit"`
	DefaultScrollback        uint32 `toml:"default_scrollback"`
	StatePersistence         bool   `toml:"state_persistence"`
	StatePersistenceInterval int    `toml:"state_persistence_interval"`
}

// ClientConfig holds client-related settings.
type ClientConfig struct {
	DetachKeybind string `toml:"detach_keybind"`
}

// SessionConfig holds session-related settings.
type SessionConfig struct {
	DefaultCommand string   `toml:"default_command"`
	ForwardEnv     []string `toml:"forward_env"`
}

// Default returns a Config populated with default values.
func Default() *Config {
	return &Config{
		Daemon: DaemonConfig{
			DefaultScrollback:        10000,
			StatePersistence:         true,
			StatePersistenceInterval: 30,
		},
		Client: ClientConfig{
			DetachKeybind: `ctrl+\`,
		},
		Session: SessionConfig{
			ForwardEnv: []string{"GHOSTTY_RESOURCES_DIR", "GHOSTTY_BIN_DIR"},
		},
	}
}

// Load reads the configuration from the default path
// ($XDG_CONFIG_HOME/hauntty/config.toml or ~/.config/hauntty/config.toml).
// If the file does not exist, defaults are returned without error.
func Load() (*Config, error) {
	return LoadFrom(defaultPath())
}

// LoadFrom reads the configuration from the given path.
// If the file does not exist, defaults are returned without error.
func LoadFrom(path string) (*Config, error) {
	cfg := Default()

	_, err := toml.DecodeFile(path, cfg)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return nil, err
	}

	applyDefaults(cfg)
	return cfg, nil
}

// applyDefaults fills in zero-valued fields with their default values.
func applyDefaults(cfg *Config) {
	d := Default()
	if cfg.Daemon.DefaultScrollback == 0 {
		cfg.Daemon.DefaultScrollback = d.Daemon.DefaultScrollback
	}
	if cfg.Daemon.StatePersistenceInterval == 0 {
		cfg.Daemon.StatePersistenceInterval = d.Daemon.StatePersistenceInterval
	}
	if cfg.Client.DetachKeybind == "" {
		cfg.Client.DetachKeybind = d.Client.DetachKeybind
	}
	if cfg.Session.ForwardEnv == nil {
		cfg.Session.ForwardEnv = d.Session.ForwardEnv
	}
}

// defaultPath returns the default config file path.
func defaultPath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "hauntty", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "hauntty", "config.toml")
}
