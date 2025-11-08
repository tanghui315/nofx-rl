# Proposal: Add Tavily-backed Coin News Ingestion and Prompt Injection

Owner: core/backend
Status: Draft
Change-ID: add-tavily-news-ingestion
Created: 2025-11-08

## Summary
Add a lightweight background service in the Go backend to periodically collect the latest crypto news for configured symbols (default: `system_config.default_coins`) via Tavily Search API, cache the latest 5 items per symbol, and (optionally) inject a trimmed subset into the LLM decision context when enabled per-trader.

This proposal keeps scope minimal: one background worker, one simple cache table, environment-driven API key, and an opt-in per-trader switch to include news context.

## Goals
- Pull news every 15 minutes (configurable) for symbols in `default_coins`.
- Store at most the latest 5 articles per symbol (dedupe by URL), with title/url/source/published_at/summary.
- Provide a per-trader toggle (e.g., `include_news`) to inject the 3 most recent items for the evaluated symbol into the decision prompt.

## Non-Goals (now)
- Full‑text search UI, ranking, or advanced summarization.
- Multi‑provider aggregation; we start with Tavily only.
- Persisting large history; we keep only the latest 5 per symbol.

## Rationale
- Market news materially impacts short‑term behavior; a small, recent context improves LLM decisions.
- Tavily provides a simple search API and abstracts sources; a minimal integration is low-risk.

## Security
- Read Tavily API key from env `TAVILY_API_KEY` (no plaintext in DB initially). Optionally later: store encrypted via existing crypto service.
- Network calls are backend‑initiated; follow existing HTTP client timeouts.

## Rollout
- Behind per-trader toggle `include_news` (default false) and global interval/config defaults.
- Safe to run with no API key (service no-op with warnings).

## Alternatives considered
- Real‑time RSS per source: higher maintenance, source‑specific parsing.
- Use LLM web browsing in‑prompt: latency and cost spike; worse control.

## Risks
- Prompt bloat: mitigate via 3-item cap + short fields.
- Rate limits: limit concurrency and add jitter; backoff on failures.
