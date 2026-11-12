#!/bin/sh
set -eu

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat >&2 <<'EOF'
usage:
  core-release.sh download VERSION [ARCH]
  core-release.sh verify VERSION ARCH KEYS_FILE

Environment:
  CORE_RELEASE_DIR   download/verification directory
  CORE_RELEASE_BASE  release base URL
  TRUSTED_FINGERPRINTS  space- or comma-separated trusted builder fingerprints
EOF
	exit 2
}

[ "$#" -ge 2 ] || usage
action="$1"
version="$2"
arch="${3:-x86_64-linux-gnu}"
printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+(\.[0-9]+)?$' || die "invalid Core version: $version"
case "$arch" in
x86_64-linux-gnu | aarch64-linux-gnu | arm-linux-gnueabihf | riscv64-linux-gnu | powerpc64le-linux-gnu) ;;
*) die "unsupported Core architecture: $arch" ;;
esac
dir="${CORE_RELEASE_DIR:-$PWD/.cache/core-release/$version}"
base="${CORE_RELEASE_BASE:-https://bitcoincore.org/bin/bitcoin-core-$version}"

case "$action" in
download)
	command -v curl >/dev/null 2>&1 || die "curl is required"
	mkdir -p "$dir"
	for file in SHA256SUMS SHA256SUMS.asc "bitcoin-$version-$arch.tar.gz"; do
		if [ -s "$dir/$file" ]; then
			printf 'already present: %s\n' "$dir/$file"
			continue
		fi
		tmp="$dir/$file.part"
		rm -f "$tmp"
		printf 'downloading: %s\n' "$base/$file"
		curl --proto '=https' --proto-redir '=https' --tlsv1.2 -fL \
			--retry 3 --retry-delay 2 --connect-timeout 15 \
			--max-filesize 250000000 \
			--output "$tmp" "$base/$file"
		mv "$tmp" "$dir/$file"
	done
	printf 'downloaded into %s\n' "$dir"
	;;
verify)
	keys="${4:-${CORE_KEYS_FILE:-}}"
	trusted="${TRUSTED_FINGERPRINTS:-}"
	[ -n "$keys" ] || die "a builder keys file or directory is required (fourth argument or CORE_KEYS_FILE)"
	if [ -d "$keys" ]; then
		found=0
		for keyfile in "$keys"/*.gpg; do
			[ -e "$keyfile" ] || continue
			found=1
		done
		[ "$found" -eq 1 ] || die "no .gpg builder keys found in directory: $keys"
	elif [ -f "$keys" ]; then
		[ -s "$keys" ] || die "builder keys file is empty: $keys"
	else
		die "builder keys file or directory not found: $keys"
	fi
	[ -n "$trusted" ] || die "set TRUSTED_FINGERPRINTS to one or more trusted builder fingerprints"
	[ -s "$dir/SHA256SUMS" ] || die "run the download action first: $dir/SHA256SUMS is missing"
	[ -s "$dir/SHA256SUMS.asc" ] || die "run the download action first: $dir/SHA256SUMS.asc is missing"
	tarfile="$dir/bitcoin-$version-$arch.tar.gz"
	[ -s "$tarfile" ] || die "run the download action first: $tarfile is missing"
	command -v gpg >/dev/null 2>&1 || die "gpg is required"
	command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required"

	tmp="$(mktemp -d "${TMPDIR:-/tmp}/sof-gpg.XXXXXX")"
	trap 'rm -rf "$tmp"' EXIT HUP INT TERM
	GNUPGHOME="$tmp"
	export GNUPGHOME
	if [ -d "$keys" ]; then
		for keyfile in "$keys"/*.gpg; do
			gpg --batch --quiet --import "$keyfile" || die "failed to import builder key: $keyfile"
		done
	else
		gpg --batch --quiet --import "$keys" 2>"$tmp/import.err" || {
			cat "$tmp/import.err" >&2
			die "failed to import builder keys"
		}
	fi
	status="$tmp/status"
	if ! gpg --batch --status-fd 3 --verify "$dir/SHA256SUMS.asc" "$dir/SHA256SUMS" 3>"$status" 2>"$tmp/verify.err"; then
		cat "$tmp/verify.err" >&2
		die "GPG verification failed"
	fi
	valid="$(awk '/VALIDSIG/ {print $3}' "$status" | sort -u)"
	[ -n "$valid" ] || die "GPG verification returned no valid signatures"
	trusted_list="$(printf '%s\n' "$trusted" | tr ',' ' ' | tr ' ' '\n' | sed '/^$/d')"
	matched=""
	for fingerprint in $trusted_list; do
		if printf '%s\n' "$valid" | grep -Fqx "$fingerprint"; then
			matched="$matched $fingerprint"
		fi
	done
	[ -n "$matched" ] || die "no signature matched a trusted builder fingerprint"

	line="$(grep -E "[[:space:]]bitcoin-${version}-${arch}\\.tar\\.gz$" "$dir/SHA256SUMS" || true)"
	[ -n "$line" ] || die "SHA256SUMS does not contain bitcoin-${version}-${arch}.tar.gz"
	hash="$(printf '%s\n' "$line" | awk '{print $1}')"
	printf '%s\n' "$line" | (cd "$dir" && sha256sum -c -) || die "tarball checksum verification failed"

	umask 077
	{
		printf 'version=%s\n' "$version"
		printf 'arch=%s\n' "$arch"
		printf 'tarball=bitcoin-%s-%s.tar.gz\n' "$version" "$arch"
		printf 'sha256=%s\n' "$hash"
		printf 'signers=%s\n' "$(printf '%s\n' "$matched" | sed 's/^ //')"
		printf 'verified_at=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
	} >"$dir/.verified"
	printf 'verified %s\n' "$tarfile"
	printf 'trusted signers:%s\n' "$matched"
	;;
*)
	usage
	;;
esac
