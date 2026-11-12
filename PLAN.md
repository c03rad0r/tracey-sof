# Source-of-Funds UTXO Provenance Plan

Last updated: 2026-11-12

This file is the implementation plan and living checklist for this project. Update it whenever scope, architecture, commands, or completion status changes. A checked item means both its implementation and relevant verification are complete.

## Objectives

- Discover the user's current UTXOs from JSON-listed outpoints, addresses, watch-only descriptors, and unambiguously configured xpubs.
- Query only the user's Bitcoin Core node at runtime; no AI or third-party service is part of normal operation.
- Trace exact outpoint ancestry backward to user-documented purchases.
- Report fully documented, partially documented, undocumented, and incomplete cases without inventing sat-by-sat lineage.
- Persist labels and purchase-review annotations in a local JSON file.
- Produce an interactive transaction graph plus JSON, CSV, self-contained HTML, SVG, PDF, and a reproducible accountant bundle.
- Test with synthetic graphs and immutable real Bitcoin mainnet transactions. Do not publish until all required checks pass.

## Decisions

1. **TRACEY basis:** this is a modular reimplementation derived from TRACEY.diy's presentation ideas, reviewed at upstream commit `32d206717c76498666b46e9c47f7df9dc148a37c`. Its MIT license and attribution are retained. Its public API client, external explorer links, heuristic tracing, monolithic structure, and browser-only labels are not retained.
2. **Scope:** current UTXOs and their backward ancestry. Historical addresses with no current UTXO are reported as having no current UTXO, not as undocumented money.
3. **Node:** Bitcoin Core 29+ is the primary target. Discovery uses `scantxoutset`; targets are checked with `gettxout`. Transaction retrieval uses `txindex` when available and otherwise explicit-block retrieval where Core can supply sufficient metadata. Missing history is never silently treated as an unknown purchase source.
4. **Accounting:** exact transaction/outpoint paths are blockchain facts. User purchase records and annotations are evidence assertions. Through mixed-input transactions the report lists contributing purchases and unknown branches but does not allocate sats using FIFO, LIFO, or pro-rata assumptions.
5. **Matching:** an explicitly identified purchase transaction is a complete provenance boundary. A known received outpoint or purchase-address match applies only to its exact evidence scope. Amounts and dates are consistency signals, not autonomous proof.
6. **Status model:** documentation (`documented`, `partially_documented`, `undocumented`) and traversal (`complete`, `incomplete`) are independent. Incomplete is never converted into proven absence of documentation.
7. **Privacy:** no key material is accepted, no wallet is imported, no signing RPC is called, and the web service binds to loopback. Private inputs, annotations, caches, and exports are not versioned.
8. **Implementation:** Go for a small CLI, RPC, analysis, persistence, and export pipeline; HTML/CSS/JavaScript for the offline interface. Satoshis remain integers and BTC JSON values are parsed exactly.
9. **Trace policy boundaries:** optional JSON policies may cap hops/transactions, stop at a large ancestor output, stop at large input/output counts, or stop before a block height. These boundaries are auditable safety/review controls, never purchase evidence. A heuristic stop leaves the target incomplete and review-required. The large-value rule applies to an ancestor output being traversed, not to the current target's total value.
10. **Unmatched evidence diagnostics:** reports list supplied purchase transaction IDs, outpoints, and addresses that were not encountered, so missing matches can be investigated without mistaking them for proof.
11. **Purchase intake:** scanned receipts and address lists are extracted into a staging file, never directly into `purchases.json`. Candidate roles distinguish received-at addresses and purchase txids/outpoints from paid-to and operator addresses. OCR output is untrusted, local, and optional; promotion requires explicit human review. Resolution uses only the current UTXO set; historical spent-address discovery remains deferred.
12. **Provisioning:** optional Ansible roles install local build/runtime dependencies and versioned Bitcoin Core releases. Core installation is explicit, versioned, and refuses to proceed without GPG-verified release checksums and SHA-256 verification. The roles never overwrite `bitcoin.conf`, a data directory, or wallet data. `make provision` is limited to dependencies; Core updates use separate verification and installation targets.

## Planned files and commands

