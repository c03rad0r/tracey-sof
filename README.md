# tracey-sof

Local, Core-only source-of-funds analysis for current Bitcoin UTXOs. It discovers outpoints, walks exact transaction ancestry backward, associates user-supplied purchase evidence, and produces an accountant-friendly graph and audit bundle. Runtime operation uses no AI or third-party services.

## Status

Implementation and release verification are complete for the current source tree. See [`PLAN.md`](PLAN.md) for decisions and the living verification checklist. Also see [`docs/verification.md`](docs/verification.md) for repeatable checks and the successful `dq05` live-node integration record. The latest local release check passed with 100% first-party Go statement coverage and 100% JavaScript statement/branch/function/line coverage. Do not infer provenance completeness from an interrupted or partial trace.

## Quick start

Requirements: Go, Make, a fully synchronized Bitcoin Core 29+ node, and a modern browser. Chromium is required for the default local PDF-generation step. All application network access is confined to your configured Core node and loopback UI.

```sh
make setup
install -m 600 examples/node.env.example node.env
install -d -m 700 data
install -m 600 examples/owned.example.json data/owned.json
install -m 600 examples/purchases.example.json data/purchases.json
install -m 600 examples/annotations.example.json data/annotations.json
install -m 600 examples/trace-policy.example.json data/trace-policy.json
# Edit node.env and replace every example identifier with your own public data.
make node-check
make audit
make ui
```

The service listens on `127.0.0.1` by default. Never expose the annotation server or Bitcoin RPC directly to the public internet. Xpubs, addresses, purchase records, and their combined transaction graph are sensitive financial information. See [`docs/inputs.md`](docs/inputs.md) for complete JSON examples and [`docs/bitcoin-core.md`](docs/bitcoin-core.md) for node configuration.

Outputs are written below `out/`. `make example` runs without a node or private data. `make help` lists all supported commands.

`make report` and `make export` reuse the cached ancestry for label/note edits. After editing purchase records, `purchase_links`, or `data/trace-policy.json`, always rerun `make audit` so evidence and policy boundaries are recomputed.

## Evidence limits

Bitcoin establishes exact links between previous outputs and spending inputs, not which individual satoshis move to each output of a mixed-input transaction. This tool therefore distinguishes `documented`, `partially_documented`, and `undocumented` from `complete` and `incomplete` traversal. It never labels a capped or failed trace as conclusive evidence that no purchase documentation exists. See `PLAN.md` and `docs/` for the full methodology.
