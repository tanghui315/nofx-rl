#!/usr/bin/env python3
"""
External validator (Phase 4 stub)

Validates an LLM decision against combined parameters (defaults + overrides with TTL) and
enforces trial-entry downsizing and hard constraints (limits we can check outside prompt).

Input: single JSON via stdin with shape:
{
  "decision": {"action":"open_long|open_short|hold|wait|close_long|close_short",
                "confidence": 79,
                "stop_loss": 98.0,
                "take_profit": 106.0,
                "position_size_usd": 300.0,
                "risk_budget_pct": 1.5},
  "context": {"checklist_hits": 3, "has_key3": true, "confidence": 79,
              "account": {"total_equity": 1000.0, "margin_used_pct": 50.0, "positions_count": 2}},
  "defaults": {"confidence_threshold_base": 80, "checklist_min_hits": 4,
               "cooldown_minutes": 9, "max_positions": 5, "max_margin_used_pct": 80.0},
  "overrides": { ... same shape as config/policy_overrides.json or omit }
}

Output: JSON to stdout
{
  "accepted": true|false,
  "decision": {... possibly adjusted ...},
  "meta": {"policy_overrides_applied": true|false, "trial_entry": true|false, "reasons": [..]}
}

Notes:
- This is a minimal, opinionated implementation to demonstrate enforcement. Extend as needed.
- We do not check BTC gate or fake-breakout here (needs richer feature context in Go core).
"""
import sys, json, math
from datetime import datetime, timezone, timedelta
from typing import Any, Dict, Optional


def parse_iso8601(ts: str) -> Optional[datetime]:
    try:
        return datetime.fromisoformat(ts.replace('Z', '+00:00'))
    except Exception:
        return None


def clamp(v: Optional[float], lo: Optional[float], hi: Optional[float]) -> Optional[float]:
    if v is None:
        return None
    x = v
    if lo is not None and x < lo:
        x = lo
    if hi is not None and x > hi:
        x = hi
    return x


def parse_band(band: Optional[str]) -> Optional[tuple]:
    if not band:
        return None
    try:
        parts = band.split('..')
        return (float(parts[0]), float(parts[1]))
    except Exception:
        return None


def load_overrides(ov: Optional[Dict[str, Any]]) -> (Dict[str, Any], bool):
    if not ov:
        return ({}, False)
    meta = ov.get('meta', {})
    gen = parse_iso8601(meta.get('generated_at', ''))
    ttl = meta.get('ttl_hours', None)
    if not gen or ttl is None:
        return ({}, False)
    now = datetime.now(timezone.utc)
    if (now - gen).total_seconds() > float(ttl) * 3600.0:
        return ({}, False)
    return (ov.get('global', {}), True)


def is_open_action(a: str) -> bool:
    return a in ("open_long", "open_short")


def is_close_action(a: str) -> bool:
    return a in ("close_long", "close_short")


def main():
    payload = json.load(sys.stdin)
    decision = dict(payload.get('decision', {}))
    context = payload.get('context', {})
    defaults = payload.get('defaults', {})
    ov_global, ov_applied = load_overrides(payload.get('overrides'))

    reasons = []
    meta = {"policy_overrides_applied": ov_applied, "trial_entry": False, "reasons": reasons}

    # Effective params
    threshold = float(defaults.get('confidence_threshold_base', 80))
    # override threshold
    ctb = ov_global.get('confidence_threshold_base', {}) if ov_applied else {}
    if ctb:
        v = ctb.get('value')
        mn = ctb.get('min')
        mx = ctb.get('max')
        v = clamp(float(v), float(mn) if mn is not None else None, float(mx) if mx is not None else None)
        if v is not None:
            threshold = float(v)

    checklist_min = int(defaults.get('checklist_min_hits', 4))
    allow_3_if_key3 = False
    chk = ov_global.get('checklist_min_hits', {}) if ov_applied else {}
    if isinstance(chk, dict):
        checklist_min = int(chk.get('default', checklist_min))
        allow_3_if_key3 = (chk.get('allow_3_if') == 'key3_strong')

    cooldown_min = float(defaults.get('cooldown_minutes', 9))
    cool = ov_global.get('cooldown_minutes', {}) if ov_applied else {}
    if isinstance(cool, dict) and cool.get('base') is not None:
        base = float(cool.get('base'))
        lo = float(cool.get('min')) if cool.get('min') is not None else None
        hi = float(cool.get('max')) if cool.get('max') is not None else None
        cooldown_min = clamp(base, lo, hi) or cooldown_min

    trial_cfg = ov_global.get('trial_entry', {}) if ov_applied else {}
    trial_enabled = bool(trial_cfg.get('enabled', False))
    band = parse_band(trial_cfg.get('confidence_band'))
    trial_risk_cap = float(trial_cfg.get('risk_budget_pct', 0.5))  # percent

    # Context
    conf = float(context.get('confidence', decision.get('confidence', 0.0)))
    hits = int(context.get('checklist_hits', 0))
    has_key3 = bool(context.get('has_key3', False))
    account = context.get('account', {})
    max_positions = int(defaults.get('max_positions', 5))
    max_margin_used = float(defaults.get('max_margin_used_pct', 80.0))

    # Basic account caps
    pos_n = int(account.get('positions_count', 0))
    mar = float(account.get('margin_used_pct', 0.0))
    if pos_n >= max_positions:
        reasons.append(f"positions_count {pos_n} >= max {max_positions}")
        print(json.dumps({"accepted": False, "decision": decision, "meta": meta}, ensure_ascii=False))
        return
    if mar >= max_margin_used:
        reasons.append(f"margin_used_pct {mar}% >= max {max_margin_used}%")
        print(json.dumps({"accepted": False, "decision": decision, "meta": meta}, ensure_ascii=False))
        return

    # Checklist enforcement (with conditional relax)
    required_hits = checklist_min
    if allow_3_if_key3 and has_key3 and checklist_min >= 4:
        required_hits = 3
    if hits < required_hits and is_open_action(decision.get('action', '')):
        reasons.append(f"checklist hits {hits} < required {required_hits}")
        print(json.dumps({"accepted": False, "decision": decision, "meta": meta}, ensure_ascii=False))
        return

    # Confidence / trial-entry enforcement
    eff_threshold = threshold
    acceptable = conf >= eff_threshold
    trial_ok = False
    if not acceptable and trial_enabled and band and has_key3 and is_open_action(decision.get('action','')):
        lo, hi = band
        if lo <= conf <= hi:
            trial_ok = True
            meta["trial_entry"] = True
    if not acceptable and not trial_ok and is_open_action(decision.get('action','')):
        reasons.append(f"confidence {conf} < threshold {eff_threshold}")
        print(json.dumps({"accepted": False, "decision": decision, "meta": meta}, ensure_ascii=False))
        return

    # Enforce stop loss / take profit for open actions
    if is_open_action(decision.get('action','')):
        if decision.get('stop_loss') is None or decision.get('take_profit') is None:
            reasons.append("missing stop_loss or take_profit")
            print(json.dumps({"accepted": False, "decision": decision, "meta": meta}, ensure_ascii=False))
            return

    # Trial downsizing of risk_budget_pct if present
    if meta["trial_entry"]:
        rb = decision.get('risk_budget_pct')
        if rb is None or float(rb) > trial_risk_cap:
            decision['risk_budget_pct'] = trial_risk_cap
            reasons.append(f"trial downsized risk_budget_pct to {trial_risk_cap}%")

    print(json.dumps({"accepted": True, "decision": decision, "meta": meta}, ensure_ascii=False))


if __name__ == '__main__':
    main()

