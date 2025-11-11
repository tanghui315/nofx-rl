# Tasks

## Phase 1: Telemetry (Owner: Backend)
- [x] Define JSONL schemas for logs and labels (UTC roll, per-day or per-hour files)
- [x] Implement tick logging (features/risk/checklist/results) at every scan tick
- [x] Implement T+5/15/30/60 label jobs; write labels JSONL
- [x] Add config for data paths and rotation; add basic schema validation
- [x] Verify with a dry-run: N ticks recorded with valid JSON; labels appear after delay

## Phase 2: Scout Analysis (Owner: Data)
- [ ] Implement intraday regime detection (ATR percentile, BB width, breakout/fake ratio)
- [x] Implement template mining (restricted combos) with metrics (support, winrate, RR, Sharpe, uplift)
- [x] Add statistical guards (Wilson lower bound, FDR<=10%, min support >=80)
- [x] Output templates to `analytics/opportunity_templates.json` + human report `analytics/reports/*.md`
- [x] Add micro refresh (3m) to detect "key3" hit and emit short-lived recommendation

## Phase 3: Policy Overrides (Owner: Backend)
- [x] Define `config/policy_overrides.json` schema (meta.ttl, global, regimes, symbols)
- [x] Implement override loader and TTL check; clamp values to bounds
- [x] Generate runtime override header text from current valid overrides
- [x] Ensure only soft params are overridable (confidence threshold, checklist relax-if-key3, cooldown min/max, trial_entry)

## Phase 4: Runtime Integration (Owner: Backend)
- [x] Inject override header at call time (no prompt file edits)
- [x] Implement external validator applying combined params (default+override)
- [x] Enforce trial-entry downsized risk; reject actions violating hard rules
- [ ] Add audit fields to decision JSON: `meta.policy_overrides_applied`, `meta.trial_entry`

## Phase 5: Observability & Rollout (Owner: DevOps)
- [x] Metrics: candidate recall, open rate, winrate, RR, Sharpe, MaxDD, refusal reasons TopN (summary stub)
- [ ] Gray release limits: per-day new opens cap, risk budget down 30% first week
- [ ] AB/Replay harness for moderate vs moderate+overrides
- [x] Runbook for quick disable/rollback (remove overrides or let TTL expire)

Validation
- [x] Dry-run end-to-end on recorded data (no live orders) producing report + overrides
- [ ] Live shadow mode: inject headers but keep external validator in "observe only" for 24–48h
- [ ] Promote to active once KPIs stable; keep TTL <= 6h for first week
