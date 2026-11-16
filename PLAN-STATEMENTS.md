# Plan: PDF statement evidence in the overview

Status: **implemented**; candidate confirmation on the private tree pending

## Goal

Extract the relevant bank-statement context from local PDFs, attach confirmed
PDF artifacts to purchases, and show statement coverage and bank-row context in
the printable overview.

## Findings

- The statement PDFs are text-based; `pdftotext -layout` works.
- The 2023–2024 PDFs contain the expected purchase debits.
- The May 2017 checking statement contains no Bitcoin/exchange booking.
- The May 2017 closed-account request is outstanding; the transfer reference
  suggests a possible 1 June 2017 transfer to that account.
- The current checkout is on clean history `8759929`.
- All PDFs stay under `$TRACEY_INPUTS/data/bank-statements/` and are never committed.

## Design

1. Add a statement parser that reuses the existing PDF text/OCR path.
2. Promote parsed PDFs to `bank.Transaction` rows with source file, page,
   SHA-256, statement month, raw booking text, and reference.
3. Add `sof statement-import` / `make statement-import`:
   - parse a directory of PDFs;
   - store each PDF under `$TRACEY_INPUTS/data/evidence/bank/`;
   - match purchases as candidates;
   - require explicit `CONFIRM` before attaching evidence;
   - preserve existing CSV matches.
4. Extend `bank.Cache`/`MatchRecord` with statement metadata and per-record
   artifacts.
5. Extend the overview with:
   - a `Bank statement coverage` table;
   - a `Statement evidence` appendix;
   - purchase status flags (`statement confirmed`, `statement candidate`,
     `statement ambiguous`).
6. Update docs and the private postal request templates.

## Checklist

- [x] Add `internal/statement` parser and tests.
- [x] Export/reuse the existing PDF text and OCR path.
- [x] Extend bank transaction/match/cache metadata.
- [x] Extend `bank.Apply` for per-record PDF artifacts.
- [x] Add `statement-import` CLI and Make target.
- [x] Default `STATEMENT_DIR` to `$TRACEY_INPUTS/data/bank-statements` and create
      it (and `data/inbox`) from `make inputs` with mode 700.
- [x] Preserve CSV matches when PDFs are imported.
- [x] Add statement coverage and evidence sections to the overview.
- [x] Add purchase statement status flags.
- [x] Update `docs/inputs.md`, `PLAN-OVERVIEW.md`, and `HANDOVER.md`.
- [x] Update the private postal request notes.
- [x] Parse the real statement PDFs in candidate mode.
- [ ] Confirm the expected sample purchases. Current PDF run: 37 candidates, 5 ambiguous, 3 CSV-only 2024-11 purchases, 3 inbound payouts.
- [x] Regenerate `overview.pdf` and inspect coverage/appendix.
- [x] Run `make verify` with Node 22 and `make leak-check`. (`make coverage-go` is 100%.)
- [x] Fresh-clone smoke test (`make validate` and `make verify` passed).
