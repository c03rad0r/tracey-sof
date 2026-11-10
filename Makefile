# Local Makefile: dependency/build caches stay inside this project.
SHELL := /bin/sh
PROJECT := $(CURDIR)
export GOPATH := $(PROJECT)/.cache/gopath
export GOMODCACHE := $(PROJECT)/.cache/gomod
export GOCACHE := $(PROJECT)/.cache/build
export GOFLAGS := -mod=vendor
export TMPDIR := $(PROJECT)/.cache/tmp

DATA_DIR ?= ./data
OUT_DIR ?= ./out
CACHE_DIR ?= ./cache
SOF := ./bin/sof
ENV_FILE ?= node.env

ifneq ($(wildcard $(ENV_FILE)),)
include $(ENV_FILE)
export BITCOIND_RPC_URL BITCOIND_COOKIE_FILE BITCOIND_RPC_USER BITCOIND_RPC_PASSWORD
export TRACE_MAX_DEPTH TRACE_MAX_TRANSACTIONS SOF_LISTEN CHROMIUM
endif

.PHONY: help setup init vendor build clean validate node-check discover trace report audit ui export example test test-go test-web test-node coverage coverage-go coverage-web lint privacy-check verify release-check pdf

help: ## Show commands
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "%-20s %s\n", $$1, $$2}' Makefile

init: ## Create project-local Go test temporary space
	@mkdir -p $(TMPDIR)

setup: init vendor ## Install pinned development dependencies locally
	@mkdir -p $(TMPDIR)
	@node --version
	@go version
	@printf 'Setup complete. Runtime reports require no online dependencies.\n'

vendor: init ## Vendor Go dependencies for an offline build
	GOFLAGS= go mod tidy
	GOFLAGS= go mod vendor

build: init ## Build the CLI with embedded offline UI
	mkdir -p bin
	go build -trimpath -o $(SOF) ./cmd/sof

validate: build ## Validate local JSON inputs and evidence hashes
	$(SOF) validate -data-dir $(DATA_DIR)

node-check: build ## Check Core connectivity and capabilities without changing the node
	$(SOF) node-check -data-dir $(DATA_DIR)

discover: build ## Discover current owned UTXOs from Core
	$(SOF) discover -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR)

trace: build ## Trace cached targets using exact transaction ancestry
	$(SOF) trace -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR)

report: build ## Regenerate reports from local trace data without chain access
	$(SOF) report -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR)

audit: build ## Validate, discover, trace, and generate all reports
	$(SOF) audit -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR)

ui: build ## Serve reports and local annotations on loopback
	$(SOF) ui -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR)

export: build ## Create accountant bundle and SHA-256 manifest
	$(SOF) export -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR)

example: build ## Build an offline example from real mainnet fixtures
	$(SOF) example -out-dir ./out/example -fixtures ./testdata/mainnet

pdf: build ## Regenerate PDF through a local Chromium installation
	$(SOF) pdf -out-dir $(OUT_DIR)

test: test-go test-web ## Run all offline tests

test-go: init
	go test -race ./...

test-web:
	node --test ./web/graph.test.js
	go test ./webtest

test-node: build ## Verify committed mainnet fixtures through the configured Core node
	$(SOF) test-node -fixtures ./testdata/mainnet

coverage: coverage-go coverage-web ## Generate coverage reports and enforce first-party coverage
	@printf 'Coverage reports written below coverage/.\n'

coverage-go: init
	mkdir -p coverage
	go test -covermode=atomic -coverprofile=coverage/go.cover ./...
	go tool cover -func=coverage/go.cover
	GOFLAGS= go run ./cmd/coveragegate -profile coverage/go.cover -minimum 100

coverage-web:
	mkdir -p coverage
	node --experimental-test-coverage --test-coverage-lines=100 --test-coverage-branches=100 --test-coverage-functions=100 --test ./web/graph.test.js

lint: init
	gofmt -w $$(find cmd internal -name '*.go')
	test -z "$$(gofmt -l $$(find cmd internal -name '*.go'))"
	go vet ./...
	node --check web/graph.js
	node --check web/app.js

privacy-check: init ## Reject public APIs, key-material examples, and accidentally tracked private data
	@if find cmd internal web examples -type f ! -name '*_test.go' -exec grep -HnE 'https?://(mempool\.space|blockstream\.info|arkm\.com|arkham)' {} + | grep .; then echo 'prohibited third-party endpoint present'; exit 1; fi
	! grep -RInE '(xprv|yprv|zprv|tprv)' data 2>/dev/null || false
	go test ./internal/core ./internal/input ./internal/report ./webtest -run Privacy
	go test ./webtest

verify: lint test coverage privacy-check build example ## Run all offline release checks

release-check: verify test-node ## Include read-only verification against the configured Core node

clean: ## Remove generated binaries, coverage, and caches only
	rm -rf bin coverage .cache
	find out -mindepth 1 -maxdepth 1 ! -name '.gitkeep' -exec rm -rf {} + 2>/dev/null || true
