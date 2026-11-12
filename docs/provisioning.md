# Host provisioning and Bitcoin Core updates

This document describes the optional Ansible workflow for installing local build/runtime dependencies and updating Bitcoin Core. It is separate from the normal application workflow: `make audit` never installs software or changes the node.

The roles do **not** modify `bitcoin.conf`, a data directory, or wallet data. Core installation is explicit, versioned, and gated by GPG and SHA-256 verification.

## Requirements

- Ansible on the control machine.
- Ubuntu or Debian on the target. The current dependency role supports the Debian package family.
- `sudo` access on the target.
- Network access to Ubuntu/Debian package repositories and `bitcoincore.org` during provisioning/download.
- For remote hosts, SSH key access and an inventory entry.

## Install host dependencies

The default inventory targets localhost:

```sh
make ansible-check
make provision
```

If sudo requires a password, pass the prompt through Ansible:

```sh
make provision ANSIBLE_ARGS='--ask-become-pass'
```

For a remote host:

```sh
cp ansible/inventory.example.ini ansible/inventory.ini
# Edit the host, user, and SSH key settings.
make provision ANSIBLE_INVENTORY=ansible/inventory.ini ANSIBLE_ARGS='--ask-become-pass'
```

`ansible/inventory.ini` is ignored by Git. Do not put RPC credentials in it.

The `sof_deps` role installs:

- build tools and Git
- Go 1.25+
- Node.js 20+
- Chromium for PDF and browser verification
- `poppler-utils` for `pdftotext` and `pdftoppm`
- Tesseract OCR and English language data
- `curl`, `gnupg`, `jq`, `make`, and `python3`

On Ubuntu, `chromium-browser` is a transitional package that installs the Chromium snap. If snaps are unavailable, set `sof_deps_install_chromium: false` and install a Chromium binary manually.

Check the playbooks without changing the host:

```sh
make ansible-check
```

## Check the installed Core

```sh
make core-check
```

This prints the `bitcoin-cli` version, the resolved `bitcoind` path, warnings for versions below 29, and RPC/index status when a node is reachable.

If the node uses a non-default configuration, set:

```sh
make core-check BITCOIN_CONF=/path/to/bitcoin.conf
```

For a custom data directory:

```sh
make core-check BITCOIN_CONF=/path/to/bitcoin.conf BITCOIN_DATADIR=/path/to/datadir
```

`bitcoind --version` can fail when `bitcoin.conf` points at a missing data directory. `make core-check` uses `bitcoin-cli --version` so it can still report the installed client. Use `make node-check` separately to verify RPC connectivity and node capabilities.

### Preflight before a managed update

```sh
make core-preflight CORE_DATADIR=/path/to/datadir CORE_SERVICE=bitcoind
```

This read-only check fails when the configured datadir does not exist, or when a systemd unit required by `bitcoind.service` is not active. It does not stop or start anything.

`core-update` also performs a target-side preflight when `bitcoin_core_manage_service` is true: it requires the service and its required mounts to be active before changing binaries. Set `bitcoin_core_require_active_service=false` only if you deliberately want to update an inactive service.

## Why update

Bitcoin Core 29+ is the documented target. The project may operate against older Core versions for some read-only calls, but it warns about them and cannot assume newer behavior. Update to a current stable release from:

```text
https://bitcoincore.org/bin/bitcoin-core-<VERSION>/
```

Pick an explicit version. Do not use an unpinned `latest` URL in an automated install.

## Verify the release manually

Example values; replace them with the release and architecture you are installing:

```sh
VERSION=31.1.0
ARCH=x86_64-linux-gnu
BASE="https://bitcoincore.org/bin/bitcoin-core-${VERSION}"
WORK="$HOME/bitcoin-core-${VERSION}"

mkdir -p "$WORK"
cd "$WORK"

curl -fSLO "${BASE}/SHA256SUMS"
curl -fSLO "${BASE}/SHA256SUMS.asc"
curl -fSLO "${BASE}/bitcoin-${VERSION}-${ARCH}.tar.gz"
```

### Obtain builder keys

Use the builder keys linked from the official Bitcoin Core download page:

```text
https://github.com/bitcoin-core/guix.sigs/tree/main/builder-keys
```

Download the individual `.gpg` files into a local directory. You can pass that directory directly to `CORE_KEYS_FILE`, or concatenate the keys into one file. Verify each full fingerprint against the official download page and, ideally, a second independent source. Do not trust a key only because it came from a keyserver or because its name looks correct.

A plain list of fingerprints is **not** a public-key file. Keep fingerprints in `CORE_TRUSTED_FINGERPRINTS`; keep the actual public keys in `.gpg` files.

```sh
# If you use a single concatenated file:
cat /path/to/builder-keys/*.gpg > builder-keys.txt

# Import for a manual check:
gpg --import /path/to/builder-keys/*.gpg
gpg --list-keys --with-fingerprint
```

### Verify the signed checksums

```sh
gpg --verify SHA256SUMS.asc SHA256SUMS
```

Require at least one `Good signature` from a builder fingerprint you have independently verified. Investigate:

