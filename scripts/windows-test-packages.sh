#!/usr/bin/env bash
# Lists the packages the Windows services job of distributed-checks.yml tests.
#
#   windows-test-packages.sh              the Windows services packages, one
#                                         import path per line
#   windows-test-packages.sh --linux-only the Linux-only packages, one import
#                                         path per line
#
# The Windows services list is `go list ./...` minus the Linux-only packages and
# minus the packages another Windows step owns. Every package runs on Windows
# unless it is Linux-only. A Linux-only package still runs in the ubuntu-latest
# all job (`go test ./...`), so its coverage is not lost; a dispatch can bring
# it back to Windows with the windows_extra_packages input.
#
# Classification rule. A package may be Linux-only when all of these hold; the
# test in windows_test_packages_test.go checks them for every listed package:
#   1. No .go file (tests included) has a //go:build line, whatever the
#      condition, and no file name carries a GOOS or GOARCH suffix (_windows.go,
#      _amd64.go, _linux_arm64_test.go, _unix.go and the like).
#   2. No .go file (tests included) mentions runtime.GOOS or imports os/exec,
#      syscall, golang.org/x/sys/..., net or net/http/httptest.
#   3. Every package of this module in `go list -test -deps` is itself
#      Linux-only.
# A package that meets the rule but whose tests build, walk or compare paths, or
# touch processes, terminals or git subprocesses, stays on Windows. When in
# doubt, the package stays on Windows. A new package is not listed here, so it
# runs on Windows until someone shows that it meets the rule.
#
# List entries are repository-relative directories. An entry that ends in "/"
# matches the packages below that directory ("internal/statemachine/" matches
# internal/statemachine/dispatch, not internal/statemachinex). Any other entry
# matches exactly that package ("internal/version" does not match
# internal/versionx). The second list holds the packages other Windows steps
# run; its entries always match exactly, so internal/board/boardtest stays in
# the services list.
#
# The script fails, with a non-zero status, when `go list` fails or when the
# services list is empty, so a broken run never tests fewer packages silently.
set -euo pipefail

linux_only=(
  internal/statemachine/
  internal/version
)

# Packages with a Windows step of their own: the board step and the remote step.
other_steps=(
  internal/board
  internal/remote
)

mode=services
case "${1:-}" in
  '') ;;
  --linux-only) mode=linux-only ;;
  *)
    echo "usage: windows-test-packages.sh [--linux-only]" >&2
    exit 2
    ;;
esac
if [ "$#" -gt 1 ]; then
  echo "usage: windows-test-packages.sh [--linux-only]" >&2
  exit 2
fi

module=$(go list -m)
packages=$(go list ./...)

# is_linux_only REL succeeds when the Linux-only list covers REL.
is_linux_only() {
  local rel=$1 entry
  for entry in "${linux_only[@]}"; do
    case "$entry" in
      */) [[ $rel == "$entry"* ]] && return 0 ;;
      *) [[ $rel == "$entry" ]] && return 0 ;;
    esac
  done
  return 1
}

# has_other_step REL succeeds when REL is exactly an other_steps entry.
has_other_step() {
  local rel=$1 entry
  for entry in "${other_steps[@]}"; do
    [[ $rel == "$entry" ]] && return 0
  done
  return 1
}

count=0
while IFS= read -r package; do
  [ -n "$package" ] || continue
  rel=${package#"$module"/}
  if [ "$mode" = linux-only ]; then
    is_linux_only "$rel" || continue
  else
    ! is_linux_only "$rel" || continue
    ! has_other_step "$rel" || continue
  fi
  echo "$package"
  count=$((count + 1))
done <<<"$packages"

if [ "$count" -eq 0 ]; then
  echo "windows-test-packages.sh: no packages selected" >&2
  exit 1
fi
