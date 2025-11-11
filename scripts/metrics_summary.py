#!/usr/bin/env python3
"""
Phase 5 stub: Observability metrics summary.

Reads logs/trading/*-trade-log.jsonl and *-trade-labels.jsonl, computes simple KPIs:
 - ticks_total
 - actions count by type
 - labels per horizon (support, winrate, rr_mean, sharpe)
 - refusal reasons TopN (if present in decision.reasons)

Outputs a markdown report to analytics/reports/metrics-YYYYMMDD_HHMM.md
"""
import os, glob, json, math
from collections import Counter, defaultdict
from datetime import datetime, timezone


def load_jsonl(path):
    out = []
    try:
        with open(path, 'r', encoding='utf-8') as f:
            for line in f:
                line=line.strip()
                if line:
                    try:
                        out.append(json.loads(line))
                    except Exception:
                        pass
    except FileNotFoundError:
        pass
    return out


def rr_mean(vals):
    pos=[v for v in vals if v>0]
    neg=[-v for v in vals if v<0]
    if not pos or not neg:
        return float('nan')
    return (sum(pos)/len(pos))/(sum(neg)/len(neg))


def sharpe(vals):
    if not vals:
        return float('nan')
    m=sum(vals)/len(vals)
    var=sum((v-m)**2 for v in vals)/len(vals)
    sd=math.sqrt(var) if var>0 else 0.0
    if sd==0:
        return float('nan')
    return m/sd


def main():
    logs=[]
    labels=[]
    for p in glob.glob(os.path.join('logs','trading','*-trade-log.jsonl')):
        logs.extend(load_jsonl(p))
    for p in glob.glob(os.path.join('logs','trading','*-trade-labels.jsonl')):
        labels.extend(load_jsonl(p))

    ticks_total=len(logs)
    actions=Counter()
    refusals=Counter()
    for x in logs:
        d=x.get('decision',{})
        a=d.get('action')
        if a:
            actions[a]+=1
        for r in d.get('reasons',[]) or []:
            refusals[r]+=1

    by_h = defaultdict(list)
    for l in labels:
        h=l.get('horizon_min')
        r=l.get('directional_return')
        if isinstance(r,(int,float)):
            by_h[h].append(float(r))

    ts=datetime.now(timezone.utc).strftime('%Y%m%d_%H%M')
    os.makedirs(os.path.join('analytics','reports'), exist_ok=True)
    out=os.path.join('analytics','reports', f'metrics-{ts}.md')
    with open(out,'w',encoding='utf-8') as f:
        f.write(f"# Metrics Summary ({ts}Z)\n\n")
        f.write(f"- ticks_total: {ticks_total}\n")
        f.write(f"- actions: {dict(actions)}\n")
        if refusals:
            f.write(f"- refusals_top: {refusals.most_common(10)}\n")
        f.write("\n## Labels KPIs\n")
        for h, vals in sorted(by_h.items()):
            sup=len(vals)
            win=sum(1 for v in vals if v>0)
            winrate=win/sup if sup else float('nan')
            f.write(f"- T+{h}m: support={sup}, winrate={winrate:.4f}, rr_mean={rr_mean(vals):.4f}, sharpe={sharpe(vals):.4f}\n")
    print(out)


if __name__=='__main__':
    main()

