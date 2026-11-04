# portman: run `make help` for the list of targets.

SHELL := /bin/bash
.DEFAULT_GOAL := help

BIN ?= bin/pm
GO  ?= go

# Release version: the nearest v* tag (plus commits and hash past it), or dev
VERSION ?= $(or $(shell git describe --tags --match 'v*' --dirty 2>/dev/null),dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
TARGETS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

##@ General

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make \033[36m<target>\033[0m\n"} \
		/^[a-zA-Z_-]+:.*?##/ { printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2 } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) }' $(MAKEFILE_LIST)

##@ Build

.PHONY: build
build: ## Build bin/pm for this machine
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/pm

.PHONY: dist
dist: ## Build every release binary and SHA256SUMS into dist/
	rm -rf dist && mkdir dist
	@for t in $(TARGETS); do \
		os=$${t%/*} arch=$${t#*/} ext=; [[ $$os == windows ]] && ext=.exe; \
		echo "  $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags '$(LDFLAGS)' \
			-o dist/pm_$(VERSION)_$${os}_$${arch}$$ext ./cmd/pm || exit 1; \
	done
	cp install.sh dist/
	cd dist && sha256sum * > SHA256SUMS

##@ Quality

.PHONY: test
test: ## Run Go and release-script tests
	$(GO) test -race ./...
	bash scripts/release_test.sh

.PHONY: check
check: ## gofmt check and go vet
	@out=$$(gofmt -l cmd internal); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	$(GO) vet ./...

.PHONY: ci
ci: check test build ## Everything CI runs

##@ Release

.PHONY: release-beta
release-beta: ## Tag and push the next beta, like v0.2.0-beta.3
	bash scripts/release.sh beta

.PHONY: release-stable
release-stable: ## Promote the newest beta to a stable tag, like v0.2.0
	bash scripts/release.sh stable

.PHONY: clean
clean: ## Remove build output
	rm -rf bin dist
