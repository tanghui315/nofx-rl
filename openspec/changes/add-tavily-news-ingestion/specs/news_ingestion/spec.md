# Capability: News Ingestion Service (Tavily)

## ADDED Requirements
- The backend SHALL start a background news ingestion worker on service boot.
- The worker SHALL read `system_config.default_coins` and schedule queries for each symbol.
- The worker SHALL query Tavily Search API using env `TAVILY_API_KEY`.
- The worker SHOULD refresh every 15 minutes by default; interval configurable via `system_config.news_interval_minutes` (int, default 15).
- The service SHALL store per-symbol up to the latest 5 items (drop older on insert), with fields: `symbol, title, url, source, published_at, summary, fetched_at`.
- The service SHALL deduplicate by URL per symbol.
- The service SHALL backoff and log on API failure; it MUST NOT crash the process.
- The service SHOULD bound concurrent requests (default 4) and add jitter to avoid bursts.

## ADDED Storage
- Table `news_items` (SQLite):
  - `id INTEGER PRIMARY KEY`, `symbol TEXT`, `title TEXT`, `url TEXT UNIQUE`, `source TEXT`, `published_at DATETIME NULL`, `summary TEXT`, `fetched_at DATETIME NOT NULL`.
  - Index: `idx_news_symbol_fetched` on (symbol, fetched_at DESC).

## ADDED Configuration
- `system_config.news_interval_minutes` (int, default 15)
- `TAVILY_API_KEY` (env) — if unset, worker logs a warning and remains no-op.

## ADDED API (optional/dev)
- GET `/api/news?symbol=ETHUSDT&limit=5` — return cached items (for debug/UI).

## ADDED Trader Option
- Field `include_news` (bool, default false) on trader config. When true, decision context SHALL include top N (default 3) news items for the evaluated symbol.

## Constraints
- Decision prompt injection MUST keep total token budget controlled; include only `title`, `source`, `published_at`, and `url` by default (omit long summaries unless explicitly enabled later).
- Symbols are normalized via existing `market.Normalize()`.

#### Scenario: Worker boot without API key
Given TAVILY_API_KEY is empty,
When the backend boots,
Then the worker logs a warning and does not schedule fetches.

#### Scenario: Insert 7 items for BTCUSDT, keep latest 5
Given 7 items are fetched for BTCUSDT,
When inserted,
Then only the 5 most recent (by published_at or fetched_at as fallback) remain.

#### Scenario: Trader with include_news=true
Given a trader with include_news=true,
When building decision context for ETHUSDT,
Then the latest up to 3 news items for ETHUSDT are included in the LLM prompt JSON context.
