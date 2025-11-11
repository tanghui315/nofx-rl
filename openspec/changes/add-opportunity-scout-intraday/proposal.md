# Change: Add Opportunity Scout (Intraday) And Policy Overrides

## Why
Opening frequency is too low under strict prompts. We need a data-driven, intraday "Scout" to surface high-quality opportunities and a safe, reversible way to adjust soft parameters without touching hard risk controls.

## What Changes
- Introduce trade telemetry logging (features + labels) for intraday data (minutes/hours)
- Add Scout analysis to produce opportunity templates and short-lived recommendations
- Add policy overrides (config/policy_overrides.json) with TTL and strict bounds
- Add runtime override injection and external hard validation layer (no prompt edits required)
- Document intraday cadence and guardrails (based on docs/opportunity-scout.md)

## Impact
- Affected specs: telemetry, scout, policy-overrides, runtime-injection
- Affected code (future):
  - Logging points in decision loop (tick and T+n label jobs)
  - Offline/periodic analysis jobs (3m micro refresh; 30–60m main refresh)
  - Override loader + header injector; external validator to enforce combined params
  - Report generation and basic dashboards

## Non-Goals
- Do not change hard risk rules (BTC gate, checklist, fake-breakout, SL/TP, liquidation distance, cooldown minimums)
- Do not ship complex ML; start with simple interpretable statistics

