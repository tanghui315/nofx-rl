#!/usr/bin/env python3
import json, sys, datetime
from typing import Any, Dict

# Minimal header generator; no external deps

def parse_iso8601(ts: str) -> datetime.datetime:
    return datetime.datetime.fromisoformat(ts.replace('Z', '+00:00'))

def is_valid_ttl(meta: Dict[str, Any]) -> bool:
    try:
        gen = parse_iso8601(meta.get('generated_at', ''))
        ttl_h = float(meta.get('ttl_hours', 0))
        now = datetime.datetime.utcnow().replace(tzinfo=datetime.timezone.utc)
        return (now - gen).total_seconds() <= ttl_h * 3600
    except Exception:
        return False


def clamp(val, lo=None, hi=None):
    if lo is not None and val < lo:
        return lo
    if hi is not None and val > hi:
        return hi
    return val


def build_header(overrides: Dict[str, Any]) -> str:
    meta = overrides.get('meta', {})
    if not is_valid_ttl(meta):
        return ''
    valid_until = parse_iso8601(meta['generated_at']) + datetime.timedelta(hours=float(meta['ttl_hours']))

    g = overrides.get('global', {})
    ctb = g.get('confidence_threshold_base', {})
    ct_val = ctb.get('value')
    ct_min = ctb.get('min')
    ct_max = ctb.get('max')
    if ct_val is not None:
        ct_eff = clamp(float(ct_val), float(ct_min) if ct_min is not None else None, float(ct_max) if ct_max is not None else None)
    else:
        ct_eff = None

    chk = g.get('checklist_min_hits', {})
    chk_default = chk.get('default')
    chk_relax = chk.get('allow_3_if')

    cool = g.get('cooldown_minutes', {})
    cool_base = cool.get('base')
    cool_min = cool.get('min')
    cool_max = cool.get('max')

    trial = g.get('trial_entry', {})
    trial_enabled = trial.get('enabled', False)
    trial_band = trial.get('confidence_band')
    trial_risk = trial.get('risk_budget_pct')

    lines = []
    lines.append(f"运行时策略覆盖（有效期至 {valid_until.isoformat().replace('+00:00','Z')}）")
    if ct_eff is not None:
        rng = []
        if ct_min is not None and ct_max is not None:
            rng = [f"区间 {ct_min}–{ct_max}"]
        lines.append(f"- confidence_threshold_base: {ct_eff} {' '.join(rng)}")
    if chk_default is not None:
        extra = f"；关键三项齐可放宽为3/7" if (chk_relax == 'key3_strong') else ''
        lines.append(f"- checklist_min_hits: 默认 {chk_default}/7{extra}")
    if cool_base is not None:
        rng = []
        if cool_min is not None and cool_max is not None:
            rng = [f"（下限 {cool_min}，上限 {cool_max}）"]
        lines.append(f"- cooldown_minutes: {cool_base} {' '.join(rng)}")
    if trial_enabled:
        lines.append(f"- trial_entry: 开启；band={trial_band}；risk_budget_pct={trial_risk}%")
    lines.append("- 注：BTC 门槛/防假突破/SLTP/清算距离/冷却下限等硬规则不变")

    return "\n".join(lines)


def main():
    path = sys.argv[1] if len(sys.argv) > 1 else 'config/policy_overrides.json'
    try:
        with open(path, 'r', encoding='utf-8') as f:
            data = json.load(f)
    except FileNotFoundError:
        print('', end='')
        return
    header = build_header(data)
    print(header)

if __name__ == '__main__':
    main()
