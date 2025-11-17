# 实施任务清单（滚动维护）

负责人: NOFX
更新时间: 2025-11-17（已同步至当前实现，含前端调试页）

说明: 此清单持续更新。每次任务状态变化请在此标注。

## 后端

- [x] 决策结构扩展：加入 `order_type`、`limit_price`、`expire_kbars`（decision/engine.go）
- [x] 硬风控：每单保证金≥10U；交易所 MIN_NOTIONAL 校验 + 数量步长对齐；预计“绝对净利”阈值（decision/engine.go）
- [x] PreCheck 门禁（v1）：无持仓时仅在机会分达标才调用 LLM；按符号去抖；每轮放行数量上限（trader/auto_trader.go）
- [x] 简易机会评分器：EMA 贴近、3m 量能放大、OI 变化（decision/opportunity.go）
- [x] Regime/Router 骨架：占位实现（decision/regime.go, decision/router.go）
- [x] 调试接口：`/api/dev/regime` 返回分型+机会分；`/api/dev/losses` 交易所优先（api/replay_dev.go）
- [x] 提示词同步：v6.2/v6.3 增加“每单保证金≥10U / 名义≥MIN_NOTIONAL / 步长对齐”（prompts/）
- [x] 文档更新：高优事项、档位预设、ReAct-lite 工具（docs/TECH_PLAN_market_regime_routing.md）
- [x] PreCheck 完善：记录 Regime/机会分/否决原因；carry/range 路由默认不唤起 LLM（记录并 wait）
- [x] Router 集成：在用户提示与决策日志中注入 Regime/Route/constraints（system prompt 后续可选）
- [x] 限价单校验路径（引擎-基础）：校验 order_type/limit_price 与现价方向/偏离阈值，expire_kbars 合法性
- [x] 档位映射（基础版）：读取 config.json 的 `"profile"` → 注入预设 env（env 显式值优先）
- [x] 档位映射（完整）：config.json.llm + trading_thresholds 作为主来源，profile 补充派生值（env 显式覆盖）
- [x] `/api/dev/regime` 增强：增加 3m ATR/BB 带宽/ADX/Ichimoku 基本证据；支持 include_news=1 返回最近新闻
- [x] NewsScore：轻量新闻打分（关键词+时效+来源白名单）（news/score.go + news/worker.go + /api/dev/regime）
- [x] PositionManager：规则化 BE/ATR 追踪止损 + 事件节流（partial/结构退出仍由 LLM 决策）
 - [x] 可观测性：否决/拦截/有效下单率等计数与简要指标（/api/dev/metrics；前端 DevMonitor 页面接入）
- [x] ReAct-lite（Go）脚手架：LocalReActRunner 可出单（EMA/MACD+ATR 风险+名义/净利估算），灰度开关 NOFX_AGENT_ENABLED；只读工具集齐（get_market/get_rules/get_account/get_positions/estimate_trade/opp_score）
 - [x] 提示词目录重组（按策略分类：base, trend_carry, pullback_ema, breakout_volexp, range_grid）
   - [x] 目录重组（初步）：新增 prompts/pullback_ema/prompt_pullback_ema.txt、prompts/trend_carry/prompt_trend_carry.txt；PromptManager 支持递归加载；Router 注入模板键（相对路径）
- [x] 限价单执行（交易所）：GTC/IOC、部分成交、过期/撤单管理（Binance 通过 NOFX_LIMIT_TIF=GTC|IOC|FOK 控制）
  - [x] 限价下单（开仓）路径（Binance）：OpenLongLimit/OpenShortLimit（GTC）；Aster 复用现有限价实现；Hyperliquid 暂不支持（占位）
  - [x] 限价生命周期（基础）：AutoTrader 记录 pending → 到期撤单/成交后补挂SL/TP；/api/dev/open_orders、/api/dev/pending_limits 调试接口
  - [x] Dev 取消接口：/api/dev/cancel_order；避免同向重复挂单（执行层拦截）
  - [x] 部分成交处理（基础）：部分成交即为当前持仓补挂/校验 SL/TP，保留 pending 等完全成交或到期

## 前端

- [x] 风险档位选择：一个滑块/下拉（ultra_safe | safe | balanced | bold | ultra_bold）（前端只读/引导，实际生效由 config.json/env 决定）
- [x] PreCheck 可视：DevMonitor 显示 metrics（拦截/放行/原因、Agent/LLM 次数、限价生命周期统计）
- [x] 亏损分析：DevMonitor 集成 `/api/dev/losses`
- [x] Regime/Opportunities/News：DevMonitor 集成 `/api/dev/regime`
- [x] 挂单可视：DevMonitor 集成 `/api/dev/open_orders`、`/api/dev/pending_limits`、`/api/dev/cancel_order`
- [x] 路由与软化通道标识：在主页面/决策卡片展示 route/template/constraints，软化通道徽标
- [x] 档位选择器：前端滑块/下拉（profile），与当前 server profile 同步展示（保存仍通过配置文件/env 完成）
- [x] 限价单面板（增强）：挂单价格/数量/到期/部分成交状态（Trader 页面 Pending Limit Orders 模块）
- [x] 新闻分数展示（增强）：NewsScore 与来源/时效（Dashboard Debug 区块展示 BTC NewsScore + 最新标题）

## 配置与运维

- [x] 环境变量可用：AI_TEMPERATURE、NOFX_ENFORCE_MIN_SL、NOFX_MIN_SL_PCT_BASE、NOFX_MIN_SL_ATR_MULT、NOFX_ENFORCE_PROTECT_ORDER_DIR、NOFX_PRECHECK_ENABLED、NOFX_OPP_SCORE_MIN、NOFX_EMA_PROX_PCT、NOFX_SYMBOL_MIN_INTERVAL_MIN、NOFX_GLOBAL_MAX_PER_SCAN
- [x] config.json.example：示例含 llm 阈值/预算与 trading_thresholds
- [x] 改为以 config.json 为主：读取 profile→派生内部阈值；env 作为覆盖
- [x] 启动打印“当前档位 + 派生参数摘要”
- [x] 简易指标：有效下单率、LLM 调用频次、主要否决原因（/api/dev/metrics 返回汇总率与 top reason）

## 里程碑

- 阶段 1（灰度）
  - [x] 完成 Router 注入 + PreCheck 日志增强 + `/api/dev/regime` 增强 + 档位映射（基础）+ DevMonitor 前端页
- 阶段 2
  - [x] 限价单校验路径 + ReAct-lite v1 工具 + 基础观测（/api/dev/metrics）
- 阶段 3
  - [x] PositionManager 规则化 v2：在现有 BE+ATR 追踪基础上，增加规则化分批（2 档）+ EMA20 简化结构退出（按阈值，非 LLM 决策）
  - [ ] 交易所限价路径 v2：按不同 route 配置更细的 IOC/短效路径与参数（当前已支持全局 NOFX_LIMIT_TIF=GTC|IOC|FOK）
  - [x] 提示词目录重组（完善）：补齐各策略专用模板与说明（base/trend_carry/pullback_ema/breakout_volexp/range_grid，Router 输出模板键与 PromptManager 对齐）

## 备注

- 固定硬约束（始终开启）：每单保证金≥10U；MIN_NOTIONAL + 步长对齐；止损距离 ≥ max(1.0%, 0.8×3m ATR%)；止盈止损方向正确。
- 小资金护栏：绝对净利阈值 = max(手续费×倍数, 固定美元)。
- PreCheck 默认（env 可覆盖）：opp_score_min=8；ema_prox_pct=0.4；symbol_min_interval_min=10；global_max_per_scan=1。
