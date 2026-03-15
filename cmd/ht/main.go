package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	hauntty "code.selman.me/hauntty"
	"code.selman.me/hauntty/internal/client"
	"code.selman.me/hauntty/internal/config"
	"code.selman.me/hauntty/internal/daemon"
	"code.selman.me/hauntty/internal/protocol"
	"github.com/BurntSushi/toml"
	"github.com/alecthomas/kong"
	"golang.org/x/term"
)

type CLI struct {
	Version    kong.VersionFlag  `help:"Print version."`
	Socket     string            `help:"Unix socket path override." env:"HAUNTTY_SOCKET"`
	Attach     AttachCmd         `cmd:"" aliases:"a" help:"Attach to a session (create if needed)."`
	New        NewCmd            `cmd:"" help:"Create a session without attaching."`
	Restore    RestoreCmd        `cmd:"" help:"Restore a dead session from saved state."`
	List       ListCmd           `cmd:"" aliases:"ls" help:"List sessions."`
	Kill       KillCmd           `cmd:"" help:"Kill a session."`
	Send       SendCmd           `cmd:"" help:"Send input to a session."`
	Dump       DumpCmd           `cmd:"" help:"Dump session contents."`
	Kick       KickCmd           `cmd:"" help:"Disconnect a specific attached client."`
	Wait       WaitCmd           `cmd:"" help:"Wait for session output to match a pattern."`
	Status     StatusCmd         `cmd:"" aliases:"st" help:"Show daemon and session status."`
	Prune      PruneCmd          `cmd:"" help:"Delete dead session state files."`
	Init       InitCmd           `cmd:"" help:"Create default config file."`
	Config     ConfigCmd         `cmd:"" help:"Print current configuration."`
	Daemon     DaemonCmd         `cmd:"" help:"Start daemon in foreground."`
	Completion CompletionCmd     `cmd:"" help:"Print shell completion setup instructions."`
	Complete   CompletionDataCmd `cmd:"" hidden:"" name:"__complete" help:"Internal completion data provider."`
}

type AttachCmd struct {
	Name     string   `arg:"" optional:"" help:"Session name."`
	Command  []string `arg:"" optional:"" help:"Command to run."`
	ReadOnly bool     `short:"r" help:"Attach in read-only mode (no input forwarded)."`
}

func (cmd *AttachCmd) Run(cfg *config.Config) error {
	if s := os.Getenv("HAUNTTY_SESSION"); s != "" {
		return fmt.Errorf("already inside session %q, nested sessions are not supported", s)
	}

	if !isInteractiveAttachTTY() {
		if cmd.Name == "" {
			return fmt.Errorf("interactive attach requires a TTY; use `ht new`")
		}
		return fmt.Errorf("interactive attach requires a TTY; use `ht new %s`", cmd.Name)
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

	command := resolveDefaultCommand(cmd.Command, cfg)

	return c.RunAttach(client.AttachOpts{
		Name:       cmd.Name,
		Command:    command,
		DetachKey:  dk,
		ForwardEnv: cfg.Client.ForwardEnv,
		ReadOnly:   cmd.ReadOnly,
	})
}

type NewCmd struct {
	Name    string   `arg:"" optional:"" help:"Session name."`
	Command []string `arg:"" optional:"" help:"Command to run."`
	Force   bool     `short:"f" help:"Overwrite dead session state if it exists."`
}

func (cmd *NewCmd) Run(cfg *config.Config) error {
	if err := ensureDaemon(cfg.Daemon.SocketPath); err != nil {
		return err
	}
	c, err := client.Connect(cfg.Daemon.SocketPath)
	if err != nil {
		return err
	}
	defer c.Close()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}

	env := client.CollectForwardedEnv(cfg.Client.ForwardEnv)
	created, err := c.Create(&protocol.Create{
		Name:       cmd.Name,
		Command:    resolveDefaultCommand(cmd.Command, cfg),
		Env:        env,
		CWD:        cwd,
		Scrollback: 0,
		Force:      cmd.Force,
	})
	if err != nil {
		return err
	}

	fmt.Printf("created session %q (pid %d)\n", created.Name, created.PID)
	return nil
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

	sessions, err := c.List(false)
	if err != nil {
		return err
	}

	home, err := os.UserHomeDir()
	if err != nil {
		slog.Debug("resolve home dir", "err", err)
		home = ""
	}

	rows := sessionListRows(sessions.Sessions, cmd.All, home)
	if len(rows) == 1 {
		fmt.Fprintln(os.Stderr, "no sessions")
		return nil
	}
	return writeSessionRows(os.Stdout, rows)
}

