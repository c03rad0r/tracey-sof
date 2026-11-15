# Overview PDF — implementation plan

Status: **complete** (2026-11-15)

## Goal

One printable document, `$TRACEY_INPUTS/out/overview.pdf`, that can be read without zooming:

1. **Evidence gaps first** — inbound payouts without purchase evidence and
   outbound transfers without sale evidence, with the exact txid/outpoint and
   the action needed.
2. **Chronology, newest block first** — one section per block height present in
   the trace, walking down to the purchase/source rows at the bottom.
3. Normal, readable type sizes; the document grows across pages instead of
   shrinking to fit one screen.

`$TRACEY_INPUTS/out/overview.html` is the printable source,
`$TRACEY_INPUTS/out/overview.pdf` the artifact.
`report.pdf` and the detailed HTML report remain unchanged.

## Design decisions

- **Block-height grouping, not months.** Only heights that occur in the trace
  get a section; no empty columns.
- **Purchases are source rows.** A graph output that is itself a purchase is
  represented once, by the purchase row (avoids duplicate rows).
- **Collapsed boundary outputs.** The graph already collapses boundary outputs,
  so the overview inherits the reduced, readable graph structure.
- **Undated rows last.** Events without a height (e.g. purchase records without
  a known block) appear after the dated chronology.
- **Unredacted by default** (`sof overview` renders identifiers and amounts);
  use the existing redaction switch only when publishing.
- **Bundle inclusion is optional.** `accountant.zip` picks up `overview.pdf` and
  `overview.html` when they exist; `make export` does not require Chromium.

## Checklist

### A. Generator and legibility

- [x] `internal/report/overview.go` builds the HTML (gaps, chronology, UTXOs).
- [x] `sof overview` + `make overview` wired to `PrintHTML`.
- [x] Drop the impossible error return from `overviewHTML` (dead branch).
- [x] Raise print sizes: body 10pt, tables 9pt, code 8pt, wider padding.
- [x] Keep `break-inside: avoid` on rows and `break-after: avoid` on headings.
- [x] Add a chronology **Status** column: `unspent` for current UTXOs,
      `funds unspent output` for their funding transactions, and
      `inbound gap`/`outbound gap` for the transactions listed in the gaps
      sections. Combined markers are joined with `·`.
- [x] Add a **Bank statement coverage** table and a **Statement evidence**
      appendix. PDF statements are referenced by month, file/page, SHA-256 and
      match status; the PDFs themselves are not embedded.
- [x] Mark purchase rows with `statement confirmed`, `statement candidate` or
      `statement ambiguous` when a PDF match exists.

### B. Tests and the 100% coverage gate

- [x] Overview output node with `PurchaseIDs` is skipped (no duplicate row).
- [x] Output node with height 0 inherits the purchase height.
- [x] Same-height, same-kind tie-break orders by identifier.
- [x] No inbound and no outbound gaps → "No exchange gaps remain."
- [x] Outbound-only report renders the outbound table and labels.
- [x] Event without txid/outpoint falls back to the event ID.
- [x] Provider + label are concatenated (`Provider · note`).
- [x] `WriteOverview` propagates a write failure (outDir is a file).
- [x] CLI success path with a fake browser writes `overview.pdf`.
- [x] `run(["overview", "-bad-flag"])` returns a flag error.
- [x] Bundle includes `overview.pdf`/`overview.html` when present, ignores them
      when absent, and fails on an unreadable path (e.g. a directory).
- [x] `make coverage-go` reports **100.00%**.

### C. Verification

- [x] `make verify` green (lint, tests, coverage, privacy, build, example).
- [x] `make overview` with the configured Chromium produces both files.
- [x] Inspect the PDF: gaps precede chronology; heights descend
      `975,8xx → … → sources`; purchase rows come last.
- [x] Record page count and confirm no tiny-font/zoom workaround is needed.

### D. Documentation

- [x] `docs/inputs.md`: overview target, ordering, output names, bundle note.
- [x] `PLAN-LABELS-USD.md`: overview section with current status.
- [x] This file: tick off the checklist and update Status.

## Result

- `make overview` writes `out/overview.html` and `out/overview.pdf` with
  print-normal sizes (10 pt body / 9 pt tables) across as many A4 pages as
  needed.
- The gaps section precedes the chronology; the chronology descends from the
  newest block height to the purchase/source rows at the bottom.
- Matched purchase rows show their bank fiat and statement reference; other
  purchases retain their wallet notes.
- `accountant.zip` contains `overview.html` and `overview.pdf` when present.
- First-party coverage gates remain green.

## Verification commands

```sh
make coverage-go
make verify
make overview            # uses CHROMIUM from $TRACEY_INPUTS/node.env
pdfinfo "$TRACEY_INPUTS/out/overview.pdf"
pdftotext "$TRACEY_INPUTS/out/overview.pdf" - | head -60
```

## Files touched

| File | Change |
|---|---|
| `internal/report/overview.go` | chronology + gaps HTML, print CSS, no dead error |
| `internal/report/overview_test.go` | ordering and branch coverage |
| `internal/report/bundle.go` | optional `overview.*` entries |
| `internal/report/coverage_bundle_test.go` | bundle inclusion/error cases |
| `internal/report/pdf.go` | shared `PrintHTML` (already added) |
| `cmd/sof/app.go` | `overview` command |
| `cmd/sof/main.go` | `overview` flags |
| `cmd/sof/overview_test.go` | CLI errors + fake-browser success |
| `Makefile` | `overview` target |
| `docs/inputs.md`, `PLAN-LABELS-USD.md` | user-facing docs |
