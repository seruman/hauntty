# Protocol Redesign Proposal: Split Create and Attach

## Status

Draft for discussion. Updated with current decisions.

## Why change the protocol?

The current protocol works, but it mixes concerns in ways that make implementation and UX harder than needed.

### 1) `Attach` currently does two jobs

Today `Attach` both creates missing sessions and attaches clients.

**Consequence:**
- `ht new` must do a full attach path (including state dump) for a headless create.
- Callers need to understand attach-specific response choreography.

### 2) Response shape is overloaded

`OK` carries attach metadata (`cols`, `rows`, `pid`, `created`) but is also used as a generic ack for kill/send/sendkey.

**Consequence:**
- One response type has command-specific semantics.

### 3) `State` is effectively attach-only response tail

`State` follows `OK` only for attach.

**Consequence:**
- Hidden two-message response contract.

### 4) External detach targeting is ambiguous

Detach from a separate control connection cannot reliably identify which attached client to detach in multi-client sessions.

**Consequence:**
- Wrong client can be detached.

### 5) Creation policy leaks into client messages

`ScrollbackLines` is daemon/session policy, not per-attach intent.

**Consequence:**
- Wire protocol carries config that should live in daemon config.

---

## Design goals

1. One message, one responsibility.
2. Explicit response types.
3. No hidden multi-message response contracts.
4. Keep streaming path simple.
5. Deterministic client targeting.
6. Keep policy in daemon config unless per-request behavior is intentional.

---

## Decided protocol direction

## Handshake

Client → Server: protocol version + revision  
Server → Client: accepted version + revision (`accepted=0` rejects)

---

## Control mode (request → response)

### `Create`

`Create { name, command, env, cwd, mode }`

`mode`:
- `require_new` → error if session already exists (used by `ht new`)
- `open_or_create` → create if missing, reuse if existing (used by `ht attach`)

Response:
- `Created { session, pid, outcome }`
- `Error { message }`

`outcome`:
- `created`
- `existing`

Notes:
- `name == ""` means auto-generate.
- No cols/rows/xpixel/ypixel in `Create`.
- No scrollback in `Create`; daemon uses `default_scrollback`.

### `Attach`

`Attach { session, cols, rows, xpixel, ypixel, read_only, client_tty }`

Response:
- `Attached { session, pid, cols, rows, client_id, screen_dump, cursor_row, cursor_col, is_alt_screen }`
- `Error { message }`

Notes:
- Success transitions this connection to streaming mode.
- Initial screen state is in `Attached` (no separate `State` message).
- `client_id` is server-generated stable identifier for this attached client.

### `List`

`List { include_clients }`

Response:
- `Sessions { sessions[] }`

When `include_clients=true`, each session includes attached clients:
- `clients[] { client_id, tty, read_only, version }`

### `Kill`

`Kill { session }` → `OK {}` or `Error`

### `Send`

`Send { session, data }` → `OK {}` or `Error`

### `SendKey`

`SendKey { session, key_code, mods }` → `OK {}` or `Error`

### `Dump`

`Dump { session, format }` → `DumpResponse { data }` or `Error`

### `Detach` (external control)

`Detach { session, target }` → `OK {}` or `Error`

`target` (explicit only):
- `client_id`, or
- `client_tty`

No wildcard/all-clients variant in this message.

### `Status`

`Status { session }` → `StatusResponse { daemon, session? }`

Session status includes attached clients:
- `clients[] { client_id, tty, read_only, version }`

### `Prune`

`Prune {}` → `PruneResponse { count }`

---

## Streaming mode (after successful `Attach`)

Client → Server:
- `Input { data }`
- `Resize { cols, rows, xpixel, ypixel }`
- `Detach {}` (self-detach current attached connection)

Server → Client:
- `Output { data }`
- `Exited { exit_code }`
- `ClientsChanged { count, cols, rows }`
- `Error { message }`

---

## Session lifecycle decision: dead-session TTL window

Decision: keep dead sessions in memory for a short TTL before eviction.

Reason:
- protects the `Create -> Attach` window for short-lived commands,
- avoids unbounded growth from keeping dead sessions forever,
- simpler behavior than immediate delete + restore race.

Suggested default: `dead_session_ttl = 60s` (configurable).

Behavior:
1. Process exits → session marked dead, remains attachable for TTL.
2. During TTL, `Attach` can still resolve session and receive final state.
3. After TTL, session is evicted from live map; persisted state remains discoverable until prune.

---

## Why enum mode/outcome instead of boolean flags?

Decision: use enums for policy and outcome.

Reason:
- avoids ambiguous booleans (`already_exists=true` vs caller intent),
- makes CLI intent explicit (`require_new` vs `open_or_create`),
- easier to extend later without protocol shape churn.

---

## Expected CLI behavior

### `ht new`

Flow:
1. `Create(mode=require_new)`
2. Print created session

No attach, no state drain.

### `ht attach`

Flow:
1. `Create(mode=open_or_create)`
2. `Attach`
3. Stream IO

### `ht detach`

- Inside attached client: self-detach via streaming `Detach {}`.
- External control command: explicit target required (`--client-id` or `--tty`).

---

## Implementation checklist (file-by-file)

## Phase 0: lock decisions

- [ ] Keep `List` clients optional (`include_clients=false` default)
- [ ] `client_tty` optional in `Attach` (empty allowed)
- [ ] Global daemon TTL only (`dead_session_ttl_seconds`)

## Phase 1: wire protocol

### `internal/protocol/messages.go`

- [ ] Add client→daemon messages:
  - [ ] `Create`
  - [ ] `Attach` (new attach-only payload)
- [ ] Add daemon→client messages:
  - [ ] `Created`
  - [ ] `Attached`
