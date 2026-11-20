#!/usr/bin/env bash
# Decides whether the model search (TestModelCompositeStates of internal/board)
# has to run, and writes run=true or run=false to $GITHUB_OUTPUT. The reason goes
# to standard output and to $GITHUB_STEP_SUMMARY. The script always exits 0 once
# it has decided; any step that fails decides "run", so an error never skips the
# test.
#
# The search only exercises the board commands and what they import, so a change
# outside the test dependencies of ./internal/board cannot change its result. The
# rule:
#   1. REF is one of FULL_REFS (space separated): run, whatever changed. For a
#      push, the changed files of PUSH_BEFORE..HEAD are printed for reference
#      only.
#   2. Otherwise take the changed files between a base and HEAD. The base is:
#      MODEL_BASE for a workflow_dispatch that gives one (a full hexadecimal
#      commit ID); PR_BASE for a pull_request; else the merge base of
#      origin/v2 and HEAD.
#   3. The test dependencies are computed now, with
#      `go list -test -deps ./internal/board`, so no directory list is kept
#      here. A changed file is relevant when it lies below one of these package
#      directories (testdata and go:embed files included), or when it is go.mod,
#      go.sum, WORKFLOW_FILE, or this script.
#   4. Any relevant file: run. None, or no changed file at all: skip.
#
# Inputs come only from the environment, never from the command line: EVENT, REF,
# PUSH_BEFORE, PR_BASE, MODEL_BASE, FULL_REFS and WORKFLOW_FILE. The script runs
# in a git checkout with enough history, and with the module's Go toolchain.
# Keep it compatible with bash 3.2, the bash of macOS.
set -u

readonly self=scripts/model-test-scope.sh
readonly target=./internal/board

tmp=$(mktemp) || exit 1
trap 'rm -f "$tmp"' EXIT

# emit RUN REASON writes the decision and ends the script.
emit() {
  echo "model-test-scope: $2"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    echo "$2" >>"$GITHUB_STEP_SUMMARY" || true
  fi
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    echo "run=$1" >>"$GITHUB_OUTPUT" || exit 1
  fi
  exit 0
}

# is_commit_id VALUE succeeds for a full SHA-1 or SHA-256 commit ID in lower case.
is_commit_id() {
  case $1 in
    '' | *[!0-9a-f]*) return 1 ;;
  esac
  [ "${#1}" -eq 40 ] || [ "${#1}" -eq 64 ]
}

# resolve_commit VALUE prints the commit ID of a full hexadecimal commit ID.
resolve_commit() {
  is_commit_id "$1" || return 1
  git rev-parse --verify --end-of-options "$1^{commit}" 2>/dev/null
}

# print_push_range prints the files a v2 push changed. It only informs.
print_push_range() {
  local before=${PUSH_BEFORE:-} files
  if ! is_commit_id "$before"; then
    echo "model-test-scope: no usable commit before this push; the changed files are not listed"
    return
  fi
  if [ -z "${before//0/}" ]; then
    echo "model-test-scope: the push created the branch; the changed files are not listed"
    return
  fi
  if ! files=$(git diff --name-only "$before" HEAD -- 2>/dev/null); then
    echo "model-test-scope: the commit before this push is not in the checkout; the changed files are not listed"
    return
  fi
  echo "model-test-scope: files changed since the push base ${before}:"
  printf '%s\n' "$files"
}

for full in ${FULL_REFS:-}; do
  if [ "${REF:-}" = "$full" ]; then
    if [ "${EVENT:-}" = push ]; then
      print_push_range
    fi
    emit true "model search runs: ${REF} always runs it"
  fi
done

event=${EVENT:-}
if [ "$event" = workflow_dispatch ] && [ -n "${MODEL_BASE:-}" ]; then
  if ! is_commit_id "$MODEL_BASE"; then
    emit true "model search runs: model_base is not a full hexadecimal commit ID"
  fi
  base=$(resolve_commit "$MODEL_BASE") || emit true "model search runs: model_base ${MODEL_BASE} is not a commit of this checkout"
  base_from="model_base"
elif [ "$event" = pull_request ]; then
  base=$(resolve_commit "${PR_BASE:-}") || emit true "model search runs: the pull request base ${PR_BASE:-} is not a commit of this checkout"
  base_from="the pull request base"
else
  if ! git rev-parse --verify --quiet refs/remotes/origin/v2 >/dev/null; then
    git fetch --no-tags --quiet origin v2 || emit true "model search runs: cannot fetch origin/v2 for the merge base"
  fi
  base=$(git merge-base refs/remotes/origin/v2 HEAD) || emit true "model search runs: no merge base of origin/v2 and HEAD in this checkout"
  base_from="the merge base with origin/v2"
fi

git diff --no-renames --name-only -z "$base" HEAD -- >"$tmp" || emit true "model search runs: git diff against ${base} failed"

deps=$(go list -test -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' "$target") || emit true "model search runs: go list failed for ${target}"
module=$(go list -m) || emit true "model search runs: go list -m failed"
dirs=
while IFS= read -r import_path; do
  case $import_path in
    "$module") emit true "model search runs: the module root is among the dependencies of ${target}" ;;
    "$module"/*) ;;
    *) continue ;;
  esac
  dir=${import_path#"$module"/}
  dir=${dir%% \[*}
  dir=${dir%.test}
  dir=${dir%_test}
  dirs="$dirs$dir
"
done <<<"$deps"
dirs=$(printf '%s' "$dirs" | sort -u)
if ! printf '%s\n' "$dirs" | grep -qx 'internal/board'; then
  emit true "model search runs: the dependency list of ${target} does not name internal/board"
fi
echo "model-test-scope: dependency directories of ${target}: $(printf '%s' "$dirs" | tr '\n' ' ')"

count=0
relevant=
while IFS= read -r -d '' file; do
  count=$((count + 1))
  [ -z "$relevant" ] || continue
  case $file in
    go.mod | go.sum | "$self" | "${WORKFLOW_FILE:-}")
      relevant="$file (always runs the search)"
      continue
      ;;
  esac
  while IFS= read -r dir; do
    case $file in
      "$dir"/*)
        relevant="$file (in the test dependency ${dir})"
        break
        ;;
    esac
  done <<<"$dirs"
done <"$tmp"

if [ -n "$relevant" ]; then
  emit true "model search runs: ${relevant} is among the ${count} files changed since ${base_from} ${base}"
fi
emit false "model search skipped: none of the ${count} files changed since ${base_from} ${base} is in the test dependencies of ${target}"
