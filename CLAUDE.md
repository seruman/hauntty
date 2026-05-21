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

- MUST run `gofumpt`, `goimports`, and `go fix ./...` before considering formatting complete.
- `go vet ./...` and `go tool staticcheck ./...` MUST pass. No warnings tolerated.

## Git

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
