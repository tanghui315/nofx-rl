# LLM 多 Agent 策略路由 实施任务清单

负责人: NOFX  
状态: 草案（待拆解与认领）  
关联文档: `docs/TECH_PLAN_llm_router_agents.md`

说明：  
本清单仅针对“路由/策略 LLM 多 Agent 架构”的增量工作，不包含现有引擎/风控已实现内容。

---

## Phase 1：Router Agent 影子模式

- [ ] 设计 Router Agent JSON 输出格式
  - [ ] route / confidence / route_ttl_cycles / reason / tags 字段定义与示例
  - [ ] 明确错误/回退策略（解析失败、调用超时时如何 fallback）

- [ ] 补充 Router Agent Prompt（仅日志用途）
  - [ ] 在 prompts/ 新增 `router/router_agent_prompt.txt`（或等价结构）
  - [ ] 覆盖资金档位 / Regime / NewsScore / 历史表现的路由规则文字
  - [ ] 要求输出严格 JSON，禁止自然语言混杂

- [ ] AutoTrader 中增加 Router Agent 调用入口（影子模式）
  - [ ] 在 `runCycle()` 中（0 仓位 + 无 pending + routeTTL<=0 时）构建 Router 上下文
  - [ ] 调用 LLM（Router Prompt），仅记录输出到日志与 DecisionRecord（不改变 currentRoute）
  - [ ] 在控制台与决策日志中打印 Router Agent 的 route 建议，供人工对比

- [ ] 与现有规则路由对比
  - [ ] 增加简单对比脚本/工具：对比 Regime+Router 与 Router Agent 输出的一致性
  - [ ] 归纳明显不合理的 Router Agent 决策，迭代 Prompt


## Phase 2：路由决策切换（Router Agent → 主路由器）

- [ ] AutoTrader 状态扩展
  - [ ] 为 AutoTrader 增加 `currentRoute` / `routeTTL` / `routeSource` / `rerouteRequested` / `strategyScenarioID` 字段
  - [ ] 在启动/重置时初始化路由状态

- [ ] Go 层接管 Router Agent 决策
  - [ ] 在 Router Agent 调用成功且解析正确时，将 `currentRoute` 设置为 LLM 输出的 route
  - [ ] 设置 `routeTTL` 为 LLM 建议值（带上合理上限）
  - [ ] 标记 `routeSource="router_agent"`，写入 DecisionRecord

- [ ] 旧 Regime+Router 逻辑退化为 fallback
  - [ ] 当 Router Agent 调用失败（超时/解析失败）时，回退到现有 Regime+Router 逻辑选择 route
  - [ ] 标记 `routeSource="legacy_rule"`，便于后续统计

- [ ] 前端与日志增强
  - [ ] 在决策卡片中显示 route_source（router_agent / legacy_rule）
  - [ ] 在控制台日志中打印 Router Agent 的路由结果与 TTL
  - [ ] 在决策日志中附带当前策略场景 ID（strategyScenarioID）


## Phase 3：Strategy Agents（LLM ReAct）集成（基础版）

> 目标：先为 `pullback_ema` 和 `range_grid` 两条路线引入 LLM 策略 Agent，其余路线继续沿用现有模板。

- [ ] 统一 Strategy Agent 调用接口
  - [ ] 在 decision 层定义 `StrategyAgentRunner` 接口（与 LocalReActRunner 风格类似）
  - [ ] 增加 route → agent 类型的简单映射表（pullback_ema → PullbackAgent；range_grid → RangeGridAgent）

- [ ] PullbackAgent Prompt & 调度
  - [ ] 在 prompts/ 新增 `pullback_ema/agent_pullback_ema.txt`
  - [ ] 定义工具使用原则与输出格式（decisions + request_reroute）
  - [ ] 在 AutoTrader 中，当 currentRoute=pullback_ema 时，调用 PullbackAgent 而非旧 LLM 模板（可保留旧模板作为 fallback）

- [ ] RangeGridAgent Prompt & 调度（基础版）
  - [ ] 在 prompts/ 新增 `range_grid/agent_range_grid.txt`
  - [ ] 利用工具集读取区间结构 / ATR / NewsTag，输出网格式限价建议或 wait
  - [ ] 在 AutoTrader 中，当 currentRoute=range_grid 时，调用 RangeGridAgent

