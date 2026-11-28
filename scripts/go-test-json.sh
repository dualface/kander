#!/usr/bin/env bash
# Runs `go test -json` for the release gate and keeps the result readable.
#
#   go-test-json.sh <json-file> <go test arguments...>
#
# The JSON stream goes to <json-file>, which release-gate.yml uploads as an
# artifact: every test event carries its Elapsed seconds, the data the gate's
# shard plan is built from. The log gets the plain test output (the Output of
# the "output" and "build-output" events), so a failing test shows its name and
# output in the step log as it does without -json. A line that is not JSON, such
# as a go command error, is printed as it is.
#
# The exit status is the status of go test. The script also fails when the
# filter (jq) fails, so a broken filter never hides a result.
set -euo pipefail

if [ "$#" -lt 2 ]; then
  echo "usage: go-test-json.sh <json-file> <go test arguments...>" >&2
  exit 2
fi
json_file=$1
shift
mkdir -p "$(dirname "$json_file")"

filter='. as $line
  | try (fromjson
      | if .Action == "output" or .Action == "build-output" then .Output else empty end)
    catch ($line + "\n")'

set +e
go test -json "$@" | tee "$json_file" | jq -R -j --unbuffered "$filter"
statuses=("${PIPESTATUS[@]}")
set -e

if [ "${statuses[1]}" -ne 0 ] || [ "${statuses[2]}" -ne 0 ]; then
  echo "go-test-json.sh: saving or printing the test output failed (tee ${statuses[1]}, jq ${statuses[2]})" >&2
  exit 1
fi
exit "${statuses[0]}"
