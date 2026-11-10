# JSON inputs and routine use

All files are local JSON with `schema_version: 1`. The schemas under `schema/` document the shape; the Go validator additionally enforces network, checksums, duplicate IDs, private-key rejection, artifact hashes, and safe paths.

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

## Commands

```sh
make node-check                  # connectivity and chain capabilities only
make validate                    # local JSON and artifact checks
make discover                    # current UTXO set and target funding tx cache
make trace                       # exact backward ancestry from cached targets
make report                      # analysis and HTML/JSON/CSV/SVG/PDF without rediscovery
make audit                       # all of the above with a stable chain snapshot
make ui                          # labels and local graph at 127.0.0.1:8765
make export                      # accountant.zip and manifest
make example                     # public mainnet fixture example; no Core needed
```

`report` reuses cached trace data and current label/note annotations, so labels can be reviewed without another chain scan. `export` regenerates reports before bundling them, including the latest annotations. **Changing `purchases.json`, `purchase_links`, or `trace-policy.json` requires a fresh `make audit`.** A fresh trace is needed both to stop at newly confirmed evidence and to resume beyond evidence that has been rejected; rejecting a purchase cannot conjure ancestors that the old trace never fetched. `SOF_LISTEN=127.0.0.1:8766 make ui` chooses another loopback port.

Typical overrides are `TRACE_MAX_DEPTH`, `TRACE_MAX_TRANSACTIONS`, `CHROMIUM`, `DATA_DIR`, `OUT_DIR`, `CACHE_DIR`, and `ENV_FILE`. Environment hop/transaction limits apply only when `data/trace-policy.json` is absent. The chain tip is checked before and after discovery/tracing; a reorganized or partially synced node is not allowed to produce a current-UTXO audit silently.
