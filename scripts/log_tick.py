#!/usr/bin/env python3
"""
Minimal tick logger.

Usage:
  - echo '{...json...}' | scripts/log_tick.py
  - scripts/log_tick.py --file tick.json

It appends a single JSON object to logs/trading/YYYY-MM-DD-trade-log.jsonl (UTC day).
It performs a lightweight schema check (required keys only) to avoid malformed rows.

This is a Phase 1 stub: real systems should populate fields from the decision loop.
"""
import sys
import json
import argparse
import os
from datetime import datetime, timezone


REQUIRED_TOP = ["ts", "symbol", "reason", "features", "risk", "decision"]

def utc_day(dt_iso: str) -> str:
    try:
        dt = datetime.fromisoformat(dt_iso.replace('Z', '+00:00'))
    except Exception:
        # fallback to now if ts invalid
        dt = datetime.now(timezone.utc)
    return dt.astimezone(timezone.utc).strftime('%Y-%m-%d')


def min_validate(obj: dict) -> None:
    missing = [k for k in REQUIRED_TOP if k not in obj]
    if missing:
        raise ValueError(f"missing required keys: {missing}")
    # simple nested checks
    feats = obj.get("features", {})
    for sub in ("m15", "h1", "h4", "btc_state", "checklist_hits"):
        if sub not in feats:
            raise ValueError(f"features.{sub} is required")
    risk = obj.get("risk", {})
    if not isinstance(risk, dict):
        raise ValueError("risk must be object")
    dec = obj.get("decision", {})
    if not isinstance(dec, dict) or "action" not in dec or "confidence" not in dec:
        raise ValueError("decision.action and decision.confidence are required")


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
        day = utc_day(obj.get('ts', ''))
        out = os.path.join('logs', 'trading', f'{day}-trade-log.jsonl')
        append_jsonl(out, obj)
        count += 1
    print(f"ok: appended {count} entries", file=sys.stderr)


if __name__ == '__main__':
    main()

