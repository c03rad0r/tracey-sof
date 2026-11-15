# JSON inputs and routine use

All files are local JSON with `schema_version: 1`. The schemas under `schema/` document the shape; the Go validator additionally enforces network, checksums, duplicate IDs, private-key rejection, artifact hashes, and safe paths.

The public repository contains no financial inputs. The Makefile reads a private tree at `$HOME/tracey-inputs` by default; run `make inputs` once to create it, or set `TRACEY_INPUTS` to another private directory. Paths written below as `data/`, `cache/` and `out/` mean `$TRACEY_INPUTS/data`, `$TRACEY_INPUTS/cache` and `$TRACEY_INPUTS/out`. Never commit that tree, and do not put real values in the example files under `examples/`.

## `data/owned.json`

List any combination of current addresses, explicit UTXOs, descriptors, and xpub configurations. Duplicate or overlapping declarations are merged by exact outpoint while retaining all associated owner IDs.

```json
{
  "schema_version": 1,
  "network": "mainnet",
  "addresses": [
    {"address": "bc1q-your-owned-address", "label": "cold wallet receive 001"}
  ],
  "utxos": [
    {"txid": "64-lowercase-hex-characters", "vout": 0, "label": "savings"}
  ],
  "descriptors": [
    {
      "id": "cold-wallet-receive",
      "descriptor": "wpkh(xpub-your-account-key/0/*)#checksum-from-core",
      "range": [0, 499],
      "label": "cold wallet receiving addresses"
    }
  ],
  "xpubs": []
}
```

The address and txid placeholders above must be replaced; they are not valid identifiers.

Prefer watch-only output descriptors exported from your wallet because they encode the script type and derivation path unambiguously. Core computes/validates descriptor checksums. A raw descriptor such as `raw(0020...)` is also accepted. Fixed descriptors and ranged descriptors use different local ownership mapping paths, both of which are exercised by fake-Core tests.

For a bare account xpub, specify:

```json
{
  "id": "atm-wallet",
  "xpub": "xpub...your-real-public-key...",
  "script_type": "wpkh",
  "branches": [0, 1],
  "range": [0, 999],
  "label": "wallet receiving and internal/change branches"
}
```

- `pkh` corresponds to the usual BIP44 legacy outputs.
- `sh(wpkh)` corresponds to BIP49 wrapped SegWit.
- `wpkh` corresponds to BIP84 native SegWit.
- `tr` corresponds to BIP86 Taproot.
- Branch `0` is external/receive; branch `1` is internal/change.
- Range bounds are zero-based and inclusive, maximum 10,000 indices per descriptor/branch.
- A bare xpub does not by itself prove which script convention your wallet uses; it is intentionally not guessed.
- Seeds, xprvs/private descriptors and WIF keys are rejected and are never needed.

An address with no current UTXO is not treated as an undocumented current holding. An explicitly supplied outpoint that is spent or absent results in an error requiring correction or rescanning.

## `data/purchases.json`

Each record is evidence about one purchase:

```json
{
  "schema_version": 1,
  "purchases": [
    {
      "id": "atm-2024-001",
      "date": "2024-01-31T14:05:00Z",
      "provider": "ATM location or operator label",
      "txids": ["64-lowercase-hex-characters"],
      "addresses": ["bc1q-associated-source-or-receipt-address"],
      "outpoints": [{"txid": "64-lowercase-hex-characters", "vout": 0}],
      "amount_sats": 1500000,
      "fiat": {"amount": "100.00", "currency": "EUR"},
      "artifacts": [
        {
          "path": "evidence/atm-2024-001-receipt.pdf",
          "sha256": "64-lowercase-hex-characters",
          "description": "Paper receipt scanned at purchase time"
        }
      ],
      "notes": "Optional user assertion and context"
    }
  ]
}
```

Replace all placeholder IDs and addresses. `amount_sats` is checked as a positive integer but is not proof of a match. Fiat is copied from input; no exchange-rate service is used.

The most useful invoice-to-chain links are an exact purchase **txid** or received **outpoint**. An explicitly identified purchase txid stops backward tracing at that complete purchase transaction. An associated address only stops the exact matching branch; it should not be interpreted as proving all other inputs or outputs of a transaction.

Artifact paths are relative to `data/`, cannot escape that directory, and are checked byte-for-byte against SHA-256. Obtain an artifact hash locally, for example with `sha256sum data/evidence/receipt.pdf`. Artifact content is never uploaded or executed by the application.

## Purchase intake

