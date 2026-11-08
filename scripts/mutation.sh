#!/bin/sh
# Mutation testing of application code through the entrypoint suite. Prints
# the score, then every mutant that LIVED, was NOT COVERED, or TIMED OUT, and
# fails if that list is not empty. The efficacy percentage is reported, never
# gated on.
set -eu
. "$(dirname "$0")/scope.sh"
mkdir -p build
log=build/mutation.log

# shellcheck disable=SC2046
go tool gremlins unleash --integration \
	--coverpkg="$(app_pkgs)" \
	$(mutation_excludes) \
	--timeout-coefficient=100 \
	--invert-assignments --invert-bitwise --invert-bwassign --invert-logical \
	--invert-loopctrl --remove-self-assignments \
	--output=build/mutation.json . 2>&1 | tee "$log"

if ! grep -q "^Mutation testing completed" "$log"; then
	echo "mutation gate FAILED: gremlins did not complete (see above)"
	exit 1
fi

survivors=$(grep -E '^[[:space:]]*(LIVED|NOT COVERED|TIMED OUT)' "$log" || true)
if [ -n "$survivors" ]; then
	echo
	echo "mutation gate FAILED: surviving mutants:"
	printf '%s\n' "$survivors"
	exit 1
fi
echo "mutation gate ok: no surviving mutants"
