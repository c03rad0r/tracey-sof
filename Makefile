# Local Makefile: dependency/build caches stay inside this project.
SHELL := /bin/sh
PROJECT := $(CURDIR)
export GOPATH := $(PROJECT)/.cache/gopath
export GOMODCACHE := $(PROJECT)/.cache/gomod
export GOCACHE := $(PROJECT)/.cache/build
export GOFLAGS := -mod=vendor
export TMPDIR := $(PROJECT)/.cache/tmp

# Private financial inputs live outside this repository. Override TRACEY_INPUTS
# to point at another private tree; never commit that tree.
TRACEY_INPUTS ?= $(HOME)/tracey-inputs
DATA_DIR ?= $(TRACEY_INPUTS)/data
OUT_DIR ?= $(TRACEY_INPUTS)/out
CACHE_DIR ?= $(TRACEY_INPUTS)/cache
INBOX ?= $(DATA_DIR)/inbox
SOF := ./bin/sof
NODE ?= node
ENV_FILE ?= $(TRACEY_INPUTS)/node.env
# Identifiers are visible by default for review. Set UNREDACTED=0 before
# generating a shareable copy.
UNREDACTED ?= 1
ANSIBLE_INVENTORY ?= ansible/inventory.example.ini
ANSIBLE_ARGS ?=
ANSIBLE_CONFIG := $(PROJECT)/ansible/ansible.cfg
export ANSIBLE_CONFIG
CORE_VERSION ?=
CORE_ARCH ?= x86_64-linux-gnu
CORE_LOCAL_RELEASE_DIR ?= $(PROJECT)/.cache/core-release/$(CORE_VERSION)
CORE_RELEASE_DIR ?= $(CORE_LOCAL_RELEASE_DIR)
CORE_KEYS_FILE ?=
CORE_TRUSTED_FINGERPRINTS ?=
CORE_MANAGE_SERVICE ?= false
CORE_SERVICE ?= bitcoind
CORE_DATADIR ?=

ifneq ($(wildcard $(ENV_FILE)),)
include $(ENV_FILE)
export TRACE_MAX_DEPTH TRACE_MAX_TRANSACTIONS SOF_LISTEN SOF_TIMEZONE CHROMIUM
endif

# Merchant/provider/exchange labels are private configuration used only by
# bank/evidence reporting targets. They are not exposed to unrelated recipes.
MERCHANT ?=
PROVIDER ?=
SOF_EXCHANGE_LABELS ?=
BANK_LABEL_TARGETS := bank-import gaps gaps-inflow gaps-outflow gaps-check gaps-check-all report audit overview export ui
unexport MERCHANT PROVIDER SOF_EXCHANGE_LABELS
$(BANK_LABEL_TARGETS): export MERCHANT := $(MERCHANT)
$(BANK_LABEL_TARGETS): export PROVIDER := $(PROVIDER)
$(BANK_LABEL_TARGETS): export SOF_EXCHANGE_LABELS := $(SOF_EXCHANGE_LABELS)

# Only RPC-using targets inherit Bitcoin node credentials. Keeping these out of
# unrelated recipes avoids exposing them to package installers, OCR, Chromium,
# and other child processes. unexport also covers values supplied on the make
# command line or inherited from the environment.
unexport BITCOIND_RPC_URL BITCOIND_COOKIE_FILE BITCOIND_RPC_USER BITCOIND_RPC_PASSWORD
RPC_TARGETS := node-check discover trace audit resolve-intake test-node
$(RPC_TARGETS): export BITCOIND_RPC_URL := $(BITCOIND_RPC_URL)
$(RPC_TARGETS): export BITCOIND_COOKIE_FILE := $(BITCOIND_COOKIE_FILE)
$(RPC_TARGETS): export BITCOIND_RPC_USER := $(BITCOIND_RPC_USER)
$(RPC_TARGETS): export BITCOIND_RPC_PASSWORD := $(BITCOIND_RPC_PASSWORD)

