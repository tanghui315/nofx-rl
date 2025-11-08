# 新闻功能（Tavily 新闻抓取）使用说明

本文档介绍如何在系统中开启并使用“新闻抓取 + 决策注入”能力，以便在 LLM 决策时参考币种的最新资讯。

## 功能概览
- 后端在服务启动时检测环境变量 `TAVILY_API_KEY`，若存在即启动新闻抓取 Worker。
- Worker 每隔固定时间（默认 15 分钟）对系统默认币种（`default_coins`）抓取相关新闻并缓存到本地数据库（`config.db`）。
- 每个币种仅保留最近若干条（默认 5 条，按发布时间/抓取时间倒序）。
- 交易员层面可单独开启“包含新闻”开关，开启后在 LLM 决策上下文中注入该币种的最近 3 条新闻（标题/来源/链接/时间），提升对事件驱动行情的响应能力。
- 前端在“AI 交易员”页面提供“📰 新闻缓存”调试面板，可按币种查看已缓存的新闻。

## 开启步骤
1. 准备 Tavily API Key（注册 Tavily 后获得）。
2. 在项目根目录 `.env` 或系统环境中设置：
   - `TAVILY_API_KEY=tvly-xxxxxxx`
3. 启动/重启服务：
   - `go run -tags dev main.go`
4. 观察日志：
   - 若看到 `💡 News worker disabled: TAVILY_API_KEY missing`，表示未设置密钥，Worker 未启动。
   - 设置密钥后重启，首轮抓取完成后会打印各币种抓取/保存日志。

提示：模板文件 `.env.example` 已包含 `TAVILY_API_KEY` 占位，可参考复制为 `.env`。

## 配置项与默认值
- 抓取间隔：`news_interval_minutes`（系统配置，默认 15）
- 注入到 LLM 的条数：固定 3 条（控制 Token 成本；后续如需改为可配再行开放）
- 每个币种缓存条数（数据库保留）：默认 5 条
- 币种列表来源：`default_coins`（系统配置，JSON 数组）。

说明：以上系统配置存储在 `system_config` 表，`default_coins` 会在启动时根据 `config.json` 同步到数据库。若要让 Worker 监控更多币对，可修改 `config.json` 中的 `default_coins` 并重启服务。

## 前端使用
- 在“创建/修改交易员”弹窗中勾选“在 LLM 决策中包含最新新闻”。此为 **按交易员** 的独立开关，默认关闭。
- 在“AI 交易员”页面右上角，点击“📰 新闻缓存”按钮打开调试面板：
  - 选择币种
  - 选择条数（3/5/10）
  - 点击“刷新”查看后端缓存内容（标题 / 来源 / 发布时间 / 链接）

## API 调试（需要认证）
- 路由：`GET /api/news?symbol=ETHUSDT&limit=5`
- 说明：返回该 `symbol` 的最新缓存新闻，`limit` 可选，默认 5。
- 示例：
  ```bash
  curl -H "Authorization: Bearer $TOKEN" \
       "http://localhost:8080/api/news?symbol=ETHUSDT&limit=5"
  ```
  响应示例（简化）：
  ```json
  [
    {
      "title": "ETH price rebounds as ...",
      "url": "https://www.coindesk.com/...",
      "source": "coindesk.com",
      "published_at": "2025-11-08T09:22:00Z",
      "summary": "..."
    }
  ]
  ```

## LLM 决策中的注入
- 当交易员的“包含新闻”开关开启时，系统在构建 LLM 决策上下文时为当前评估的 `symbol` 注入 `news` 数组：
  - 结构：`[{ title, source, url, published_at }]`
  - 条数：最多 3 条（固定）
- 若后台尚未抓取到新闻或密钥未设置，则不会注入该字段。

## 数据落地与去重
- 数据库表：`news_items`（SQLite，位于 `config.db`）
  - 唯一约束：`UNIQUE(symbol, url)`，避免重复
  - 索引：`(symbol, fetched_at DESC)`
  - 超限清理：每个 `symbol` 超过保留阈值会自动按时间淘汰旧记录

## 抓取策略（当前实现）
- 请求：`POST https://api.tavily.com/search`
- 查询参数（精简）：
  - query: `"<SYMBOL> crypto news"`
  - include_domains: `coindesk.com, cointelegraph.com, decrypt.co, theblock.co, ambcrypto.com`（可后续扩展）
  - time_range: `d7`（近 7 天）
  - max_results: `10`

## 常见问题（FAQ）
1) 日志显示“News worker disabled: TAVILY_API_KEY missing”
   - 未设置 `TAVILY_API_KEY`；在 `.env` 或环境变量设置后重启。

2) `/api/news` 返回空数组
   - 刚启动还未到抓取周期；等待首轮抓取（启动后会先执行一次），或检查网络与密钥有效性。
   - `symbol` 不在 `default_coins` 列表中；调整 `config.json` 的 `default_coins` 并重启。

3) 前端“新闻缓存”面板无数据
   - 需要登录（接口受保护）。确保有 `Authorization: Bearer` 令牌。
   - 同 2) 检查 Worker 与密钥。

4) LLM 决策里看不到 `news` 字段
   - 当前评估币种无缓存或交易员未勾选“包含新闻”。
   - 为控制 Token 成本，仅注入 3 条简要字段，不含正文。

## 最佳实践与注意事项
- 建议只对与策略相关的交易员开启“包含新闻”，降低 Token 与上下文噪音。
- 若观察到 Tavily 频率/配额限制，可适当提高 `news_interval_minutes`。
- 默认仅保留新闻标题/链接/来源/时间；如需将摘要纳入上下文，请评估 Token 成本后再开启。

—— 以上为当前阶段的最小实现文档。后续如需：可配置注入条数、域名白名单编辑、并发抓取与重试策略等，可在 openspec 中提出变更提案迭代。

