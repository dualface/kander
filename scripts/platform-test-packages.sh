#!/usr/bin/env bash
# Lists the packages the macOS and Windows jobs of release-gate.yml test, and the
# platform-neutral packages the daily distributed-checks.yml job tests.
#
#   platform-test-packages.sh              the operating-system packages, one
#                                          import path per line: the service
#                                          packages of the macOS and Windows
#                                          jobs
#   platform-test-packages.sh --linux-only the platform-neutral packages, one
#                                          import path per line
#
# The operating-system list is `go list ./...` minus the platform-neutral
# packages and minus the packages another macOS or Windows step owns. Every
# package runs on macOS and Windows unless it is platform-neutral. A
# platform-neutral package still runs in the ubuntu-latest job of release-gate.yml
# (`go test ./...`) and in the daily job of distributed-checks.yml, so its
# coverage is not lost; a dispatch of release-gate.yml can bring it back to
# Windows with the windows_extra_packages input.
#
# Classification rule. A package may be platform-neutral when all of these
# hold; the test in platform_test_packages_test.go checks them for every listed
# package:
#   1. No .go file (tests included) has a //go:build line, whatever the
#      condition, and no file name carries a GOOS or GOARCH suffix (_windows.go,
#      _amd64.go, _linux_arm64_test.go, _unix.go and the like).
#   2. No .go file (tests included) mentions runtime.GOOS or imports os/exec,
#      syscall, golang.org/x/sys/..., net or net/http/httptest.
#   3. Every package of this module in `go list -test -deps` is itself
#      platform-neutral.
# A package that meets the rule but whose tests build, walk or compare paths, or
# touch processes, terminals or git subprocesses, stays on macOS and Windows.
# When in doubt, the package stays. A new package is not listed here, so it runs
# on macOS and Windows until someone shows that it meets the rule.
#
# List entries are repository-relative directories. An entry that ends in "/"
# matches the packages below that directory ("internal/statemachine/" matches
# internal/statemachine/dispatch, not internal/statemachinex). Any other entry
# matches exactly that package ("internal/version" does not match
# internal/versionx). The second list holds the packages other macOS and Windows
# steps run; its entries always match exactly, so internal/board/boardtest stays
# in the operating-system list.
#
# The script fails, with a non-zero status, when `go list` fails or when the
# operating-system list is empty, so a broken run never tests fewer packages
# silently.
set -euo pipefail

linux_only=(
  internal/i18n
  internal/statemachine/
  internal/version
  skills
)

# Packages with a step of their own in the macOS and Windows jobs: the board
# step and the remote step.
other_steps=(
  internal/board
  internal/remote
)

mode=services
case "${1:-}" in
  '') ;;
  --linux-only) mode=linux-only ;;
  *)
    echo "usage: platform-test-packages.sh [--linux-only]" >&2
    exit 2
    ;;
esac
if [ "$#" -gt 1 ]; then
  echo "usage: platform-test-packages.sh [--linux-only]" >&2
  exit 2
fi

module=$(go list -m)
packages=$(go list ./...)

# is_linux_only REL succeeds when the platform-neutral list covers REL.
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
  echo "platform-test-packages.sh: no packages selected" >&2
  exit 1
fi