.PHONY: help inputs setup init vendor build clean clean-outputs validate labels-import node-check discover trace report audit ui graphs bank-import statement-requests gaps gaps-inflow gaps-outflow gaps-check gaps-check-all export example test test-go test-web test-node coverage coverage-go coverage-web lint privacy-check leak-check history-leak-check verify release-check pdf intake resolve-intake review-intake promote-intake provision ansible-check core-check core-preflight core-download core-verify core-install core-update

help: ## Show commands
	@awk 'BEGIN {FS = ":.*## "}; /^[a-zA-Z0-9_.-]+:.*## / {printf "%-20s %s\n", $$1, $$2}' Makefile

init: ## Create project-local Go test temporary space
	@mkdir -p $(TMPDIR)

inputs: ## Create the private inputs tree and seed it from examples
	@mkdir -p "$(DATA_DIR)" "$(CACHE_DIR)" "$(OUT_DIR)" "$(TRACEY_INPUTS)/notes"
	@for name in owned annotations purchases; do \
		test -e "$(DATA_DIR)/$$name.json" || cp "examples/$$name.example.json" "$(DATA_DIR)/$$name.json"; \
	done
	@test -e "$(DATA_DIR)/trace-policy.json" || cp examples/trace-policy.example.json "$(DATA_DIR)/trace-policy.json"
	@test -e "$(ENV_FILE)" || { cp examples/node.env.example "$(ENV_FILE)"; chmod 600 "$(ENV_FILE)"; }
	@chmod 700 "$(TRACEY_INPUTS)" "$(DATA_DIR)" "$(CACHE_DIR)" "$(OUT_DIR)" "$(TRACEY_INPUTS)/notes"
	@chmod -R go-rwx "$(TRACEY_INPUTS)"
	@printf 'Private inputs ready under %s\n' "$(TRACEY_INPUTS)"

setup: init vendor ## Install pinned development dependencies locally
	@mkdir -p $(TMPDIR)
	@$(NODE) --version
	@go version
	@printf 'Setup complete. Runtime reports require no online dependencies.\n'

provision: ansible-check ## Install host dependencies through Ansible
	ansible-playbook -i $(ANSIBLE_INVENTORY) ansible/playbooks/provision.yml $(ANSIBLE_ARGS)

ansible-check: ## Validate the Ansible playbooks without changing the host
	ansible-playbook -i $(ANSIBLE_INVENTORY) --syntax-check ansible/playbooks/provision.yml
	ansible-playbook -i $(ANSIBLE_INVENTORY) --syntax-check ansible/playbooks/bitcoin-core.yml

core-check: ## Report installed Bitcoin Core version and RPC/index status
	./scripts/core-check.sh

core-preflight: ## Check configured Core datadir and required systemd mounts
	CORE_SERVICE="$(CORE_SERVICE)" CORE_DATADIR="$(CORE_DATADIR)" ./scripts/core-preflight.sh

core-download: ## Download a Bitcoin Core release for explicit verification
	@test -n "$(CORE_VERSION)" || { echo 'CORE_VERSION is required'; exit 1; }
	CORE_RELEASE_DIR="$(CORE_LOCAL_RELEASE_DIR)" ./scripts/core-release.sh download "$(CORE_VERSION)" "$(CORE_ARCH)"

core-verify: ## Verify Core GPG signatures and tarball SHA-256
	@test -n "$(CORE_VERSION)" || { echo 'CORE_VERSION is required'; exit 1; }
	@test -n "$(CORE_KEYS_FILE)" || { echo 'CORE_KEYS_FILE is required'; exit 1; }
	@test -n "$(CORE_TRUSTED_FINGERPRINTS)" || { echo 'CORE_TRUSTED_FINGERPRINTS is required'; exit 1; }
	CORE_RELEASE_DIR="$(CORE_LOCAL_RELEASE_DIR)" TRUSTED_FINGERPRINTS="$(CORE_TRUSTED_FINGERPRINTS)" ./scripts/core-release.sh verify "$(CORE_VERSION)" "$(CORE_ARCH)" "$(CORE_KEYS_FILE)"

