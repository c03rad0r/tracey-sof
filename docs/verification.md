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
- A real policy cutoff against the first-transfer fixture produced an incomplete trace with `review_required=true`, without fetching the parent beyond the cutoff.
- A live address and transaction resolution check runs through the configured Core node; it uses only current-UTXO, raw-transaction, and outpoint lookups.
- Intake tests cover PDF text extraction, image/PDF OCR fallbacks, CSV/TSV/TXT/JSON parsing, missing OCR binaries, malformed staging files, current-UTXO resolution, review decisions, promotion, artifact hashes, and every atomic-write failure stage.
- Provisioning playbooks pass `ansible-playbook --syntax-check`; `scripts/core-check.sh` and `scripts/core-release.sh` pass `sh -n`.
- The Core release helper was exercised with a temporary GPG key and fake release artifacts: missing keys, untrusted fingerprints, and checksum failures are refused, while a trusted signature writes a mode-0600 `.verified` marker.
- Bitcoin Core 32.1 was independently downloaded and GPG-verified against official builder key `152812300785C96444D3334D17565732E08E5E41` (`achow101`). The verified tarball SHA-256 is `61545c17c3120fb7412f896d248b11993af04ec8817aad3649c55b2afb044963`. The staged client reports v32.1.0.
- `make core-check` after the update reports the installed client as Bitcoin Core v32.1.0. The application-local RPC credentials have been generated but not yet activated in the node config.
- The `BLOCKCHAIN` mount is active and `bitcoind` is running from `/opt/bitcoin-core/32.1`. The systemd restart result is successful. The unit is currently disabled and must be enabled if it should start at boot.
- `shellcheck` passes for all provisioning scripts. The privacy scanner rejects an unknown external host and a known transaction-explorer host, and passes the current repository.
- Make environment tests confirm that `BITCOIND_RPC_PASSWORD` reaches `node-check` but not `provision`.

The smoke inputs, RPC credentials, reports, and caches used for this test are excluded from source control. They exercise public blockchain data, not private purchase evidence.

## Coverage interpretation

`web/graph.js` is gated at 100% statement, branch, function, and line coverage with Node's built-in coverage runner. The current complete run has 100.00% statement coverage across all first-party Go packages (3131/3131 statements) after the purchase-intake addition, and the JavaScript graph utilities also pass 100% statement/branch/function/line gates. `make release-check` passed on 2026-11-11 and again on 2026-11-12 after provisioning remediation against the live `dq05` node, including address and transaction resolution. Coverage is necessary but not sufficient: real mainnet and browser integration checks remain mandatory.

## Limitations reviewers should check

- A current-UTXO scan can take minutes. Killing a waiting local client does not necessarily abort `scantxoutset` inside Core; check `bitcoin-cli scantxoutset status` before restarting a scan.
- Purchase associations and notes are user evidence, not names or intentions proved by the blockchain.
- Mixed inputs do not have an objective per-sat allocation. Partially documented targets are not partitioned between invoices.
- Redaction is a display and SVG-export aid only: embedded report JSON still contains sensitive data.
- Offline label edits affect the open page only. Durable edits require the loopback annotation server.
- Media inspection found and fixed an important UI root-ID issue: graph nodes use `out:<txid>:<vout>`, so the browser filter now prefixes selected outpoints identically. Chromium tests require actual rendered SVG-node classes, and the corrected PDF graph was visually inspected.
