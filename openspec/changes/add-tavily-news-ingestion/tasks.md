# Tasks: add-tavily-news-ingestion

1) DB schema
 - Add table `news_items` with indexes (migration in config.Database).
 - Add CRUD helpers: InsertOrReplaceByURL(symbol, item), ListBySymbol(symbol, limit), TrimOld(symbol, keep=5).

2) Tavily client (package `news/tavily`)
 - Env config: `TAVILY_API_KEY`, base URL, timeouts.
 - API: SearchNews(symbol) -> []Item {title,url,source,published_at,summary}.
 - Map symbol to search query (e.g., `"BTC OR Bitcoin site:coindesk.com OR cointelegraph.com"` minimal first).

3) Worker
 - Start on boot if key present.
 - Schedule every `news_interval_minutes` (default 15) with jitter.
 - For each symbol in `default_coins`, fetch, upsert, trim to 5.
 - Concurrency semaphore (default 4), backoff & logging.

4) API (optional)
 - GET `/api/news?symbol=...&limit=...` for debugging/inspection.

5) Trader config & prompt wiring
 - Add `include_news` (bool) to TraderRecord + API + UI.
 - Decision context builder to load top `news_max_items` for the evaluated symbol and include minimal fields.

6) Docs
 - Add `.env.example` note for `TAVILY_API_KEY`.
 - Add README section for the feature and troubleshooting.

7) Validation
 - Manual: set API key, run, verify periodic logs and DB rows per symbol.
 - Unit: small tests for database trim/dedupe helpers.