- [ ] Add enums:
  - [ ] `CreateMode` (`require_new`, `open_or_create`)
  - [ ] `CreateOutcome` (`created`, `existing`)
- [ ] Add identity structs for list/status payloads:
  - [ ] `ClientInfo { ClientID, TTY, ReadOnly, Version }`
- [ ] Make `OK` empty struct (`type OK struct{}`)
- [ ] Remove `State` message type
- [ ] Update `Detach` payload to explicit target form in control mode:
  - [ ] `Detach { Name, TargetClientID, TargetTTY }`
  - [ ] streaming self-detach remains empty payload on attached connection

### `internal/protocol/codec.go`

- [ ] Add new message type dispatch cases
- [ ] Remove `TypeState`
- [ ] Bump `ProtocolVersion`
- [ ] Keep frame format unchanged

### Type code plan (proposal)

- Client→Daemon:
  - [ ] `TypeCreate = 0x01`
  - [ ] `TypeAttach = 0x0D`
  - [ ] keep existing others where possible
- Daemon→Client:
  - [ ] `TypeCreated = 0x83`
  - [ ] `TypeAttached = 0x8A`
  - [ ] remove `TypeState`

(Exact numbers can be adjusted, but freeze once merged.)

### `internal/protocol/codec_test.go`

- [ ] Replace round-trips for removed types (`State`, old `OK`)
- [ ] Add round-trips for `Create`, `Created`, `Attach`, `Attached`
- [ ] Add enum value round-trips and target detach round-trips

## Phase 2: daemon behavior

### `internal/daemon/server.go`

- [ ] Split handlers:
  - [ ] `handleCreate`
  - [ ] `handleAttach`
- [ ] `handleCreate`:
  - [ ] evaluate `CreateMode`
  - [ ] `require_new` + exists => error
  - [ ] `open_or_create` + exists => `Created{Outcome:existing}`
  - [ ] create new session => `Created{Outcome:created}`
- [ ] `handleAttach`:
  - [ ] lookup existing session only
  - [ ] return `Attached` with screen dump payload
- [ ] Store attached client identity: `client_id`, optional `tty`, `version`, `read_only`
- [ ] External `Detach` target resolution by `client_id` or `tty`
- [ ] For ambiguous/missing target return explicit errors

### Dead-session TTL (`internal/daemon/server.go`, maybe `session.go`)

- [ ] Add daemon config `DeadSessionTTLSeconds` (default 60)
- [ ] On process exit:
  - [ ] mark session dead
  - [ ] schedule eviction at `now + ttl`
  - [ ] allow attach/list/status during TTL
- [ ] On attach/create reopen of dead session:
  - [ ] cancel pending eviction timer
- [ ] Ensure auto-exit semantics account for dead sessions retained by TTL

### `internal/daemon/session.go`

- [ ] `attach()` sends `Attached` (not `State`)
- [ ] client struct includes `id` and `tty`
- [ ] helper lookups:
  - [ ] detach by client id
  - [ ] detach by tty
- [ ] keep streaming IO path unchanged (`Output`, `Input`, `Resize`, `Exited`)

### `internal/daemon/persist.go`

- [ ] Verify dead-session state file interaction with TTL
- [ ] Ensure prune still only touches persisted dead state files

## Phase 3: client API + CLI

### `client/client.go`

- [ ] Add `Create(...) (*protocol.Created, error)`
- [ ] Change `Attach(...)` to attach-only request returning `*protocol.Attached`
- [ ] Update detach API to support explicit target (`client_id`/`tty`)
- [ ] Keep error wrapping style consistent

### `client/attach.go`

- [ ] Flow becomes:
  1. [ ] `Create(mode=open_or_create)`
  2. [ ] `Attach(...)`
  3. [ ] stream loop
- [ ] Use `Attached.ScreenDump` directly
- [ ] Keep current raw mode / resize / detach-key behavior
- [ ] record `client_id` from `Attached` if needed for UI/logging

### `cmd/ht/main.go`

- [ ] `NewCmd`:
  - [ ] call `Create(mode=require_new)` only
  - [ ] no attach and no state drain
- [ ] `AttachCmd`:
  - [ ] keep user-facing behavior (create-if-missing)
- [ ] `DetachCmd`:
  - [ ] support explicit target flags:
    - [ ] `--client-id`
    - [ ] `--tty`
  - [ ] validate exactly one target for external detach
- [ ] `ListCmd`:
  - [ ] add `--clients` view showing tty/client_id/read_only/version
- [ ] `StatusCmd`:
  - [ ] print attached client details

### `internal/config/config.go`

- [ ] Add `daemon.dead_session_ttl_seconds`
- [ ] Set default value (60)
- [ ] Add config tests

## Phase 4: tests

### Unit tests

- [ ] protocol codec tests for new messages and enums
- [ ] daemon tests:
  - [ ] create mode behavior
  - [ ] attach existing-only behavior
  - [ ] detach target resolution
  - [ ] TTL retention and eviction
- [ ] client tests for new API surfaces

### E2E tests (`cmd/ht/e2e_test`)

- [ ] `ht new` duplicate should fail (`require_new`)
- [ ] `ht attach` creates when missing
- [ ] fast-exit command still attachable during TTL window
- [ ] external detach by tty
- [ ] external detach by client id
- [ ] list/status show client identity data

## Phase 5: rollout

- [ ] Update README/protocol docs
- [ ] Bump protocol version in release notes
- [ ] Validate formatting + static checks + full test suite

---

## Remaining open questions

1. Should `List` include clients by default, or only with `--clients`/`include_clients`?
2. Should `client_tty` be optional in `Attach` (for non-tty automation clients), with `client_id` as primary identity?
3. Do we want configurable TTL per session type, or only global daemon TTL?
