# 多 Agent × LLM 策略路由 技术方案（草案）

作者: NOFX  
状态: 草案（待迭代）  
适用范围: 自动交易主流程（AutoTrader），替代/增强现有 Regime + Router 规则逻辑

---

## 1. 背景与目标

现状：
- Router 目前在 Go 层通过简单规则实现：
  - Regime = trend_early/mid → pullback_ema（小资金）或 trend_carry；
  - Regime = trend_late → no_trade；
  - Regime = range → range_grid；
- Regime 只看 BTCUSDT 的 ADX/ATR 近似 + 简单 NewsScore；逻辑较粗；
- range_grid 仍处于“观察模式”：路由有了，但没有真正规则化网格执行器，默认不调 LLM；
- 策略收敛与行为调整需要改 Go 代码（if/else 越来越复杂），维护成本高。

目标：
- 引入「多 Agent 协同」的 LLM 策略路由架构：
  - Router Agent: 专职做“环境评估 + route 选择”，只负责选路线；
  - Strategy Agents: 按 route 分策略 Agent，负责具体交易决策（**含开仓与持仓管理/退出**）；
  - Orchestrator（Go）做唯一“总控”：调度 Agent、执行下单、承担全部硬风控；
- 将“路由逻辑”和“策略细节”尽量收敛到 Prompt 中，通过改 Prompt 调整行为；
- 保持风控由 Go 层统一验证（validateDecisions + 执行层），避免 LLM 直接控制下单；
- 逐步淡出/删除 PositionManager 规则逻辑，由各策略 Agent LLM 负责持仓管理与退出。


## 2. 总体架构

### 2.1 角色划分

1）Router Agent（LLM）  
- 触发条件（正常模式）：
  - AutoTrader 当前 0 仓位（无任何持仓）；
  - AutoTrader 当前无 pending 限价挂单；
  - 且满足以下之一：
    - 尚未有有效 route / 策略场景（系统启动后第一次进入“空仓 + 无挂单”状态）；
    - route 有效期（TTL）已到；
    - 某个 Strategy Agent 明确提出“请求重路由”。
- 输入（通过上下文 + Prompt 注入）：
  - 账户资金规模、风险档位（profile）、历史表现摘要；
  - BTC/ETH 的趋势/波动/结构（Regime evidence）；
  - NewsScore（全局 & BTC 级）+ 最近相关新闻摘要；
  - 当前交易所 / 环境开关（如 PRECHECK 阈值、限价策略等）。
- 输出（JSON）：
  - `route`: string（pullback_ema | trend_carry | range_grid | no_trade | ...）
  - `confidence`: int（0–100）
  - `route_ttl_cycles`: int（建议在多少个扫描周期后才允许自动重评）
  - `reason`: string（简要中文理由，便于日志和前端展示）
  - 可选 `tags`: string[]（如 ["risk_off","high_vol"]）用于后续可视化。

2）Strategy Agents（LLM ReAct Agent）  
- 每个 route 对应一个 Agent Prompt（可以共用模型）：
  - PullbackAgent: 针对 pullback_ema 路由；
  - CarryAgent: 针对 trend_carry 路由；
  - RangeGridAgent: 针对 range_grid 路由；
  - 其他 route 可按需补充。
- 输入：
  - 当前 route（由 Router Agent 决定，作为强约束注入）；
  - 当前「策略场景」元信息（如 route、生效时间、上次 Router Agent 输出、用户 profile 等）；
  - 账户、持仓、候选币、市场数据（与现有 LLM 决策相似的上下文）；
  - 环境标签（如 risk_on/off、regime evidence、profile 派生参数）；
  - 可调用的“工具”列表（见 §4）。
- 输出：
  - `decisions`: Decision[]（与现有 LLM 决策格式兼容，覆盖开仓、加仓、减仓、平仓、调整 SL/TP 等）；
  - 可选 `request_reroute`: bool（当前环境不再适合该 route，建议重路由）；
  - 可选 `reroute_reason`: string（为什么建议重路由）。

3）Orchestrator（Go：AutoTrader + PreCheck 升级版）  
- 保持为唯一“总控”（不再做策略层的 PositionManager 规则）：
  - 决定何时调 Router Agent / 哪个 Strategy Agent；
  - 解析 Agent 的 JSON 输出；
  - 调用现有 `validateDecisions` + 执行下单逻辑；
  - 决定是否执行 Strategy Agent 的重路由建议。
- 内部新增状态：
  - `currentRoute`：当前生效的策略路由；
  - `routeTTL`：当前 route 还需坚持多少个决策周期；
  - `routeSource`：路由来源（"router_agent" / "legacy_rule" / "manual_fallback"）；
  - `rerouteRequested`：bool（上一轮策略 Agent 是否建议重路由）。
  - `strategyScenarioID`：当前账户/Trader 的策略场景 ID（用于持久化和恢复）；


### 2.2 调度流程（高层）

每次 AutoTrader 执行一个 runCycle：

