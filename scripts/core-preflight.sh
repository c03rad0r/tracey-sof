#!/bin/sh
set -eu

service="${CORE_SERVICE:-bitcoind}"
datadir="${CORE_DATADIR:-}"
conf="${BITCOIN_CONF:-$HOME/.bitcoin/bitcoin.conf}"
failed=0

if [ -n "$datadir" ]; then
	if [ -d "$datadir" ]; then
		printf 'datadir: %s\n' "$datadir"
	else
		printf 'error: configured datadir does not exist: %s\n' "$datadir" >&2
		failed=1
	fi
elif [ -f "$conf" ]; then
	configured="$(sed -n 's/^[[:space:]]*datadir[[:space:]]*=[[:space:]]*//p' "$conf" | tail -1)"
	if [ -n "$configured" ]; then
		if [ -d "$configured" ]; then
			printf 'datadir from config: %s\n' "$configured"
		else
			printf 'error: datadir in %s does not exist: %s\n' "$conf" "$configured" >&2
			failed=1
		fi
	fi
fi

if command -v systemctl >/dev/null 2>&1; then
	if systemctl cat "$service.service" >/dev/null 2>&1; then
		printf 'service: %s\n' "$service.service"
		requires="$(systemctl show -p Requires --value "$service.service" 2>/dev/null || true)"
		for unit in $requires; do
			case "$unit" in
			*.mount)
				if systemctl is-active --quiet "$unit"; then
					printf 'mount: %s active\n' "$unit"
				else
					printf 'error: required mount is not active: %s\n' "$unit" >&2
					failed=1
				fi
				;;
			esac
		done
	else
		printf 'warning: systemd unit %s.service was not found\n' "$service" >&2
	fi
fi

if [ "$failed" -ne 0 ]; then
	printf 'Core preflight failed; do not run core-update until these paths/services are repaired\n' >&2
	exit 1
fi
printf 'Core preflight passed\n'