- [ ] 为 Strategy Agent 增加重路由建议能力
  - [ ] 在 Strategy Agent 输出格式中加入 `request_reroute` / `reroute_reason` 字段
  - [ ] 在 AutoTrader 中识别该字段，将 `rerouteRequested=true` 并记录到 DecisionRecord

- [ ] 策略场景持久化与 Bootstrap
  - [ ] 设计策略场景持久化结构（如：trader_id → {strategyScenarioID, currentRoute, lastRouterDecision, profile 等}）
  - [ ] 启动 AutoTrader 时：
    - [ ] 若存在持仓/挂单但无已持久化场景 → 强制调用 Router Agent + 对应 Strategy Agent 一次，生成策略场景；
    - [ ] 将首次 Router/Strategy 决策结果持久化，供下次重启恢复；
  - [ ] 若存在已持久化场景 → 基于该场景直接选择 Strategy Agent，而非回退到旧规则 Router。


## Phase 4：工具层抽象与 LLM Tools 接口（可选）

- [ ] 抽象现有 Go 工具函数为统一 Tool 层
  - [ ] 将 `decision/agent/tools.go` 中的函数整理为标准工具接口（get_market/get_rules/get_account/get_positions/estimate_trade/opp_score 等）
  - [ ] 为未来 LLM Tool Calling 模式预留参数与返回结构

- [ ] MCP / LLM 客户端层支持 Tools
  - [ ] 为 Router Agent / Strategy Agent 设计简单的工具调用接口（本阶段可以先模拟 ReAct trace，不做真正 function-calling）
  - [ ] 若未来采用 OpenAI-style function-calling，再引入自动解析工具调用请求的能力

- [ ] 调用配额和超时控制
  - [ ] 在 Orchestrator 层为每个 Agent 设置最大工具调用次数与 LLM 时间预算
  - [ ] 超出限制时，强制 Agent 给出 wait 或简化决策


## Phase 5：可观测性与运营支持

- [ ] DecisionRecord 扩展
  - [ ] 增加 `route_source` / `agent_type` / `request_reroute` / `reroute_reason` 字段
  - [ ] 确保 Router Agent 与 Strategy Agents 均正确写入这些字段

- [ ] 指标与统计
  - [ ] 在 /api/dev/metrics 或新接口中统计：
    - Router Agent 与 legacy rule 的 route 命中率对比；
    - 各 route 的 winrate / PnL / 最大回撤；
    - request_reroute 次数、主要原因分布。

- [ ] 前端展示
  - [ ] 决策卡片中展示：
    - Route + Template + Route Source + Agent Type；
    - 当本轮 route 来自 Router Agent 时，显示简要 reason；
  - [ ] DevMonitor 页新增：
    - 最近 N 次 route 变更时间线；
    - 依据 route_source 过滤/对比表现的简单视图。


## Phase 6：灰度与回滚策略

- [ ] 灰度开关设计
  - [ ] 增加环境变量/配置项控制 Router Agent / Strategy Agents 是否启用（如 NOFX_ROUTER_AGENT_ENABLED/NOFX_STRATEGY_AGENT_ENABLED）
  - [ ] 支持快速切回 legacy Regime+Router + 旧模板的模式

- [ ] 回滚预案
  - [ ] 确定在 router_agent 表现异常或 Strategy Agent 决策质量明显下降时的自动回退条件（如连续 N 次高损失、特定错误码频繁出现）
  - [ ] 在日志中明确标记回滚事件，方便事后审计


## 备注

- 本清单依赖现有硬风控与执行路径保持稳定：  
  - validateDecisions / validateDecision、限价校验 等均视为既有基础设施。
- PositionManager 逻辑将逐步淡出：  
  - 持仓管理与退出策略改由各 Strategy Agent LLM 负责；  
  - 在多 Agent 架构稳定后，可考虑删除或大幅简化现有 PositionManager 代码。
- 多 Agent 架构的核心原则：
  - LLM 负责“分析与建议”；  
  - Go 负责“节奏与边界”；  
  - Trader 负责“执行与对账”。  
  三者职责清晰，便于迭代和调试。