1）构建 Context（保持现有逻辑）：账户、持仓、候选币、行情、News、Performance 等。  
2）加载/恢复策略场景：
   - 尝试从本地持久化（如 config.db / Trader 扩展字段）读取 `strategyScenarioID` + `currentRoute` 等；
   - 若存在，则作为当前策略场景使用；
   - 若不存在（首次启动或旧数据），进入“场景 Bootstrap”路径：
     - 如果存在持仓或 pending 限价：
       - 仍可构建完整 Context，并**立即调用 Router Agent + 对应 Strategy Agent 一次**，让 LLM 基于当前真实环境给出一个策略场景（包括如何管理已有仓位）；
       - 将新产生的 `currentRoute` / `strategyScenarioID` 持久化存储（方便下次重启恢复）；
     - 如果无持仓且无 pending，则按正常流程在后续步骤触发 Router Agent。
3）检查是否需要重新路由评估（正常模式）：
   - 若满足「0 持仓 + 无 pending 限价」且（`currentRoute` 为空 or `routeTTL <= 0` or `rerouteRequested` 为真），则：
     - 调用 Router Agent；
     - 更新 `currentRoute`、`routeTTL`、`routeSource="router_agent"`、`strategyScenarioID`；
     - 持久化策略场景信息；
     - 记录路由决策到 DecisionRecord（供前端和日志展示）。
4）根据 `currentRoute` 决定调用哪个 Strategy Agent：
   - `no_trade` → 本轮直接 wait（可不调用策略 Agent，只写日志）；
   - 其它 route → 调用对应 Strategy Agent，带上 route 信息、当前策略场景信息和工具列表。
5）解析 Strategy Agent 输出：
   - 对 `decisions` 调用 `validateDecisions` 严格校验；
   - 执行通过校验的决策，记录 ExecutionLog；
   - 若 `request_reroute == true`，设置 `rerouteRequested=true`，在下一轮（或本轮执行完）调用 Router Agent；
   - 若 Agent 对持仓管理策略有显式更新（如“从 pullback_ema 过渡到 carry 风格”），可更新并持久化策略场景中的元信息。
6）更新 routeTTL：
   - 每轮正常结束后 `routeTTL--`（下限 0）；
   - 当有新 Router Agent 决策时重置。


## 3. Prompt 设计概述

整体目录结构建议（与多 Agent 角色对齐）：

```text
prompts/
  base/                      # 公共基座与可复用片段（风险约束、输出格式、交易所规则等）
    base_risk.txt
    base_output_format.txt
    base_exchange_rules.txt
    ...

  router/                    # Router Agent 专用 Prompt
    router_agent_prompt.txt

  strategy/                  # 策略 Agent（统一入口，内部按 route 划分片段）
    strategy_pullback_ema.txt
    strategy_trend_carry.txt
    strategy_range_grid.txt
    # 将来如有其他 route 再补充

  legacy/                    # 旧版单模板（仍可作为 fallback 使用）
    adaptive_moderate_hist_v6_3.txt
    adaptive_v7.txt
    ...
```

- 不再按“每个策略一个独立目录 + 独立 LLM 调用”的思路组织，而是按**角色**划分：
  - `base/`：所有 Agent 共用的风险约束/输出格式/交易所规则片段；
  - `router/`：只给 Router Agent 使用；
  - `strategy/`：为 Strategy Agent 提供各 route 的风格说明和规则片段；
  - `legacy/`：现有 adaptive 系列模板，作为过渡和回退方案。
- Strategy Agent 可以在一个 Prompt 中通过 include/拼接的方式引入不同 route 的片段，而不必为每个 route 单独起一个 LLM 调用。

### 3.1 Router Agent Prompt（概念稿）

主意图：  
- 不直接发交易决策；  
- 只做“环境评估 + 选 route”；
- 在 System Prompt 中强调：所有实际下单由系统按硬风控决定，你只负责选路线。

关键内容：
- 明确列出可选 route 及适用 / 不适用场景；
- 按资金档位（小资金 / 中等 / 大）区分倾向；
- 引入 Regime / NewsScore / 表现摘要等因素；
- 约束输出格式为严格 JSON（route / confidence / route_ttl_cycles / reason）。

### 3.2 Strategy Agents Prompt

各 Agent Prompt 的共通结构：
- 明确“你当前必须遵守的 route 是 X”，禁止越权切换 route；
- 列出可用工具及建议使用原则（例如先 get_market，再 estimate_trade）；
- 明确输出格式：
  - `decisions`: 标准 Decision 数组；
  - `request_reroute`: 可选 bool；
  - `reroute_reason`: 可选 string（仅 request_reroute=true 时必填）。

RangeGridAgent 例外点：
- 网格策略可利用 tools 读取当前区间、高低点、ATR 等；  
- 但**仍不得直接下单**，只输出决策 JSON，由 Orchestrator 校验执行。


## 4. 工具（Tools）设计

注意：此处工具均为「读-only」，不会直接下单。

### 4.1 共用基础工具（供各策略 Agent 使用）

初始工具集可与现有 LocalReActRunner 一致，并扩展：

- `get_market(symbol)`  
  - 返回简化行情快照：价格、EMA20、MACD、RSI7、ATR3m、BBWidth、VWAP、Funding、OI 等。
