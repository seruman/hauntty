package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/alecthomas/kong"
	"github.com/selman/hauntty/client"
	"github.com/selman/hauntty/config"
	"github.com/selman/hauntty/daemon"
)

type CLI struct {
	Attach AttachCmd `cmd:"" aliases:"a" help:"Attach to a session (create if needed)."`
	List   ListCmd   `cmd:"" aliases:"ls" help:"List sessions."`
	Kill   KillCmd   `cmd:"" help:"Kill a session."`
	Send   SendCmd   `cmd:"" help:"Send input to a session."`
	Dump   DumpCmd   `cmd:"" help:"Dump session contents."`
	Detach DetachCmd `cmd:"" help:"Detach from current session."`
	Daemon DaemonCmd `cmd:"" help:"Start daemon in foreground."`
}

type AttachCmd struct {
	Name    string   `arg:"" optional:"" help:"Session name."`
	Command []string `arg:"" optional:"" passthrough:"" help:"Command to run (after --)."`
}

func (cmd *AttachCmd) Run() error {
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

	return c.RunAttach(cmd.Name, command)
}

type ListCmd struct{}

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

	if len(sessions.Sessions) == 0 {
		fmt.Println("no sessions")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tSIZE\tPID\tCREATED")
	for _, s := range sessions.Sessions {
		if s.PID == 0 {
			fmt.Fprintf(w, "%s\t%s\t-\t-\t-\n", s.Name, s.State)
		} else {
			created := time.Unix(int64(s.CreatedAt), 0).Format("2006-01-02 15:04:05")
			fmt.Fprintf(w, "%s\t%s\t%dx%d\t%d\t%s\n",
				s.Name, s.State, s.Cols, s.Rows, s.PID, created)
		}
	}
	w.Flush()
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
	Args []string `arg:"" passthrough:"" help:"Input text and --keys interleaved."`
}

func (cmd *SendCmd) Run() error {
	args := cmd.Args
	// Strip leading "--" that kong passes through.
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}

	var data []byte
	for i := 0; i < len(args); i++ {
		if args[i] == "--keys" {
			i++
			if i >= len(args) {
				return fmt.Errorf("--keys requires a value")
			}
			keyBytes, err := client.ParseKeyNotation(args[i])
			if err != nil {
				return err
			}
			data = append(data, keyBytes...)
		} else {
			data = append(data, args[i]...)
		}
	}

	if len(data) == 0 {
		return fmt.Errorf("send requires input")
	}

	c, err := client.Connect()
	if err != nil {
		return err
	}
	defer c.Close()

	return c.Send(cmd.Name, data)
}

type DumpCmd struct {
	Name   string `arg:"" help:"Session name."`
	Format string `enum:"plain,vt,html" default:"plain" help:"Output format (plain, vt, html)."`
}

func (cmd *DumpCmd) Run() error {
	var format uint8
	switch cmd.Format {
	case "vt":
		format = 1
	case "html":
		format = 2
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

	wasmPath := resolveWASMPath()
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return fmt.Errorf("read wasm: %w", err)
	}

	ctx := context.Background()
	srv, err := daemon.New(ctx, wasmBytes, &cfg.Daemon)
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

// resolveWASMPath finds the WASM module: env var > next to executable > hardcoded fallback.
func resolveWASMPath() string {
	if p := os.Getenv("HAUNTTY_WASM_PATH"); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "hauntty-vt.wasm")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "vt/zig-out/bin/hauntty-vt.wasm"
}
