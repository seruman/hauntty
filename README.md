# hauntty

Terminal session manager. Run persistent sessions that survive disconnects, reattach from anywhere.

## Install

```
go install code.selman.me/hauntty/cmd/ht@latest
```

## Usage

```bash
# Attach to a session (creates it if it doesn't exist)
ht attach my-session

# Attach with a specific command
ht attach build -- make -j8

# Detach with ctrl+; (default keybind)

# List sessions
ht list

# Reattach
ht attach my-session

# Show status
ht status

# Kill a session
ht kill my-session
```

The daemon starts automatically on first attach. Sessions persist until explicitly killed or the process exits.

## Configuration

```bash
# Create default config
ht init

# Print effective config
ht config
```

Config lives at `$XDG_CONFIG_HOME/hauntty/config.toml` (default `~/.config/hauntty/config.toml`).

## Building

```
go build -o ht ./cmd/ht
```