Scanned receipts, address lists, and other purchase sources are staged in `data/inbox/` and extracted into `data/purchase-intake.json`. The intake file is **not** consumed by `make audit`; only explicitly promoted records enter `data/purchases.json`.

```sh
make intake INBOX=./data/inbox
make resolve-intake
make review-intake
make promote-intake
make audit
```

### Supported sources

- PDF receipts with an embedded text layer through `pdftotext`.
- Image-only PDFs or image files through `pdftoppm` and `tesseract`.
- CSV/TSV files with columns such as `address`, `txid`, `outpoint`, and `role`.
- TXT and JSON files through token extraction.

OCR is optional. If the local binaries are absent, structured lists still work and the command reports which source needs OCR. Source files must remain below `data/`; each is hashed with SHA-256 and becomes the promoted purchase artifact.

### Candidate roles

Every extracted candidate starts as `role: "unknown"`. Promotion requires a human to assign one of:

| Role | Meaning | Promotable |
|---|---|---|
| `received_at` | Address that received BTC on your behalf | Yes |
| `purchase_txid` | Transaction that paid you | Yes |
| `purchase_outpoint` | Exact output that paid you | Yes |
| `paid_to` | Address you paid the ATM or counterparty | No |
| `operator_address` | ATM/exchange operational address | No |
| `unknown` | Not yet classified | No |

A paid-to or operator address is never source-of-funds evidence. Do not use an ATM deposit address as a purchase boundary.

### Review commands

```sh
sof review-intake -data-dir ./data -list
sof review-intake -data-dir ./data --accept <candidate-id> --role received_at
sof review-intake -data-dir ./data --reject <candidate-id> --note "operator deposit address"
sof review-intake -data-dir ./data --accept <candidate-id> --role purchase_txid \
  --purchase-id atm-2024-001 --date 2024-01-31T14:05:00Z --provider "ATM operator"
```

`make review-intake` lists candidates. Use the `sof` command directly for accept/reject and metadata edits. `make promote-intake` merges accepted candidates and refuses pending, rejected, unknown-role, paid-to, and operator-address candidates. It also refuses duplicate purchase IDs unless `--replace` is supplied.

### Resolution

`make resolve-intake` asks the node about **current** UTXOs only:

- Addresses are batch-checked with `scantxoutset`.
- Txids are confirmed with `getrawtransaction` when available.
- Outpoints are checked with `gettxout`.
- If `cache/trace.json` exists, candidates observed in the cached ancestry are marked `in_trace`.

No historical spent-address scan is performed. An address with no current UTXO is not invalid and is not proof of anything. Historical `scanblocks`/`getdescriptoractivity` lookup remains deferred.

### File shape

The staging file records source path, SHA-256, media type, extraction methods, candidates, optional resolution data, review decisions, and editable purchase metadata. The schema is `schema/purchase-intake.schema.json`. Example shape:

```json
{
  "schema_version": 1,
  "generated_at": "2026-11-11T00:00:00Z",
  "sources": [
    {
      "id": "src-example00001",
      "path": "inbox/example-receipt.txt",
      "sha256": "64-lowercase-hex-characters",
      "media_type": "text/plain",
      "extraction": ["text"],
      "candidates": [
        {
          "id": "cand-example00001",
          "kind": "address",
          "value": "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
          "role": "unknown",
          "confidence": "high",
          "line": 1,
          "review": {"decision": "pending"}
        }
      ],
      "purchase": {"id": "example-receipt-000000"}
    }
  ]
}
```

OCR never writes purchase dates or amounts automatically. Edit those fields during review; a misread digit must not become accounting evidence.

## `data/trace-policy.json`

This optional file limits expensive or unproductive ancestry traversal:

```json
{
  "schema_version": 1,
  "max_hops": 8,
  "max_transactions": 250,
  "stop_if_prevout_value_sats_gte": 500000000,
  "stop_if_input_count_gte": 50,
  "stop_if_output_count_gte": 50,
  "stop_before_block_height": 600000
}
```

All stop thresholds are optional and zero/omitted means disabled. `max_hops` and `max_transactions` default to 25 and 10,000 when omitted.

- `stop_if_prevout_value_sats_gte` applies to an **ancestor output that the tracer is about to follow**, not to the current UTXO's total value.
- `stop_if_input_count_gte` and `stop_if_output_count_gte` apply to a transaction before its inputs are expanded. They are useful for exchange consolidation and batched-payout patterns.
- `stop_before_block_height` stops before expanding a transaction mined below the configured height.
- Exact purchase txids/outpoints/addresses take precedence over policy stops. Coinbase is also identified before policy rules.
- A policy stop sets `review_required=true` and `trace_status=incomplete`. It **never** proves that the output came from an exchange and never changes it to `documented`.
- Every boundary records the rule, observed value/count, threshold, and participating node/transaction.
- If the file is present it is authoritative over legacy `TRACE_MAX_DEPTH` and `TRACE_MAX_TRANSACTIONS` environment variables.

