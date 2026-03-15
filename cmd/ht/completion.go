package main

import (
	"fmt"
	"os"
	"path/filepath"

	"code.selman.me/hauntty/internal/client"
	"code.selman.me/hauntty/internal/completion"
	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/protocol"
	"github.com/alecthomas/kong"
)

type CompletionCmd struct {
	Shell string `arg:"" optional:"" help:"Shell name." enum:"bash,zsh,fish," default:""`
	Code  bool   `short:"c" help:"Generate shell completion code."`
}

func (cmd *CompletionCmd) Help() string {
	return `
Displays a command that you need to execute in order activate tab completion for this program.

For permanent activation (i.e. beyond the current shell session), paste the command in your shell’s init file.

If no shell is specified, it tries to detect your current login shell automatically.
`
}

func (cmd *CompletionCmd) Run(ctx *kong.Context) error {
	shell, err := resolveCompletionShell(cmd.Shell)
	if err != nil {
		return err
	}

	if cmd.Code {
		script, err := completionScript(shell, ctx.Model.Node)
		if err != nil {
			return err
		}
		if _, err := ctx.Stdout.Write(script); err != nil {
			return fmt.Errorf("write completion script: %w", err)
		}
		ctx.Exit(0)
		return nil
	}

	binName := ctx.Model.Name
	fmt.Fprintf(ctx.Stdout, "Execute the following command to activate tab completion for %s in %s:\n\n", binName, shell)
	fmt.Fprintf(ctx.Stdout, "    %s\n\n", completionInitCommand(binName, shell))
	fmt.Fprintln(ctx.Stdout, "Note that this only takes effect for your current shell session. For permanent activation, add the command to your shell init file.")

	ctx.Exit(0)
	return nil
}

type CompletionDataCmd struct {
	Topic string `arg:"" help:"Completion topic." enum:"sessions,live_sessions,dead_sessions,dumpable_sessions"`
}

func (cmd *CompletionDataCmd) Run(cfg *config.Config) error {
	c, err := client.Connect(cfg.Daemon.SocketPath)
	if err != nil {
		// Daemon not reachable; return no completions instead of an error
		// so the shell tab-complete UX degrades gracefully.
		return nil
	}
	defer c.Close()

	sessions, err := c.List(false)
	if err != nil {
		// Same graceful degradation: no completions rather than a shell error.
		return nil
	}

	for _, name := range completionTopicNames(cmd.Topic, sessions.Sessions) {
		fmt.Fprintln(os.Stdout, name)
	}
	return nil
}

func completionDynamicTopics() map[string]string {
	return map[string]string{
		"attach":  "live_sessions",
		"dump":    "dumpable_sessions",
		"kill":    "live_sessions",
		"kick":    "live_sessions",
		"restore": "dead_sessions",
		"send":    "live_sessions",
		"status":  "sessions",
		"wait":    "dumpable_sessions",
	}
}

func completionTopicNames(topic string, sessions []protocol.Session) []string {
	names := make([]string, 0, len(sessions))
	for _, session := range sessions {
		if !includeCompletionSession(topic, session) {
			continue
		}
		names = append(names, session.Name)
	}
	return names
}

func includeCompletionSession(topic string, session protocol.Session) bool {
	switch topic {
	case "live_sessions":
		return session.State == protocol.SessionStateRunning
	case "dead_sessions":
		return session.State == protocol.SessionStateDead
	case "dumpable_sessions", "sessions":
		return true
	default:
		return false
	}
}

func resolveCompletionShell(shell string) (string, error) {
	if shell == "" {
		shell = filepath.Base(os.Getenv("SHELL"))
	}
	switch shell {
	case "bash", "zsh", "fish":
		return shell, nil
	case "":
		return "", fmt.Errorf("couldn't determine user's shell")
	default:
		return "", fmt.Errorf("this shell is not supported (%s)", shell)
	}
}

func completionInitCommand(binName string, shell string) string {
	switch shell {
	case "fish":
		return fmt.Sprintf("%s completion -c fish | source", binName)
	default:
		return fmt.Sprintf("source <(%s completion -c %s)", binName, shell)
	}
}

func completionScript(shell string, node *kong.Node) ([]byte, error) {
	if node == nil {
		return nil, fmt.Errorf("nil command tree")
	}
	spec := completion.BuildSpec(node, completionDynamicTopics())
	script, err := completion.Generate(shell, node.Name, "__complete", spec)
	if err != nil {
		return nil, err
	}
	return []byte(script), nil
}
