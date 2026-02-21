# Implementation Plan: `ht tail` and `ht attach --read-only`

Two separate features for observing sessions without sending input.

## Feature 1: `ht attach --read-only`

Full TUI attach (raw mode, screen rendering, state dump), but input is blocked.

### Current attach flow (for reference)

1. **CLI** (`cmd/ht/main.go` `AttachCmd.Run`): calls `client.RunAttach()`
2. **Client** (`client/attach.go` `RunAttach`):
   - Gets terminal size, sends `protocol.Attach` message
   - Receives `protocol.OK` + `protocol.State` (screen dump)
   - Enters raw mode
   - Spawns goroutine: reads stdin → sends `protocol.Input` to daemon
   - Spawns goroutine: SIGWINCH → sends `protocol.Resize` to daemon
   - Main loop: reads daemon messages → writes `protocol.Output` to stdout
   - On detach key: sends `protocol.Detach`, closes connection
3. **Daemon** (`internal/daemon/session.go`):
   - `Session.attach()`: resizes, dumps screen, creates `attachedClient`, starts `writeLoop`
   - `readLoop`: broadcasts raw PTY output to all clients via `broadcastOutput`
   - Resize arbitration considers all clients

### Changes needed

#### 1.1 Protocol: Add read-only flag to Attach message
- **File**: `internal/protocol/messages.go`
- Add `ReadOnly bool` field to `Attach` struct
- Update `encode`/`decode` methods (append bool at end for wire compat)

