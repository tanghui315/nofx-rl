#!/usr/bin/env python3
"""
Minimal label writer.

Usage:
  - echo '{"source_ts":"...","symbol":"ETHUSDT","horizon_min":30, ...}' | scripts/write_label.py
  - scripts/write_label.py --file labels.json   # labels.json can be a single object or a JSON array

This does not compute labels; it only appends provided label entries to
logs/trading/YYYY-MM-DD-trade-labels.jsonl (UTC day derived from source_ts).

Phase 1 stub: upstream jobs should compute directional_return, MFE/MAE, hit, etc.
"""
import sys
import json
import argparse
import os
from datetime import datetime, timezone


REQUIRED = ["source_ts", "symbol", "horizon_min"]

def utc_day(dt_iso: str) -> str:
    try:
        dt = datetime.fromisoformat(dt_iso.replace('Z', '+00:00'))
    except Exception:
        dt = datetime.now(timezone.utc)
    return dt.astimezone(timezone.utc).strftime('%Y-%m-%d')


def min_validate(obj: dict) -> None:
    missing = [k for k in REQUIRED if k not in obj]
    if missing:
        raise ValueError(f"missing required keys: {missing}")
    # basic horizon whitelist
    if obj.get("horizon_min") not in [5, 15, 30, 60, 180]:
        raise ValueError("horizon_min must be one of [5, 15, 30, 60, 180]")


def append_jsonl(path: str, obj: dict) -> None:
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, 'a', encoding='utf-8') as f:
        f.write(json.dumps(obj, ensure_ascii=False) + "\n")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--file', '-f', help='read JSON from file (default: stdin)')
    args = ap.parse_args()

    if args.file:
        with open(args.file, 'r', encoding='utf-8') as fp:
            data = json.load(fp)
    else:
        raw = sys.stdin.read()
        data = json.loads(raw)

    if isinstance(data, list):
        entries = data
    else:
        entries = [data]

    count = 0
    for obj in entries:
        min_validate(obj)
        day = utc_day(obj.get('source_ts', ''))
        out = os.path.join('logs', 'trading', f'{day}-trade-labels.jsonl')
        append_jsonl(out, obj)
        count += 1
    print(f"ok: appended {count} labels", file=sys.stderr)


if __name__ == '__main__':
    main()