- `get_rules(symbol)`  
  - 交易所规则：最小名义、步长、精度、最大杠杆上限等。
- `get_account()`  
  - 净值、可用余额、保证金占用比例、当前持仓数。
- `get_positions()` / `get_positions(symbol)`  
  - 当前持仓列表 / 指定 symbol 的持仓详情。
- `estimate_trade(symbol, side, leverage, position_size_usd, stop_loss, take_profit)`  
  - 返回估算 RR、所需保证金、手续费估算、净利估算、是否满足 MinNotional/步长等。
- `opp_score(symbol)`  
  - 复用 SimpleOpportunityScore：EMA 贴近度、量能、OI 增幅等。

### 4.2 Router Agent 追加工具（可选）

- `get_regime()`  
  - 当前简易 Regime 和证据（对现有 DetectRegime 的封装）。
- `get_news_summary()`  
  - 返回 BTC 及全局 NewsScore + 最近几条重要新闻（标题 + 来源 + 摘要）。
- `get_profile()`  
  - 返回当前风险档位（profile）及派生阈值（opp_score_min、rr_min、risk_pct_max 等）。
- `get_loss_stats()`  
  - 最近 N 笔交易中按 route/策略维度聚合的亏损归因（与 `/api/dev/losses` 一致）。


## 5. 风控边界与安全约束

即使引入多 Agent + 工具，**所有实质风险控制仍在 Go 层**：

- 决策验证（不变）：  
  - `validateDecisions` + `validateDecision` 继续负责：
    - 杠杆上限 / 保证金下限（≥10U）；
    - 交易所 MinNotional / 步长对齐；
    - 单笔名义不得超过账户净值倍数上限；
    - RR ≥ 3.0；
    - 绝对净利阈值（覆盖手续费 × 倍数）；
    - 限价价格方向与偏离限制；
    - SL/TP 方向和距离合理性。
- 工具完全读-only：  
  - 不提供任何“下单/撤单”类工具；  
  - 所有开仓 / 平仓 / 调整 SL/TP 仍由 Orchestrator 调 Trader 实现。
- 调用配额 & 超时控制：  
  - 每个 Agent 调用 LLM + Tools 的次数和耗时需在 Orchestrator 层设置上限；  
  - 避免 ReAct 探索过度拉长决策时间或消耗过多 Token。


## 6. 日志与前端可视化

为了便于调试与运营，需要增强如下可观测性：

- 决策日志（DecisionRecord）新增字段：
  - `route_source`: router_agent / legacy_rule / manual_fallback；
  - `agent_type`: router | strategy_pullback | strategy_range_grid | strategy_carry | ...；
  - `request_reroute`: bool；
  - `reroute_reason`: string。
- 控制台日志：
  - 每轮打印 `Regime/Route` 摘要（已新增），追加 `route_source` 和 `agent_type`；
  - Router Agent 调用时打印路由 JSON 摘要；
  - Strategy Agent 调用时打印使用的 route 和工具次数（简要统计）。
- 前端：
  - 决策卡片顶部增加：
    - Route source / Agent type 徽标；
    - 若本轮由 Router Agent 选路由，展示简要 reason；
  - DevMonitor / debug 页展示：
    - 最近 N 轮 route 变更轨迹；
    - request_reroute 次数与原因统计。


## 7. 迭代与灰度策略

建议分阶段实施，避免一次切换所有 route：

1）Phase 1：Router Agent 影子模式  
  - 实现 Router Agent Prompt + 调用逻辑，但只写入日志，不真正改变 `currentRoute`；  
  - 与现有 Regime+Router 结果对比，调整 Router Prompt 行为。

2）Phase 2：切换 route 决策来源  
  - 当 Router Agent 表现稳定时，将 `currentRoute` 改为以 LLM 输出为主；  
  - 旧 Regime+Router 逻辑退化为 fallback（例如 Router Agent 调用失败时使用）。

3）Phase 3：逐步引入策略 Agents  
  - 先对 pullback_ema 引入 LLM ReAct Agent，仍保留旧 LLM 模板作为备份；  
  - 再为 range_grid 引入 RangeGridAgent，使用工具集加强结构识别与净利评估；  
  - 最后视需要对 trend_carry 引入 CarryAgent。

4）Phase 4：优化工具和路由策略  
  - 引入更多统计工具（按 route 聚合表现、风险集中度）；  
  - 根据实盘表现调整 Router / Strategy Prompt 的规则文字。


## 8. 需要的代码改动（概要）

- 新增 Router Agent 调度逻辑（AutoTrader 层）；
- 新增 Strategy Agent 入口（按 route 映射 Agent 类型）；
- 在 MCP 客户端层支持 LLM+Tools 的调用模式（未来如需 function-calling）；
- 扩展 decision/agent 工具集作为 LLM Tools 的后端实现；
- 增强 DecisionRecord / 日志字段以记录 route_source / agent_type 等；
- 前端增加 Agent / Route 来源的可视化元素。

详细任务拆分见 `docs/IMPLEMENTATION_CHECKLIST_llm_router_agents.md`。
