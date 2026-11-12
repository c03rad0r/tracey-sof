#!/bin/sh
set -eu

root="${1:-.}"
cd "$root"

allowed_hosts="127.0.0.1 localhost [::1] www.w3.org bitcoincore.org github.com"
banned='mempool\.space|blockstream\.info|blockchain\.com|blockchair\.com|oxt\.me|arkm\.com|arkham|chainalysis|ciphertrace|glassnode|cryptoquant|dune\.com|etherscan\.io'
tmp="$(mktemp "${TMPDIR:-/tmp}/sof-privacy.XXXXXX")"
trap 'rm -f "$tmp"' EXIT HUP INT TERM

set -- Makefile ansible scripts cmd internal web examples
if find "$@" -type f ! -name '*_test.go' ! -path 'scripts/privacy-scan.sh' -exec grep -HnE "$banned" {} + >"$tmp" 2>/dev/null; then
	if [ -s "$tmp" ]; then
		cat "$tmp" >&2
		printf 'prohibited third-party transaction or analytics endpoint present\n' >&2
		exit 1
	fi
fi

if find "$@" -type f ! -name '*_test.go' ! -path 'scripts/privacy-scan.sh' -exec grep -HnoE "https?://[^[:space:]\"'<>)]+" {} + >"$tmp" 2>/dev/null; then
	while IFS= read -r match; do
		url="${match#*:*:}"
		host="$(printf '%s\n' "$url" | sed -E 's#^https?://##; s#^\[([^]]+)\].*#[\1]#; s#[/:].*##')"
		case "$host" in
		*%* | *'$'* | *'{'*) continue ;;
		esac
		case " $allowed_hosts " in
		*" $host "*) ;;
		*)
			printf 'unexpected external host %s in %s\n' "$host" "$match" >&2
			exit 1
			;;
		esac
	done <"$tmp"
fi

printf 'privacy scan passed\n'
