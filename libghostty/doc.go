// Package libghostty wraps the repo-local Ghostty VT WASM runtime used by
// hauntty internals for terminal state, screen dumps, and key encoding.
//
// It exists to support this repository's daemon, client, and test packages.
// The package is top-level for convenience, not as a promise of a stable,
// independently versioned public API.
package libghostty
