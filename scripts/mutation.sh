#!/bin/sh
# Mutation testing of application code through the entrypoint suite. Prints
# the score, then every mutant that LIVED, was NOT COVERED, or TIMED OUT, and
# fails if that list is not empty. The efficacy percentage is reported, never
# gated on.
#
# With DOORCODE_MUTATION_BASE set to a git ref, only the lines changed since
# that ref are mutated (verify-mutation-diff); the full run is verify-mutation.
set -eu
. "$(dirname "$0")/scope.sh"
mkdir -p build
log=build/mutation.log

# gremlins compiles the suite once per mutant. Those builds are never reused,
# so they go in a cache of their own that is emptied when the run ends,
# instead of growing the shared Go build cache by gigabytes per run.
GOCACHE="${DOORCODE_MUTATION_GOCACHE:-${XDG_CACHE_HOME:-$HOME/.cache}/doorcode-mutation}"
export GOCACHE
mkdir -p "$GOCACHE"
trap 'go clean -cache' EXIT

diff=""
if [ -n "${DOORCODE_MUTATION_BASE:-}" ]; then
	diff="--diff=$DOORCODE_MUTATION_BASE"
	echo "mutating only lines changed since $DOORCODE_MUTATION_BASE"
fi

# shellcheck disable=SC2046,SC2086
go tool gremlins unleash --integration $diff \
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
