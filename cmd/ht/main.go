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

	"github.com/selman/hauntty/client"
	"github.com/selman/hauntty/daemon"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "attach", "a":
		cmdAttach(os.Args[2:])
	case "list", "ls":
		cmdList()
	case "kill":
		cmdKill(os.Args[2:])
	case "send":
		cmdSend(os.Args[2:])
	case "dump":
		cmdDump(os.Args[2:])
	case "detach":
		cmdDetach()
	case "daemon":
		cmdDaemon(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "ht: unknown command %q\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Usage: ht <command> [args]

Commands:
  attach [name] [-- command]   Attach to a session (create if needed)
  list                         List sessions
  kill <name>                  Kill a session
  send <name> <input>          Send input to a session
  dump <name>                  Dump session contents
  detach                       Detach from current session
  daemon                       Start daemon in foreground
`)
}

// ensureDaemon starts the daemon if it is not already running and waits
// for the socket to appear.
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
	// Release the child so it doesn't become a zombie.
	cmd.Process.Release()

	// Wait up to 3 seconds for the socket to appear.
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

func cmdAttach(args []string) {
	var name, command string
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			if i+1 < len(args) {
				command = strings.Join(args[i+1:], " ")
			}
			break
		}
		if name == "" {
			name = args[i]
		}
	}

	if err := ensureDaemon(); err != nil {
		fmt.Fprintf(os.Stderr, "ht: %v\n", err)
		os.Exit(1)
	}

	c, err := client.Connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ht: %v\n", err)
		os.Exit(1)
	}
	defer c.Close()

	if err := c.RunAttach(name, command); err != nil {
		fmt.Fprintf(os.Stderr, "ht: %v\n", err)
		os.Exit(1)
	}
}

func cmdList() {
	c, err := client.Connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ht: %v\n", err)
		os.Exit(1)
	}
	defer c.Close()

	sessions, err := c.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ht: %v\n", err)
		os.Exit(1)
	}

	if len(sessions.Sessions) == 0 {
		fmt.Println("no sessions")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tSTATE\tSIZE\tPID\tCREATED")
	for _, s := range sessions.Sessions {
		created := time.Unix(int64(s.CreatedAt), 0).Format("2006-01-02 15:04:05")
		fmt.Fprintf(w, "%s\t%s\t%dx%d\t%d\t%s\n",
			s.Name, s.State, s.Cols, s.Rows, s.PID, created)
	}
	w.Flush()
}

func cmdKill(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "ht: kill requires a session name")
		os.Exit(1)
	}

	c, err := client.Connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ht: %v\n", err)
		os.Exit(1)
	}
	defer c.Close()

	if err := c.Kill(args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "ht: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("killed session %q\n", args[0])
}

func cmdSend(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "ht: send requires a session name and input")
		os.Exit(1)
	}

	name := args[0]
	args = args[1:]

	// Process args left-to-right: plain args are literal text, --keys args are key notation.
	var data []byte
	for i := 0; i < len(args); i++ {
		if args[i] == "--keys" {
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "ht: --keys requires a value")
				os.Exit(1)
			}
			keyBytes, err := client.ParseKeyNotation(args[i])
			if err != nil {
				fmt.Fprintf(os.Stderr, "ht: %v\n", err)
				os.Exit(1)
			}
			data = append(data, keyBytes...)
		} else {
			data = append(data, args[i]...)
		}
	}

	c, err := client.Connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ht: %v\n", err)
		os.Exit(1)
	}
	defer c.Close()

	if err := c.Send(name, data); err != nil {
		fmt.Fprintf(os.Stderr, "ht: %v\n", err)
		os.Exit(1)
	}
}

func cmdDump(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "ht: dump requires a session name")
		os.Exit(1)
	}

	name := args[0]
	var format uint8 // 0=plain (default)

	for i := 1; i < len(args); i++ {
		if args[i] == "--format" {
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "ht: --format requires a value")
				os.Exit(1)
			}
			switch args[i] {
			case "plain":
				format = 0
			case "vt":
				format = 1
			case "html":
				format = 2
			default:
				fmt.Fprintf(os.Stderr, "ht: unknown format %q (use plain, vt, or html)\n", args[i])
				os.Exit(1)
			}
		}
	}

	c, err := client.Connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ht: %v\n", err)
		os.Exit(1)
	}
	defer c.Close()

	data, err := c.Dump(name, format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ht: %v\n", err)
		os.Exit(1)
	}
	os.Stdout.Write(data)
}

func cmdDetach() {
	sessionName := os.Getenv("HAUNTTY_SESSION")
	if sessionName == "" {
		fmt.Fprintln(os.Stderr, "ht: not inside a hauntty session")
		os.Exit(1)
	}
	// Send Ctrl-\ to trigger detach in the attach loop.
	os.Stdout.Write([]byte{0x1c})
}

func cmdDaemon(args []string) {
	_ = args
	wasmPath := "vt/zig-out/bin/hauntty-vt.wasm"
	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ht: read wasm: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	srv, err := daemon.New(ctx, wasmBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ht: init daemon: %v\n", err)
		os.Exit(1)
	}

	if err := srv.Listen(); err != nil {
		fmt.Fprintf(os.Stderr, "ht: %v\n", err)
		os.Exit(1)
	}
}
