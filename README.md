# hauntty

Terminal session persistence. Run sessions that survive disconnects, reattach later.

## Why does this exist

Wanted to understand PTY internals without leaving the comfort of Go, so I prompted the shit out of Claude to help me. You're probably better off with [tmux](https://github.com/tmux/tmux), [zellij](https://github.com/zellij-org/zellij), [shpool](https://github.com/shell-pool/shpool), or [zmx](https://github.com/neurosnap/zmx).

## How it works

A Go daemon manages sessions. Each session runs a shell in a PTY and tracks terminal state using [libghostty/ghostty-vt](https://github.com/ghostty-org/ghostty) compiled to WASM. When you reattach, it reconstructs the screen from that state.

If Ghostty is installed, hauntty injects its shell integration scripts.

The client (`ht`) talks to the daemon over a Unix socket.

## Usage

```bash
ht attach work          # attach to session (creates if needed)
ht new work npm run dev # create/start without attaching
ht list                 # list sessions
ht kill work            # kill a session
# detach with ctrl+;
```

Daemon starts on first attach/new. Sessions persist until killed or the shell exits.

## Commands

```
attach (a)    Attach to a session, create if needed
new           Create/start a session without attaching
list (ls)     List sessions
kill          Kill a session
send          Send input to a session without attaching
dump          Dump session screen contents
detach        Detach from current session
wait          Wait for output to match a pattern
status (st)   Show daemon and session status
prune         Delete dead session state files
init          Create default config file
config        Print current configuration
daemon        Start daemon in foreground
```

Run `ht <command> --help` for details.

## Install

```
go install code.selman.me/hauntty/cmd/ht@latest
```

## Building

```
go build -o ht ./cmd/ht
```

## Config

`~/.config/hauntty/config.toml` - run `ht init` to create it.

## Environment

Forwarded from client to session:
- `TERM`, `SHELL` (always)
- `COLORTERM`, `GHOSTTY_RESOURCES_DIR`, `GHOSTTY_BIN_DIR` (default config)

Configure `forward_env` in config to change. Pass `--env KEY` for one-off.

Set by hauntty:
- `HAUNTTY_SESSION` - session name, set inside sessions
- `HAUNTTY_SOCKET` - socket path override for client commands

## Prior art

- [Ghostty](https://github.com/ghostty-org/ghostty)
- [shpool](https://github.com/shell-pool/shpool)
- [zmx](https://github.com/neurosnap/zmx)
- [abduco](https://github.com/martanne/abduco)
