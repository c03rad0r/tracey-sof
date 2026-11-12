# Handover: run tracey-sof with your Bitcoin Core node

This document is the manual follow-up for the current Core 32.1 installation. It assumes a Linux host with `sudo`, an existing Bitcoin Core service, and a mounted blockchain datadir. No AI is required.

**Current verified state:** Core 32.1 is installed under `/opt/bitcoin-core/32.1` and its archive passed GPG verification with builder key `152812300785C96444D3334D17565732E08E5E41` (`achow101`). The running service uses the `/run/media/admin/BLOCKCHAIN/bitcoin-full` datadir on `/dev/sda1`.

**Remaining work:** enable the unit for reboot, activate the dedicated application RPC credentials, fill in your inputs, validate the node and chain state, then run reports.

Do not start with the full audit. Complete steps 1–5 in order.

## 1. Prepare a fresh checkout and host tools

```sh
git clone https://github.com/c03rad0r/tracey-sof.git
cd ~/tracey-sof
```

On the existing host, `node.env` and `cache/local-rpcauth.conf` were already generated locally. **Those files are intentionally not committed and will be absent from a fresh clone.** Recreate them in step 3; do not overwrite existing credential files unnecessarily.

Install dependencies if any are missing:

```sh
sudo -v
cd ~/tracey-sof
make ansible-check
make provision
```

If Ansible asks for a sudo timestamp that cannot be shared, use a terminal and enter the sudo password when prompted. Alternatively run the Make target with `sudo` and accept that some cache files may become root-owned. Never put passwords in Make variables, command history, inventory files, or Git.

`make provision` installs Go, Node, Chromium, Poppler, Tesseract, and supporting tools. The Go build can also use the committed vendor directory:

```sh
make build
make help
```

## 2. Check Core and datadir paths

```sh
readlink -f /usr/local/bin/bitcoind
/usr/local/bin/bitcoin-cli --version
systemctl is-active bitcoind
findmnt /run/media/admin/BLOCKCHAIN
```

The binary should point to `/opt/bitcoin-core/32.1`. The datadir should be mounted. The unit may be `disabled`; step 5 enables it for future boots.

Do not change ownership or permissions on the datadir to make its cookie accessible. Use the dedicated RPC user in the next steps instead.

## 3. Generate fresh application RPC credentials (once)

The following uses Core's official `rpcauth.py`. It creates a new salted `rpcauth` entry and stores the matching plaintext password only in `node.env` with mode 600. No password is printed or committed.

```sh
cd ~/tracey-sof
umask 077
mkdir -p cache
python3 - <<'PY'
import json
import pathlib
import subprocess

root = pathlib.Path.cwd()
result = json.loads(subprocess.check_output([
    "python3",
    "/opt/bitcoin-core/32.1/share/rpcauth/rpcauth.py",
    "tracey-sof", "-j",
], text=True))
(root / "cache/local-rpcauth.conf").write_text(f"rpcauth={result['rpcauth']}\n")
(root / "node.env").write_text(
    "BITCOIND_RPC_URL=http://127.0.0.1:8332\n"
    "BITCOIND_RPC_USER=tracey-sof\n"
    f"BITCOIND_RPC_PASSWORD={result['password']}\n"
    "DATA_DIR=./data\n"
    "OUT_DIR=./out\n"
    "CACHE_DIR=./cache\n"
    "TRACE_MAX_DEPTH=25\n"
    "TRACE_MAX_TRANSACTIONS=10000\n"
)
(root / "cache/local-rpcauth.conf").chmod(0o600)
(root / "node.env").chmod(0o600)
PY
```

Both files are ignored by Git. If either already exists, do not overwrite it. Re-running the generator produces a different salt/password, so update both the node configuration and `node.env` together if you intentionally regenerate credentials.

## 4. Activate the application RPC user

Back up the configuration and append `cache/local-rpcauth.conf` to `/run/media/admin/BLOCKCHAIN/bitcoin-full/bitcoin.conf`. If that file uses INI sections, place the `rpcauth=` line in the **global** section instead of simply appending it inside a section.

```sh
CONF=/run/media/admin/BLOCKCHAIN/bitcoin-full/bitcoin.conf
sudo cp -a "$CONF" "$CONF.before-tracey-sof"
if ! sudo grep -q '^rpcauth=tracey-sof:' "$CONF"; then
  sudo tee -a "$CONF" < cache/local-rpcauth.conf >/dev/null
fi
sudo systemctl enable bitcoind
sudo systemctl restart bitcoind
```

Verify the application can use the new credentials:

```sh
make node-check
```

If this returns HTTP 401: confirm the `rpcauth` line is in the global section, the password and `rpcauth` entry were generated together, and Core was restarted. If it fails with a datadir permission error, do **not** relax datadir permissions; the dedicated RPC user is intended to avoid cookie sharing.

`make core-check` is a different check: it uses the local `bitcoin-cli` config/datadir. Use `make node-check` for application readiness.

## 5. Require a usable chain snapshot

```sh
sudo -u admin bitcoin-cli \
  -datadir=/run/media/admin/BLOCKCHAIN/bitcoin-full getblockchaininfo
sudo -u admin bitcoin-cli \
  -datadir=/run/media/admin/BLOCKCHAIN/bitcoin-full getindexinfo
```

