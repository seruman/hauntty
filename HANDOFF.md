# Handoff: protocol-v2

## Branch State
- Branch: `protocol-v2`
- Snapshot base: `f3da534`
- This checkpoint includes the daemon restore/dead-session fixes, the new regression coverage, the dead-dump golden files, and the protocol redesign note in `docs/protocol-redesign-create-attach.md`.

## What Changed
- `internal/daemon/session.go` already contained the `pendingFeed` actor-loop fix for the readonly-detach hang before this pass. It was left as-is.
- `internal/daemon/server.go`
  - restore attach now reports `created=false`
  - persisted state is deleted only after a successful restore attach
  - dead-session `dump` rebuilds a temporary Ghostty terminal and honors the requested format and flags
- `cmd/ht/e2e_test/lifecycle_test.go`
  - `TestRestoreDeadSession` now restores from a fresh shell and verifies the saved screen is replayed
- `cmd/ht/e2e_test/automation_test.go`
  - adds exact dead-session dump golden coverage for `plain`, `html`, and wrapped-output `-J`
- `cmd/ht/e2e_test/testdata/*.golden`
  - capture the expected dead-session dump outputs

## Reviewer Findings Resolved
1. Restored sessions no longer masquerade as newly created sessions.
2. Persisted dumps preserve the requested format and `-J` behavior.
3. Restore no longer deletes the only saved state before attach succeeds.

## Validation
- `go tool staticcheck ./...`
- `go tool gofumpt -l .`
- `go tool goimports -l .`
- `go test ./... -count=1`

All passed before this handoff was written.

## Notes
- Dead-session `plain` output is now deterministic and tested with goldens. Replayed VT cannot recover the original live soft-wrap metadata byte-for-byte, so parity is asserted against dead-session golden output rather than live-session output.
- `protogen`, `PROTOGEN_REFACTOR.md`, and local `.pi` state are intentionally outside this branch.

## Next Steps
1. Manually validate the readonly-detach flow in a real terminal.
2. If more protocol-v2 work continues, use `docs/protocol-redesign-create-attach.md` as the current design note.
