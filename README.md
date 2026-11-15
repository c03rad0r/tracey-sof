# tracey-sof

Local, Core-only source-of-funds analysis for current Bitcoin UTXOs. It discovers outpoints, walks exact transaction ancestry backward, associates user-supplied purchase evidence, and produces an accountant-friendly graph and audit bundle. Runtime operation uses no AI or third-party services.

## Status

Implementation and release verification are complete for the current source tree. See [`PLAN.md`](PLAN.md) for decisions and the living verification checklist. Also see [`docs/verification.md`](docs/verification.md) for repeatable checks and the successful live-node integration record. The latest local release check passed with 100% first-party Go statement coverage and 100% JavaScript statement/branch/function/line coverage. Do not infer provenance completeness from an interrupted or partial trace.

## Quick start

Requirements: Go, Make, a fully synchronized Bitcoin Core 29+ node, and a modern browser. Chromium is required for the default local PDF-generation step. `make verify` requires Node 22+ for the JavaScript coverage flags; set `NODE=/path/to/node` if your default is older. All application network access is confined to your configured Core node and loopback UI.

Optional host provisioning is available through Ansible. It installs Go, Node, Chromium, Poppler, Tesseract, and related tools. Bitcoin Core installation and updates use separate verification-gated targets:

```sh
make ansible-check
make provision
```

See [`docs/provisioning.md`](docs/provisioning.md) for remote inventory examples, GPG signature verification, Core preflight checks, updates, and rollback.

```sh
make setup
make inputs
# Edit $TRACEY_INPUTS/node.env and the JSON files under $TRACEY_INPUTS/data.
# Replace every example identifier with your own public data.
make node-check
make audit
make ui
```

`make inputs` creates a mode-700 private tree at `$HOME/tracey-inputs` (override with `TRACEY_INPUTS=...`) and seeds it from the public examples. Keep that tree outside Git.

To start from scanned ATM receipts or address lists, place the files under `$TRACEY_INPUTS/data/inbox/` and use the intake workflow before `make audit`:

```sh
make intake
make resolve-intake
make review-intake
make promote-intake
make audit
```


Optional printable overview and bank matching:

```sh
# First produce candidate matches, then inspect the candidate/confirmation columns.
make bank-import BANK="$TRACEY_INPUTS/data/export.csv"
# Apply only after independent corroboration or an explicit reviewed confirmation.
make bank-import BANK="$TRACEY_INPUTS/data/export.csv" \
  APPLY=1 CONFIRM="purchase|your-purchase-id"
make overview
```

Merchant/date matching alone does not establish purchase evidence. See [`docs/inputs.md`](docs/inputs.md) for exact confirmation/corroboration semantics and available reporting targets. Reports are visible by default for local review; use `UNREDACTED=0 make report` before sharing identifiers. Sensitive source receipts, bank exports, credentials, and accountant bundles remain sensitive after display redaction.

The service listens on `127.0.0.1` by default. Never expose the annotation server or Bitcoin RPC directly to the public internet. Xpubs, addresses, purchase records, and their combined transaction graph are sensitive financial information. See [`docs/inputs.md`](docs/inputs.md) for complete JSON examples and [`docs/bitcoin-core.md`](docs/bitcoin-core.md) for node configuration.

Outputs are written below `$TRACEY_INPUTS/out`. `make example` runs without a node or private data. `make help` lists all supported commands. For the manual Core 32.1/RPC/input/unmount setup and readiness checklist, see [`HANDOVER.md`](HANDOVER.md).

`make report` and `make export` reuse the cached ancestry for label/note edits. After editing purchase records, `purchase_links`, `$TRACEY_INPUTS/data/trace-policy.json`, or promoted intake evidence, always rerun `make audit` so evidence and policy boundaries are recomputed.

## Evidence limits

Bitcoin establishes exact links between previous outputs and spending inputs, not which individual satoshis move to each output of a mixed-input transaction. This tool therefore distinguishes `documented`, `partially_documented`, and `undocumented` from `complete` and `incomplete` traversal. It never labels a capped or failed trace as conclusive evidence that no purchase documentation exists. See `PLAN.md` and `docs/` for the full methodology.
