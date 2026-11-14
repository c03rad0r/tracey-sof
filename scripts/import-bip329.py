#!/usr/bin/env python3
"""Merge Sparrow/BIP-329 wallet labels into tracey-sof annotations.

BIP-329 records are JSON Lines. Supported types: tx, addr, output, input, xpub.
Labels are merged without discarding existing notes; optional wallet metadata
(value, fiat value at the time, rate, time, height, keypath, origin) is copied
so the report can show BTC and wallet-recorded USD amounts.

Usage:
  import-bip329.py LABELS.jsonl --annotations data/annotations.json [--overwrite]

Exit codes: 0 success, 1 invalid input, 2 usage error.
"""

import argparse
import json
import os
import sys
import tempfile


def parse_args():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("labels", help="BIP-329 JSON Lines export")
    parser.add_argument("--annotations", required=True, help="annotations.json to merge into")
    parser.add_argument("--overwrite", action="store_true", help="replace existing non-empty labels")
    return parser.parse_args()


def amount(value):
    """Return a decimal string for a wallet amount, or None."""
    if value is None or isinstance(value, bool):
        return None
    if isinstance(value, int):
        return str(value)
    if isinstance(value, str):
        return value.strip() or None
    return str(value)


def fiat_map(value):
    if not isinstance(value, dict):
        return None
    result = {}
    for currency, entry in value.items():
        text = amount(entry)
        if text is not None:
            result[str(currency)] = text
    return result or None


def target_map(annotation_file, kind):
    key = {"tx": "transactions", "addr": "addresses", "output": "outpoints",
           "input": "outpoints", "xpub": "xpubs"}[kind]
    if not isinstance(annotation_file.get(key), dict):
        annotation_file[key] = {}
    return annotation_file[key]


def merge(annotation_file, kind, ref, record, overwrite):
    if not isinstance(ref, str) or not ref:
        return False
    target = target_map(annotation_file, kind)
    current = target.get(ref)
    if not isinstance(current, dict):
        current = {}
    label = (record.get("label") or "").strip()
    if label and (overwrite or not current.get("label")):
        current["label"] = label
    value = record.get("value")
    if isinstance(value, int) and not isinstance(value, bool) and value >= 0:
        current.setdefault("value_sats", value)
    fiat = fiat_map(record.get("fmv"))
    if fiat and "fiat" not in current:
        current["fiat"] = fiat
    rate = fiat_map(record.get("rate"))
    if rate and "rate" not in current:
        current["rate"] = rate
    for field in ("time", "keypath", "origin"):
        text = record.get(field)
        if isinstance(text, str) and text and field not in current:
            current[field] = text
    height = record.get("height")
    if isinstance(height, int) and not isinstance(height, bool) and height >= 0:
        current.setdefault("height", height)
    if not current:
        return False
    target[ref] = current
    return True


def main():
    args = parse_args()
    annotation_file = {"schema_version": 1, "transactions": {}, "addresses": {},
                       "outpoints": {}, "xpubs": {}, "purchase_links": []}
    if os.path.exists(args.annotations):
        with open(args.annotations, encoding="utf-8") as handle:
            loaded = json.load(handle)
        if not isinstance(loaded, dict) or loaded.get("schema_version") != 1:
            print("annotations file has an unsupported schema_version", file=sys.stderr)
            return 1
        annotation_file.update(loaded)
    if not isinstance(annotation_file.get("purchase_links"), list):
        annotation_file["purchase_links"] = []

    counts = {"tx": 0, "addr": 0, "output": 0, "input": 0, "xpub": 0, "skipped": 0}
    # Process outputs before inputs so a received-output label wins over the
    # later "spent as input" description of the same outpoint.
    order = {"addr": 0, "tx": 1, "output": 2, "input": 3, "xpub": 4}
    records = []
    with open(args.labels, encoding="utf-8-sig") as handle:
        for number, line in enumerate(handle, 1):
            line = line.strip()
            if not line:
                continue
            try:
                record = json.loads(line, parse_float=str)
            except json.JSONDecodeError as error:
                print(f"{args.labels}:{number}: invalid JSON: {error}", file=sys.stderr)
                return 1
            if not isinstance(record, dict) or record.get("type") not in order:
                counts["skipped"] += 1
                continue
            records.append(record)
    records.sort(key=lambda record: order[record["type"]])

    for record in records:
        kind = record["type"]
        ref = record.get("ref")
        if kind == "tx" and (not isinstance(ref, str) or len(ref) != 64):
            counts["skipped"] += 1
            continue
        if kind in ("output", "input") and (not isinstance(ref, str) or ":" not in ref):
            counts["skipped"] += 1
            continue
        if merge(annotation_file, kind, ref, record, args.overwrite):
            counts[kind] += 1
        else:
            counts["skipped"] += 1

    directory = os.path.dirname(os.path.abspath(args.annotations)) or "."
    os.makedirs(directory, mode=0o700, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(dir=directory, prefix=".annotations-", suffix=".tmp")
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as handle:
            json.dump(annotation_file, handle, indent=2)
            handle.write("\n")
            handle.flush()
            os.fsync(handle.fileno())
        os.chmod(temporary, 0o600)
        os.replace(temporary, args.annotations)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)

    print("imported BIP-329 labels: " + ", ".join(f"{key}={counts[key]}" for key in ("tx", "addr", "output", "input", "xpub")) +
          f", skipped={counts['skipped']}")
    print(f"wrote {args.annotations}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
