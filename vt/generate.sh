#!/bin/sh
set -eu

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
GHOSTTY_DIR="$ROOT/x/ghostty"
GHOSTTY_REV="1576a09b0169b437b454067f9b10750d9efea9e0"
PATCH_DIR="$ROOT/vt"

# Clone or verify Ghostty at pinned commit.
if [ -d "$GHOSTTY_DIR/.git" ]; then
    current=$(git -C "$GHOSTTY_DIR" rev-parse HEAD)
    if [ "$current" != "$GHOSTTY_REV" ]; then
        git -C "$GHOSTTY_DIR" fetch origin "$GHOSTTY_REV" --depth 1
        git -C "$GHOSTTY_DIR" checkout FETCH_HEAD
    fi
else
    mkdir -p "$GHOSTTY_DIR"
    git -C "$GHOSTTY_DIR" init
    git -C "$GHOSTTY_DIR" remote add origin https://github.com/mitchellh/ghostty.git
    git -C "$GHOSTTY_DIR" fetch --depth 1 origin "$GHOSTTY_REV"
    git -C "$GHOSTTY_DIR" checkout FETCH_HEAD
fi

# Apply patches one by one (idempotent — skips if already applied).
for patch in "$PATCH_DIR"/ghostty*.patch; do
    [ -f "$patch" ] || continue
    if git -C "$GHOSTTY_DIR" apply --check "$patch" 2>/dev/null; then
        git -C "$GHOSTTY_DIR" apply "$patch"
    fi
done

# Build WASM.
cd "$ROOT/vt"
zig build -Doptimize=ReleaseSmall

# Copy to wasm/ package for go:embed.
cp "$ROOT/vt/zig-out/bin/hauntty-vt.wasm" "$ROOT/wasm/hauntty-vt.wasm"