core-install: core-verify ## Install an already-verified Bitcoin Core release
	ansible-playbook -i $(ANSIBLE_INVENTORY) ansible/playbooks/bitcoin-core.yml \
		-e bitcoin_core_version="$(CORE_VERSION)" \
		-e bitcoin_core_arch="$(CORE_ARCH)" \
		-e bitcoin_core_release_dir="$(CORE_RELEASE_DIR)" \
		-e bitcoin_core_local_release_dir="$(CORE_LOCAL_RELEASE_DIR)" \
		-e bitcoin_core_manage_service=$(CORE_MANAGE_SERVICE) \
		-e bitcoin_core_service_name="$(CORE_SERVICE)" \
		-e bitcoin_core_datadir="$(CORE_DATADIR)" $(ANSIBLE_ARGS)

core-update: CORE_MANAGE_SERVICE = true
core-update: core-install ## Stop, install, and restart a verified Core update

vendor: init ## Vendor Go dependencies for an offline build
	GOFLAGS= go mod tidy
	GOFLAGS= go mod vendor

build: init ## Build the CLI with embedded offline UI
	mkdir -p bin
	go build -trimpath -o $(SOF) ./cmd/sof

validate: build ## Validate local JSON inputs and evidence hashes
	$(SOF) validate -data-dir $(DATA_DIR)

labels-import: build ## Merge a Sparrow/BIP-329 JSONL export into data/annotations.json
	@test -n "$(LABELS)" || { echo 'LABELS is required (path to a BIP-329 .jsonl export)'; exit 1; }
	python3 ./scripts/import-bip329.py "$(LABELS)" --annotations $(DATA_DIR)/annotations.json $(if $(filter 1 true yes,$(OVERWRITE)),--overwrite,)
	$(SOF) validate -data-dir $(DATA_DIR)

node-check: build ## Check Core connectivity and capabilities without changing the node
	$(SOF) node-check -data-dir $(DATA_DIR)

discover: build ## Discover current owned UTXOs from Core
	$(SOF) discover -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR)

intake: build ## Extract purchase candidates from receipts and address lists
	$(SOF) intake -data-dir $(DATA_DIR) -inbox $(INBOX)

resolve-intake: build ## Resolve intake candidates against current UTXOs
	$(SOF) resolve-intake -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR)

review-intake: build ## List staged intake candidates for review
	$(SOF) review-intake -data-dir $(DATA_DIR) -list

promote-intake: build ## Merge accepted intake candidates into purchases.json
	$(SOF) promote-intake -data-dir $(DATA_DIR)

trace: build ## Trace cached targets using exact transaction ancestry
	$(SOF) trace -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR)

report: build ## Regenerate reports from local trace data without chain access
	$(SOF) report -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR) -unredacted=$(UNREDACTED)

audit: build ## Validate, discover, trace, and generate all reports
	$(SOF) audit -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR) -unredacted=$(UNREDACTED)

ui: build ## Serve reports and local annotations on loopback
	$(SOF) ui -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR)

overview: build ## One PDF/HTML overview: gaps first, blocks newest to oldest
	$(SOF) overview -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR)

graphs: build ## Write graph-all.svg and one focused SVG per current UTXO
	$(SOF) graphs -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR)

BANK ?=
APPLY ?= 0
CONFIRM ?=

