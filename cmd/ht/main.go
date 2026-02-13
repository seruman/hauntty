package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/alecthomas/kong"
	"github.com/selman/hauntty/client"
	"github.com/selman/hauntty/config"
	"github.com/selman/hauntty/daemon"
	"github.com/selman/hauntty/protocol"
)

type CLI struct {
	Attach AttachCmd `cmd:"" aliases:"a" help:"Attach to a session (create if needed)."`
	List   ListCmd   `cmd:"" aliases:"ls" help:"List sessions."`
	Kill   KillCmd   `cmd:"" help:"Kill a session."`
	Send   SendCmd   `cmd:"" help:"Send input to a session."`
	Dump   DumpCmd   `cmd:"" help:"Dump session contents."`
	Detach DetachCmd `cmd:"" help:"Detach from current session."`
	Prune  PruneCmd  `cmd:"" help:"Delete dead session state files."`
	Daemon DaemonCmd `cmd:"" help:"Start daemon in foreground."`
}

type AttachCmd struct {
	Name    string   `arg:"" optional:"" help:"Session name."`
	Command []string `arg:"" optional:"" passthrough:"" help:"Command to run (after --)."`
}

func (cmd *AttachCmd) Run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	dk, err := client.ParseDetachKey(cfg.Client.DetachKeybind)
	if err != nil {
		return fmt.Errorf("invalid detach_keybind %q: %w", cfg.Client.DetachKeybind, err)
	}

	if err := ensureDaemon(); err != nil {
		return err
	}
	c, err := client.Connect()
	if err != nil {
		return err
	}
	defer c.Close()

	var command string
	if len(cmd.Command) > 0 {
		// Strip leading "--" that kong passes through.
		args := cmd.Command
		if len(args) > 0 && args[0] == "--" {
			args = args[1:]
		}
		command = strings.Join(args, " ")
	}

	return c.RunAttach(cmd.Name, command, dk)
}

type ListCmd struct {
	All bool `short:"a" help:"Show all sessions including dead."`
}

func (cmd *ListCmd) Run() error {
	c, err := client.Connect()
	if err != nil {
		return err
	}
	defer c.Close()

	sessions, err := c.List()
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tSIZE\tPID\tCREATED")
	n := 0
	for _, s := range sessions.Sessions {
		if !cmd.All && s.State == "dead" {
			continue
		}
		n++
		if s.PID == 0 {
			created := "-"
			if s.CreatedAt != 0 {
				created = time.Unix(int64(s.CreatedAt), 0).Format("2006-01-02 15:04:05")
			}
			fmt.Fprintf(w, "%s\t%s\t%dx%d\t-\t%s\n", s.Name, s.State, s.Cols, s.Rows, created)
		} else {
			created := time.Unix(int64(s.CreatedAt), 0).Format("2006-01-02 15:04:05")
			fmt.Fprintf(w, "%s\t%s\t%dx%d\t%d\t%s\n",
				s.Name, s.State, s.Cols, s.Rows, s.PID, created)
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

func (cmd *KillCmd) Run() error {
	c, err := client.Connect()
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

func (cmd *SendCmd) Run() error {
	if len(cmd.Text) == 0 && len(cmd.Key) == 0 {
		return fmt.Errorf("send requires input")
	}

	c, err := client.Connect()
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
	Name   string `arg:"" help:"Session name."`
	Format string `enum:"plain,vt,html" default:"plain" help:"Output format (plain, vt, html)."`
	Join   bool   `short:"J" help:"Join soft-wrapped lines."`
}

func (cmd *DumpCmd) Run() error {
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

	c, err := client.Connect()
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

func (cmd *DetachCmd) Run() error {
	sessionName := os.Getenv("HAUNTTY_SESSION")
	if sessionName == "" {
		return fmt.Errorf("not inside a hauntty session")
	}

	c, err := client.Connect()
	if err != nil {
		return err
	}
	defer c.Close()

	return c.DetachSession(sessionName)
}

type PruneCmd struct{}

func (cmd *PruneCmd) Run() error {
	c, err := client.Connect()
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

type DaemonCmd struct {
	AutoExit bool `help:"Exit when last session dies."`
}

func (cmd *DaemonCmd) Run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cmd.AutoExit {
		cfg.Daemon.AutoExit = true
	}

	ctx := context.Background()
	srv, err := daemon.New(ctx, &cfg.Daemon)
	if err != nil {
		return fmt.Errorf("init daemon: %w", err)
	}

	return srv.Listen()
}

func main() {
	var cli CLI
	ctx := kong.Parse(&cli, kong.UsageOnError())
	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}

func ensureDaemon() error {
	if client.DaemonRunning() {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}
	cmd := exec.Command(exe, "daemon")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}
	cmd.Process.Release()

	sock := client.SocketPath()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err == nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for daemon socket at %s", sock)
}