- `BAD signature`
- `Can't check signature: No public key`
- a signature from an unexpected fingerprint

`WARNING: This key is not certified with a trusted signature` means GnuPG has no local trust decision for the key. Verify the fingerprint manually before assigning trust.

### Verify the tarball

```sh
grep "bitcoin-${VERSION}-${ARCH}.tar.gz" SHA256SUMS | sha256sum -c -
```

Do not install if the checksum or signature check fails.

## Verify with the repository workflow

The local helper keeps verification state under `.cache/core-release/<version>/` and uses a temporary GnuPG home so it does not modify your normal keyring.

Download:

```sh
make core-download CORE_VERSION=31.1.0
```

Verify:

```sh
make core-verify \
  CORE_VERSION=31.1.0 \
  CORE_KEYS_FILE=/path/to/builder-keys.txt \
  CORE_TRUSTED_FINGERPRINTS="FINGERPRINT1 FINGERPRINT2"
```

`CORE_TRUSTED_FINGERPRINTS` is a space- or comma-separated list. `CORE_KEYS_FILE` may point to a key file or a directory containing official `.gpg` builder keys. The helper requires at least one valid signature from the trusted list. It also checks the tarball SHA-256 against the signed `SHA256SUMS` and writes a `.verified` marker containing the expected archive hash.

The helper refuses to verify without a keys file and trusted fingerprints. It does not fetch or trust keys automatically.

To re-download after a failed or interrupted transfer, remove the version directory:

```sh
rm -rf .cache/core-release/31.1.0
```

## Install or update Core

The `bitcoin_core` role installs each version under:

```text
/opt/bitcoin-core/<version>/
```

and updates symlinks in `/usr/local/bin`. This avoids depending on a release directory under a user's Downloads folder and makes rollback a symlink change.

### Service not managed by Ansible

Stop the node first, using the existing service or RPC method:

```sh
sudo systemctl stop bitcoind
```

or:

```sh
bitcoin-cli -conf=/path/to/bitcoin.conf stop
```

Then install:

```sh
make core-install \
  CORE_VERSION=31.1.0 \
  CORE_KEYS_FILE=/path/to/builder-keys.txt \
  CORE_TRUSTED_FINGERPRINTS="FINGERPRINT1 FINGERPRINT2"
```

Start the node manually and verify it.

### Existing systemd service

If a `bitcoind` systemd unit already exists, let Ansible install and restart it:

```sh
make core-update \
  CORE_VERSION=31.1.0 \
  CORE_KEYS_FILE=/path/to/builder-keys.txt \
  CORE_TRUSTED_FINGERPRINTS="FINGERPRINT1 FINGERPRINT2"
```

`core-update` sets `CORE_MANAGE_SERVICE=true` and installs only after `core-verify` succeeds. The role updates the symlinks and the handler restarts `bitcoind`.

For a remote target, download and verify on the control machine, then let Ansible copy the verified artifacts to the target:

```sh
make core-update \
  ANSIBLE_INVENTORY=ansible/inventory.ini \
  CORE_VERSION=31.1.0 \
  CORE_RELEASE_DIR=/var/tmp/bitcoin-core-31.1.0 \
  CORE_KEYS_FILE=/path/to/builder-keys.txt \
  CORE_TRUSTED_FINGERPRINTS="FINGERPRINT1 FINGERPRINT2"
```

`CORE_LOCAL_RELEASE_DIR` remains the controller-side `.cache/core-release/<version>` directory. When `CORE_RELEASE_DIR` differs, the role creates the target directory and copies the tarball, checksums, signature, and `.verified` marker before installation.

To use a different unit:

```sh
make core-update \
  CORE_VERSION=31.1.0 \
  CORE_KEYS_FILE=/path/to/builder-keys.txt \
  CORE_TRUSTED_FINGERPRINTS="FINGERPRINT1 FINGERPRINT2" \
  ANSIBLE_ARGS='-e bitcoin_core_service_name=bitcoind-mainnet'
```

### sudo/become timeout

If Ansible reports `Timed out waiting for become success or become password prompt`, the install did not begin. Authenticate sudo in the same terminal first:

```sh
sudo -v
```

Then run the update **without** `ANSIBLE_ARGS='--ask-become-pass'`:

```sh
make core-update \
  CORE_VERSION=32.1 \
  CORE_KEYS_FILE="$PWD/.cache/core-release/32.1/keys" \
  CORE_TRUSTED_FINGERPRINTS="..." \
  CORE_DATADIR=/run/media/admin/BLOCKCHAIN/bitcoin-full \
  CORE_SERVICE=bitcoind
```

Ansible will use the cached sudo timestamp. Do not put the sudo password in a Make variable, command line, or tracked file.

If `sudo -n true` still reports `interactive authentication is required` after `sudo -v`, sudo is using a tty- or process-specific timestamp. In that case run the Make target itself under sudo:

```sh
sudo make core-update \
  CORE_VERSION=32.1 \
  CORE_KEYS_FILE="$PWD/.cache/core-release/32.1/keys" \
  CORE_TRUSTED_FINGERPRINTS="..." \
  CORE_DATADIR=/run/media/admin/BLOCKCHAIN/bitcoin-full \
  CORE_SERVICE=bitcoind
```

