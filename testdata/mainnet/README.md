# Bitcoin mainnet development fixtures

These JSON files are immutable, development-only snapshots of real Bitcoin mainnet transactions. Each file contains the complete raw transaction as `hex` (including witness data where applicable), its confirmation context, a source URL, and a short description. They are **not runtime chain access**: runtime/production tooling must query the local Bitcoin Core node and must not call mempool.space or any other public API.

Research and retrieval used the public mempool.space transaction explorer and API on 2026-11-10. For each transaction, metadata came from `https://mempool.space/api/tx/<txid>` and raw hex came from `https://mempool.space/api/tx/<txid>/hex`. The `source_url` field in each fixture links to the corresponding explorer page.

## Fixtures

### `0437cd7f8525ceed2324359c2d0ba26006d92d856a9c20fa0241106ee5a597c9.json`

- **Height:** 9
- **Block hash:** `000000008d9dc510f23c2657fc4f67bea30078cc05a90eb89e84cc475c080805`
- **Block time:** `1231473279` (Unix timestamp)
- **Raw size:** 134 bytes
- **Case:** legacy coinbase/P2PK transaction and provenance root. Output 0 pays 50 BTC and is the prevout spent by input 0 of the block 170 transfer below. A provenance traversal reaching this transaction can terminate at its coinbase input rather than looking for another parent.

Source: https://mempool.space/tx/0437cd7f8525ceed2324359c2d0ba26006d92d856a9c20fa0241106ee5a597c9

### `f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16.json`

- **Height:** 170
- **Block hash:** `00000000d1145790a8694403d4063f323d499e655c83426834d4ce2f8dd4a2ee`
- **Block time:** `1231731025` (Unix timestamp)
- **Raw size:** 275 bytes
- **Case:** the historical block 170 transfer, widely documented as the first person-to-person Bitcoin transaction (10 BTC to Hal Finney, with 40 BTC change). It is a one-input, two-output legacy P2PK transaction. Its exact provenance edge is `f4184fc5…1e9e16`, input 0, to `0437cd7f…597c9`, output 0, followed by the included coinbase root.

Source: https://mempool.space/tx/f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16

### `a38d861a0584254429a8e84747c0d203ddada49d6355bbf9de92e4fffc230c47.json`

- **Height:** 600000
- **Block hash:** `00000000000000000007316856900e76b4f7a9139cfbfba89842c8d196cd5f91`
- **Block time:** `1571443461` (Unix timestamp)
- **Raw size:** 353 bytes
- **Case:** representative SegWit and multi-input handling. The transaction has two inputs and one output; according to the retrieved transaction metadata, both spent prevouts are native v0 P2WPKH. The raw transaction contains SegWit marker/flag bytes and witness stacks, so it exercises witness parsing and the distinction between txid and wtxid. This is a real, confirmed transaction, but it is not claimed to be the first SegWit transaction.

Source: https://mempool.space/tx/a38d861a0584254429a8e84747c0d203ddada49d6355bbf9de92e4fffc230c47

This last fixture is intended for decoding and multi-prevout cases, not an expected multi-hop provenance test. Its ancestors are deliberately omitted to keep the fixture set compact.

## Local verification

All three filenames were checked against txids recomputed locally from the saved raw bytes:

- For the two legacy transactions, double-SHA256 the complete raw serialization and reverse the digest bytes for display order.
- For the SegWit transaction, parse the complete transaction, remove the marker/flag and every input's witness stack, double-SHA256 the legacy serialization, and reverse the digest bytes. The saved `hex` itself remains complete and witness-bearing.

The parser also checked that it consumed each raw transaction exactly and that its byte length matched the API-reported size. Independently, `rust-bitcoin` 0.32.5 decoded every saved transaction, re-serialized it byte-for-byte, and reproduced the expected txid; this validation ran offline. No transaction or confirmation data in this directory is synthetic.
