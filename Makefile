# Verification interface. See AGENTS.md for what each gate enforces.
GO ?= go
LINT = $(GO) tool golangci-lint
# Runtime budget for everything verify-fast runs as tests, in seconds.
FAST_BUDGET_SECONDS ?= 10

.PHONY: verify-fast verify-mutation verify-integration verify-all fmt

verify-fast:
	$(GO) build ./...
	$(GO) vet ./...
	$(GO) vet -tags=slow ./test/connector/...
	$(LINT) run
	$(LINT) run --build-tags=slow
	$(LINT) run -c .golangci.connector.yml
	sh scripts/fast-tests.sh $(FAST_BUDGET_SECONDS)
	sh scripts/deadcode.sh
	@echo "duplicate-code report (never fails):"
	$(LINT) run -c .golangci.dupl.yml --issues-exit-code=0
	@echo "verify-fast ok"

verify-mutation:
	sh scripts/mutation.sh

verify-integration:
	$(GO) test -count=1 -tags=slow ./test/connector/...

verify-all:
	$(MAKE) verify-fast
	$(MAKE) verify-mutation
	$(MAKE) verify-integration

fmt:
	$(LINT) fmt
