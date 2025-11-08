# Project Context

## Purpose
NOFX is an agentic trading operating system that closes the loop from multi‑agent decisioning → risk control → low‑latency execution → monitoring/backtesting. The repo currently ships a full crypto implementation (Binance Futures, Hyperliquid, Aster DEX) and a React dashboard. The same architecture is designed to generalize to stocks, futures, options, and forex.

Goals
- Provide a unified, exchange‑agnostic Trader interface for execution.
- Orchestrate LLM‑driven decision making with strict risk/routing rules.
- Offer a database‑backed configuration UI for models, exchanges, and traders.
- Keep deployment simple: single Go binary + SQLite; optional Docker.

Non‑Goals (current scope)
- Portfolio optimization across non‑crypto asset classes (planned).
- Distributed multi‑node execution and HA clustering.

## Tech Stack
- Backend: Go 1.25+, Gin, SQLite (modernc.org/sqlite), JWT auth, Logrus.
- Frontend: React 18, TypeScript 5, Vite, TailwindCSS, SWR, Zustand.
- Realtime/Market: WebSocket market monitor, custom indicator library.
- AI Providers: DeepSeek, Qwen, and custom OpenAI‑compatible endpoints.
- Packaging: Docker (multi‑stage; includes TA‑Lib runtime), PM2 dev runner.
- Scripts/Infra: docker‑compose, nginx (static proxy), Husky + lint‑staged.

Key repos paths
- Backend: `main.go`, `api/`, `config/`, `market/`, `decision/`, `trader/`, `manager/`, `auth/`, `logger/`.
- Frontend: `web/` (Vite), assets under `web/public`, UI in `web/src`.
- OpenSpec: `openspec/` with project conventions and future specs/changes.

## Project Conventions

### Code Style
- Go
  - Format with `go fmt`; vet with `go vet`. Prefer explicit error handling; wrap with `%w`.
  - Interfaces describe capabilities (e.g., `trader.Trader`). Keep packages cohesive and import‑acyclic.
  - Naming: exported types/functions use PascalCase; receivers short but meaningful (avoid single letters outside tight scopes).
- TypeScript/React
  - TS strict mode on (`web/tsconfig.json`). Prefer explicit interfaces/types for data shapes.
  - Lint with ESLint + Prettier (`npm run lint`, `npm run format`). JSX: functional components + hooks.
  - State: colocate local state; global state via Zustand stores in `web/src/contexts|lib`.
  - Styling: TailwindCSS utilities; keep components presentational and small.

### Architecture Patterns
- Layered modules with clear responsibilities:
  - API layer (`api/`): Gin HTTP server, CORS, JWT middleware, admin‑mode routes, health/config endpoints.
  - Config layer (`config/`): SQLite schema, system/trader/model/exchange persistence, migrations, triggers.
  - Market layer (`market/`): WebSocket monitor, kline aggregation, indicators (EMA, ADX, Bollinger, VWAP, Stochastic, OBV), anomaly detection, OI integration.
  - Decision layer (`decision/`): prompt construction, LLM call via `mcp/`, JSON parsing into structured decisions, risk constraints enforcement points.
  - Execution layer (`trader/`): exchange adapters behind `Trader` interface (Binance Futures, Hyperliquid, Aster) with precision/format helpers and risk‑aware order orchestration.
  - Management (`manager/`): load/start/stop traders, in‑memory registry per user.
  - Auth/Security (`auth/`, `crypto/`): JWT, optional 2FA, RSA‑AES hybrid storage encryption and audit logs.
- Configuration is DB‑driven; `config.json` bootstraps defaults into `system_config`. Admin mode requires `NOFX_ADMIN_PASSWORD`.
- Frontend talks to backend at `/api` (Vite proxy to `localhost:8080`). Polling via SWR (5–10s) for live stats.

### Testing Strategy
- Backend
  - Unit tests where practical (example: `crypto/encryption_test.go`). Aim to cover parsing, risk calculations, and exchange precision logic as they stabilize.
  - Manual smoke flows: build `nofx`, run, verify `GET /api/health`, create a trader via UI, observe positions endpoints.
- Frontend
  - Vitest + @testing-library for components with logic; prefer focused tests near components.
  - Lint/type checks in CI/local before PRs (`npm run lint`, `tsc --noEmit`).
- E2E (optional/dev)
  - Run backend and `web` dev server locally; confirm login/admin, model/exchange CRUD, and live charts.

### Git Workflow
- Branches
  - Work from `dev`. Feature branches: `feature/<name>`, fixes: `fix/<name>`, docs: `docs/<topic>`, etc.
  - Keep PRs focused (< ~300 LOC ideal). Rebase on latest `dev` before opening PRs.
- Commits
  - Conventional Commits: `feat(scope): ...`, `fix(trader): ...`, `docs(readme): ...`, `perf(ai): ...`, `refactor(core): ...`, `test: ...`, `chore|ci|security: ...`.
  - First line ≤ 72 chars; explain the what/why in body; link issues.
- Quality gates (before PR)
  - Backend builds (`go build`), `go test ./...` where present; frontend `npm run build`; `eslint`/`prettier` clean.

## Domain Context
- Problem space: real‑time derivatives trading with LLM‑assisted decisioning and strict guardrails.
- Exchanges: Binance USDM Futures, Hyperliquid DEX, Aster DEX. All normalized behind `trader.Trader`.
- Risk model highlights: cross/isolated margin toggle, per‑asset leverage caps (BTC/ETH vs altcoins), SL/TP enforcement, anti‑stacking, margin utilization ceilings.
- Data signals: 3m realtime + 4h trend klines, EMA/RSI/MACD, OI filters, optional external “coin pool” and “OI top” feeds.
- UX: admin‑only mode by default; web UI for models, exchanges, traders, competition/equity views, decision logs.

## Important Constraints
- Security
  - Secrets at rest are encrypted with RSA‑AES hybrid; API keys/private keys are never stored plaintext post‑migration.
  - Admin mode requires `NOFX_ADMIN_PASSWORD`; all protected endpoints require JWT. Token TTL ~24h with in‑memory blacklist (use shared store for multi‑instance).
- Performance
  - Execution aims for low latency; precision/formatting handled per exchange; WS monitor targets stable ~150 symbols.
- Reliability
  - Single‑binary service with SQLite; consider external store/Redis if running multiple instances (token blacklist sync).
- Compliance/Risk
  - Educational use only; trading is risky. Users must comply with their local regulations and exchange ToS.
- Ports/Runtime
  - Default API port from DB `system_config` is 8080; frontend dev on 3000 via Vite proxy.

## External Dependencies
- Exchanges/APIs
  - Binance Futures (REST/WebSocket), Hyperliquid, Aster DEX.
  - Optional feeds configured in DB: `coin_pool_api_url` (AI500‑style coin pool) and `oi_top_api_url` (OI growth ranking) with on‑disk caching and retries.
- AI Providers
  - DeepSeek, Qwen (DashScope), or custom OpenAI‑compatible endpoints via `mcp.Client`.
- Tooling/Runtime
  - Docker images include TA‑Lib for indicator work; local dev may require installing TA‑Lib per README.

Notes for Spec Authors (OpenSpec)
- Use verb‑led change IDs (add‑/update‑/remove‑/refactor‑...).
- Place capability specs under `openspec/specs/<capability>/spec.md` with SHALL/MUST wording and `#### Scenario:` blocks.
- Validate with `openspec validate --strict` before requesting implementation.