An exchange withdrawal transaction from a statement is stronger evidence than a large-value threshold. Add that exact txid or outpoint to `purchases.json` and attach the statement as an artifact.

Changing this file does not alter an existing trace. Rerun `make audit`; `make report` will reject a cached trace whose policy differs from the current policy.

## `data/annotations.json`

The loopback UI writes transaction and address labels to this file with an atomic replacement. It is fine to leave the maps empty initially:

```json
{
  "schema_version": 1,
  "transactions": {},
  "addresses": {},
  "outpoints": {},
  "purchase_links": []
}
```

Manual purchase links can be added with an exact `target` (txid, `txid:vout`, or address), a valid `purchase_id` from `purchases.json`, and a `decision` of `confirmed` or `rejected`:

```json
{
  "target": "exact-txid-or-outpoint-or-address",
  "purchase_id": "atm-2024-001",
  "decision": "confirmed",
  "note": "Checked against receipt reference 12345"
}
```

A rejection excludes that candidate only at that target. It does not erase purchase records or reinterpret unrelated trace branches. Labels and notes are user assertions; a label called “ATM” by itself does not create a provenance boundary.

## Wallet labels and fiat amounts (BIP-329)

Sparrow and other BIP-329 wallets export labels as JSON Lines. Import them instead of editing `annotations.json` by hand:

```sh
make labels-import LABELS=~/Downloads/wallet-labels.jsonl
make validate
make report
```

The importer merges without discarding existing notes and maps `tx`, `addr`,
`output` and `input` records to the matching annotation map; `input` records only
fill outpoints that no received-output label already describes. `xpub` records go
to the `xpubs` map and appear as keystore labels in the report metadata.

Optional wallet metadata is copied verbatim for display:

```json
{
  "label": "example receive",
  "value_sats": 100000,
  "fiat": {"USD": "0.50"},
  "rate": {"USD": "63504.42"},
  "time": "2024-01-15T12:00:00Z",
  "height": 123456,
  "keypath": "/0/3",
  "origin": "wpkh([a1b2c3d4/84h/0h/0h])"
}
```

`fiat` is the wallet's recorded value at transaction time (`fmv` in BIP-329) and
`rate` the wallet's recorded exchange rate. They are display-only, are never
fetched from a price service, and never affect provenance status or trace
boundaries. Ancestors with no imported label show no fiat line; the tool does not
estimate one.

Reports show sats, BTC and wallet-recorded fiat in the graph, the graph table,
the static SVG and the PDF. Reports are generated with identifiers and amounts
visible by default for review; set `UNREDACTED=0` to produce a redacted copy:

```sh
make audit                  # unredacted (default)
make report                 # unredacted (default)
UNREDACTED=0 make audit     # [redacted] placeholders for sharing
```

The standalone HTML embeds the full report data either way; redaction only
changes the display.

## Commands

```sh
make node-check                  # connectivity and chain capabilities only
make validate                    # local JSON and artifact checks
make labels-import LABELS=...     # merge a Sparrow/BIP-329 JSONL label export
make intake                      # extract purchase candidates from data/inbox
make resolve-intake             # resolve candidates against current UTXOs
make review-intake              # list staged candidates
make promote-intake             # merge accepted candidates into purchases.json
make discover                    # current UTXO set and target funding tx cache
make trace                       # exact backward ancestry from cached targets
make report                      # analysis and HTML/JSON/CSV/SVG/PDF without rediscovery
make overview                    # one printable overview: gaps first, blocks newest to oldest
make pdf                         # printable PDF with identifiers and amounts visible
make audit                       # all of the above with a stable chain snapshot
make ui                          # labels and local graph at 127.0.0.1:8765
make graphs                      # graph-all.svg + one focused SVG per UTXO
make export                      # accountant.zip and manifest
make example                     # public mainnet fixture example; no Core needed
```

`report` reuses cached trace data and current label/note annotations, so labels can be reviewed without another chain scan. `export` regenerates reports before bundling them, including the latest annotations. **Changing `purchases.json`, `purchase_links`, or `trace-policy.json` requires a fresh `make audit`.** A fresh trace is needed both to stop at newly confirmed evidence and to resume beyond evidence that has been rejected; rejecting a purchase cannot conjure ancestors that the old trace never fetched. `SOF_LISTEN=127.0.0.1:8766 make ui` chooses another loopback port.

