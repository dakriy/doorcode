#!/bin/sh
# Fails when the checked-in docs/openapi.json differs from the document the
# route table produces. `make openapi` rewrites the checked-in file.
set -eu
mkdir -p build
go run ./cmd/doorcode-openapi >build/openapi.json
if ! diff -u docs/openapi.json build/openapi.json; then
	echo "OpenAPI drift gate FAILED: docs/openapi.json is not what the route table produces; run make openapi and commit it"
	exit 1
fi
echo "OpenAPI drift gate ok"
