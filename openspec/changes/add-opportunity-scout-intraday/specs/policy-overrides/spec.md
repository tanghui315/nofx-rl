## ADDED Requirements

### Requirement: Policy Overrides Schema And TTL
The system SHALL support a JSON overrides file at `config/policy_overrides.json` with fields: `meta.ttl_hours`, `global`, optional `regimes`, optional `symbols`, and SHALL treat overrides as expired beyond TTL.

#### Scenario: Expired Overrides
- GIVEN an overrides file with `meta.ttl_hours = 4` generated 6 hours ago
- WHEN loading overrides for the current decision
- THEN the system SHALL ignore the file and proceed with defaults

### Requirement: Soft Parameters Only
The system MUST restrict overrides to soft parameters: `confidence_threshold_base`, `checklist_min_hits` with conditional relax-if-key3, `cooldown_minutes` bounds, and `trial_entry` (enabled, confidence_band, risk_budget_pct). Hard constraints remain immutable.

#### Scenario: Hard Rule Attempt
- WHEN an overrides file attempts to modify a hard rule (e.g., fake-breakout block, BTC gate removal)
- THEN the loader SHALL reject the change and log a warning, continuing without applying that field

### Requirement: Bounds And Clamping
The system SHALL clamp each override to defined min/max bounds before use.

#### Scenario: Out-of-Range Value
- GIVEN `confidence_threshold_base.min=78` and `.max=85`
- WHEN an override sets value=76
- THEN the system SHALL clamp it to 78 and continue

