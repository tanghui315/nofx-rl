## Context
Intraday (minutes/hours, 24/7) decision loop is constrained by strict prompts. We add a Scout to mine recent data for high-value patterns and a bounded, reversible override layer to slightly adjust soft parameters.

## Goals / Non-Goals
- Goals: Minimal, interpretable analytics; hard guardrails preserved; reversible overrides; intraday cadence
- Non-Goals: Complex ML; changing hard risk logic; external dependencies beyond local scripts

## Decisions
- Data format: JSONL, UTC roll, minimal fields per docs/opportunity-scout.md
- Cadence: micro 3m (override header TTL 6–12m); main 30–60m (override TTL 2–6h)
- Overrides: JSON file with TTL; applied at runtime via header + external validator; only soft params allowed
- Safety: bounds, TTL, min support, Wilson lower bound, FDR control, audit trail

## Risks / Trade-offs
- Overfitting: mitigated via sample minimums, uplift vs baseline, time-decay weights, FDR
- Drift: frequent refresh and TTL ensure rapid reversion
- Complexity creep: start rules-first; consider LLM only for report text later

## Migration Plan
1) Ship telemetry + labels without changing decisions
2) Add Scout reports; manual review
3) Wire overrides in observe mode
4) Enable overrides with tight bounds + TTL + gray limits

## Open Questions
- Template conflict resolution policy (default: prefer conservative; sort by support*uplist)
- Symbol-level overrides granularity (start global/regime-level; expand later)

