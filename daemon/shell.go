package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The returned tempDir (if non-empty) must be cleaned up by the caller.
func SetupShellEnv(command string, env []string, sessionName string) (cmd string, modifiedEnv []string, tempDir string, err error) {
	cmd = command
	modifiedEnv = make([]string, len(env))
	copy(modifiedEnv, env)

	modifiedEnv = setEnv(modifiedEnv, "HAUNTTY_SESSION", sessionName)

	resourcesDir := getEnv(modifiedEnv, "GHOSTTY_RESOURCES_DIR")
	if resourcesDir == "" {
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

func detectShell(command string) string {
	base := filepath.Base(command)
	base = strings.TrimPrefix(base, "-") // -zsh → zsh (login shell)
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

// Creates a temporary ZDOTDIR with a .zshenv that sources Ghostty
// integration, then delegates to the user's real .zshenv.
func setupZsh(command string, env []string, resourcesDir string) (string, []string, string, error) {
	origZdotdir := getEnv(env, "ZDOTDIR")

	tmpDir, err := os.MkdirTemp("", "hauntty-zsh-*")
	if err != nil {
		return command, env, "", fmt.Errorf("create zsh temp dir: %w", err)
	}

	realZdotdir := origZdotdir
	if realZdotdir == "" {
		realZdotdir = os.Getenv("HOME")
	}

	integrationScript := filepath.Join(resourcesDir, "shell-integration", "zsh", "ghostty-integration")

	var zshenv strings.Builder
	// Restore original ZDOTDIR so zsh finds .zshrc in the right place.
	if origZdotdir != "" {
		fmt.Fprintf(&zshenv, "export ZDOTDIR=%q\n", origZdotdir)
	} else {
		zshenv.WriteString("unset ZDOTDIR\n")
	}

	fmt.Fprintf(&zshenv, "source %q\n", integrationScript)

	userZshenv := filepath.Join(realZdotdir, ".zshenv")
	fmt.Fprintf(&zshenv, "[[ -f %q ]] && source %q\n", userZshenv, userZshenv)

	if err := os.WriteFile(filepath.Join(tmpDir, ".zshenv"), []byte(zshenv.String()), 0o600); err != nil {
		os.RemoveAll(tmpDir)
		return command, env, "", fmt.Errorf("write .zshenv: %w", err)
	}

	env = setEnv(env, "ZDOTDIR", tmpDir)
	return command, env, tmpDir, nil
}

func setupBash(command string, env []string, resourcesDir string) (string, []string, error) {
	integrationScript := filepath.Join(resourcesDir, "shell-integration", "bash", "ghostty.bash")
	env = setEnv(env, "GHOSTTY_BASH_INJECT", "1")
	env = setEnv(env, "ENV", integrationScript)

	// Can't use --init-file since we exec directly; BASH_ENV covers both.
	env = setEnv(env, "BASH_ENV", integrationScript)

	return command, env, nil
}

func setupFish(env []string, resourcesDir string) []string {
	fishDir := filepath.Join(resourcesDir, "shell-integration", "fish")
	existing := getEnv(env, "XDG_DATA_DIRS")
	if existing == "" {
		existing = "/usr/local/share:/usr/share"
	}
	env = setEnv(env, "XDG_DATA_DIRS", fishDir+":"+existing)
	return env
}

func getEnv(env []string, key string) string {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}

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
