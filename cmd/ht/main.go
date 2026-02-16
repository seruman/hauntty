package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	hauntty "code.selman.me/hauntty"
	"code.selman.me/hauntty/client"
	"code.selman.me/hauntty/config"
	"code.selman.me/hauntty/daemon"
	"code.selman.me/hauntty/protocol"
	"github.com/BurntSushi/toml"
	"github.com/alecthomas/kong"
)

type CLI struct {
	Version kong.VersionFlag `help:"Print version."`
	Socket  string           `help:"Unix socket path override." env:"HAUNTTY_SOCKET"`
	Attach  AttachCmd        `cmd:"" aliases:"a" help:"Attach to a session (create if needed)."`
	List    ListCmd          `cmd:"" aliases:"ls" help:"List sessions."`
	Kill    KillCmd          `cmd:"" help:"Kill a session."`
	Send    SendCmd          `cmd:"" help:"Send input to a session."`
	Dump    DumpCmd          `cmd:"" help:"Dump session contents."`
	Detach  DetachCmd        `cmd:"" help:"Detach from current session."`
	Wait    WaitCmd          `cmd:"" help:"Wait for session output to match a pattern."`
	Status  StatusCmd        `cmd:"" aliases:"st" help:"Show daemon and session status."`
	Prune   PruneCmd         `cmd:"" help:"Delete dead session state files."`
	Init    InitCmd          `cmd:"" help:"Create default config file."`
	Config  ConfigCmd        `cmd:"" help:"Print effective configuration."`
	Daemon  DaemonCmd        `cmd:"" help:"Start daemon in foreground."`
}

type AttachCmd struct {
	Name    string   `arg:"" optional:"" help:"Session name."`
	Command []string `arg:"" optional:"" help:"Command to run."`
}

func (cmd *AttachCmd) Run(cfg *config.Config) error {
	if s := os.Getenv("HAUNTTY_SESSION"); s != "" {
		return fmt.Errorf("already inside session %q, nested sessions are not supported", s)
	}

	dk, err := client.ParseDetachKey(cfg.Client.DetachKeybind)
	if err != nil {
		return fmt.Errorf("invalid detach_keybind %q: %w", cfg.Client.DetachKeybind, err)
	}

	if err := ensureDaemon(cfg.Daemon.SocketPath); err != nil {
		return err
	}
	c, err := client.Connect(cfg.Daemon.SocketPath)
	if err != nil {
		return err
	}
	defer c.Close()

	command := cmd.Command
	if len(command) == 0 && cfg.Session.DefaultCommand != "" {
		command = strings.Fields(cfg.Session.DefaultCommand)
	}

	return c.RunAttach(cmd.Name, command, dk, cfg.Session.ForwardEnv)
}

type ListCmd struct {
	All bool `short:"a" help:"Show all sessions including dead."`
}

func (cmd *ListCmd) Run(cfg *config.Config) error {
	c, err := client.Connect(cfg.Daemon.SocketPath)
	if err != nil {
		return err
	}
	defer c.Close()

	sessions, err := c.List()
	if err != nil {
		return err
	}

	home, _ := os.UserHomeDir()

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tSIZE\tCWD\tPID\tCREATED")
	n := 0
	for _, s := range sessions.Sessions {
		if !cmd.All && s.State == "dead" {
			continue
		}
		n++
		cwd := s.CWD
		if home != "" && strings.HasPrefix(cwd, home) {
			cwd = "~" + cwd[len(home):]
		}
		if s.PID == 0 {
			created := "-"
			if s.CreatedAt != 0 {
				created = time.Unix(int64(s.CreatedAt), 0).Format("2006-01-02 15:04:05")
			}
			fmt.Fprintf(w, "%s\t%s\t%dx%d\t%s\t-\t%s\n", s.Name, s.State, s.Cols, s.Rows, cwd, created)
		} else {
			created := time.Unix(int64(s.CreatedAt), 0).Format("2006-01-02 15:04:05")
			fmt.Fprintf(w, "%s\t%s\t%dx%d\t%s\t%d\t%s\n",
				s.Name, s.State, s.Cols, s.Rows, cwd, s.PID, created)
		}
	}
	if n == 0 {
		fmt.Fprintln(os.Stderr, "no sessions")
	} else {
		w.Flush()
	}
	return nil
}

type KillCmd struct {
	Name string `arg:"" help:"Session name."`
}

