#!/bin/sh
set -eu

# Reject personal data, private artifacts, and public transaction/analytics
# endpoints. Usage:
#   scripts/privacy-scan.sh [--history] [root]
#
# The working-tree scan covers every tracked file. --history additionally scans
# every commit reachable from any local ref; use it before publishing history.

root="."
history=0
for arg in "$@"; do
	case "$arg" in
	--history) history=1 ;;
	-h | --help)
		printf 'usage: %s [--history] [root]\n' "$0"
		exit 0
		;;
	*) root="$arg" ;;
	esac
done
cd "$root"

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	printf 'privacy scan requires a git worktree\n' >&2
	exit 1
fi

tmp="$(mktemp "${TMPDIR:-/tmp}/sof-privacy.XXXXXX")"
trap 'rm -f "$tmp" "$tmp.fp"' EXIT HUP INT TERM

allowed_hosts="127.0.0.1 localhost [::1] www.w3.org bitcoincore.org github.com json-schema.org"

# Personal data patterns. These apply to code, tests, docs, examples and plans.
personal='/run/media/|/home/(admin|c08rad0r|c03rad0r)/|d24da9f2|Sparda|Pocket App|Pocket Bitcoin|jeffpocket|bull bitcoin|\bbringin\b|dq05|\b(xprv|yprv|zprv|tprv|vprv)[1-9A-HJ-NP-Za-km-z]{50,}\b|\bDE[0-9]{20}\b|\bRF[0-9]{2}[A-Z0-9]{8,}\b'
private_paths='(^|/)(data|out|cache|tracey-inputs|node\.env|\.env)(/|$)'

fail=0
report() {
	printf '%s\n' "$*" >&2
	fail=1
}

# Private artifacts must not live inside the public repository, even when
# gitignored: sharing the directory or a tarball would leak them.
for path in data out cache tracey-inputs node.env .env; do
	if [ -e "$path" ] || [ -L "$path" ]; then
		report "private path present in the public repository: $path"
	fi
done

if git ls-files | grep -E "$private_paths" >"$tmp"; then
	cat "$tmp" >&2
	report "private path is tracked by git"
fi

# Personal-data scan. Vendor code and public mainnet fixtures are excluded;
# they contain upstream key test vectors and documented fixture provenance.
if git grep -n -I -E "$personal" -- . \
	':(exclude)vendor/**' ':(exclude)testdata/**' \
	':(exclude)scripts/privacy-scan.sh' >"$tmp"; then
	cat "$tmp" >&2
	report "personal data, bank identifier, or prohibited endpoint present"
fi

# BIP32 fingerprints in derivation origins. a1b2c3d4 is the documented example;
# anything else is treated as real wallet metadata.
if git grep -n -I -E '\[[0-9a-f]{8}/' -- . \
	':(exclude)vendor/**' ':(exclude)testdata/**' \
	':(exclude)scripts/privacy-scan.sh' >"$tmp"; then
	if grep -vE '\[(a1b2c3d4|00000000)/' "$tmp" >"$tmp.fp"; then
		cat "$tmp.fp" >&2
		report "wallet fingerprint present in a derivation origin"
	fi
fi

# Generic /home/name paths are rejected unless they are clearly placeholders.
if git grep -n -I -E '/home/[A-Za-z0-9._-]+' -- . \
	':(exclude)vendor/**' ':(exclude)testdata/**' \
	':(exclude)scripts/privacy-scan.sh' >"$tmp"; then
	while IFS= read -r hit; do
		name="$(printf '%s\n' "$hit" | sed -n 's#.*/home/\([A-Za-z0-9._-]*\).*#\1#p')"
		case "$name" in
		user | example | runner | youruser | YOUR_USER) ;;
		*) printf '%s\n' "$hit" >&2; fail=1 ;;
		esac
	done <"$tmp"
fi

# Runtime endpoint scan: exclude tests (they assert rejections) and public
# fixture provenance, which intentionally cites where the data came from.
if git grep -n -I -E 'mempool\.space|blockstream\.info|blockchain\.com|blockchair\.com|oxt\.me|arkm\.com|arkham|chainalysis|ciphertrace|glassnode|cryptoquant|dune\.com|etherscan\.io' -- . \
	':(exclude)vendor/**' ':(exclude)testdata/**' ':(exclude)*_test.go' \
	':(exclude)scripts/privacy-scan.sh' >"$tmp"; then
	cat "$tmp" >&2
	report "prohibited third-party transaction or analytics endpoint present"
fi

# External URLs: only loopback, W3C, schemas, Bitcoin Core, and the project host.
if git grep -n -I -o -E 'https?://[^[:space:]"'"'"'<>)]+' -- . \
	':(exclude)vendor/**' ':(exclude)testdata/**' ':(exclude)*_test.go' \
	':(exclude)scripts/privacy-scan.sh' >"$tmp"; then
	while IFS= read -r match; do
		url="${match#*:*:}"
		# Drop credentials, then extract the host, stopping at any path,
		# port, query or fragment marker.
		host="$(printf '%s\n' "$url" | sed -E 's#^https?://[^/@]*@##; s#^https?://##; s#[/:?#].*##')"
		case "$host" in
		*%* | *'$'* | *'{'* | *'['* | *']'*) continue ;;
		esac
		case " $allowed_hosts " in
		*" $host "*) ;;
		*)
			report "unexpected external host $host in $match"
			;;
		esac
	done <"$tmp"
fi

if [ "$history" -eq 1 ]; then
	for commit in $(git rev-list --all); do
		if git ls-tree -r --name-only "$commit" | grep -E "$private_paths" >"$tmp"; then
			printf 'commit %s\n' "$commit" >&2
			cat "$tmp" >&2
			report "private path in git history"
		fi
		if git grep -n -I -E "$personal" "$commit" -- . \
			':(exclude)vendor/**' ':(exclude)testdata/**' \
			':(exclude)scripts/privacy-scan.sh' >"$tmp"; then
			printf 'commit %s\n' "$commit" >&2
			cat "$tmp" >&2
			report "personal data in git history"
		fi
		if git grep -n -I -E '\[[0-9a-f]{8}/' "$commit" -- . \
			':(exclude)vendor/**' ':(exclude)testdata/**' \
			':(exclude)scripts/privacy-scan.sh' >"$tmp"; then
			if grep -vE '\[(a1b2c3d4|00000000)/' "$tmp" >"$tmp.fp"; then
				printf 'commit %s\n' "$commit" >&2
				cat "$tmp.fp" >&2
				report "wallet fingerprint in git history"
			fi
		fi
		if git grep -n -I -E 'mempool\.space|blockstream\.info|blockchain\.com|blockchair\.com|oxt\.me|arkm\.com|arkham|chainalysis|ciphertrace|glassnode|cryptoquant|dune\.com|etherscan\.io' "$commit" -- . \
			':(exclude)vendor/**' ':(exclude)testdata/**' ':(exclude)*_test.go' \
			':(exclude)Makefile' ':(exclude)scripts/privacy-scan.sh' >"$tmp"; then
			printf 'commit %s\n' "$commit" >&2
			cat "$tmp" >&2
			report "prohibited endpoint in git history"
		fi
	done
fi

if [ "$fail" -ne 0 ]; then
	printf 'privacy scan failed\n' >&2
	exit 1
fi
printf 'privacy scan passed\n'
