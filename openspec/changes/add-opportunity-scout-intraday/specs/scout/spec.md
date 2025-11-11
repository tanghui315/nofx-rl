## ADDED Requirements

### Requirement: Opportunity Template Generation (Intraday)
The system SHALL analyze the last 24–72 hours of intraday data with time-decay weights and produce opportunity templates with metrics and significance checks.

#### Scenario: Template Output
- WHEN the main refresh runs (every 30–60 minutes)
- THEN write `analytics/opportunity_templates.json`
- AND each template SHALL include conditions (e.g., `tf_agree_min`, `vol_z_min`, `oi_chg_15m_min`, `funding_abs_max`), metrics (support, winrate, RR, Sharpe, uplift), and validity window (TTL hours)

### Requirement: Statistical Guardrails
The system MUST apply minimum support, Wilson lower bound uplift over baseline, and FDR (BH) <= 10% before recommending a template.

#### Scenario: Template Rejection
- GIVEN a candidate template with support < 80 or winrate lower-bound not above baseline by 5%
- WHEN evaluating it
- THEN it SHALL NOT be emitted as a valid template

### Requirement: Micro Refresh (Key Conditions)
The system SHALL run a micro refresh every 3 minutes to detect "key-three" confirmation (trend resonance + volume + BTC support) and emit a short-lived recommendation.

#### Scenario: Short-Lived Recommendation
- WHEN key-three conditions are met during micro refresh
- THEN emit a recommendation object with TTL 6–12 minutes
- AND make it available for runtime override header injection

