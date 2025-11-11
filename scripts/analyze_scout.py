#!/usr/bin/env python3
"""
Intraday Scout analysis (Phase 2 stub).

Reads logs/trading/*-trade-log.jsonl and *-trade-labels.jsonl within a lookback window,
computes baseline metrics and evaluates a simple "key-three" template:
  - tf_agree_min >= 3 (approximated; if features.tf_agree_min exists, else 0)
  - vol_z >= 1.5 (if present; else 0)
  - BTC support present (features.btc_state non-empty)

Outputs:
  - analytics/opportunity_templates.json    (array; may be empty if no significant support)
  - analytics/reports/scout-YYYYMMDD_HHMM.md (human-readable summary)

This is a minimal, interpretable baseline to be extended later.
"""
import os
import sys
import json
import glob
import math
import random
import argparse
from datetime import datetime, timezone
from typing import Dict, Any, List, Tuple


def load_jsonl(path: str) -> List[Dict[str, Any]]:
    out = []
    try:
        with open(path, 'r', encoding='utf-8') as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    out.append(json.loads(line))
                except Exception:
                    continue
    except FileNotFoundError:
        pass
    return out


def parse_iso(ts: str) -> datetime:
    try:
        return datetime.fromisoformat(ts.replace('Z', '+00:00'))
    except Exception:
        return datetime.now(timezone.utc)


def within(d: datetime, now: datetime, hours: int) -> bool:
    return (now - d).total_seconds() <= hours * 3600


def rr_mean(returns: List[float]) -> float:
    pos = [r for r in returns if r > 0]
    neg = [-r for r in returns if r < 0]
    if not pos or not neg:
        return float('nan')
    return (sum(pos) / len(pos)) / (sum(neg) / len(neg))


def sharpe(returns: List[float]) -> float:
    if not returns:
        return float('nan')
    m = sum(returns) / len(returns)
    var = sum((r - m) ** 2 for r in returns) / len(returns) if len(returns) > 0 else 0.0
    sd = math.sqrt(var) if var > 0 else 0.0
    if sd == 0:
        return float('nan')
    return m / sd


def normal_cdf(x: float) -> float:
    # Using error function approximation
    # CDF of standard normal = 0.5*(1+erf(x/sqrt(2)))
    return 0.5 * (1.0 + math.erf(x / math.sqrt(2.0)))


def two_proportion_z_test(x1: int, n1: int, x2: int, n2: int) -> float:
    """One-sided (p1 > p2) z-test p-value for two proportions. Returns NaN if invalid."""
    if n1 <= 0 or n2 <= 0:
        return float('nan')
    p1 = x1 / n1
    p2 = x2 / n2
    p_pool = (x1 + x2) / (n1 + n2)
    denom = math.sqrt(max(1e-12, p_pool * (1 - p_pool) * (1 / n1 + 1 / n2)))
    if denom == 0:
        return float('nan')
    z = (p1 - p2) / denom
    # one-sided p (greater)
    p = 1.0 - normal_cdf(z)
    return max(0.0, min(1.0, p))


def permutation_p_value(group_a: List[float], group_b: List[float], iters: int = 500) -> float:
    """One-sided permutation test p-value for mean difference (mean(a) > mean(b))."""
    if len(group_a) == 0 or len(group_b) == 0:
        return float('nan')
    # observed diff
    obs = (sum(group_a) / len(group_a)) - (sum(group_b) / len(group_b))
    pooled = group_a + group_b
    count = 0
    for _ in range(max(1, iters)):
        random.shuffle(pooled)
        a = pooled[:len(group_a)]
        b = pooled[len(group_a):]
        diff = (sum(a) / len(a)) - (sum(b) / len(b))
        if diff >= obs - 1e-18:
            count += 1
    # add-one smoothing
    p = (count + 1) / (iters + 1)
    return max(0.0, min(1.0, p))