- `data/owned.json`: current addresses, xpub configurations, descriptors, and explicit outpoints.
- `data/purchases.json`: purchase IDs, txids, associated addresses, outpoints, optional amounts, notes, and local evidence artifacts with SHA-256 hashes.
- `data/annotations.json`: labels, notes, and manual review decisions, written atomically by the UI.
- `data/trace-policy.json`: optional auditable limits for hops, transaction count, large ancestor values, fan-in/fan-out, and block-height cutoff.
- `data/purchase-intake.json`: staged receipt/address candidates and source hashes; never consumed by audit until promoted.
- `ansible/`: optional host provisioning roles and inventory examples.
- `scripts/`: local release verification and Core health-check helpers.
- `schema/`: versioned JSON Schemas.
- `examples/`: sanitized inputs and an offline mainnet-fixture report.
- `internal/core`: read-only RPC, chain capability checks, and cache.
- `internal/intake`: local receipt/list extraction, candidate staging, current-UTXO resolution, review, and promotion.
- `internal/model`, `internal/input`, `internal/provenance`: normalized model, validation, graph traversal, evidence matching, and classification.
- `internal/report`: JSON/CSV/HTML/SVG/PDF and accountant bundle generation.
- `internal/server`: loopback annotation/report UI API.
- `cmd/sof`: command-line entry point.
- `web`: offline graph and annotation UI derived from TRACEY presentation ideas.
- `testdata/mainnet`: documented real transaction fixtures and expected results.
- `Makefile`: user and verification entry points.

Primary workflow:

```sh
make node-check
make audit
make ui
make export
```

Purchase intake workflow:

```sh
make intake
make resolve-intake
make review-intake
make promote-intake
make audit
```

Optional host provisioning and Core update workflow:

```sh
make ansible-check
make provision
make core-download CORE_VERSION=...
make core-verify CORE_VERSION=... CORE_KEYS_FILE=... CORE_TRUSTED_FINGERPRINTS=...
make core-install CORE_VERSION=... CORE_KEYS_FILE=... CORE_TRUSTED_FINGERPRINTS=...
```

Quality workflow:

```sh
make test
make coverage
make test-node
make privacy-check
make release-check
```

## Living checklist

### 1. Repository, documentation, and safety

- [x] **P1.1** Create project license, TRACEY attribution, README, schemas, examples, and private-path exclusions.
- [x] **P1.2** Record the architecture and this living checklist in `PLAN.md`.
- [x] **P1.3** Provide `make help`, setup/build/clean, user workflow, and verification targets.
- [x] **P1.4** Add project documentation for Core configuration, evidence semantics, limitations, and accountant exports.

### 2. Input and normalized data model

- [x] **P2.1** Define versioned owned, purchase, and annotation JSON formats with strict validation.
- [x] **P2.2** Reject wrong-network addresses, malformed outpoints/descriptors, private key material, duplicate IDs, unsafe paths, and invalid artifact hashes.
- [x] **P2.3** Normalize raw transactions to exact integer-satoshi transactions, outpoints, and optional addresses; verify txids.

### 3. Bitcoin Core discovery and tracing

- [x] **P3.1** Implement authenticated, bounded, read-only Core RPC with actionable capability checks and no credential logging.
- [x] **P3.2** Implement descriptor/address UTXO discovery and explicit-outpoint unspent checks.
- [x] **P3.3** Implement cached exact-prevout backward traversal, coinbase boundaries, shared ancestry, terminal reasons, limits, and snapshot/reorg checks.

### 4. Evidence and reporting

- [x] **P4.1** Implement scoped purchase matches, manual annotation overrides, independent documentation/trace statuses, and stable JSON/CSV summaries.
- [x] **P4.2** Generate a self-contained network-free HTML UI with graph, filters, timeline, labels, and annotation persistence.
- [x] **P4.3** Generate standalone SVG and print-ready PDF.
- [x] **P4.4** Export an accountant bundle with chain snapshot, methodology, limitations, selected artifacts, and SHA-256 manifest.

### 5. Mainnet and quality verification