func (cmd *KillCmd) Run(cfg *config.Config) error {
	c, err := client.Connect(cfg.Daemon.SocketPath)
	if err != nil {
		return err
	}
	defer c.Close()

	if err := c.Kill(cmd.Name); err != nil {
		return err
	}
	fmt.Printf("killed session %q\n", cmd.Name)
	return nil
}

type SendCmd struct {
	Name string   `arg:"" help:"Session name."`
	Text []string `arg:"" optional:"" help:"Text to send."`
	Key  []string `short:"k" name:"key" help:"Key notation (repeatable)." sep:"none"`
}

func (cmd *SendCmd) Run(cfg *config.Config) error {
	if len(cmd.Text) == 0 && len(cmd.Key) == 0 {
		return fmt.Errorf("send requires input")
	}

	c, err := client.Connect(cfg.Daemon.SocketPath)
	if err != nil {
		return err
	}
	defer c.Close()

	for _, t := range cmd.Text {
		if err := c.Send(cmd.Name, []byte(t)); err != nil {
			return err
		}
	}
	for _, k := range cmd.Key {
		ki, err := client.ParseKeyNotation(k)
		if err != nil {
			return err
		}
		if err := c.SendKey(cmd.Name, ki.Code, ki.Mods); err != nil {
			return err
		}
	}
	return nil
}

type DumpCmd struct {
	Name       string `arg:"" optional:"" help:"Session name (default: current session)."`
	Format     string `enum:"plain,vt,html" default:"plain" help:"Output format (plain, vt, html)."`
	Join       bool   `short:"J" help:"Join soft-wrapped lines."`
	Scrollback bool   `short:"S" help:"Include scrollback history."`
}

func (cmd *DumpCmd) Run(cfg *config.Config) error {
	if cmd.Name == "" {
		cmd.Name = os.Getenv("HAUNTTY_SESSION")
		if cmd.Name == "" {
			return fmt.Errorf("session name required (or run inside a hauntty session)")
		}
	}

	var format uint8
	switch cmd.Format {
	case "vt":
		format = protocol.DumpVT
	case "html":
		format = protocol.DumpHTML
	}
	if cmd.Join {
		format |= protocol.DumpFlagUnwrap
	}
	if cmd.Scrollback {
		format |= protocol.DumpFlagScrollback
	}

	c, err := client.Connect(cfg.Daemon.SocketPath)
	if err != nil {
		return err
	}
	defer c.Close()

	data, err := c.Dump(cmd.Name, format)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(data)
	return err
}

type DetachCmd struct{}

func (cmd *DetachCmd) Run(cfg *config.Config) error {
	sessionName := os.Getenv("HAUNTTY_SESSION")
	if sessionName == "" {
		return fmt.Errorf("not inside a hauntty session")
	}

	c, err := client.Connect(cfg.Daemon.SocketPath)
	if err != nil {
		return err
	}
	defer c.Close()

	return c.DetachSession(sessionName)
}

type StatusCmd struct{}

func (cmd *StatusCmd) Run(cfg *config.Config) error {
	c, err := client.Connect(cfg.Daemon.SocketPath)
	if err != nil {
		return err
	}
	defer c.Close()

	resp, err := c.Status(os.Getenv("HAUNTTY_SESSION"))
	if err != nil {
		return err
	}

	home, _ := os.UserHomeDir()

	d := resp.Daemon
	fmt.Printf("daemon:   running (pid %d, uptime %s)\n", d.PID, formatUptime(d.Uptime))
	fmt.Printf("version:  client=%s server=%s\n", hauntty.Version(), d.Version)
	fmt.Printf("socket:   %s\n", d.SocketPath)
	fmt.Printf("sessions: %d running, %d dead\n", d.RunningCount, d.DeadCount)

	if resp.Session != nil {
		s := resp.Session
		cwd := s.CWD
		if home != "" && strings.HasPrefix(cwd, home) {
			cwd = "~" + cwd[len(home):]
		}
		fmt.Println()
		fmt.Printf("session:  %s\n", s.Name)
		fmt.Printf("state:    %s\n", s.State)
		fmt.Printf("size:     %dx%d\n", s.Cols, s.Rows)
		fmt.Printf("cwd:      %s\n", cwd)
		fmt.Printf("pid:      %d\n", s.PID)
		fmt.Printf("clients:  %d\n", s.ClientCount)
		for i, v := range s.ClientVersions {
			fmt.Printf("  [%d]:    %s\n", i, v)
		}
	}

	return nil
}

