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

# A GOROOT inherited from the shell can point at a different version than
# the go binary chosen above -- goenv sets it, and it goes stale as soon
# as another version is selected. go then runs one version's driver
# against another's compiler and fails with "does not match go tool
# version". Unset, go works its own GOROOT out from where it lives, which
# is exactly the version picked above.
unexport GOROOT

# Print the help when make is run with no target.
.DEFAULT_GOAL := help

IMAGE := torrnado

.PHONY: help build run test test-race e2e cover vet fmt fmt-check tidy check clean docker docker-test systemd-test

help: ## Show this help
	@grep -hE '^[a-z0-9-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk -F':.*?## ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary into the repo root
	$(GO) build -o $(BINARY) $(PKG)

run: build ## Build, then run it
	./$(BINARY)

test: ## Run all tests
	$(GO) test ./...

test-race: ## Run all tests with the race detector
	$(GO) test -race -count=1 ./...

# systemd_test.sh is excluded on purpose: it needs a booted systemd and
# drives a real service, so it only ever runs through `make systemd-test`
# (the script refuses to run outside that container anyway).
E2E_TESTS := $(filter-out e2e/systemd_test.sh,$(wildcard e2e/*_test.sh))

e2e: build ## Drive the built binary through the shell tests
	@for t in $(E2E_TESTS); do \
		echo "== $$t"; \
		bash "$$t" || exit 1; \
	done

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

docker: ## Build the container image
	docker build -t $(IMAGE) .

docker-test: ## Run the whole suite on linux, in a container
	docker build --target test --progress plain .

# systemd needs a booted system to test against, which a build stage
# cannot give it -- pid 1 there is the build command. So this one builds
# an image and runs it, with the privileges systemd requires (not
# torrnado: the service inside runs unprivileged).
systemd-test: ## Test the systemd unit against a real systemd, in a container
	docker build -f Dockerfile.systemd -t $(IMAGE)-systemd .
	-docker rm -f $(IMAGE)-systemd >/dev/null 2>&1
	docker run -d --name $(IMAGE)-systemd --privileged --cgroupns=host \
		-v /sys/fs/cgroup:/sys/fs/cgroup:rw $(IMAGE)-systemd >/dev/null
	@status=0; docker exec $(IMAGE)-systemd bash /opt/systemd_test.sh || status=$$?; \
		docker rm -f $(IMAGE)-systemd >/dev/null; \
		exit $$status

clean: ## Remove build artifacts
	rm -f $(BINARY) coverage.out
