#!/bin/sh
# Usage: fast-tests.sh BUDGET_SECONDS
# Runs everything verify-fast runs as tests: the entrypoint suite with
# coverage over application packages, then the fast connector tests with
# connector coverage reported for information. Fails if the tests fail, if
# coverage is below 100%, or if the tests together exceed the budget.
set -eu
. "$(dirname "$0")/scope.sh"
budget=$1
mkdir -p build

start=$(date +%s%N)
go test -count=1 -coverpkg="$(app_pkgs)" -coverprofile=build/entrypoint.cover ./test/entrypoint/...
go test -count=1 -coverpkg="$(driver_pkgs)" -coverprofile=build/connector.cover ./test/connector/...
end=$(date +%s%N)

echo "connector coverage (fast connector tests, information only):"
go tool cover -func=build/connector.cover | sed 's/^/  /'

sh "$(dirname "$0")/coverage-gate.sh" build/entrypoint.cover

elapsed_ms=$(((end - start) / 1000000))
budget_ms=$((budget * 1000))
if [ "$elapsed_ms" -gt "$budget_ms" ]; then
	echo "runtime budget FAILED: fast tests took ${elapsed_ms}ms, budget ${budget_ms}ms"
	exit 1
fi
echo "runtime budget ok: fast tests took ${elapsed_ms}ms of ${budget_ms}ms"
