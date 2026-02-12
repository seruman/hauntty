# CLAUDE.md

## Code Style

- Write idiomatic Go. Prefer stdlib patterns and conventions.
- Keep code simple and readable. Less abstraction is better than more.
- MUST NOT abstract prematurely or for the sake of abstraction.
- Follow Go conventions: small interfaces, composition over inheritance.
- Be explicit about ownership of structs and resources, even with GC.
- Be explicit about component responsibilities. MUST NOT mix responsibilities.
- If test code needs to be injected into production code, rethink the design.
- MUST NOT add comments that restate what the code already says. Comments are for non-obvious intent, workarounds, and subtle gotchas — not narration.

## Naming

- Follow Go naming conventions: short receiver names, unexported by default.
- Only export symbols when needed across package boundaries.

## Error Handling

- Wrap errors with `fmt.Errorf("context: %w", err)`.
- MUST NOT discard errors silently. Return errors to the caller.
- Only log-and-continue at top-level boundaries (main, daemon loop).

## Concurrency

- Prefer channels over mutexes when the communication pattern is clear.
- Document goroutine lifecycle and shutdown.

## Logging

- Use `log/slog` for structured logging.

## Dependencies

- Minimize external dependencies. Prefer stdlib.
- Every new dependency needs justification.

## Packages

- MUST NOT split into packages for the sake of organization.
- Only create a new package when there is a clear, independent responsibility.

## Testing

- MUST use `gotest.tools/v3` for test assertions and helpers.
- MUST write tests for new features and bug fixes. Cover edge cases.
- MUST assert full results. MUST NOT use partial assertions (contains, has prefix, substring matches) — not even wrapped in `assert.Assert`. Compare the actual complete value. Use `assert.DeepEqual` for structs and byte slices, `assert.Equal` for scalars. When the expected value is unknown, run the test with a placeholder to capture the actual output from the diff, then use that.
- Use table-driven tests when cases share the same test body.
- When table cases need different behavior or setup, use `t.Run()` subtests instead.
- MUST NOT extract shared test bodies into helper functions across `t.Run()` cases. Repeat the test body in each case for clarity.
- Test behavior (expected output for given input), not implementation details.

## Formatting

- MUST run `gofumpt`, `goimports`, and `go fix ./...` BEFORE `os task complete` (since complete auto-commits).
- `go vet ./...` and `go tool staticcheck ./...` MUST pass. No warnings tolerated.

## Task Workflow (Overseer)

Use the `os` binary for all task management.

### Hierarchy
- Milestone (depth 0) → Task (depth 1) → Subtask (depth 2, max depth).
- Create milestones for features, tasks for units of work, subtasks for atomic steps.
- Prefer many small, focused tasks over fewer large ones.

### Lifecycle
- `os task create -d "description" [--context "..."] [--parent ID] [--blocked-by ID1,ID2]`
- `os task start ID` — creates a `task/{id}` git branch at current HEAD and checks it out. Records the current commit as `start_commit`.
- `os task complete ID [--result "..."] [--learning "..."]` — commits changes, checks out back to `start_commit` (not main), then deletes the task branch.
- Auto-completes parent when all siblings are done.

### VCS constraints
- `os task start` and `os task complete` REQUIRE a clean working tree.
- `os task start` changes HEAD — it checks out a new branch. Be aware of which branch you're on.
- `os task complete` returns HEAD to where you were when you called `start`, not to main. If you started from main, you'll return to main. If you started from another task branch, you'll return there.
- `os task complete` commits ALL changes in the working tree. MUST NOT have uncommitted changes from other work when completing.
- Overseer v1 is single-working-tree. Complete tasks one at a time: start → work → complete → start next. Do NOT attempt to interleave VCS-managed tasks.

### Context & learnings
- Add as much context to task descriptions as possible — they are your plan log.
- Update task details (`os task update ID --context "..."`) if the plan changes mid-task.
- Use `--learning` on complete to record insights. Learnings bubble up to the parent.

### Dependencies
- `os task block ID --by BLOCKER_ID` to declare ordering.
- Cancelled tasks do NOT satisfy blockers — only completion unblocks dependents.
- `os task next-ready` finds the deepest unblocked leaf to work on next.

## Git

- Overseer (`os task complete`) is the primary commit path — it commits automatically.
- MUST NOT run `git commit` manually without explicit user approval.
- Commit message style: Go stdlib (e.g., `cmd/ht: add session timeout flag`).
- Use imperative mood: "add", "fix", "remove" — not "added", "fixed", "removed".
- Describe *what* changed and *why*, not *how*. Never explain implementation details.
- MUST NOT use vague messages like "fix bug" or "update code".
- Reference relevant issue/ticket numbers when applicable.

## Interactive Testing (bootty)

Use `bootty` for automated interactive testing of `ht`. Spawn shell sessions
and run `ht` commands inside them — the session stays alive after `ht` exits.

```bash
# Build ht first
go build -o ht ./cmd/ht

# Spawn shells (not ht directly — keeps session alive after commands exit)
bootty spawn --name daemon
bootty spawn --name client

# Start daemon
bootty type -s daemon "./ht daemon --auto-exit"
bootty key -s daemon Enter
bootty wait -s daemon "listening"

# Attach to a session
bootty type -s client "./ht attach my-session"
bootty key -s client Enter
bootty wait -s client "my-session"

# Interact with the attached session
bootty type -s client "echo hello"
bootty key -s client Enter
bootty wait -s client "hello"
bootty screenshot -s client

# Test detach — session returns to shell prompt, ready for next command
bootty key -s client "Ctrl+]"
bootty wait -s client "$"

# Test list (reuse the same shell)
bootty type -s client "./ht list"
bootty key -s client Enter
bootty wait -s client "my-session"
bootty screenshot -s client

# Cleanup
bootty kill client
bootty kill daemon
```

Key patterns:
- Spawn shell sessions, run `ht` inside — session survives after `ht` exits.
- Always `bootty wait` before interacting — ensures the app is ready.
- Use `--name` to target specific sessions with `-s`.
- `bootty screenshot --format text` for assertions, `--format vt` for escape sequences.
- `bootty key` for special keys: `Enter`, `Escape`, `Ctrl+C`, `Up`, `Down`, etc.

## Communication

- When in doubt, ask for clarification. MUST NOT assume or guess on ambiguous requirements.
