# Source-of-Funds UTXO Provenance Plan

Last updated: 2026-11-10

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

## Planned files and commands

- `data/owned.json`: current addresses, xpub configurations, descriptors, and explicit outpoints.
- `data/purchases.json`: purchase IDs, txids, associated addresses, outpoints, optional amounts, notes, and local evidence artifacts with SHA-256 hashes.
- `data/annotations.json`: labels, notes, and manual review decisions, written atomically by the UI.
- `schema/`: versioned JSON Schemas.
- `examples/`: sanitized inputs and an offline mainnet-fixture report.
- `internal/core`: read-only RPC, chain capability checks, and cache.
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
- [ ] **P5.2** Cover unit, RPC, malformed input, graph, matching, cache, annotation, report, security, UI, and CLI behavior with automated tests. **In progress:** functional suites pass when in-flight coverage-test files are complete; CLI/server/fixture coverage is still being expanded.
- [ ] **P5.3** Enforce 100% statement and branch coverage for first-party Go and JavaScript analysis modules; browser integration tests supplement coverage rather than substitute for it. **The user explicitly confirmed the literal 100% target after the remaining defensive filesystem/marshal gaps were identified.** Current measured repository-wide Go statement coverage is **94.86%**; narrowly scoped fault-injection is required for the remaining low-level I/O branches. Go uses standard statement-coverage profiles plus explicit success/failure-path cases; JavaScript additionally enforces branch/function/line metrics. No unmeasured control-flow coverage metric is claimed for Go.
- [x] **P5.4** Run read-only tests against a real Core node where available, and record any genuinely unavailable external validation explicitly.
- [x] **P5.5** Generate and inspect offline HTML, SVG, PDF, and accountant artifacts; assert no third-party requests.
- [ ] **P5.6** Require `make release-check` to pass before declaring the project release-qualified. **Development publication was explicitly authorized on the current `main` branch while coverage work continues; this is not a release tag or a claim that the 100% gate has passed.** No release tag will be created until the gate passes.

### 6. Explicitly deferred

- [ ] Historical spent-address discovery when no current UTXO is supplied.
- [ ] Self-hosted Esplora adapter.
- [ ] Optional FIFO/LIFO/pro-rata amount allocation.
- [ ] TRACEY's speculative exchange clustering, change detection, peel-chain, and wallet-fingerprint heuristics.
