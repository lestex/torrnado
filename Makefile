# Common tasks, so nobody has to remember the exact go invocation.
# Run `make` on its own to see what is available.

BINARY := torrnado
PKG    := ./cmd/torrnado

# Go is pinned by .go-version and installed by goenv, which is not on the
# PATH a non-interactive shell gets -- an editor task or a CI step never
# sources a login shell, so `go` is simply missing there.
#
# So find the toolchain by absolute path when it is there, and fall back
# to whatever `go` is on PATH when it is not (no goenv on this machine).
# Two details make this the shape it is:
#
#   - It points at the pinned version's own bin directory, not goenv's
#     shims. A shim is a script that re-execs `goenv`, so it needs goenv
#     itself on PATH and fails confusingly when that is missing.
#   - The recipes call $(GO), not `go`. make runs a simple recipe line
#     directly instead of through a shell, and resolves the program using
#     the PATH make started with -- so exporting PATH below is not enough
#     on its own.
GO_VERSION := $(shell cat .go-version 2>/dev/null)
GOENV_BIN  := $(HOME)/.goenv/versions/$(GO_VERSION)/bin
GO         := $(if $(wildcard $(GOENV_BIN)/go),$(GOENV_BIN)/go,go)
GOFMT      := $(if $(wildcard $(GOENV_BIN)/gofmt),$(GOENV_BIN)/gofmt,gofmt)

# Still exported, for anything the recipes shell out to.
export PATH := $(GOENV_BIN):$(PATH)

# Print the help when make is run with no target.
.DEFAULT_GOAL := help

.PHONY: help build run test test-race cover vet fmt fmt-check tidy check clean

help: ## Show this help
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk -F':.*?## ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary into the repo root
	$(GO) build -o $(BINARY) $(PKG)

run: build ## Build, then run it
	./$(BINARY)

test: ## Run all tests
	$(GO) test ./...

test-race: ## Run all tests with the race detector
	$(GO) test -race -count=1 ./...

cover: ## Run tests and open a coverage report
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out

vet: ## Report suspicious constructs
	$(GO) vet ./...

fmt: ## Rewrite source files to canonical formatting
	$(GOFMT) -w .

fmt-check: ## Fail if any file is not gofmt-clean
	@unformatted=$$($(GOFMT) -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

tidy: ## Add missing and drop unused dependencies
	$(GO) mod tidy

check: fmt-check vet test ## Everything that must pass before committing

clean: ## Remove build artifacts
	rm -f $(BINARY) coverage.out
