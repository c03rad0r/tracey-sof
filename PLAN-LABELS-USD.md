# Plan: readable graph labels, BTC/USD amounts, and printable reports

Status: **implemented**. The concrete audit results (counts, amounts, dates and
providers) are deliberately kept outside this public repository; see the private
notes tree created by `make inputs`.

## Printable overview PDF (current)

Status: **implemented** — one gaps-first, block-height chronology.

- [x] `make overview` / `sof overview` renders `out/overview.html` and
      `out/overview.pdf` through the configured Chromium (`PrintHTML`).
- [x] Evidence gaps (inbound payouts, outbound transfers) come first, before
      the chronology.
- [x] Chronology is grouped by block height, newest first, ending with purchase
      source rows; undated rows come last. Purchase outputs are not duplicated.
- [x] Print sizes are readable (10 pt body, 9 pt tables, 8 pt monospace) and
      content spans multiple A4 pages rather than shrinking.
- [x] `accountant.zip` includes `overview.pdf`/`overview.html` when present.
- [x] Tests cover ordering, empty/outbound-only gaps, height fallback, label
      composition and PDF failure paths; Go coverage 100%.

## Bank statement ingestion (current)

- [x] Bank CSV parser (UTF-8 BOM, `;`, German amounts/dates, decorative quotes).
- [x] `make bank-import BANK=… [APPLY=1 CONFIRM=kind|id]`: matches merchant keywords within ±3
      days, stores the CSV as `data/evidence/bank/<name>` with SHA-256, writes
      `bank-matches.csv` and `statement-requests.csv`. Candidate matches are not
      automatically attached; corroboration or explicit human confirmation is required.
- [x] `statement-requests.csv` placeholders for every purchase and inbound
      payout without bank evidence; confirmed rows carry bank date, amount,
      reference and hash. Included in `accountant.zip`.
- [x] Merchant keywords, provider label and exchange labels are private
      configuration (`MERCHANT`, `PROVIDER`, `SOF_EXCHANGE_LABELS` in
      `$TRACEY_INPUTS/node.env`) and are exported only to evidence-report targets;
      the public repository and unrelated recipes have no personal values.
- [x] Confirmed rows create purchase records with fiat, statement date and bank
      artifact. Existing fiat with a different amount is not overwritten. A
      fresh `make audit` stops the trace at the new boundary.
- [x] Tests: Go coverage 100%, JS 100%, `make verify` green, including exact
      large-amount parsing, quoted/oversized CSV handling, confirmation gating,
      and provider-label environment scoping.

## Automated exchange gap lists (current)

- [x] `out/exchange-payouts-unaccounted.csv` — inbound policy-stopped payouts
      with no purchase evidence.
- [x] `out/exchange-outflows-unaccounted.csv` — provider-labelled outbound
      transfers; change detection uses spent-in-graph or `change` labels.
- [x] `out/exchange-payout-months.csv` and
      `out/exchange-outflow-months.csv`.
- [x] Split terminal lists: `make gaps-inflow`, `make gaps-outflow`,
      `make gaps` (both with section headers and per-direction month counts).
- [x] `make gaps-check` gates inbound only; `make gaps-check-all` gates both.
- [x] Provider labels configurable via `SOF_EXCHANGE_LABELS`; there is no
      built-in provider list.
- [x] CSVs included in `accountant.zip`; docs updated.
- [x] Tests: Go coverage 100%, JS 100%, `make verify` green.

## Unmatched and monthly purchase lists (current)

- [x] `matched_current_ancestry` column on `out/purchases.csv`.
- [x] `out/purchases-unmatched.csv` — purchases with exact on-chain
      identifiers but no current-UTXO ancestry match, plus `match_status`.
- [x] `out/purchase-months.csv` — one row per year/month with counts and totals.
- [x] `out/purchase-years.csv` — one row per year with counts and totals.
- [x] HTML/PDF “Purchase months” and “Unmatched purchases” tables.
- [x] All five purchase CSVs included in `accountant.zip`.
- [x] Tests: Go coverage 100%, JS graph coverage 100%, `make verify` green.

## Purchase statement days (current)

- [x] Optional `statement_date` on purchase records (`model`, schema, validator).
- [x] `out/purchases.csv`: one row per purchase with statement date, UTC/local
      dates, provider, sats/BTC, fiat, txids, outpoints.
- [x] `out/purchase-dates.csv`: one row per statement day with counts, totals,
      providers, purchase IDs and UTC days.
- [x] HTML/PDF section “Purchase days for bank statements”.
- [x] Both CSVs included in `accountant.zip`.
- [x] `SOF_TIMEZONE` selects the statement timezone; UTC is always preserved.
- [x] Tests: Go coverage 100%, JS graph coverage 100%, `make verify` green.

