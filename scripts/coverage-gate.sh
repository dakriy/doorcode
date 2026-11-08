#!/bin/sh
# Usage: coverage-gate.sh PROFILE
# Fails unless every function in the profile (application code only, minus
# generated paths) is at 100.0% statement coverage. Prints every uncovered
# function first.
set -eu
. "$(dirname "$0")/scope.sh"
profile=$1
filtered=$(mktemp)
patterns=$(mktemp)
trap 'rm -f "$filtered" "$patterns"' EXIT

grep -v -e '^#' -e '^[[:space:]]*$' "$(dirname "$0")/generated-paths.txt" |
	sed "s|^|$MODULE/|" >"$patterns" || true
if [ -s "$patterns" ]; then
	grep -v -F -f "$patterns" "$profile" >"$filtered" || true
else
	cp "$profile" "$filtered"
fi

report=$(go tool cover -func="$filtered")
uncovered=$(printf '%s\n' "$report" | grep -v '^total:' | awk '$NF != "100.0%"' || true)
total=$(printf '%s\n' "$report" | awk '/^total:/ { print $NF }')
if [ -n "$uncovered" ]; then
	echo "coverage gate FAILED: application functions below 100% from the entrypoint suite:"
	printf '%s\n' "$uncovered"
	echo "total: $total"
	exit 1
fi
echo "coverage gate ok: application statement coverage $total"
