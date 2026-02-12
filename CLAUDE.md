# CLAUDE.md

## Code Style

- Write idiomatic Go. Prefer stdlib patterns and conventions.
- Keep code simple and readable. Less abstraction is better than more.
- MUST NOT abstract prematurely or for the sake of abstraction.
- Follow Go conventions: small interfaces, composition over inheritance.
- Be explicit about ownership of structs and resources, even with GC.
- Be explicit about component responsibilities. MUST NOT mix responsibilities.
- If test code needs to be injected into production code, rethink the design.

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
- MUST assert full results. MUST NOT use partial assertions (contains, has prefix, substring matches). Use `assert.DeepEqual` for structs, even large ones.
- Use table-driven tests when cases share the same test body.
- When table cases need different behavior or setup, use `t.Run()` subtests instead.
- MUST NOT extract shared test bodies into helper functions across `t.Run()` cases. Repeat the test body in each case for clarity.
- Test behavior (expected output for given input), not implementation details.

## Formatting

- MUST run `gofumpt` and `goimports` after completing each task, before review.
- MUST run `go fix ./...` after completing each task to apply modern Go idioms (slices.Contains, maps.Copy, min/max, strings.Cut, etc.).
- `go vet ./...` MUST pass. No warnings tolerated.

## Task Workflow

- Use Overseer (`os` binary) to track all work.
- Create a task before starting. Set it to active when you begin. Mark done when finished.
- Add as much context to task descriptions as possible — they are your plan log.
- Update task details if the plan changes mid-task.
- Declare task dependencies in Overseer to track ordering.
- Prefer many small, focused tasks over fewer large ones.

## Git

- MUST NOT commit without explicit user approval. Always ask first.
- Commit message style: Go stdlib (e.g., `cmd/ht: add session timeout flag`).
- Use imperative mood: "add", "fix", "remove" — not "added", "fixed", "removed".
- Describe *what* changed and *why*, not *how*. Never explain implementation details.
- MUST NOT use vague messages like "fix bug" or "update code".
- Reference relevant issue/ticket numbers when applicable.

## Communication

- When in doubt, ask for clarification. MUST NOT assume or guess on ambiguous requirements.
