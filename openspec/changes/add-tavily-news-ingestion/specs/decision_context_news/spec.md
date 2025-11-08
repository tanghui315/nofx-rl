# Capability: Decision Context News Injection

## ADDED Requirements
- When `include_news=true` on the trader, the decision engine SHALL include a `news` array for the evaluated symbol in the LLM context map.
- The array SHALL contain up to `news_max_items` (default 3) recent items from `news_items` for that symbol.
- Each entry SHOULD include `{title, source, published_at, url}`; summary is OPTIONAL.
- The engine MUST NOT include news when no cache exists or the feature is disabled.

## ADDED Configuration
- `system_config.news_max_items` (int, default 3)

#### Scenario: Prompt size guard
Given include_news=true,
When the context exceeds a safe size,
Then the engine trims items to fit the `news_max_items` limit and excludes summaries by default.