- [x] **P5.1** Add documented immutable real-mainnet fixtures, including the block-170 transfer `f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16`, its coinbase parent, and representative multi-input/segwit cases.
- [x] **P5.2** Cover unit, RPC, malformed input, graph, matching, cache, annotation, report, security, UI, and CLI behavior with automated tests.
- [x] **P5.3** Enforce 100% statement and branch coverage for first-party Go and JavaScript analysis modules; browser integration tests supplement coverage rather than substitute for it. **Verified on 2026-11-11 after purchase intake: Go 100.00% (3131/3131 statements); JavaScript 100% statement/branch/function/line.** No unmeasured control-flow coverage metric is claimed for Go.
- [x] **P5.4** Run read-only tests against a real Core node where available, and record any genuinely unavailable external validation explicitly.
- [x] **P5.5** Generate and inspect offline HTML, SVG, PDF, and accountant artifacts; assert no third-party requests.
- [x] **P5.6** Require `make release-check` to pass before declaring the project release-qualified. **A development snapshot was published to <https://github.com/c03rad0r/tracey-sof> on `main` at commit `0cb5b1255dd92d84155d0e21734dbc6601f4eca4`.** The completed trace-policy follow-up (`tracey-sof/0.2.0`) passed the full release gate and was authorized for commit/push on 2026-11-11. The purchase-intake follow-up (`tracey-sof/0.3.0`) also passed the live release gate. No release tag has been created.

### 6. Trace policy and unmatched evidence

- [x] **P6.1** Define optional `trace-policy.json` schema and strict loader for hop/transaction limits, large ancestor value, fan-in/fan-out, and block-height cutoff.
- [x] **P6.2** Apply every policy rule during exact traversal, deduplicate boundaries, preserve existing explicit evidence priority, and record the effective policy in the trace.
- [x] **P6.3** Mark policy/limit boundaries as review-required and incomplete; never classify them as documented purchases or allocate sats.
- [x] **P6.4** Add matched/unmatched purchase-evidence diagnostics to JSON/CSV/HTML reports and display the effective policy and review summary.
- [x] **P6.5** Reject stale reports when `trace-policy.json` no longer matches the cached trace; require a fresh audit rather than silently applying old boundaries.
- [x] **P6.6** Add documentation, sanitized policy examples, report explanations, and Make workflow notes.
- [x] **P6.7** Test every rule, precedence, exact thresholds, malformed policy, stale policy, report fields, status/review semantics, and rerun the full offline suite, live-node check, and release gate. **All rule/precedence/report tests pass; `make release-check` passed with 100% Go/JavaScript coverage and live `dq05` policy verification.**

### 7. Purchase intake

- [x] **P7.1** Define a versioned `purchase-intake.json` schema and strict local loader/saver with source hashes, candidate roles, confidence, resolution, and review state.
- [x] **P7.2** Scan local PDFs, images, CSV/TSV, TXT, and JSON; use optional `pdftotext`, `pdftoppm`, and `tesseract` adapters with timeout and size limits.
- [x] **P7.3** Extract and validate mainnet addresses, txids, and outpoints without auto-trusting OCR dates or amounts.
- [x] **P7.4** Resolve candidate addresses through Core's current UTXO set and confirm txids/outpoints read-only; optionally mark candidates observed in the cached trace.
- [x] **P7.5** Provide CLI review commands for role assignment, accept/reject/pending, purchase metadata, and listing.
- [x] **P7.6** Promote only accepted, role-confirmed evidence into `purchases.json`, attach source artifacts and hashes, require human review, and refuse paid-to/operator/unknown roles.
- [x] **P7.7** Add `make intake`, `make resolve-intake`, `make review-intake`, and `make promote-intake` without changing the audit path.
- [x] **P7.8** Document the intake workflow, role semantics, OCR limitations, privacy rules, and deferred historical lookup.
- [x] **P7.9** Test extraction, tokenization, CSV parsing, command failures, resolution, review, promotion, malformed staging files, path confinement, and atomic writes. **Verified with 100% Go statement coverage across all first-party packages.**
- [x] **P7.10** Rerun `make verify`, `make release-check`, and live-node address/txid resolution before publication. **`make release-check` passed on the finished `tracey-sof/0.3.0` tree; live `dq05` verified current-UTXO address resolution and transaction resolution.**

### 8. Provisioning and Bitcoin Core updates

