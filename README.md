# hauntty

Terminal session persistence. Run sessions that survive disconnects, reattach later.

## Why does this exist

Wanted to understand PTY internals without leaving the comfort of Go, so I prompted the shit out of Claude to help me. You're probably better off with [tmux](https://github.com/tmux/tmux), [zellij](https://github.com/zellij-org/zellij), [shpool](https://github.com/shell-pool/shpool), or [zmx](https://github.com/neurosnap/zmx).

## How it works

A Go daemon manages sessions. Each session runs a shell in a PTY and tracks terminal state using [libghostty/ghostty-vt](https://github.com/ghostty-org/ghostty) compiled to WASM. When you reattach, it reconstructs the screen from that state.

If Ghostty is installed, hauntty injects its shell integration scripts.

The `ht` client talks to the daemon over a Unix socket.

## Commands

```
attach, a     Attach to a session, create if needed
new           Create/start a session without attaching
restore       Restore a dead session from saved state
list, ls      List sessions
kill          Kill a session
send          Send input to a session without attaching
dump          Dump session screen contents
kick          Disconnect a specific attached client
wait          Wait for output to match a pattern
status, st    Show daemon and session status
prune         Delete dead session state files
init          Create default config file
config        Print current configuration
daemon        Start daemon in foreground
completion    Print shell completion setup instructions
```

Run `ht <command> --help` for details.

## Usage

```bash
ht attach work             # attach to session, create it if needed
ht attach -r work          # attach read-only
ht new work npm run dev    # create/start without attaching
ht restore work            # restore a dead session from saved state
ht status                  # show daemon/session status
ht kick work 1             # disconnect attached client 1
# detach from an attached client with ctrl+;, configured by detach_keybind
```

Daemon starts on first attach, new, or restore. Sessions persist until killed or the shell exits.
When a session exits, its saved state can be restored with `ht restore <name>`
or removed with `ht prune`.

## Install

```
go install code.selman.me/hauntty/cmd/ht@latest
```

## Building

```
go build -o ht ./cmd/ht
```

## Config

Default path is `$XDG_CONFIG_HOME/hauntty/config.toml` when `XDG_CONFIG_HOME` is set; otherwise it is `~/.config/hauntty/config.toml`. Run `ht init` to create it.

```toml
[daemon]
# Leave empty to use the default runtime socket path.
socket_path = ""

# Exit automatically when the last live session ends.
auto_exit = false

# Default scrollback size for new and attached sessions.
default_scrollback = 10000

# Persist dead session state to disk.
state_persistence = true

# Save session state every N seconds while the session is running. Must be > 0.
state_persistence_interval = 30

[client]
# Key used to detach from an attached client.
detach_keybind = "ctrl+;"

# Extra environment variables to forward from client to session.
forward_env = ["COLORTERM", "GHOSTTY_RESOURCES_DIR", "GHOSTTY_BIN_DIR"]

[session]
# Leave empty to use the user's shell as the default command.
default_command = ""

# Resize arbitration policy for multi-client sessions.
# Valid values are "smallest", "largest", "first", and "last".
resize_policy = "smallest"
```

## Environment

Forwarded from client to session:
- `TERM`, `SHELL`; always forwarded
- `COLORTERM`, `GHOSTTY_RESOURCES_DIR`, `GHOSTTY_BIN_DIR`; forwarded by default config

Configure `forward_env` in config to change the extra forwarded variables.

Set by hauntty:
- `HAUNTTY_SESSION` - session name, set inside sessions
- `HAUNTTY_SOCKET` - socket path override for client commands

## Prior art

- [Ghostty](https://github.com/ghostty-org/ghostty)
- [shpool](https://github.com/shell-pool/shpool)
- [zmx](https://github.com/neurosnap/zmx)
- [abduco](https://github.com/martanne/abduco)
