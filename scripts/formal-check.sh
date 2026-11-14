#!/usr/bin/env bash
# Checks the formal toolchain without rewriting the repository:
#   1. the Lean build passes and no build output reports `sorry`;
#   2. the axiom audit passes (formal/Audit.lean);
#   3. regenerating into a temporary directory reproduces every generated file
#      byte for byte, and no stale generated file remains;
#   4. every generated header carries the SHA-256 of its source file;
#   5. the generated Go is gofmt-clean, vets, and replays its vectors.
# Requires elan and Go. See docs/formal-verification.md.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
if [ -d "$HOME/.elan/bin" ]; then
  PATH="$HOME/.elan/bin:$PATH"
fi
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
failed=0
fail() {
  echo "formal-check: $*" >&2
  failed=1
}

cd "$root/formal"
echo "== lake build"
lake build 2>&1 | tee "$tmp/build.log"
if grep -Eq 'declaration uses .sorry.' "$tmp/build.log"; then
  fail "the Lean build reports sorry"
fi

echo "== axiom audit"
lake env lean Audit.lean

echo "== regenerate and compare"
lake exe kander-formal gen "$root" "$tmp/out" >/dev/null
gen="$tmp/out/internal/statemachine"
while IFS= read -r -d '' f; do
  rel=${f#"$tmp/out/"}
  if [ ! -f "$root/$rel" ]; then
    fail "missing generated file: $rel"
  elif ! cmp -s "$f" "$root/$rel"; then
    fail "generated file differs from a fresh generation: $rel"
  fi
done < <(find "$gen" -type f -print0)
while IFS= read -r -d '' f; do
  rel=${f#"$root/"}
  if [ ! -f "$tmp/out/$rel" ]; then
    fail "stale generated file: $rel"
  fi
done < <(find "$root/internal/statemachine" -type f \
  \( -name 'zz_generated*.go' -o -path '*/testdata/vectors.jsonl' \) -print0)

echo "== source digests"
while IFS= read -r -d '' f; do
  src=$(sed -n 's|^// Source: ||p' "$f" | head -n 1)
  digest=$(sed -n 's|^// Source SHA-256: ||p' "$f" | head -n 1)
  actual=$(sha256sum "$root/$src" | cut -d ' ' -f 1)
  if [ "$digest" != "$actual" ]; then
    fail "${f#"$root/"}: header digest $digest, $src is $actual"
  fi
done < <(find "$root/internal/statemachine" -type f -name 'zz_generated.go' -print0)

cd "$root"
echo "== gofmt, vet, test"
unformatted=$(gofmt -l internal/statemachine)
if [ -n "$unformatted" ]; then
  fail "gofmt reports: $unformatted"
fi
go vet ./internal/statemachine/...
go test ./internal/statemachine/...

if [ "$failed" -ne 0 ]; then
  exit 1
fi
echo "formal-check: OK"
