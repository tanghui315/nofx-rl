#!/usr/bin/env python3
"""
Key3 micro-refresh detector (Phase 2 stub).

Reads a single tick JSON from stdin or --file and if key-three conditions are met,
emits a short-lived recommendation JSON to stdout and writes analytics/key3_recommendation.json.

Key3 conditions (configurable via CLI):
  - tf_agree_min >= --tf (default 3)
  - vol_z >= --vol (default 1.5)
  - btc_state non-empty

Output example:
{
  "recommendation": "key3",
  "ttl_minutes": 10,
  "conditions": {"tf_agree_min": ">=3", "vol_z": ">=1.5", "btc_support": true}
}
"""
import sys, json, argparse, os


def hit(features: dict, tf_req: int, vol_req: float) -> bool:
    tf = features.get('tf_agree_min')
    if tf is None:
        tf = features.get('tf_agree_count')
    if tf is None:
        tf = 0
    vol_z = features.get('vol_z', 0.0)
    btc = features.get('btc_state', {})
    btc_ok = isinstance(btc, dict) and len(btc) > 0
    return (tf >= tf_req) and (vol_z >= vol_req) and btc_ok


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--file', '-f', help='tick JSON file (default: stdin)')
    ap.add_argument('--tf', type=int, default=3)
    ap.add_argument('--vol', type=float, default=1.5)
    ap.add_argument('--ttl', type=int, default=10)
    args = ap.parse_args()

    if args.file:
        with open(args.file, 'r', encoding='utf-8') as fp:
            obj = json.load(fp)
    else:
        obj = json.loads(sys.stdin.read())

    feats = obj.get('features', {})
    if hit(feats, args.tf, args.vol):
        rec = {
            "recommendation": "key3",
            "ttl_minutes": args.ttl,
            "conditions": {"tf_agree_min": f">={args.tf}", "vol_z": f">={args.vol}", "btc_support": True}
        }
        os.makedirs('analytics', exist_ok=True)
        with open('analytics/key3_recommendation.json', 'w', encoding='utf-8') as f:
            json.dump(rec, f, ensure_ascii=False, indent=2)
        print(json.dumps(rec, ensure_ascii=False))
    else:
        # no output if not hit (by design)
        pass


if __name__ == '__main__':
    main()

