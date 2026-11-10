# Verification guide

## Offline automated checks

```sh
make setup
make test
make coverage
make privacy-check
make example
```

`make test` does not query a chain service or use wallet data. It includes raw-mainnet fixture normalization, provenance behavior, fake-Core RPC tests, JSON/evidence tests, report/ZIP checks, and real Chromium report rendering. Browser artifacts are created under the project `.cache` directory because Chromium snap packages may not be able to read arbitrary `/tmp` paths.

`make release-check` requires the offline checks plus `make test-node`. It intentionally fails when a capability, coverage, or privacy requirement is unmet. There is no automatic `git push` target.

## Live read-only node checks

Set the same Core environment as for an audit, then run:

```sh
make node-check
make test-node
```

The fixture verifier fetches each documented confirmed transaction by txid and containing block and compares raw bytes with the committed fixture. It does not ask for a wallet or owned address. The configured user on `dq05` uses a non-default data directory; plain `bitcoin-cli` there is inappropriate because it looks for the cookie in the wrong place. The application was verified through an IPv6-loopback SSH tunnel and a temporary, local, mode-0600 cookie which was removed after testing. Do not expose a remote Core RPC port publicly.

## Live integration result

On 2026-11-10, against the synchronized Bitcoin Core v31.1.0 node on `dq05`:

- Both `txindex` and the basic block-filter index were synchronized.
- All three committed mainnet raw transactions matched byte-for-byte at blocks 9, 170, and 600000.
- The first transfer was also traced backward through its exact prevout to its real block-9 coinbase parent, with a complete non-purchase terminal.
- A real current-UTXO-set scan using a public raw witness-script descriptor found 63 matching current outputs at block 975299.
- All 63 funding transactions were coinbase transactions. Every trace completed and was correctly classified as a non-purchase origin, with no fabricated purchase links or sat allocation.
- The resulting graph contained 189 exact nodes and 126 exact edges.
- HTML, JSON, CSV, SVG, and PDF were generated successfully.
- `accountant.zip` contained methodology, all report formats, the three JSON inputs, and a manifest. Nine manifest hashes independently matched nine payload files (the tenth entry is the manifest itself).
- A real loopback UI test saved a transaction annotation and rejected a cross-origin annotation attempt.

The smoke inputs, RPC credentials, reports, and caches used for this test are excluded from source control. They exercise public blockchain data, not private purchase evidence.

## Coverage interpretation

`web/graph.js` is gated at 100% statement, branch, function, and line coverage with Node's built-in coverage runner. The Go `internal/provenance` analysis package independently reached 100% statement coverage and passed its race-enabled tests. Repository-wide Go coverage and its remaining work are measured by `make coverage`; see `PLAN.md` for current checklist status. Do not equate statement coverage alone with correctness: real mainnet and browser integration checks remain mandatory.

## Limitations reviewers should check

- A current-UTXO scan can take minutes. Killing a waiting local client does not necessarily abort `scantxoutset` inside Core; check `bitcoin-cli scantxoutset status` before restarting a scan.
- Purchase associations and notes are user evidence, not names or intentions proved by the blockchain.
- Mixed inputs do not have an objective per-sat allocation. Partially documented targets are not partitioned between invoices.
- Redaction is a display and SVG-export aid only: embedded report JSON still contains sensitive data.
- Offline label edits affect the open page only. Durable edits require the loopback annotation server.
- Media inspection found and fixed an important UI root-ID issue: graph nodes use `out:<txid>:<vout>`, so the browser filter now prefixes selected outpoints identically. Chromium tests require actual rendered SVG-node classes, and the corrected PDF graph was visually inspected.
