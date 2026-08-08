# Common tasks, so nobody has to remember the exact go invocation.
# Run `make` on its own to see what is available.

BINARY := torrnado
PKG    := ./cmd/torrnado

# Go is pinned by .go-version and installed by goenv, which is not on the
# PATH a non-interactive shell gets - an editor task or a CI step never
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
#     the PATH make started with - so exporting PATH below is not enough
#     on its own.
GO_VERSION := $(shell cat .go-version 2>/dev/null)
GOENV_BIN  := $(HOME)/.goenv/versions/$(GO_VERSION)/bin
GO         := $(if $(wildcard $(GOENV_BIN)/go),$(GOENV_BIN)/go,go)
GOFMT      := $(if $(wildcard $(GOENV_BIN)/gofmt),$(GOENV_BIN)/gofmt,gofmt)

# Still exported, for anything the recipes shell out to.
export PATH := $(GOENV_BIN):$(PATH)

# A GOROOT inherited from the shell can point at a different version than
# the go binary chosen above - goenv sets it, and it goes stale as soon
# as another version is selected. go then runs one version's driver
# against another's compiler and fails with "does not match go tool
# version". Unset, go works its own GOROOT out from where it lives, which
# is exactly the version picked above.
unexport GOROOT

# Print the help when make is run with no target.
.DEFAULT_GOAL := help

IMAGE := torrnado

# Stamped into the binary so `torrnado version` says something useful
# before there is a release to build. The release build passes the same
# three variables; see .goreleaser.yaml.
#
# A tag when the commit has one, else a short sha - and the toolchain
# fills these in by itself when they are empty, so a plain `go build`
# still reports its revision.
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null)
COMMIT    := $(shell git rev-parse HEAD 2>/dev/null)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(BUILD_DATE)

VENV := .venv
PIP  := $(VENV)/bin/pip

.PHONY: help build run test test-race e2e cover vet fmt fmt-check tidy check clean changelog docker docker-test systemd-test docs-deps docs-serve docs-build

help: ## Show this help
	@grep -hE '^[a-z0-9-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk -F':.*?## ' '{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary into the repo root
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) $(PKG)

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
# cannot give it - pid 1 there is the build command. So this one builds
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

# The docs site is MkDocs Material, pinned in docs-requirements.txt so a
# local build and the CI build produce the same pages.
#
# NO_MKDOCS_2_WARNING silences a banner Material prints on every build
# about breaking changes coming in MkDocs 2.0. It is a notice about the
# upstream project's direction, not about this site: the versions here
# are pinned, and nothing here uses a third-party plugin or a theme
# override, which is what that release is said to break. Worth rereading
# when the pins are bumped.
$(VENV): docs-requirements.txt
	python3 -m venv $(VENV)
	$(PIP) install --quiet --upgrade pip
	$(PIP) install --quiet -r docs-requirements.txt
	@touch $(VENV)

docs-deps: $(VENV) ## Create the docs virtualenv

docs-serve: $(VENV) ## Serve the docs site locally with live reload
	NO_MKDOCS_2_WARNING=true $(VENV)/bin/mkdocs serve

docs-build: $(VENV) ## Build the docs site the way CI does
	NO_MKDOCS_2_WARNING=true $(VENV)/bin/mkdocs build --strict

# git-cliff generates CHANGELOG.md from the commit log, the same way the
# release workflow generates the release notes - one config, so the file
# and the release page cannot disagree.
#
# Used from PATH when it is installed (brew install git-cliff), through
# its container image when it is not, which keeps this runnable on a
# machine that has never heard of it.
# Pinned, and kept in step with .github/workflows/release.yml.
CLIFF_IMAGE := orhunp/git-cliff:2.13.1

# TAG names the release being prepared, so the pending commits are filed
# under it instead of "Unreleased" - run it that way just before tagging:
#
#	make changelog TAG=v0.1.0
CLIFF_ARGS := --config cliff.toml $(if $(TAG),--tag $(TAG)) --output CHANGELOG.md

changelog: ## Regenerate CHANGELOG.md (make changelog TAG=v0.1.0)
	@if command -v git-cliff >/dev/null 2>&1; then \
		git-cliff $(CLIFF_ARGS); \
	else \
		echo "git-cliff not on PATH; using $(CLIFF_IMAGE)"; \
		docker run --rm --user "$$(id -u):$$(id -g)" \
			-v "$(CURDIR)":/app -w /app $(CLIFF_IMAGE) $(CLIFF_ARGS); \
	fi
	@echo "wrote CHANGELOG.md"

clean: ## Remove build artifacts
	rm -f $(BINARY) coverage.out
	rm -rf site
