## ADDED Requirements

### Requirement: Trade Telemetry Logging (Intraday)
The system SHALL record a JSONL log entry on every decision tick capturing features, risk state, and preliminary decision rationale using UTC timestamps.

#### Scenario: Tick Logging Success
- WHEN the decision loop runs a scan tick
- THEN append one JSON object to `logs/trading/YYYY-MM-DD-trade-log.jsonl` (UTC day)
- AND include multi-timeframe features (5m/15m/1h/4h), BTC state, checklist hits, fake-breakout flags
- AND include risk context (cooldown flag, sharpe band, loss streak, margin used %, open positions)

### Requirement: Outcome Labeling Jobs
The system SHALL emit delayed labels at T+5m, T+15m, T+30m, and T+60m measuring directional return, max favorable/adverse excursion, and SL/TP hits.

#### Scenario: Label Emission
- WHEN a position is opened or a decision tick is recorded
- THEN schedule label computations at T+5/15/30/60 minutes
- AND append results to `logs/trading/YYYY-MM-DD-trade-labels.jsonl` (UTC day)
- AND include symbol, open timestamp, horizon, realized stats, and linkage back to the source tick id

### Requirement: Schema Validation And Rotation
The system SHALL validate log/label schema and rotate files by UTC day (or hour optionally) without data loss.

#### Scenario: Rotation Boundary
- GIVEN current time is close to 00:00:00 UTC
- WHEN writing a new record past the boundary
- THEN start a new file for the new day and continue writing seamlessly

