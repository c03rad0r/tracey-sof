#!/bin/sh
set -eu

CLI="${BITCOIN_CLI:-bitcoin-cli}"
DAEMON="${BITCOIND:-bitcoind}"

if ! command -v "$CLI" >/dev/null 2>&1; then
	printf 'error: bitcoin-cli not found\n' >&2
	exit 1
fi

printf 'bitcoin-cli: %s\n' "$(command -v "$CLI")"
version_line="$("$CLI" --version | head -n 1)"
printf '%s\n' "$version_line"
version="$(printf '%s\n' "$version_line" | sed -n 's/.*version v\([0-9][0-9.]*\).*/\1/p')"
major="${version%%.*}"
case "$major" in
'' | *[!0-9]*) ;;
*)
	if [ "$major" -lt 29 ]; then
		printf 'warning: Bitcoin Core %s is below the recommended 29+ target\n' "$version" >&2
	fi
	;;
esac

if command -v "$DAEMON" >/dev/null 2>&1; then
	target="$(readlink -f "$(command -v "$DAEMON")" 2>/dev/null || command -v "$DAEMON")"
	printf 'bitcoind: %s\n' "$target"
	case "$target" in
	*/Downloads/*) printf 'warning: bitcoind is linked from a Downloads directory; prefer a versioned /opt install\n' >&2 ;;
	esac
fi

rpc() {
	if [ -n "${BITCOIN_CONF:-}" ] && [ -n "${BITCOIN_DATADIR:-}" ]; then
		"$CLI" -conf="$BITCOIN_CONF" -datadir="$BITCOIN_DATADIR" "$@"
	elif [ -n "${BITCOIN_CONF:-}" ]; then
		"$CLI" -conf="$BITCOIN_CONF" "$@"
	elif [ -n "${BITCOIN_DATADIR:-}" ]; then
		"$CLI" -datadir="$BITCOIN_DATADIR" "$@"
	else
		"$CLI" "$@"
	fi
}

if ! rpc getnetworkinfo >/dev/null 2>&1; then
	printf 'RPC: unavailable; set BITCOIN_CONF or BITCOIN_DATADIR, or check node.env\n' >&2
	exit 1
fi

printf 'RPC: reachable\n'
rpc getblockchaininfo
rpc getindexinfo
