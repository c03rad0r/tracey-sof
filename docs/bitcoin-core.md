# Bitcoin Core configuration

## Privacy and trust

The application sends only read-only RPC requests to the node configured in `node.env`. Do not enter private keys, seed phrases, xprvs, WIF keys, or a Core wallet passphrase. Xpubs, addresses, purchase associations, and the combined graph are nevertheless sensitive: use a node you control and keep the data directory private.

The normal RPC endpoint is `http://127.0.0.1:8332`. For a node on another machine, use a protected private connection such as an SSH tunnel; Core RPC is not hardened for direct exposure to the public internet. Never expose the annotation UI beyond its numeric loopback listen address.

## Recommended setup

- Bitcoin Core 29 or newer.
- A synchronized mainnet chain.
- `txindex=1` enabled and fully indexed for convenient arbitrary transaction lookup.
- Keep all blocks and undo data needed for the ancestry you want to examine.

A pruned node is not automatically disqualified, but unavailable historical data produces `trace_status=incomplete`. It never produces a confident conclusion that documentation is missing. `getindexinfo` reports whether `txindex` is synchronized.

`blockfilterindex=1` and `getdescriptoractivity` are not needed for current-UTXO discovery. They become useful if historical spent-address discovery is added later.

The tool uses only these read-only capabilities:

- `getblockchaininfo`, `getnetworkinfo`, `getbestblockhash`, `getindexinfo`
- `getblockhash`, `getblockheader`, `getrawtransaction`, `gettxout`
- `getdescriptorinfo`, `deriveaddresses`, `scantxoutset`

It does not import descriptors into a wallet, call signing/broadcast methods, rescan a wallet, or alter the node's configuration. `scantxoutset start` examines the current UTXO set and can be resource-intensive; it is run only when you explicitly start discovery or an audit.

## Configuration

Copy `examples/node.env.example` to `node.env`. Cookie authentication is preferred. The configured cookie path is read at request time and cookie contents are never included in reports.

```make
BITCOIND_RPC_URL=http://127.0.0.1:8332
BITCOIND_COOKIE_FILE=/path/to/your/.bitcoin/.cookie
```

Static `BITCOIND_RPC_USER` and `BITCOIND_RPC_PASSWORD` environment variables are also accepted, but must be supplied together and must not be placed in tracked files. URLs with embedded credentials are rejected.

`make node-check` prints capabilities and warnings without scanning owned scripts. `make audit` refuses an unsynchronized chain and requires a stable chain tip across discovery and tracing.

## No txindex

Discovery retrieves each target funding transaction using its containing block, where available. As Core supplies previous-output heights, the source follows those heights to active-chain block hashes. If an ancestor is not cached and cannot be resolved without `txindex`, that branch is marked incomplete with an explanation. Enabling `txindex=1` and allowing the node to index is the simplest remedy.
