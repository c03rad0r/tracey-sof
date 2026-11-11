# Evidence and accounting methodology

## What the tool treats as fact

A raw transaction is decoded locally and its txid is recomputed. An exact edge links a spending transaction input to its previous `txid:vout`. Output values remain integer satoshis. Scripts such as P2PK and `OP_RETURN` are not assigned invented conventional addresses.

The current target list comes from Core's current UTXO set. Each report includes its chain height and best-block hash. A changing tip invalidates the scan rather than mixing snapshots.

## What the user supplies

Purchase records can identify complete purchase transactions, exact received outpoints, and narrower associated addresses. Invoice files are local artifacts whose SHA-256 hashes are checked. Notes, labels, and manual confirmed/rejected purchase links are user assertions, not independently proved blockchain facts.

An explicitly identified purchase transaction is a provenance boundary. An associated-address match stops only that exact branch. If several purchases could match, all are shown for review rather than silently selecting one.

## Mixed inputs

Bitcoin's UTXO model does not tell you which individual sats from several inputs went to each output. This project therefore uses no implicit FIFO, LIFO, pro-rata, change-detection, common-input-ownership, wallet-clustering, or fingerprint allocation.

- `documented`: every ancestry branch reached a confirmed purchase.
- `partially_documented`: at least one purchase contributes and at least one other branch is unknown, unresolved, or a different origin.
- `undocumented`: no confirmed purchase was found.
- `trace_status=complete`: every required step was available and traversed.
- `trace_status=incomplete`: at least one required step failed, was capped, or was structurally invalid.

An incomplete graph can contain a confirmed purchase. It must not be shown as proof that more documentation does not exist. Coinbase is a known origin but not a purchase record.
A purchase-intake candidate is not evidence. OCR or list extraction only proposes an address, txid, or outpoint; a human must assign a source-of-funds role and accept it before promotion into `purchases.json`. Paid-to and operator addresses are excluded even when they appear in the traced graph.
A policy boundary (hop, transaction, large ancestor value, fan-in/fan-out, or block-height cutoff) also produces `review_required=true`. Large-value and batch-pattern rules are not ownership or exchange-attribution proofs. They stop traversal and identify a branch for investigation. Reports list whether each supplied purchase transaction ID, outpoint, and address was observed, so an unmatched invoice can be distinguished from a truncated trace.

## Reports

`report.csv` has one row per current target UTXO, including `review_required` and review reasons. `evidence.csv` lists every supplied purchase txid, outpoint, and address with its matched/unmatched state and boundary node. Fully documented sat totals count only fully documented targets; the tool does not divide a partially documented target between invoices.

`report.html` is a self-contained interactive graph. `report.svg` is static and contains no JavaScript. `report.pdf` is printed locally through Chromium. `accountant.zip` adds a methodology note, user inputs, selected evidence files, and `manifest.sha256` covering all other bundle entries.

No exchange rate or historical fiat valuation is fetched. Fiat fields in purchase records are copied exactly from user input. The software organizes evidence; it does not determine tax treatment or replace professional review.