## Block-height layout (complete)

Month bucketing fixed ordering but put every same-month event in one column, so
output→funding-transaction edges became vertical. Replace the month axis with
one column per **distinct block height present in the graph** (empty heights are
skipped).

- [x] Add optional `height` to purchase records (`model`, schema, validator) and
      populate them from the chain.
- [x] Replace the month layout with a height layout in `web/graph.js`
      (`layoutGraphHeight`) and `internal/report/svg.go` (`layoutGraphHeight`,
      `SVGBytesHeight`): columns = sorted distinct heights; header
      `height · date`; leading `unknown height` column.
- [x] UI toggle becomes `Layout: height`; resolver uses node height → purchase
      height → annotation height.
- [x] Render zero-horizontal-distance edges as short vertical connectors without
      labels or arrowheads.
- [x] `make graphs` writes `graph-height-all.svg` and
      `graph-height-<txid12>-<vout>.svg`; remove stale month outputs.
- [x] Tests: JS graph 100% coverage, Go 100%, both layouts deterministic.
- [x] Update `docs/inputs.md` and this checklist.

## Chronological month layout (superseded)

The month layout is replaced by the height layout above. It had ordered columns
correctly but grouped every same-month event into one column, producing vertical
edges.

## Graph readability (complete)

The first deep trace materialised every boundary output, which dominated report
size and PDF render cost.

- [x] Collapse boundary outputs so the graph keeps a readable node count.
- [x] Per-target SVG export and `make graphs`.
- [x] Raise the UI zoom cap from 4× to 20×.
- [x] Document the static exports.
- [ ] (Deferred) wrap tall columns, per-UTXO PDF pages, hide-unlabeled toggle.

## Broker xpub work (complete)

Full scan of a broker watch-only xpub with `wpkh` receive/change branches:

- [x] Full-history scan and purchase/spend classification.
- [x] Install the purchase records into `data/purchases.json` (exact txids,
      outpoints, addresses, dates, amounts; fiat left blank where unknown).
- [x] Add the broker xpub and split multisig descriptors to `data/owned.json`.
- [x] Show block date and purchase provider/amount on transaction and boundary
      nodes; full txid in tooltips and the accessible graph table.
- [x] Make unredacted the default for `make audit`, `make report`, `make ui`
      and `make pdf`, with `UNREDACTED=0` as the explicit opt-out.
- [x] Re-run `make audit` with a depth that can reach the purchases and verify
      `out/evidence.csv` and `out/report.pdf`.

## Earlier work (complete)

- [x] Annotation model/validation: optional `value_sats`, `fiat`, `rate`,
      `time`, `height`, `keypath`, `origin`; new `xpubs` map.
- [x] `scripts/import-bip329.py` and `make labels-import LABELS=…`.
- [x] Imported BIP-329 labels; existing notes are preserved on merge.
- [x] UI hydrates `report.annotations` (notes now render) and graph nodes show
      label, address, outpoint/txid, owner xpub branch, BTC, sats,
      wallet-recorded USD at transaction time, and notes.
- [x] Graph layout enlarged to 300×150 cards; static SVG carries labels/BTC/fiat.
- [x] Unredacted PDF flag introduced (`sof pdf -unredacted`), pending the
      default flip in the current work.
- [x] HTML graph table and metadata show address, BTC, USD and keystore labels.
- [x] Tests green: `go test ./...`, Node graph coverage 100%,
      `make coverage-go` 100.00%, `make verify`.

## Usage

```sh
# merge a Sparrow/BIP-329 export (idempotent, keeps existing notes)
make labels-import LABELS="$TRACEY_INPUTS/data/wallet-labels.jsonl"
make validate

# labels are display-only: no re-audit needed
make report

# reports and PDFs are unredacted by default for review
make audit
make pdf
UNREDACTED=0 make pdf      # explicitly redacted copy for sharing
```

## Follow-ups

- [ ] Boundary table in the PDF: rule, observed count/value, threshold, txid,
      depth, and what was not expanded.
- [ ] Convergence section: shared ancestors across targets, so "does everything
      lead to one transaction?" is answered in the document.
- [ ] `make summary` / `make reevaluate` / `make audit-quick` targets.
- [ ] Per-UTXO focus pages in the PDF.
- [ ] Optional: annotate outpoints from the UI (schema already supports them).
- [ ] Fiat coverage is limited to labels imported from the wallet; ancestors
      outside the wallet history show no fiat line. Do not estimate.

## Constraints

- Core-only runtime: no explorer, no price API, no network beyond the configured node.
- Labels and fiat are user/wallet assertions; they never change provenance status
  or trace boundaries.
- Keep the 100% first-party Go coverage gate and the JavaScript graph coverage gate green.
