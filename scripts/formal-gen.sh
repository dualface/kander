#!/usr/bin/env bash
# Regenerates the Go decision kernels and differential vectors in place from
# the Lean definitions in formal/. Requires elan (the toolchain version comes
# from formal/lean-toolchain). See docs/formal-verification.md.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
if [ -d "$HOME/.elan/bin" ]; then
  PATH="$HOME/.elan/bin:$PATH"
fi

cd "$root/formal"
lake build
lake exe kander-formal gen "$root" "$root"