#### 1.2 Daemon: Track read-only flag on attachedClient
- **File**: `internal/daemon/session.go`
- Add `readOnly bool` to `attachedClient` struct
- Set it from `Attach.ReadOnly` in `Session.attach()`
- `sendInputFrom()`: skip if `ac.readOnly` (or just don't wire input — client-side is simpler)
- `arbitrateResize()`: exclude read-only clients from resize arbitration (they observe, they don't control size)

#### 1.3 Client: Read-only attach mode
- **File**: `client/attach.go`
- Add `ReadOnly bool` parameter to `RunAttach` (or use an options struct)
- When read-only:
  - Don't spawn the stdin reader goroutine at all
  - Still handle SIGWINCH → send resize (or skip — decision: skip, read-only clients shouldn't resize)
  - Actually: **don't send resize either**. Read-only clients passively observe at whatever size the session is.
  - Still write state dump to stdout and handle Output messages normally
  - Detach key still works (need stdin reader just for detach, but no Input forwarding)
  - Simplification: read stdin only to detect detach key, discard everything else

#### 1.4 CLI: --read-only flag on attach
- **File**: `cmd/ht/main.go`
- Add `ReadOnly bool` flag to `AttachCmd`
- Pass through to `client.RunAttach()`

#### 1.5 Client Attach method: forward ReadOnly flag
- **File**: `client/client.go`
- `Client.Attach()`: accept and set `ReadOnly` in the `protocol.Attach` message

### Resize behavior
Read-only clients are excluded from resize arbitration. They observe at
whatever size the session currently is. If their terminal is smaller they
see a clipped view; if larger, extra space is unused. This avoids the
classic tmux problem where a read-only viewer with a small terminal
shrinks the session for everyone. No configuration knob — one sane default.

tmux has `window-size` (smallest/latest/largest) to configure this.
Their `latest` mode effectively ignores read-only clients since they can't
send input. Our approach is simpler: just exclude them entirely.

### Other decisions
- Read-only clients see `[hauntty] attached read-only` on stderr.
- Detach key still works — you need a way to exit.
- No SIGWINCH/resize messages sent from read-only clients.

---

## Feature 2: `ht tail`

Raw byte stream of session output to stdout. No PTY, no raw mode. Pipeable.

### Design

The core idea: spin up a dedicated WASM terminal instance per tail client.
Daemon tees raw PTY output to both the session's real terminal AND the tail
client's shadow terminal. On each feed, dump the shadow terminal's scrollback,
diff against last sent position, send new lines.

This reuses the existing WASM terminal emulator for all escape sequence
filtering (`DumpVTSafe` strips dangerous sequences) instead of writing a
separate Go-side stream filter.

### Data flow

```
PTY output
    │
    ├──→ session.term (real terminal) → attached clients (TUI)
    │
    └──→ tail shadow terminal ─── dump+diff ──→ tail client (stdout)
```

### Detailed steps

#### 2.1 Protocol: New Tail message type
- **File**: `internal/protocol/messages.go`
- New client→daemon message: `Tail` (name string, format uint8, backlog bool)
  - `TypeTail uint8 = 0x0D`
  - format: plain (default) or vt-safe
  - backlog: whether to send current screen content first
- New daemon→client message: `TailOutput` (data []byte)
  - `TypeTailOutput uint8 = 0x8A`
  - Or reuse `Output` — but separate type is cleaner since tail output is
    filtered/processed differently from raw attach output
- Reuse existing `Exited` message when session dies
- Reuse existing `Error` message for errors

#### 2.2 Protocol: Register new message types in codec
- **File**: `internal/protocol/codec.go`
- Register `TypeTail` → `*Tail` and `TypeTailOutput` → `*TailOutput`

#### 2.3 Daemon: tailClient struct
- **File**: `internal/daemon/session.go`
- New struct `tailClient`:
  ```go
  type tailClient struct {
      conn   *protocol.Conn
      close  func() error
      term   *libghostty.Terminal  // shadow terminal
      outCh  chan []byte
      done   chan struct{}
      format libghostty.DumpFormat
      // Track scrollback position for diffing
      lastLines int
  }
  ```
- `tailClient.writeLoop()`: same pattern as `attachedClient.writeLoop()`

#### 2.4 Daemon: Session tracks tail clients
- **File**: `internal/daemon/session.go`
- Add `tailClients []*tailClient` to `Session` struct (protected by `clientMu`)
- `readLoop` / `broadcastOutput`: also feed raw data to each tail client's
  shadow terminal and trigger dumps
- **Feed + dump logic**:
  1. Feed raw PTY bytes to `tc.term`
  2. Dump with `DumpVTSafe | DumpFlagScrollback | DumpFlagUnwrap`
  3. Split into lines, find new lines since last dump
  4. Send new lines as `TailOutput` to the tail client
  5. Update `tc.lastLines`
- On session exit: send `Exited` to tail clients, close them

#### 2.5 Daemon: Session.tailAttach method
- **File**: `internal/daemon/session.go`
- Creates a shadow WASM terminal with same dimensions as session
- If backlog requested: dumps current session state via `session.term`,
  feeds it to the shadow terminal, then dumps shadow and sends as initial output
- Adds tailClient to session's tailClients list
- Returns the tailClient

#### 2.6 Daemon: Handle Tail message in server
- **File**: `internal/daemon/server.go`
- Add `case *protocol.Tail:` in the message switch
- Look up session, call `session.tailAttach()`, hold connection open
- Read loop for this connection: just wait for disconnect (no input expected)

#### 2.7 Daemon: Session cleanup for tail clients
- **File**: `internal/daemon/session.go`
- `Session.close()`: disconnect all tail clients, close shadow terminals
- `Session.removeTailClient()`: close shadow terminal, remove from list
- Eviction: if tail client's outCh is full, evict (same as attach clients)

#### 2.8 Daemon: Resize propagation to shadow terminals
- **File**: `internal/daemon/session.go`
- When session resizes, also resize all shadow terminals:
  `tc.term.Resize(ctx, cols, rows)`
- Shadow terminals must track session dimensions to produce correct dumps

#### 2.9 Client: Tail method
- **File**: `client/client.go`
- `Client.Tail(name string, format uint8, backlog bool) error`
  - Sends `Tail` message, reads response (OK or Error)
  - Returns error or nil

#### 2.10 Client: RunTail method
- **File**: `client/tail.go` (new file)
- `Client.RunTail(name string, format uint8, backlog bool) error`
  - Sends Tail request
  - Main loop: read messages, write `TailOutput.Data` to stdout
  - On `Exited`: return with exit code
  - On broken pipe (stdout closed): disconnect gracefully
  - Handle SIGINT/SIGPIPE for clean shutdown

#### 2.11 CLI: ht tail command
- **File**: `cmd/ht/main.go`
- New command:
  ```go
  type TailCmd struct {
      Name    string `arg:"" help:"Session name."`
      Format  string `enum:"plain,vt" default:"plain" help:"Output format."`
      Backlog bool   `short:"b" help:"Include current screen content."`
  }
  ```
- Register in CLI struct
- `Run`: connect to daemon, call `client.RunTail()`

### Diff algorithm for new lines

The shadow terminal accumulates all output. Each time we feed and dump:
1. Dump with scrollback + unwrap → get full text
2. Split by newlines → count total lines
3. Lines `[lastLines:]` are new → send those
4. Edge case: the last line may still be incomplete (cursor on it).
   Don't send the cursor line until it's complete (a new line appears below it).
   Or: always send, and on next dump, if the previously-last line changed, re-send it.
   Simpler: send everything including cursor line. The tail consumer gets partial
   lines which is fine for streaming. This matches `tail -f` behavior.

### Cost analysis

- One WASM terminal instance per tail client (~few hundred KB memory)
- One dump per PTY read batch (amortized by feedCh buffering)
- String splitting and diffing is cheap compared to WASM terminal processing
- Acceptable for the expected number of tail clients (handful at most)

### Dump performance consideration

Dumping on every feed could be expensive if PTY output is high-throughput.
Mitigation: **batch dumps**. Don't dump on every feed — instead, after feeding,
set a dirty flag. A separate goroutine dumps at a configurable interval
(e.g., 50ms) if dirty. This bounds dump frequency regardless of PTY throughput.

```go
// In tailClient:
dirty atomic.Bool
// Goroutine:
ticker := time.NewTicker(50 * time.Millisecond)
for range ticker.C {
    if tc.dirty.CompareAndSwap(true, false) {
        // dump, diff, send
    }
}
```

---

## Implementation Order

### Phase 1: `ht attach --read-only` (simpler, no new daemon-side state)
1. Protocol: add ReadOnly to Attach (1.1)
2. Daemon: respect ReadOnly in session (1.2)
3. Client: read-only attach mode (1.3, 1.5)
4. CLI: --read-only flag (1.4)
5. Tests

### Phase 2: `ht tail` (more complex, new daemon plumbing)
1. Protocol: Tail + TailOutput messages (2.1, 2.2)
2. Daemon: tailClient struct + session tracking (2.3, 2.4, 2.5, 2.6, 2.7, 2.8)
3. Client: Tail + RunTail (2.9, 2.10)
4. CLI: ht tail command (2.11)
5. Tests

### Phase 3: Polish
- E2E tests with bootty
- Documentation
- Edge cases: tail client connecting to dead session, session dying while tailing