### Purchase statement days

Every `make report` also writes these bank-statement helpers:

- `out/purchases.csv` — one row per purchase: statement date, UTC timestamp,
  local date, matched-to-current-ancestry flag, provider, sats/BTC, fiat (when
  supplied), txids, outpoints, notes;
- `out/purchases-unmatched.csv` — only purchases absent from the traced
  current-UTXO ancestry, with an explicit `match_status` column;
- `out/purchase-dates.csv` — one row per statement day with purchase count,
  total sats/BTC, providers, purchase IDs and UTC days;
- `out/purchase-months.csv` — one row per year/month with counts and totals;
- `out/purchase-years.csv` — one row per year with counts and totals.

The **statement date** is `statement_date` when a purchase record supplies it,
otherwise the purchase timestamp in the host local timezone. Set
`SOF_TIMEZONE=Europe/Berlin` (or another IANA zone) to make that explicit; the
UTC timestamp is always preserved in the CSV. Block time is the on-chain payout
time and can differ from the bank debit date, so check ±1 day or download the
monthly statement. The loopback HTML report and the PDF include the same list as
a “Purchase days for bank statements” table plus “Purchase months” and
“Unmatched purchases” tables, and the CSVs are included in `accountant.zip`.
“Unmatched” means the purchase has exact on-chain identifiers but is not part of
the current UTXOs' ancestry; it remains a valid purchase record.

```json
{"id": "purchase-2024-01-15", "date": "2024-01-15T12:00:00Z",
 "statement_date": "2024-01-16", "provider": "Example Exchange", "...": "..."}
```

`statement_date` is optional (`YYYY-MM-DD`) and is validated.

### Bank statement import and proof placeholders

`make report` always writes `out/statement-requests.csv`: one row per purchase
record and per unaccounted inbound exchange payout. Rows without bank evidence
are placeholders with `status=request_physical_statement` and an
`action=request physical statement for YYYY-MM`, so the export tells you exactly
which physical statements to request.

Import a bank CSV export to match bank debits against on-chain events. The file is parsed locally with exact integer-cent arithmetic, at most 64 MiB; malformed or overlarge inputs are rejected instead of being silently ignored.

```sh
# First, review candidate matches in out/bank-matches.csv:
make bank-import BANK="$TRACEY_INPUTS/data/export.csv"

# Then explicitly confirm one reviewed kind|ID key, copied from that file:
make bank-import BANK="$TRACEY_INPUTS/data/export.csv" \
  APPLY=1 CONFIRM="purchase|purchase-2024-01-15"
```

- The CSV is copied below `data/evidence/bank/` with mode 0600 and its SHA-256;
  it becomes an artifact only on explicitly confirmed purchase records.
- Matching uses merchant keywords supplied with `MERCHANT=...` (required; put
  machine-specific values in `$TRACEY_INPUTS/node.env`) and a ±3-day window
  (`-window-days`). There is no personal default.
- Merchant and date produce a **candidate**, not purchase proof. Candidates have
  `status=candidate_needs_confirmation`; apply only after corroboration or an
  explicit human confirmation. `APPLY=1` without a valid `CONFIRM` key is rejected.
- If a purchase already has an expected fiat amount/currency, a different bank
  amount/currency is not silently substituted. Unknown amounts can be filled only
  after explicit confirmation. Inbound payouts become purchase records with fiat
  and `statement_date`; rerun `make audit` so the trace stops at the new purchase
  boundaries.
- `out/bank-matches.csv` records candidate status (`matched`, `ambiguous`,
  `missing`) and the separate `confirmed` decision. Its SHA column is populated
  only for confirmed matches. `out/statement-requests.csv` uses
  `matched_bank_evidence` only after confirmation or another bank artifact.
- `make statement-requests` regenerates the placeholder list from the cache.

The bank file contains IBAN/account data. It stays under the private data
directory outside the repository, but `accountant.zip` includes it as evidence
when matched; redact it before sharing outside your accountant. Bank history
older than the online export window cannot be matched and stays as a placeholder
for a physical statement request.

### Exchange evidence gaps

`make report` also writes the exchange-side gap lists derived from the trace:

- `out/exchange-payouts-unaccounted.csv` — inbound exchange payouts stopped by a
  policy rule (for example `policy_output_count`) with no matched purchase
  evidence. Columns: direction, date, height, txid, outpoint, amount, counterparty,
  label, boundary reason, evidence status, suggested record, affected UTXOs.
