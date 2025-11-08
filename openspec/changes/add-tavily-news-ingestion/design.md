# Design Notes: Tavily News Ingestion

## Data Model
- `news_items(symbol TEXT, title TEXT, url TEXT UNIQUE, source TEXT, published_at DATETIME NULL, summary TEXT, fetched_at DATETIME NOT NULL)`
- Keep 5 per symbol by deleting older rows after inserts.

## Fetch Strategy
- Query per symbol; initial minimal query template. Future: per-coin canonical names, aliases, and site filters.
- Backoff on 4xx/5xx; timeout 5s; concurrency <=4; add +/- 30s jitter.

## Prompt Injection
- Include only evaluated symbol to control token budget. Future: allow `candidate_coins` injection if requested.
- Cap N=3; omit summary by default.

## Security
- Read `TAVILY_API_KEY` from env; log redacted key prefix for diagnostics; do not persist plaintext.
- Consider adding encrypted storage later via existing `crypto` service if needed.

## Extensibility
- Interface `NewsProvider` (Search(symbol) ([]Item, error)) to allow future providers.
- `news` package under backend with provider + worker; keep package import‑acyclic.

