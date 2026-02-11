package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetupShellEnv prepares the environment and command for shell integration.
// It detects the shell from the command string, applies the appropriate
// integration mechanism, and returns the modified command and environment.
// The returned tempDir (if non-empty) must be cleaned up by the caller
// after the child process exits.
func SetupShellEnv(command string, env []string, sessionName string) (cmd string, modifiedEnv []string, tempDir string, err error) {
	cmd = command
	modifiedEnv = make([]string, len(env))
	copy(modifiedEnv, env)

	// Set HAUNTTY_SESSION for the child.
	modifiedEnv = setEnv(modifiedEnv, "HAUNTTY_SESSION", sessionName)

	// Find GHOSTTY_RESOURCES_DIR from the environment.
	resourcesDir := getEnv(modifiedEnv, "GHOSTTY_RESOURCES_DIR")
	if resourcesDir == "" {
		// No resources dir means no shell integration to inject.
		return cmd, modifiedEnv, "", nil
	}

	shell := detectShell(command)
	switch shell {
	case "zsh":
		cmd, modifiedEnv, tempDir, err = setupZsh(cmd, modifiedEnv, resourcesDir)
	case "bash":
		cmd, modifiedEnv, err = setupBash(cmd, modifiedEnv, resourcesDir)
	case "fish":
		modifiedEnv = setupFish(modifiedEnv, resourcesDir)
	}

	return cmd, modifiedEnv, tempDir, err
}

// detectShell returns "zsh", "bash", "fish", or "" from a command string.
func detectShell(command string) string {
	base := filepath.Base(command)
	// Strip leading dash (login shell).
	base = strings.TrimPrefix(base, "-")
	switch {
	case base == "zsh" || strings.HasPrefix(base, "zsh "):
		return "zsh"
	case base == "bash" || strings.HasPrefix(base, "bash "):
		return "bash"
	case base == "fish" || strings.HasPrefix(base, "fish "):
		return "fish"
	}
	return ""
}

// setupZsh creates a temporary ZDOTDIR with a .zshenv that sources
// the Ghostty integration and then the user's real .zshenv.
func setupZsh(command string, env []string, resourcesDir string) (string, []string, string, error) {
	origZdotdir := getEnv(env, "ZDOTDIR")

	tmpDir, err := os.MkdirTemp("", "hauntty-zsh-*")
	if err != nil {
		return command, env, "", fmt.Errorf("create zsh temp dir: %w", err)
	}

	// Determine the real ZDOTDIR for restoring and sourcing the user's .zshenv.
	realZdotdir := origZdotdir
	if realZdotdir == "" {
		realZdotdir = os.Getenv("HOME")
	}

	integrationScript := filepath.Join(resourcesDir, "shell-integration", "zsh", "ghostty-integration")

	var zshenv strings.Builder
	// Restore original ZDOTDIR so zsh looks in the right place for .zshrc etc.
	if origZdotdir != "" {
		fmt.Fprintf(&zshenv, "export ZDOTDIR=%q\n", origZdotdir)
	} else {
		zshenv.WriteString("unset ZDOTDIR\n")
	}

	// Source Ghostty shell integration.
	fmt.Fprintf(&zshenv, "source %q\n", integrationScript)

	// Source the user's real .zshenv if it exists.
	userZshenv := filepath.Join(realZdotdir, ".zshenv")
	fmt.Fprintf(&zshenv, "[[ -f %q ]] && source %q\n", userZshenv, userZshenv)

	if err := os.WriteFile(filepath.Join(tmpDir, ".zshenv"), []byte(zshenv.String()), 0600); err != nil {
		os.RemoveAll(tmpDir)
		return command, env, "", fmt.Errorf("write .zshenv: %w", err)
	}

	env = setEnv(env, "ZDOTDIR", tmpDir)
	return command, env, tmpDir, nil
}

// setupBash modifies the command to source Ghostty's bash integration.
func setupBash(command string, env []string, resourcesDir string) (string, []string, error) {
	integrationScript := filepath.Join(resourcesDir, "shell-integration", "bash", "ghostty.bash")
	env = setEnv(env, "GHOSTTY_BASH_INJECT", "1")
	env = setEnv(env, "ENV", integrationScript)

	// For interactive shells, use --init-file with a process substitution
	// is not possible since we exec directly. Instead, set BASH_ENV for
	// non-interactive and use --rcfile for interactive.
	env = setEnv(env, "BASH_ENV", integrationScript)

	return command, env, nil
}

// setupFish prepends the Ghostty fish vendor config to XDG_DATA_DIRS.
func setupFish(env []string, resourcesDir string) []string {
	fishDir := filepath.Join(resourcesDir, "shell-integration", "fish")
	existing := getEnv(env, "XDG_DATA_DIRS")
	if existing == "" {
		existing = "/usr/local/share:/usr/share"
	}
	env = setEnv(env, "XDG_DATA_DIRS", fishDir+":"+existing)
	return env
}

// getEnv retrieves a value from an env slice of KEY=VALUE strings.
func getEnv(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}

// setEnv sets or replaces a KEY=VALUE pair in an env slice.
func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