def bh_fdr(pairs: List[Tuple[str, float]]) -> Dict[str, float]:
    """Benjamini–Hochberg FDR correction. Returns map key->qvalue."""
    m = len(pairs)
    if m == 0:
        return {}
    # sort ascending by p
    sorted_pairs = sorted([(k, p) for k, p in pairs], key=lambda x: (float('inf') if math.isnan(x[1]) else x[1]))
    qvals = {}
    prev = 1.0
    for i, (k, p) in enumerate(reversed(sorted_pairs), start=1):
        pi = (float('inf') if math.isnan(p) else p)
        q = min(prev, (pi * m) / (m - i + 1))
        qvals[k] = q
        prev = q
    return qvals


def key3_hit(features: Dict[str, Any]) -> bool:
    tf = features.get('tf_agree_min')
    if tf is None:
        tf = features.get('tf_agree_count')
    if tf is None:
        tf = 0
    vol_z = features.get('vol_z', 0)
    btc = features.get('btc_state', {})
    btc_ok = isinstance(btc, dict) and len(btc) > 0
    return (tf >= 3) and (vol_z >= 1.5) and btc_ok


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--hours', type=int, default=48, help='lookback window in hours')
    ap.add_argument('--horizon', type=int, default=30, choices=[5,15,30,60,180], help='label horizon in minutes')
    ap.add_argument('--perm-n', type=int, default=500, help='permutation iterations for returns p-value')
    ap.add_argument('--alpha', type=float, default=0.05, help='significance level for single test (used in reporting)')
    ap.add_argument('--fdr', type=float, default=0.10, help='BH FDR threshold for multiple tests (q)')
    ap.add_argument('--min-support', type=int, default=80, help='min support for global template')
    ap.add_argument('--min-support-symbol', type=int, default=100, help='min support per symbol template')
    args = ap.parse_args()

    now = datetime.now(timezone.utc)

    # Load logs and labels
    logs = []
    labels = []
    for p in glob.glob(os.path.join('logs', 'trading', '*-trade-log.jsonl')):
        logs.extend(load_jsonl(p))
    for p in glob.glob(os.path.join('logs', 'trading', '*-trade-labels.jsonl')):
        labels.extend(load_jsonl(p))

    # Filter by hours
    logs = [x for x in logs if within(parse_iso(x.get('ts','')), now, args.hours)]
    labels = [x for x in labels if within(parse_iso(x.get('source_ts','')), now, args.hours) and x.get('horizon_min') == args.horizon]

    # Index labels by (source_ts, symbol)
    lab_ix: Dict[Tuple[str,str], Dict[str, Any]] = {}
    for l in labels:
        k = (l.get('source_ts'), l.get('symbol'))
        lab_ix[k] = l

    # Build baseline series (global and by symbol)
    base_returns: List[float] = []
    base_by_symbol: Dict[str, List[float]] = {}
    for l in labels:
        r = l.get('directional_return')
        if isinstance(r, (int, float)):
            rv = float(r)
            base_returns.append(rv)
            sym = l.get('symbol')
            if isinstance(sym, str) and sym:
                base_by_symbol.setdefault(sym, []).append(rv)

    base_support = len(base_returns)
    base_win = sum(1 for r in base_returns if r > 0)
    base_winrate = (base_win / base_support) if base_support else float('nan')
    base_rr = rr_mean(base_returns)
    base_sharpe = sharpe(base_returns)

    # Evaluate key-three template (global)
    tpl_rets: List[float] = []
    tpl_support = 0
    for t in logs:
        if not key3_hit(t.get('features', {})):
            continue
        k = (t.get('ts'), t.get('symbol'))
        l = lab_ix.get(k)
        if not l:
            continue
        r = l.get('directional_return')
        if isinstance(r, (int, float)):
            tpl_rets.append(float(r))
            tpl_support += 1

    tpl_win = sum(1 for r in tpl_rets if r > 0)
    tpl_winrate = (tpl_win / tpl_support) if tpl_support else float('nan')
    tpl_rr = rr_mean(tpl_rets)
    tpl_sharpe = sharpe(tpl_rets)
    uplift = (tpl_winrate - base_winrate) if (not math.isnan(tpl_winrate) and not math.isnan(base_winrate)) else float('nan')
    # p-values
    p_ret = permutation_p_value(tpl_rets, base_returns, iters=args.perm_n) if tpl_support >= 30 and base_support >= 30 else float('nan')
    p_wr = two_proportion_z_test(tpl_win, tpl_support, base_win, base_support) if tpl_support >= 30 and base_support >= 30 else float('nan')
    p_items: List[Tuple[str, float]] = []
    if not math.isnan(p_ret):
        p_items.append(("global:ret", p_ret))
    if not math.isnan(p_wr):
        p_items.append(("global:wr", p_wr))

    os.makedirs('analytics', exist_ok=True)

    # Write templates file
    templates = []
    # To be filled after computing q-values
    global_candidate = {
        "regime": "trending",
        "template_id": "key3_basic_v1",
        "if": {"tf_agree_min": 3, "vol_z_min": 1.5, "funding_abs_max": 0.005},
        "metrics": {
            "support": tpl_support,
            "winrate": round(tpl_winrate, 4) if not math.isnan(tpl_winrate) else None,
            "rr_mean": round(tpl_rr, 4) if not math.isnan(tpl_rr) else None,
            "sharpe": round(tpl_sharpe, 4) if not math.isnan(tpl_sharpe) else None,
            "uplift_vs_baseline": round(uplift, 4) if not math.isnan(uplift) else None,
            "p_ret": round(p_ret, 6) if not math.isnan(p_ret) else None,
            "p_wr": round(p_wr, 6) if not math.isnan(p_wr) else None,
            "baseline": {
                "support": base_support,
                "winrate": round(base_winrate, 4) if not math.isnan(base_winrate) else None,
                "rr_mean": round(base_rr, 4) if not math.isnan(base_rr) else None,
                "sharpe": round(base_sharpe, 4) if not math.isnan(base_sharpe) else None
            }
        },
        "ttl_hours": 4
    }

    # Per-symbol evaluation (collect p-values first)
    tpl_rets_by_symbol: Dict[str, List[float]] = {}
    for t in logs:
        if not key3_hit(t.get('features', {})):
            continue
        sym = t.get('symbol')
        if not isinstance(sym, str) or not sym:
            continue
        k = (t.get('ts'), sym)
        l = lab_ix.get(k)
        if not l:
            continue
        r = l.get('directional_return')
        if isinstance(r, (int, float)):
            tpl_rets_by_symbol.setdefault(sym, []).append(float(r))

    per_symbol_candidates = {}
    for sym, rets in tpl_rets_by_symbol.items():
        tpl_sup = len(rets)
        base_rets = base_by_symbol.get(sym, [])
        b_sup = len(base_rets)
        if tpl_sup == 0 or b_sup == 0:
            continue
        tpl_wr = (sum(1 for r in rets if r > 0) / tpl_sup)
        b_wr = (sum(1 for r in base_rets if r > 0) / b_sup)
        up = (tpl_wr - b_wr)
        tpl_rrs = rr_mean(rets)
        tpl_sh = sharpe(rets)
        b_sh = sharpe(base_rets)
        # p-values
        p_r = permutation_p_value(rets, base_rets, iters=args.perm_n) if tpl_sup >= 30 and b_sup >= 30 else float('nan')
        p_w = two_proportion_z_test(int(tpl_wr*tpl_sup), tpl_sup, int(b_wr*b_sup), b_sup) if tpl_sup >= 30 and b_sup >= 30 else float('nan')
        if not math.isnan(p_r):
            p_items.append((f"sym:{sym}:ret", p_r))
        if not math.isnan(p_w):
            p_items.append((f"sym:{sym}:wr", p_w))

        per_symbol_candidates[sym] = {
            "regime": "trending",
            "symbol": sym,
            "template_id": "key3_basic_v1",
            "if": {"tf_agree_min": 3, "vol_z_min": 1.5, "funding_abs_max": 0.005},
            "metrics": {
                "support": tpl_sup,
                "winrate": round(tpl_wr, 4),
                "rr_mean": round(tpl_rrs, 4) if not math.isnan(tpl_rrs) else None,
                "sharpe": round(tpl_sh, 4) if not math.isnan(tpl_sh) else None,
                "uplift_vs_baseline": round(up, 4) if not math.isnan(up) else None,
                "p_ret": round(p_r, 6) if not math.isnan(p_r) else None,
                "p_wr": round(p_w, 6) if not math.isnan(p_w) else None,
                "baseline": {
                    "support": b_sup,
                    "winrate": round(b_wr, 4) if not math.isnan(b_wr) else None,
                    "rr_mean": round(rr_mean(base_rets), 4) if base_rets else None,
                    "sharpe": round(b_sh, 4) if not math.isnan(b_sh) else None
                }
            },
            "ttl_hours": 4
        }

    # FDR correction across all tests
    qmap = bh_fdr(p_items) if p_items else {}

    # Decide global
    q_global = min(qmap.get("global:ret", 1.0), qmap.get("global:wr", 1.0)) if qmap else 1.0
    significant = (
        (tpl_support >= args.min_support) and
        (not math.isnan(uplift) and uplift >= 0.05) and
        (not math.isnan(tpl_rr) and tpl_rr >= 2.0) and
        (not math.isnan(tpl_sharpe) and not math.isnan(base_sharpe) and (tpl_sharpe - base_sharpe) >= 0.1) and
        (q_global < args.fdr)
    )
    if significant:
        # attach q-values
        global_candidate["metrics"]["q_ret"] = round(qmap.get("global:ret", float('nan')), 6) if qmap else None
        global_candidate["metrics"]["q_wr"] = round(qmap.get("global:wr", float('nan')), 6) if qmap else None
        templates.append(global_candidate)
        templates.append({
            "regime": "trending",
            "template_id": "key3_basic_v1",
            "if": {"tf_agree_min": 3, "vol_z_min": 1.5, "funding_abs_max": 0.005},
            "metrics": {
                "support": tpl_support,
                "winrate": round(tpl_winrate, 4),
                "rr_mean": round(tpl_rr, 4) if not math.isnan(tpl_rr) else None,
                "sharpe": round(tpl_sharpe, 4) if not math.isnan(tpl_sharpe) else None,
                "uplift_vs_baseline": round(uplift, 4) if not math.isnan(uplift) else None,
                "baseline": {
                    "support": base_support,
                    "winrate": round(base_winrate, 4) if not math.isnan(base_winrate) else None,
                    "rr_mean": round(base_rr, 4) if not math.isnan(base_rr) else None,
                    "sharpe": round(base_sharpe, 4) if not math.isnan(base_sharpe) else None
                }
            },
            "ttl_hours": 4
        })

    # Per-symbol decisions using q-values
    for sym, cand in per_symbol_candidates.items():
        tpl_sup = cand["metrics"]["support"]
        up = cand["metrics"]["uplift_vs_baseline"]
        tpl_rrs = cand["metrics"]["rr_mean"] if cand["metrics"]["rr_mean"] is not None else float('nan')
        tpl_sh = cand["metrics"]["sharpe"] if cand["metrics"]["sharpe"] is not None else float('nan')
        b_sh = cand["metrics"]["baseline"]["sharpe"] if cand["metrics"]["baseline"]["sharpe"] is not None else float('nan')
        q_sym = min(bh_fdr([(f"sym:{sym}", min(qmap.get(f"sym:{sym}:ret", 1.0), qmap.get(f"sym:{sym}:wr", 1.0)))])[f"sym:{sym}"], 1.0) if qmap else 1.0
        sig = (tpl_sup >= args.min_support_symbol and (up is not None and up >= 0.05) and
               (not math.isnan(tpl_rrs) and tpl_rrs >= 2.0) and
               (not math.isnan(tpl_sh) and not math.isnan(b_sh) and (tpl_sh - b_sh) >= 0.1) and
               (q_sym < args.fdr))
        if sig:
            cand["metrics"]["q_ret"] = round(qmap.get(f"sym:{sym}:ret", float('nan')), 6) if qmap else None
            cand["metrics"]["q_wr"] = round(qmap.get(f"sym:{sym}:wr", float('nan')), 6) if qmap else None
            templates.append(cand)

    with open(os.path.join('analytics', 'opportunity_templates.json'), 'w', encoding='utf-8') as f:
        json.dump(templates, f, ensure_ascii=False, indent=2)

    # Write human report
    ts_tag = datetime.now(timezone.utc).strftime('%Y%m%d_%H%M')
    rep_path = os.path.join('analytics', 'reports')
    os.makedirs(rep_path, exist_ok=True)
    rep = []
    rep.append(f"# Scout Report ({ts_tag}Z)\n")
    rep.append(f"Lookback: {args.hours}h, Horizon: T+{args.horizon}m\n")
    rep.append("## Baseline\n")
    rep.append(f"- support: {base_support}\n- winrate: {round(base_winrate,4) if not math.isnan(base_winrate) else 'n/a'}\n- rr_mean: {round(base_rr,4) if not math.isnan(base_rr) else 'n/a'}\n- sharpe: {round(base_sharpe,4) if not math.isnan(base_sharpe) else 'n/a'}\n")
    rep.append("## Key3 Template (Global)\n")
    rep.append(f"- support: {tpl_support}\n- winrate: {round(tpl_winrate,4) if not math.isnan(tpl_winrate) else 'n/a'}\n- rr_mean: {round(tpl_rr,4) if not math.isnan(tpl_rr) else 'n/a'}\n- sharpe: {round(tpl_sharpe,4) if not math.isnan(tpl_sharpe) else 'n/a'}\n- uplift_vs_baseline: {round(uplift,4) if not math.isnan(uplift) else 'n/a'}\n")
    rep.append(f"- p_ret: {p_ret if not math.isnan(p_ret) else 'n/a'}, p_wr: {p_wr if not math.isnan(p_wr) else 'n/a'}\n")
    rep.append(f"- fdr_threshold: {args.fdr}, significant: {significant}\n")
    # Per-symbol summary
    if tpl_rets_by_symbol:
        rep.append("\n## Key3 Template (Per-Symbol)\n")
        for sym, rets in sorted(tpl_rets_by_symbol.items()):
            tpl_sup = len(rets)
            base_rets = base_by_symbol.get(sym, [])
            b_sup = len(base_rets)
            tpl_wr = (sum(1 for r in rets if r>0) / tpl_sup) if tpl_sup else float('nan')
            b_wr = (sum(1 for r in base_rets if r>0) / b_sup) if b_sup else float('nan')
            up = (tpl_wr - b_wr) if (not math.isnan(tpl_wr) and not math.isnan(b_wr)) else float('nan')
            q_ret = qmap.get(f"sym:{sym}:ret") if qmap else None
            q_wr = qmap.get(f"sym:{sym}:wr") if qmap else None
            rep.append(f"- {sym}: support={tpl_sup} vs base={b_sup}, wr={tpl_wr:.4f} up={up if not math.isnan(up) else 'n/a'}, q_ret={round(q_ret,6) if q_ret is not None else 'n/a'}, q_wr={round(q_wr,6) if q_wr is not None else 'n/a'}\n")
    with open(os.path.join(rep_path, f'scout-{ts_tag}.md'), 'w', encoding='utf-8') as f:
        f.write("\n".join(rep))

    print(f"templates: {len(templates)} written to analytics/opportunity_templates.json", file=sys.stderr)


if __name__ == '__main__':
    main()
