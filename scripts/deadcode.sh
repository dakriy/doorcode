#!/bin/sh
# Fails on any production function unreachable from the main packages.
# Test support is reached only from tests and is not production code.
set -eu
. "$(dirname "$0")/scope.sh"
findings=$(go tool deadcode ./... | grep -v '^internal/testsupport/' || true)
if [ -n "$findings" ]; then
	echo "dead-code check FAILED: production code unreachable from any main package:"
	printf '%s\n' "$findings"
	exit 1
fi
echo "dead-code check ok"
