## ADDED Requirements

### Requirement: Runtime Override Header Injection
The system SHALL prepend a short "override header" to the LLM system prompt per decision when a valid overrides file or short-lived recommendation is present, without editing the base prompt files.

#### Scenario: Header Applied
- GIVEN a non-expired override lowering `confidence_threshold_base` by 1 and allowing `3/7 if key3`
- WHEN composing the prompt for this decision
- THEN include a human-readable override header with the effective values and TTL note

### Requirement: External Hard Validation
The system SHALL validate the LLM output against the combined (default + override) parameters and enforce trial-entry downsizing and hard constraints.

#### Scenario: Enforcement
- WHEN the LLM proposes an open trade with confidence below the effective threshold
- THEN the validator SHALL reject the action (choose `wait`) or downsize per trial-entry if applicable

### Requirement: UTC Timestamps And Audit Flags
The system SHALL use UTC timestamps in override headers and decision outputs and record audit flags.

#### Scenario: Audit Recorded
- WHEN an override header was applied for a decision
- THEN the decision JSON includes `meta.policy_overrides_applied=true` and `meta.trial_entry` if used