func sessionListRows(sessions []protocol.Session, showAll bool, home string) [][]string {
	rows := [][]string{{"NAME", "STATE", "SIZE", "CWD", "PID", "CREATED", "SAVED"}}
	for _, s := range sessions {
		if !showAll && s.State == protocol.SessionStateDead {
			continue
		}
		cwd := s.CWD
		if home != "" && strings.HasPrefix(cwd, home) {
			cwd = "~" + cwd[len(home):]
		}
		rows = append(rows, []string{
			s.Name,
			string(s.State),
			fmt.Sprintf("%dx%d", s.Cols, s.Rows),
			cwd,
			formatSessionPID(s.PID),
			formatSessionTimestamp(s.CreatedAt),
			formatSessionTimestamp(s.SavedAt),
		})
	}
	return rows
}

func writeSessionRows(w io.Writer, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	for _, row := range rows {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", row[0], row[1], row[2], row[3], row[4], row[5], row[6]); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func formatSessionPID(pid uint32) string {
	if pid == 0 {
		return "-"
	}
	return strconv.FormatUint(uint64(pid), 10)
}

func formatSessionTimestamp(ts uint32) string {
	if ts == 0 {
		return "-"
	}
	return time.Unix(int64(ts), 0).Format("2006-01-02 15:04:05")
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

	format := dumpRequestFormat(cmd.Format, cmd.Join, cmd.Scrollback)

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

func dumpRequestFormat(format string, join, scrollback bool) protocol.DumpFormat {
	var value protocol.DumpFormat
	switch format {
	case "vt":
		value = protocol.DumpVT
	case "html":
		value = protocol.DumpHTML
	}
	if join {
		value |= protocol.DumpFlagUnwrap
	}
	if scrollback {
		value |= protocol.DumpFlagScrollback
	}
	return value
}

type RestoreCmd struct {
	Name     string `arg:"" help:"Session name to restore."`
	ReadOnly bool   `short:"r" help:"Attach in read-only mode."`
}

func (cmd *RestoreCmd) Run(cfg *config.Config) error {
	if s := os.Getenv("HAUNTTY_SESSION"); s != "" {
		return fmt.Errorf("already inside session %q, nested sessions are not supported", s)
	}

	if !isInteractiveAttachTTY() {
		return fmt.Errorf("restore requires a TTY")
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

	return c.RunAttach(client.AttachOpts{
		Name:       cmd.Name,
		DetachKey:  dk,
		ForwardEnv: cfg.Client.ForwardEnv,
		ReadOnly:   cmd.ReadOnly,
		Restore:    true,
	})
}

type KickCmd struct {
	Name     string `arg:"" help:"Session name."`
	ClientID string `arg:"" help:"Client ID to disconnect."`
}

func (cmd *KickCmd) Run(cfg *config.Config) error {
	c, err := client.Connect(cfg.Daemon.SocketPath)
	if err != nil {
		return err
	}
	defer c.Close()

	if err := c.Kick(cmd.Name, cmd.ClientID); err != nil {
		return err
	}
	fmt.Printf("kicked client %s from session %q\n", cmd.ClientID, cmd.Name)
	return nil
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

	home, err := os.UserHomeDir()
	if err != nil {
		slog.Debug("resolve home dir", "err", err)
		home = ""
	}

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
		fmt.Printf("clients:  %d\n", len(s.Clients))
		for _, cl := range s.Clients {
			ro := ""
			if cl.ReadOnly {
				ro = " (read-only)"
			}
			fmt.Printf("  [%s]:   %s%s\n", cl.ClientID, cl.Version, ro)
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
	match, err := compileWaitMatcher(cmd.Pattern, cmd.Regex)
	if err != nil {
		return err
	}

	c, err := client.Connect(cfg.Daemon.SocketPath)
	if err != nil {
		return &commandExitError{code: 2, stderr: fmt.Sprintf("error: %v\n", err)}
	}
	defer c.Close()

	deadline := time.Now().Add(time.Duration(cmd.Timeout) * time.Millisecond)
	for {
		data, err := c.Dump(cmd.Name, protocol.DumpPlain)
		if err != nil {
			return &commandExitError{code: 2, stderr: fmt.Sprintf("error: %v\n", err)}
		}

		content := waitContent(string(data), cmd.Row)

		if match(content) {
			return nil
		}

		if time.Now().After(deadline) {
			return &commandExitError{code: 1, stderr: fmt.Sprintf("timeout waiting for %q\n", cmd.Pattern)}
		}

		time.Sleep(time.Duration(cmd.Interval) * time.Millisecond)
	}
}

func compileWaitMatcher(pattern string, regex bool) (func(string) bool, error) {
	if regex {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex: %w", err)
		}
		return re.MatchString, nil
	}
	return func(s string) bool { return strings.Contains(s, pattern) }, nil
}

func waitContent(content string, row int) string {
	if row < 0 {
		return content
	}
	lines := strings.Split(content, "\n")
	if row >= len(lines) {
		return ""
	}
	return lines[row]
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
	AutoExit bool   `help:"Exit when last session dies."`
	Detach   bool   `short:"d" help:"Run daemon in background and exit."`
	LogFile  string `name:"log-file" help:"Daemon log file path (detach mode only)."`
}

func (cmd *DaemonCmd) validate() error {
	if cmd.LogFile != "" && !cmd.Detach {
		return fmt.Errorf("--log-file requires --detach")
	}
	return nil
}

func (cmd *DaemonCmd) Run(cfg *config.Config) error {
	if err := cmd.validate(); err != nil {
		return err
	}

	if cmd.Detach {
		return ensureDaemonDetached(cfg.Daemon.SocketPath, cmd.AutoExit, cmd.LogFile)
	}

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

func resolveDefaultCommand(command []string, cfg *config.Config) []string {
	if len(command) > 0 {
		return command
	}
	if cfg.Session.DefaultCommand == "" {
		return nil
	}
	return strings.Fields(cfg.Session.DefaultCommand)
}

func isInteractiveAttachTTY() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

type exitCoder interface {
	ExitCode() int
}

type stderrProvider interface {
	Stderr() string
}

type commandExitError struct {
	code   int
	stderr string
}

func (e *commandExitError) Error() string {
	if e.stderr != "" {
		return strings.TrimSuffix(e.stderr, "\n")
	}
	return fmt.Sprintf("exit with code %d", e.code)
}

func (e *commandExitError) ExitCode() int {
	return e.code
}

func (e *commandExitError) Stderr() string {
	return e.stderr
}

func main() {
	var cli CLI
	parser, err := kong.New(&cli,
		kong.UsageOnError(),
		kong.Vars{"version": hauntty.Version()},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx, err := parser.Parse(os.Args[1:])
	if err != nil {
		parser.Printf("%s", err)
		parser.Exit(1)
		return
	}

	cfg, err := config.Load()
	ctx.FatalIfErrorf(err)
	if cli.Socket != "" {
		cfg.Daemon.SocketPath = cli.Socket
	}
	err = ctx.Run(cfg)
	if err == nil {
		return
	}

	var ec exitCoder
	if errors.As(err, &ec) {
		var sp stderrProvider
		if errors.As(err, &sp) {
			if stderr := sp.Stderr(); stderr != "" {
				fmt.Fprint(os.Stderr, stderr)
			}
		}
		os.Exit(ec.ExitCode())
	}

	ctx.FatalIfErrorf(err)
}

func ensureDaemon(socketPath string) error {
	return ensureDaemonDetached(socketPath, false, "")
}

func daemonStartArgs(socketPath string, autoExit bool) []string {
	args := []string{"daemon"}
	if autoExit {
		args = append(args, "--auto-exit")
	}
	if socketPath != "" {
		args = append(args, "--socket", socketPath)
	}
	return args
}

func ensureDaemonDetached(socketPath string, autoExit bool, logPath string) error {
	sock := cmp.Or(socketPath, config.SocketPath())
	running, err := client.ProbeDaemon(sock)
	if err != nil {
		return err
	}
	if running {
		return nil
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	logFile, err := openDaemonLogFile(sock, logPath)
	if err != nil {
		return err
	}
	defer logFile.Close()
	logFilePath := logFile.Name()
	tempLog := logPath == ""
	cleanupLogOnError := tempLog
	defer func() {
		if cleanupLogOnError {
			os.Remove(logFile.Name())
		}
	}()

	cmd := exec.Command(exe, daemonStartArgs(socketPath, autoExit)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = nil
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	released := false
	reaped := false
	defer func() {
		if released || reaped {
			return
		}
		_ = cmd.Process.Release()
	}()

	if tempLog {
		finalPath := filepath.Join(filepath.Dir(sock), fmt.Sprintf("hauntty-server-%d.log", cmd.Process.Pid))
		if err := os.Rename(logFile.Name(), finalPath); err != nil {
			return fmt.Errorf("rename daemon log file: %w", err)
		}
		logFilePath = finalPath
	}
	cleanupLogOnError = false

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		running, err := client.ProbeDaemon(sock)
		if err != nil {
			return daemonStartupError(sock, logFilePath, err)
		}
		if running {
			if err := cmd.Process.Release(); err != nil {
				return fmt.Errorf("release daemon process: %w", err)
			}
			released = true
			return nil
		}

		exited, err := daemonProcessExited(cmd.Process.Pid)
		if err != nil {
			if exited {
				reaped = true
			}
			return daemonStartupError(sock, logFilePath, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return daemonStartupTimeoutError(sock, logFilePath)
}

func daemonProcessExited(pid int) (bool, error) {
	var status syscall.WaitStatus
	waited, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	if err != nil {
		if errors.Is(err, syscall.ECHILD) {
			return false, nil
		}
		return false, fmt.Errorf("wait daemon process: %w", err)
	}
	if waited == 0 {
		return false, nil
	}
	return true, daemonProcessExitError(status)
}

func daemonProcessExitError(status syscall.WaitStatus) error {
	switch {
	case status.Exited():
		return fmt.Errorf("daemon exited before ready with status %d", status.ExitStatus())
	case status.Signaled():
		return fmt.Errorf("daemon exited before ready from signal %s", status.Signal())
	default:
		return fmt.Errorf("daemon exited before ready")
	}
}

func daemonStartupError(socketPath string, logPath string, cause error) error {
	if logPath == "" {
		return fmt.Errorf("daemon failed before ready at %s: %w", socketPath, cause)
	}
	return fmt.Errorf("daemon failed before ready at %s: %w (see %s)", socketPath, cause, logPath)
}

func daemonStartupTimeoutError(socketPath string, logPath string) error {
	if logPath == "" {
		return fmt.Errorf("timed out waiting for daemon at %s", socketPath)
	}
	return fmt.Errorf("timed out waiting for daemon at %s (see %s)", socketPath, logPath)
}

func openDaemonLogFile(sock string, logPath string) (*os.File, error) {
	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
			return nil, fmt.Errorf("create daemon log directory: %w", err)
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, fmt.Errorf("open daemon log file: %w", err)
		}
		return f, nil
	}

	dir := filepath.Dir(sock)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create socket dir: %w", err)
	}

	f, err := os.CreateTemp(dir, "hauntty-server-*.log")
	if err != nil {
		return nil, fmt.Errorf("create daemon log: %w", err)
	}
	return f, nil
}