Required before an audit:

- chain `main`
- `blocks` equals `headers`
- `initialblockdownload` false
- `txindex` synchronized if you need arbitrary historical transaction lookups

Also check `debug.log` for startup/reindex errors. Pruned or unavailable history produces `trace_status=incomplete`; it must not be interpreted as proof of missing purchase documentation.

Run the public-fixture node test before using private data:

```sh
make test-node
```

That test uses committed public mainnet fixtures, not your addresses or purchase records.

## 6. Add your real inputs

Copy the templates and replace **every placeholder** with actual public data:

```sh
install -d -m 700 data/inbox
install -m 600 examples/owned.example.json data/owned.json
install -m 600 examples/purchases.example.json data/purchases.json
install -m 600 examples/annotations.example.json data/annotations.json
install -m 600 examples/trace-policy.example.json data/trace-policy.json
```

Read `docs/inputs.md` for formats and `docs/bitcoin-core.md` for node settings. Keep only public keys and addresses: never put seeds, WIF, xprvs, private descriptors, or passphrases anywhere in the project.

- `owned.json`: your addresses, descriptors, xpubs, and/or explicit current UTXOs.
- `purchases.json`: exact purchase txids, received outpoints, or received-at addresses, plus invoice artifacts with SHA-256 hashes.
- `trace-policy.json`: optional limits. Empty optional stop thresholds are disabled.
- `annotations.json`: start with an empty template; the loopback UI stores labels there.

Do not use the ATM operator/payment destination (`paid_to`) address as source-of-funds evidence. An exact purchase txid or received outpoint is stronger evidence than an address or amount heuristic. See `docs/methodology.md`.

Optional intake for receipts and address lists:

```sh
# Put documents/CSV/TXT/JSON under data/inbox/
make intake
make resolve-intake
make review-intake
./bin/sof review-intake -data-dir ./data --accept <candidate-id> --role received_at
# Add purchase ID/date/provider as appropriate, then:
make promote-intake
make validate
```

OCR candidates always begin `unknown` and require explicit human acceptance. A policy stop is a review flag, not purchase proof.

## 7. Validate, trace, and review

```sh
make validate
make node-check
make audit
make ui
```

`make audit` runs discovery and backward tracing, then writes local reports. Open the loopback UI at `http://127.0.0.1:8765`; do not expose it publicly.

Re-run `make audit` after changing purchase identifiers, confirmed/rejected links, or trace policy. For label-only edits, `make report` is sufficient.

To export for an accountant:

```sh
make export
```

Check every file before sharing. `accountant.zip` can contain addresses, xpubs, txids, graph data, annotations, and invoice artifacts. The HTML report embeds the complete report JSON; display redaction is not data removal.

## 8. Optional verification

```sh
make verify         # Offline tests and privacy checks
make release-check # The above plus public-fixture checks through your Core node
```

Network dependencies: runtime transaction queries go only to the configured Core node. `setup`/`vendor` may fetch Go modules; `provision` fetches public packages; `core-download` uses `bitcoincore.org`. No transaction explorer or analytics service is used. Privileged install targets are not required for daily reporting.

## 9. Safe disconnection of the blockchain SSD

Do not unplug `/dev/sda` while Core is running or its filesystem is mounted. Stop and unmount it first:

```sh
cd ~
sudo systemctl stop bitcoind
sync
sudo umount /run/media/admin/BLOCKCHAIN
findmnt /run/media/admin/BLOCKCHAIN
```

Only if `findmnt` returns no mount entry:

```sh
udisksctl power-off --block-device=/dev/sda
```

Optional `~/.bashrc` alias:

```sh
alias unmount-blockchain='cd "$HOME" && sudo systemctl stop bitcoind && sync && sudo umount /run/media/admin/BLOCKCHAIN && echo "Blockchain unmounted; safe to power off /dev/sda"'
```

Add it without duplicating an existing alias:

```sh
grep -q '^alias unmount-blockchain=' ~/.bashrc || cat >> ~/.bashrc <<'EOF'
alias unmount-blockchain='cd "$HOME" && sudo systemctl stop bitcoind && sync && sudo umount /run/media/admin/BLOCKCHAIN && echo "Blockchain unmounted; safe to power off /dev/sda"'
EOF
source ~/.bashrc
```

If unmount fails as busy, do not disconnect the drive. Find remaining users with `fuser -vm /run/media/admin/BLOCKCHAIN` and fix them. Avoid lazy/forced unmounts on the live blockchain filesystem. After reconnecting it, run `sudo systemctl start bitcoind` and wait for synchronization.

## Final readiness checklist

- [ ] `bitcoind` is enabled and active from `/opt/bitcoin-core/32.1`
- [ ] `make node-check` succeeds with the fresh RPC user
- [ ] Chain is synced on mainnet; required indexes are synchronized
- [ ] `make test-node` passes
- [ ] Owned and purchase JSON files have been filled in, and `make validate` succeeds
- [ ] RPC credentials, invoices, caches, and reports remain outside Git and public exposure
- [ ] No password or key material is placed in an alias, commit, or chat
- [ ] `make audit`, UI review, and only then `make export`

Additional implementation details: `README.md`, `docs/bitcoin-core.md`, `docs/inputs.md`, and `docs/provisioning.md`.