bank-import: build ## Import reviewed bank matches with explicit confirmations
	@test -n "$(BANK)" || { echo 'BANK is required (path to a bank CSV export)'; exit 1; }
	@test -n "$(MERCHANT)" || { echo 'MERCHANT is required; set it in $(ENV_FILE)'; exit 1; }
	@test -n "$(PROVIDER)" || { echo 'PROVIDER is required; set it in $(ENV_FILE)'; exit 1; }
	$(SOF) bank-import -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR) -bank-file "$(BANK)" -merchant "$(MERCHANT)" -provider "$(PROVIDER)" $(if $(filter 1 true yes,$(APPLY)),-apply,) -confirm "$(CONFIRM)"

statement-requests: build ## Regenerate bank-proof request placeholders
	$(SOF) statement-requests -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR)

gaps: build ## Print inbound and outbound exchange gap lists
	$(SOF) gaps -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR)

gaps-inflow: build ## Print only unaccounted inbound exchange payouts
	$(SOF) gaps -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR) -inflow

gaps-outflow: build ## Print only outbound exchange transfers without sale evidence
	$(SOF) gaps -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR) -outflow

gaps-check: build ## Fail while unaccounted inbound payouts remain
	$(SOF) gaps -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR) -strict

gaps-check-all: build ## Fail while inbound or outbound gaps remain
	$(SOF) gaps -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR) -strict -include-outflows

export: build ## Create accountant bundle and SHA-256 manifest
	$(SOF) export -data-dir $(DATA_DIR) -cache-dir $(CACHE_DIR) -out-dir $(OUT_DIR)

EXAMPLE_OUT ?= $(PROJECT)/.cache/example
example: build ## Build an offline example from real mainnet fixtures
	$(SOF) example -out-dir "$(EXAMPLE_OUT)" -fixtures ./testdata/mainnet

pdf: build ## Regenerate PDF through a local Chromium installation
	$(SOF) pdf -out-dir $(OUT_DIR) -unredacted=$(UNREDACTED)

test: test-go test-web ## Run all offline tests

test-go: init
	go test -race ./...

test-web:
	$(NODE) --test ./web/graph.test.js
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

coverage-web: init
	mkdir -p coverage
	@$(NODE) -e 'const major = Number(process.versions.node.split(".")[0]); if (major < 22) { console.error("coverage-web requires Node 22+; set NODE=/path/to/node (current: " + process.version + ")"); process.exit(1); }'
	$(NODE) --experimental-test-coverage --test-coverage-lines=100 --test-coverage-branches=100 --test-coverage-functions=100 --test ./web/graph.test.js

lint: init
	gofmt -w $$(find cmd internal -name '*.go')
	test -z "$$(gofmt -l $$(find cmd internal -name '*.go'))"
	go vet ./...
	$(NODE) --check web/graph.js
	$(NODE) --check web/app.js

privacy-check: init ## Reject public transaction APIs, key-material examples, and tracked private data
	./scripts/privacy-scan.sh
	@if [ -d "$(DATA_DIR)" ]; then ! grep -RInE '(xprv|yprv|zprv|tprv)' "$(DATA_DIR)"; fi
	go test ./internal/core ./internal/input ./internal/report ./webtest -run Privacy
	go test ./webtest

leak-check: ## Reject private data or prohibited transaction services in the working tree
	./scripts/privacy-scan.sh

history-leak-check: ## Advisory full-history scan; current-tree scan remains the release gate
	@-./scripts/privacy-scan.sh --history

verify: lint test coverage privacy-check leak-check build example ## Run all offline release checks

release-check: verify test-node ## Include read-only verification against the configured Core node

clean: ## Remove generated binaries, coverage, and caches only
	rm -rf bin coverage .cache

clean-outputs: ## Remove generated reports from the private output directory
	@test -n "$(OUT_DIR)" || { echo 'OUT_DIR is empty'; exit 1; }
	@test "$(OUT_DIR)" != "/" || { echo 'refusing to clean /'; exit 1; }
	@mkdir -p "$(OUT_DIR)"
	find "$(OUT_DIR)" -mindepth 1 -maxdepth 1 ! -name '.gitkeep' -exec rm -rf {} + 2>/dev/null || true