- `out/exchange-outflows-unaccounted.csv` — provider-labelled transactions that
  send coins to a counterparty and return change. They need sale/deposit records,
  not purchase evidence.
- `out/exchange-payout-months.csv` and `out/exchange-outflow-months.csv` — the
  same rows grouped by year/month.

```sh
make gaps          # print both lists with separate section headers
make gaps-inflow   # inbound payouts only (+ month count)
make gaps-outflow  # outbound transfers only (+ month count)
make gaps-check    # exit non-zero while unaccounted inbound payouts remain
make gaps-check-all # also fail on outbound transfers without sale evidence
```

Each list is independent: the CSV files and month summaries are already
separate, and the terminal output now prints `== INFLOW ==` and `== OUTFLOW ==`
sections with their own counts and month totals. `-inflow` / `-outflow` select a
single section; `-strict` gates on inbound data and `-strict -include-outflows`
gates on both.

**The source-of-funds documentation job is complete when `make gaps-check`
passes** (the inbound payout list is empty). A payout disappears from the list
when its exact txid/outpoint is added to `purchases.json` and the trace stops at
that purchase boundary. Outflows are advisory: they need sale records, not
purchases, and do not gate completeness.

Provider label matching is configurable with `SOF_EXCHANGE_LABELS` (for example
`SOF_EXCHANGE_LABELS="exchange-a,exchange-b"`). There is no built-in provider
list; put machine-specific labels in `$TRACEY_INPUTS/node.env`. Labels are user
assertions; verify them against exchange statements.

### Printable overview

`make overview` writes a single print-oriented document, `out/overview.html` and
`out/overview.pdf`:

1. **Evidence gaps first** — inbound exchange payouts without purchase evidence
   and outbound transfers without sale evidence, each with height, amount,
   counterparty and the record to request.
2. **Chronology, newest block first** — one section per block height present in
   the trace, walking down through transactions and outputs to the purchase
   rows that are the sources of the sats. Undated rows come last.
3. **Current UTXOs** with documentation and trace status.

The chronology's **Status** column marks `unspent` for a current UTXO output,
`funds unspent output` for a transaction that creates a current UTXO, and
`inbound gap` / `outbound gap` for the same transactions listed in the evidence
gap sections. A row can carry more than one marker, for example
`funds unspent output · outbound gap`.

Purchases appear once as source rows; an output that is itself a purchase is not
duplicated. Boundary outputs are collapsed as in the graph exports, so the
document stays readable. Type sizes are print-normal (10 pt body, 9 pt tables);
content flows across as many A4 pages as needed instead of shrinking. The
overview is rendered with identifiers and amounts visible, matching the default
unredacted review output. `out/graph-height-all.svg` remains the vector source
graph referenced at the end of the document.

`make export` includes `overview.pdf` and `overview.html` in `accountant.zip`
whenever they exist; it does not require a browser when they are absent.

### Static graph exports

`make graphs` writes two graph families under `out/`:

- hop layout (default): `graph-all.svg` and `graph-<txid12>-<vout>.svg`
- block-height layout: `graph-height-all.svg` and
  `graph-height-<txid12>-<vout>.svg`

SVGs are vector files, so zoom is unlimited in a browser or Inkscape. Per-UTXO
files only contain that target's ancestry. In the block-height layout the x-axis
is one column per **distinct block height present in the graph**; heights with no
traced transaction are skipped, so the axis is compact but strictly chronological
(oldest left, newest right). Headers show `height · date`. Nodes without a known
height (graph, purchase, then BIP-329 annotation) are grouped in a leading
`unknown height` column. Same-height edges are drawn as short vertical connectors
without labels. The loopback UI has a `Layout: hops` / `Layout: height` toggle.

Outputs belonging to a transaction whose ancestry was deliberately not expanded
(policy stop, purchase boundary, coinbase) are collapsed so a 300–600 output
exchange batch does not dominate the layout. Each transaction card shows its
txid, block date and rate; each output card shows the address, outpoint,
BTC/sats, wallet-recorded fiat where available, and the owner xpub branch.
Purchase boundaries are labelled with the provider.

Typical overrides are `TRACE_MAX_DEPTH`, `TRACE_MAX_TRANSACTIONS`, `CHROMIUM`, `DATA_DIR`, `OUT_DIR`, `CACHE_DIR`, and `ENV_FILE`. Environment hop/transaction limits apply only when `data/trace-policy.json` is absent. The chain tip is checked before and after discovery/tracing; a reorganized or partially synced node is not allowed to produce a current-UTXO audit silently.