- [x] **P8.1** Add an Ansible inventory example and playbooks for dependency provisioning and Bitcoin Core installation.
- [x] **P8.2** Add an idempotent `sof_deps` role for Go, Node, Chromium, Poppler, Tesseract, build tools, and release-verification utilities on Ubuntu/Debian.
- [x] **P8.3** Add a versioned `bitcoin_core` role that installs under `/opt/bitcoin-core/<version>`, updates `/usr/local/bin` symlinks, preserves configuration/data, and optionally restarts systemd.
- [x] **P8.4** Require a locally downloaded, GPG-verified `SHA256SUMS` and matching tarball checksum before Core installation; reject unverified or changed archives.
- [x] **P8.5** Add `make provision`, `make ansible-check`, `make core-check`, `make core-download`, `make core-verify`, `make core-install`, and `make core-update`.
- [x] **P8.6** Document builder-key fingerprint verification, signed checksum verification, tarball checksum verification, backup/rollback, service restart, and post-upgrade checks in `docs/provisioning.md`.
- [x] **P8.7** Test Ansible syntax, shell syntax, verification failure paths, Core installation refusal, and the read-only `make core-check` path. **Ansible syntax, shell syntax, release-helper refusal/success, and `core-check` were exercised. The role's missing-marker refusal is ordered before any install task; the privileged install itself was not executed during development.**
- [x] **P8.8** Record the current Core 28 observation and the requirement to reach Core 29+ before relying on the documented target. **Recorded in `docs/provisioning.md` and `docs/verification.md`.**

### 9. Provisioning audit remediation

- [x] **R1** Scope `BITCOIND_*` credential exports to RPC-only Make targets so provisioning, OCR, Chromium, and release-download recipes do not inherit RPC credentials.
- [x] **R2** Validate `CORE_VERSION` and `CORE_ARCH`, and restrict Core downloads to HTTPS with a minimum TLS version and redirect policy.
- [x] **R3** Add a Core preflight target and managed-service preflight in the Ansible role that refuses missing datadirs, inactive services, and inactive required mount units.
- [x] **R4** Replace the narrow privacy grep with a repository-wide external-host allowlist scan covering `Makefile`, `ansible/`, `scripts/`, and application sources.
- [x] **R5** Clarify that a fingerprint list is not a builder-key file; keep local fingerprint notes ignored and require official `.gpg` keys for verification.
- [x] **R6** Run shell syntax checks, ShellCheck, Ansible syntax check, `make verify`, GPG refusal/success tests, and the privacy scan after remediation. **All passed; `make release-check` also passed against `dq05` after credential scoping. The privacy scanner rejects unknown and banned external hosts.**
- [x] **R7** Update provisioning and verification documentation with credential scoping, preflight checks, and the current blocked host state.
- [x] **R8** Repair the host blockchain mount, datadir configuration, and systemd dependency before any Core update. **The `BLOCKCHAIN` mount is active and `bitcoind` is running again. The current user still cannot access the service datadir/cookie, and the app-local `node.env` is not configured.**
- [x] **R9** Run the privileged Core 32.1 install/update from a sudo-capable terminal and verify the post-upgrade version. **Completed: `bitcoind` is active from `/opt/bitcoin-core/32.1`, `bitcoin-cli` reports v32.1.0, and the verified install marker is present.**
- [x] **R10** Generate a dedicated local RPC user and mode-600 `node.env`; keep the password out of command output and Git. **Created `node.env` and `cache/local-rpcauth.conf` with user `tracey-sof`.**
- [ ] **R11** Append the generated `rpcauth` line to the node's `bitcoin.conf`, enable `bitcoind` for reboot, and restart it. **Requires sudo access to the `admin` datadir.**
- [ ] **R12** Verify chain/index synchronization and run `make node-check` with the new local RPC credentials.

### 10. Explicitly deferred

- [ ] Historical spent-address discovery when no current UTXO is supplied.
- [ ] Self-hosted Esplora adapter.
- [ ] Optional FIFO/LIFO/pro-rata amount allocation.
- [ ] TRACEY's speculative exchange clustering, change detection, peel-chain, and wallet-fingerprint heuristics.