func formatUptime(seconds uint32) string {
	d := time.Duration(seconds) * time.Second
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

type WaitCmd struct {
	Name     string `arg:"" help:"Session name."`
	Pattern  string `arg:"" help:"Pattern to match."`
	Regex    bool   `short:"e" help:"Use regex matching."`
	Timeout  int    `short:"t" default:"30000" help:"Timeout in milliseconds."`
	Row      int    `default:"-1" help:"Only check specific row (0-indexed)."`
	Interval int    `default:"100" help:"Poll interval in milliseconds."`
}

func (cmd *WaitCmd) Run(cfg *config.Config) error {
	var match func(string) bool
	if cmd.Regex {
		re, err := regexp.Compile(cmd.Pattern)
		if err != nil {
			return fmt.Errorf("invalid regex: %w", err)
		}
		match = re.MatchString
	} else {
		match = func(s string) bool { return strings.Contains(s, cmd.Pattern) }
	}

	c, err := client.Connect(cfg.Daemon.SocketPath)
	if err != nil {
		os.Exit(2)
	}
	defer c.Close()

	deadline := time.Now().Add(time.Duration(cmd.Timeout) * time.Millisecond)
	for {
		data, err := c.Dump(cmd.Name, protocol.DumpPlain)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(2)
		}

		content := string(data)
		if cmd.Row >= 0 {
			lines := strings.Split(content, "\n")
			if cmd.Row < len(lines) {
				content = lines[cmd.Row]
			} else {
				content = ""
			}
		}

		if match(content) {
			return nil
		}

		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "timeout waiting for %q\n", cmd.Pattern)
			os.Exit(1)
		}

		time.Sleep(time.Duration(cmd.Interval) * time.Millisecond)
	}
}

type PruneCmd struct{}

func (cmd *PruneCmd) Run(cfg *config.Config) error {
	c, err := client.Connect(cfg.Daemon.SocketPath)
	if err != nil {
		return err
	}
	defer c.Close()

	count, err := c.Prune()
	if err != nil {
		return err
	}
	if count == 0 {
		fmt.Println("no dead sessions to prune")
	} else {
		fmt.Printf("pruned %d dead session(s)\n", count)
	}
	return nil
}

type InitCmd struct{}

func (cmd *InitCmd) Run(_ *config.Config) error {
	path, err := config.DefaultPath()
	if err != nil {
		return err
	}

	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("config already exists: %s", path)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	defer f.Close()

	if err := toml.NewEncoder(f).Encode(config.Default()); err != nil {
		return fmt.Errorf("write config: %w", err)
	}

	fmt.Printf("created %s\n", path)
	return nil
}

type ConfigCmd struct{}

func (cmd *ConfigCmd) Run(cfg *config.Config) error {
	return toml.NewEncoder(os.Stdout).Encode(cfg)
}

type DaemonCmd struct {
	AutoExit bool `help:"Exit when last session dies."`
}

func (cmd *DaemonCmd) Run(cfg *config.Config) error {
	if cmd.AutoExit {
		cfg.Daemon.AutoExit = true
	}

	ctx := context.Background()
	srv, err := daemon.New(ctx, &cfg.Daemon, cfg.Session.ResizePolicy)
	if err != nil {
		return fmt.Errorf("init daemon: %w", err)
	}

	return srv.Listen()
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.UsageOnError(),
		kong.Vars{"version": hauntty.Version()},
	)
	cfg, err := config.Load()
	ctx.FatalIfErrorf(err)
	if cli.Socket != "" {
		cfg.Daemon.SocketPath = cli.Socket
	}
	err = ctx.Run(cfg)
	ctx.FatalIfErrorf(err)
}

func ensureDaemon(socketPath string) error {
	if client.DaemonRunning(socketPath) {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	dir := filepath.Dir(config.SocketPath())
	os.MkdirAll(dir, 0o700)
	logFile, err := os.CreateTemp(dir, "hauntty-server-*.log")
	if err != nil {
		return fmt.Errorf("create daemon log: %w", err)
	}

	cmd := exec.Command(exe, "daemon")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = nil
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		logFile.Close()
		os.Remove(logFile.Name())
		return fmt.Errorf("start daemon: %w", err)
	}
	finalPath := config.LogPath(cmd.Process.Pid)
	os.Rename(logFile.Name(), finalPath)
	logFile.Close()
	cmd.Process.Release()

	sock := config.SocketPathFrom(socketPath)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.Dial("unix", sock)
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for daemon at %s", sock)
}