This runs verification and Ansible as root. It may leave the `.verified` marker root-owned; restore ownership afterward if needed:

```sh
sudo chown -R "$USER:$USER" .cache/core-release
```

### Installation refusal rules

The role refuses to proceed when:

- the version, architecture, or release directory is missing
- the archive is missing
- the `.verified` marker is missing
- the checksum in the marker is malformed
- the archive hash changed after verification

It never copies or deletes `bitcoin.conf`, the data directory, or wallet data.

## Post-upgrade checks

```sh
make core-check
make node-check
make release-check
```

`make node-check` reports the RPC version, chain, synchronization state, `txindex`, and prunable data warnings. `make release-check` runs the full offline suite plus the live Core fixture checks.

Check the node manually:

```sh
bitcoin-cli getnetworkinfo
bitcoin-cli getblockchaininfo
bitcoin-cli getindexinfo
```

Wait until:

- `blocks == headers`
- `initialblockdownload` is false
- the node is on `main`
- `txindex` is synchronized if you use it

If enabling `txindex=1` for the first time, expect a long index build. Do not run `make audit` until it completes.

## Backup and rollback

Before updating:

- back up `bitcoin.conf`
- back up wallet files if the node has a wallet
- keep the previous `/opt/bitcoin-core/<version>` directory

Rollback by pointing the `/usr/local/bin` symlinks back to the previous version, then restarting the service. Do not restore a data directory while `bitcoind` is running.

## Current host observation

The `BLOCKCHAIN` drive is mounted, `bitcoind` is running from `/run/media/admin/BLOCKCHAIN/bitcoin-full`, and the verifier installed **Bitcoin Core 32.1** under `/opt/bitcoin-core/32.1`. The `bitcoin-cli` binary reports v32.1.0 and the `bitcoind.service` restart result is successful. The systemd unit is currently disabled and must be enabled if it should start at boot.

The 32.1 archive is GPG-verified against official builder key `152812300785C96444D3334D17565732E08E5E41` (`achow101`); the tarball SHA-256 is `61545c17c3120fb7412f896d248b11993af04ec8817aad3649c55b2afb044963`. The `.verified` marker is present.

Dedicated RPC setup has been prepared locally:

```text
node.env                         mode 600, RPC user tracey-sof, password not printed
cache/local-rpcauth.conf         mode 600, rpcauth line for the node config
```

The rpcauth line has **not** been appended to the node config yet, and the service has not been restarted with it. That requires sudo access to `admin`'s datadir.

Run:

```sh
sudo sh -c 'cat /home/c08rad0r/tracey-sof/cache/local-rpcauth.conf >> /run/media/admin/BLOCKCHAIN/bitcoin-full/bitcoin.conf'
sudo systemctl enable bitcoind
sudo systemctl restart bitcoind
make node-check
```

If the config uses INI sections, place `rpcauth=` in the global section rather than appending inside a section. Do not duplicate an existing `rpcauth` entry for `tracey-sof`.

## Role variables

| Variable | Default | Meaning |
|---|---|---|
| `bitcoin_core_version` | required | Release version, for example `31.1.0` |
| `bitcoin_core_arch` | `x86_64-linux-gnu` | Official Linux archive architecture |
| `bitcoin_core_release_dir` | required | Target directory containing the tarball and `.verified` |
| `bitcoin_core_local_release_dir` | empty | Controller-side verified release directory for remote copies |
| `bitcoin_core_install_root` | `/opt/bitcoin-core` | Versioned installation root |
| `bitcoin_core_bin_dir` | `/usr/local/bin` | Symlink directory |
| `bitcoin_core_manage_service` | `false` | Restart an existing systemd unit |
| `bitcoin_core_require_active_service` | `true` | Require the managed service and required mounts to be active |
| `bitcoin_core_service_name` | `bitcoind` | systemd unit name |
| `bitcoin_core_datadir` | empty | Optional target datadir required by preflight |
| `sof_deps_install_chromium` | `true` | Install Chromium |
| `sof_chromium_package` | platform-dependent | Chromium package name |

## Scope and privacy

- Provisioning installs public software packages and binaries; it does not receive wallet keys or financial data.
- Core verification downloads public release artifacts and builder keys.
- The roles do not read or upload `data/`, `cache/`, reports, invoices, or annotations.
- RPC credentials from `node.env` are exported only to the RPC-using Make targets (`node-check`, `discover`, `trace`, `audit`, `resolve-intake`, and `test-node`). They are not inherited by provisioning, release-download, OCR, browser, or test targets.
- Do not place RPC credentials in a tracked inventory or playbook.
- Keep the generated `.cache/core-release/` directory private; it contains downloaded binaries and verification metadata.
- `core-download` enforces HTTPS-only downloads and redirects, TLS 1.2 or newer, and a maximum artifact size.
- A fingerprint list is not a builder-key file. Local `trusted-keys` notes are ignored by Git; `CORE_KEYS_FILE` must point to official `.gpg` public keys or a concatenated keyring.
