# 市场分型 × 策略路由 技术方案（小资金友好版）

作者: NOFX / Architecture Draft
更新时间: 2025-11-15
状态: 草案（建议先灰度）

## 0. 高优先事项与最终决定（落地前必须明确）

目标：先把“高优先级、立刻影响交易质量”的事项敲定，作为第一阶段灰度发布范围。

- PreCheck 门禁（必须）
  - 只在“有机会时”才调用 LLM；否则直接 wait。
  - 触发条件（OpportunityScore，默认阈值 8，可配）：
    - EMA 贴近度（3m/15m vs EMA20，<=0.4%）+2
    - BB 带宽收缩后放大 +2
    - OI_delta >= 5% +1
    - 成交量放大 >=1.5x +1
    - 绝对净利 >= max(手续费×5, 固定U) +3
    - BTC 支持或软化通道 +2
  - 频控：同一 symbol 间隔 >=10 分钟；全局单次扫描最多调用 1 次（默认）。
  - 配置来源：config.json llm.*（已在示例中约定）。

- 策略路由（必须）
  - Regime ∈ {trend_early, trend_mid, trend_late, range}；小资金优先 pullback_ema。
  - 路由输出 StrategyRoute + constraints + template（第一阶段沿用 v6.3 基座模板）。
  - 默认映射：
    - trend_early/mid → pullback_ema（小资金）或 trend_carry（非小资金）
    - trend_late → no_trade
    - range → range_grid（小仓，绝对净利阈值必须满足）
  - 软化通道：仅在震荡/不明朗（1h/4h ADX<15 或方向分歧）且无显著反向时放行，附加约束：信心≥92、RR≥3.5、杠杆≤3x、risk≤1%、需贴近 EMA20（±0.4%）。

- 限价单支持（必须）
  - Decision 扩展字段：order_type("market"|"limit"), limit_price, expire_kbars（已在结构中预留）。
  - 路由默认：
    - pullback_ema：limit 优先，靠近 EMA20 的反抽/回抽位；expire_kbars 6–12。
    - trend_carry：market（再入场场景可 limit）；expire_kbars 4–6。
    - breakout_volexp：market 或短 IOC；expire_kbars 2–4。
    - range_grid：limit；expire_kbars 10–20。
  - 校验（引擎层）：方向与当前价一致（多 limit<=现价；空 limit>=现价）、偏离不超过 ~2–3%、步长与最小名义满足、绝对净利阈值通过；到期未成撤单。

- 小资金护栏（必须）
  - 每单保证金 ≥ 10U（required_margin = position_size_usd / leverage）。
  - 名义 ≥ 交易所 MIN_NOTIONAL（并做 MARKET_LOT_SIZE/LOT_SIZE 步长对齐）。
  - 预计绝对净利 ≥ max(手续费×5, 固定阈值 1.5U)。
  - 止损距离 ≥ max(1.0%, 0.8×3m ATR%)；保护单方向正确（系统已强校验）。

- LLM 频率（必须）
  - 温度默认 0.0（确定性）；仅在 preCheck 通过时调用；carry/range 阶段默认不调用。
  - 配置入口：config.json llm.*；已提供示例字段（opp_score_min、max_calls_hourly/daily、symbol_min_interval_min、global_max_per_scan、temperature）。

- 验收标准（第一阶段）
  - 交易所拒单类错误显著下降（-4164、-4003 基本归零）。
  - 触发 LLM 的调用次数下降（对比旧版 >50%）。
  - 单笔净利中位数/手续费比 > 3。
  - 有效下单率（通过引擎校验→成功成交）显著提升。


## 1. 背景与问题

现状痛点（基于最近实盘与对账）
- 小资金场景下，频繁短线 + 低名义 + 低保证金 → 手续费蚕食收益，净效益差；
- 提示词偏“顺大势+快锁盈”，但在短周期强反抽时容易被扫，收益覆盖不了费用；
- 统一模板“万能大脑”负担重，易出现规则漂移；
- 部分风控仅在提示词层，LLM 有时“高信心覆盖”导致打回或无效单；
- 交易所规则（步长/最小名义）与保护单方向导致的偶发“瞬时亏损”。

目标
- 针对小资金，优先“低频高RR、绝对收益覆盖手续费”的交易；
- 用“市场分型”驱动“策略路由”，不同阶段选不同提示词与执行节奏；
- 把关键风控下沉到引擎层，提示词仅作说明书；
- 降低 LLM 调用频次，事件触发为主；
- 保留探索位，避免过严导致“冻住不交易”。


## 2. 总体方案

核心思想：资金感知 + 市场分型 + 策略路由 + 硬风控。

- 资金感知（小资金护栏）
  - 最低保证金 ≥ 10U（已落地）；
  - 预计绝对利润 ≥ max(手续费×5, 固定阈值 1.5U) 才允许开仓；
  - 控制 LLM 决策频率（事件触发、关键结构完成）。

- 新闻因子（NewsScore，干预 Regime/Route）
  - 全局新闻（监管/宏观/CPI/FOMC/交易所事故）→ risk-on/off 标记，可直接强制 no_trade 或仅小仓；
  - 币种级新闻（主网/激励/重磅合作/安全事故）→ 对应符号的正/负修正，影响 route 权重与软化通道放行；
  - 打分范围 [-2, +2]，按时效加权：<2h×1.0；<24h×0.5；>24h×0.2；来源白名单/黑名单管理。

- 市场分型（Regime）
  - Trend-Early / Trend-Mid / Trend-Late / Range（震荡）
  - 基于 1h/4h ADX、HH/HL 结构、Ichimoku 位置、BB 带宽、背离计数等；

- 策略路由（Router）
  - Trend-Carry（趋势早/中期，顺势持有、分批止盈、减少换手）
  - Pullback-EMA（回抽到 EMA20/50 的结构确认单，RR 高，低频）
  - Breakout-VolExp（波动扩张突破，单次大空间，止损与 ATR 联动）
  - Range-Grid（震荡小仓网格，强护栏：绝对收益阈值 + 趋势切换即平）
  - No-Trade（趋势晚期/方向不明，耐心）

- 硬风控（Engine）
  - RR ≥ 1:3（已有）；
  - 止损距离 ≥ max(1.0%, 0.8×3m ATR%)（已落地）；
  - 保护单方向正确（已落地）；
  - 3m 动能冲突否决：反抽/急跌信号出现 → wait（新增）；
  - 入场结构硬条件（按路由）：例如 Pullback-EMA 要求贴近 EMA20 ±0.4% 且 3m/15m MACD 同向（新增）；
  - 交易所规则：步长对齐、最小名义（已落地，且注入给 LLM 参考）；
  - BTC 软化通道：强趋势严格，震荡期允许“受限开仓”（信心≥92、RR≥3.5、杠杆≤3x、risk≤1%）（已在提示词）。


## 3. 架构改动点

模块视图

- decision/regime: 市场分型判别器（新增）
  - 输入：MarketData（已有）、OI/资金费率（已有）、背离（已有）
  - 输出：Regime 标识 + 置信度 + 主要证据（供日志/提示词注入）

- decision/router: 策略路由器（新增）
  - 输入：Regime、Account（资金规模/持仓）、用户偏好（小资金）、风险约束
  - 输出：StrategyRoute 枚举（trend_carry/pullback_ema/breakout/range_grid/no_trade）
• 会输出固定的“策略路由”标识（含约束与提示词模板）。目前规划的策略：
  - trend_carry
      - 场景：趋势早/中期；持有为主、分批止盈、追踪止损；降频调用
      - 关键：RR≥3；ATR 联动 SL；少换手；名义足以覆盖手续费
  - pullback_ema
      - 场景：顺势回抽做入场
      - 关键：贴近 3m/15m EMA20（±0.4%）且 3m/15m MACD 同向；RR≥3.5；拒绝低位追空/高位追多
  - breakout_volexp
      - 场景：带宽收缩→放量突破/跌破
      - 关键：成交量≥1.5–2×均量；SL 以结构/ATR；若无跟随迅速减仓/退出
  - range_grid
      - 场景：震荡/ADX<15
      - 关键：小仓/轻网格；预计绝对净利≥max(手续费×5, 固定U)；趋势切换即平；严禁加码救火
  - no_trade
      - 场景：趋势晚期/方向不明；等待

- decision/engine: 决策引擎增强（改造）
  - 内嵌 Router 决定调用哪套提示词模板；
  - 在 validateDecision 前新增策略特定的“结构硬约束”；
  - 新增“绝对收益阈值”硬校验（预计净利 ≥ max(手续费×5, 固定U)）；
  - 新增“3m 动能冲突”否决（做空遇反抽/做多遇急跌）。

- prompts/: 模板分离（新增）
  - adaptive_moderate_hist_v6_3.txt（保留基座）
  - prompt_trend_carry.txt
  - prompt_pullback_ema.txt
  - prompt_breakout_volexp.txt
  - prompt_range_grid.txt

- trader/*: 执行层（已完成的大部分 + 小幅补充）
  - 已有：最小名义/步长、最低保证金、保护单方向、止损距离；
  - 补充：预计净利 ≥ 阈值校验（需要“净利估算器”，参见 §5）。

- api/dev: 调试与对账（已扩展）
  - /api/dev/losses（交易所优先 → 日志回退），已修正 LONG/SHORT 聚合；
  - 可考虑新增 /api/dev/regime?symbol=... 展示当前分型证据（可选）。


## 4. 市场分型（Regime）判别规则（V1）

指标集合
- 1h/4h ADX & 斜率（趋势强度与增强/衰减）
- Ichimoku：价格与云的位置、TK_cross、云厚度
- BB 带宽（扩张/收缩）、%B 极值计数
- HH/HL 结构（简单 swing 检测）
- 背离计数（RSI/MACD，已有）

判别（示意伪码）
```
if ADX_4h >= 20 and ADX_1h 上升 and 价格在云外 and 带宽由小变大:
    if 背离计数小 & 回撤不过 1h EMA20:
        Trend-Early
    else:
        Trend-Mid
elif ADX_4h >= 20 and 背离计数升高 or 高位连阳衰减 or 带宽极宽:
    Trend-Late
elif ADX_1h < 15 and 1h/4h 方向分歧 and 带宽收缩:
    Range
else:
    Range
```
输出结构
```
Regime {
  type: "trend_early" | "trend_mid" | "trend_late" | "range",
  confidence: 0..100,
  evidence: {
    adx_1h, adx_4h, adx_slope, ichimoku_pos, bb_width, divergence_count, ...
  }
}
```

### 4.1 新闻因子融合（V1）
- 评分：NewsScore ∈ [-2, +2]（来源/关键词/时效加权）
- Regime 修正：
  - 强负面（≤ -2）：强制 risk-off；no_trade 或仅小仓探索（按账户风险与流动性）；
  - 强正面（≥ +2）且技术面不冲突：倾向 trend_early / breakout_volexp，提升 route 权重；
- 路由约束：
  - 正向催化：允许触发“BTC 软化通道”放行；RR 不降、杠杆不升（保持审慎）；
  - 负向事件：禁用 range_grid；pullback_ema 也降权或禁入，优先 no_trade；
- 可观测：在 Regime evidence 中打印 `news_score` 与命中关键词/来源/时效。


## 5. 策略路由与策略模板

路由策略
```
if Regime=trend_early or trend_mid:
  if 小资金: route = pullback_ema (优先高RR的回抽结构单)
  else: route = trend_carry (持有/分批)
elif Regime=trend_late:
  route = no_trade （或仅小仓探索，慎重）
elif Regime=range:
  route = range_grid （小仓/绝对收益阈值/趋势切换即平）
```

模板要点（示例）
- pullback_ema：
  - 入场：贴近 3m/15m EMA20（±0.4%）；3m/15m MACD 同向；禁止低位追空/高位追多；
  - RR ≥ 1:3.5；预计绝对净利 ≥ max(手续费×5, 1.5U)；
  - 3m 动能冲突（RSI 快翻 + MACD 柱收敛）→ wait；

- trend_carry：
  - 过滤质量大于频率；追踪止损/分批止盈优先，减少换手；
  - LLM 调用降频（结构/回撤/ATR 事件触发）。

- breakout_volexp：
  - 窄带→放量突破；ATR/带宽触发；
  - 止损与 ATR 联动；失败后冷却更长。

- range_grid（小仓）：
  - 轻仓、多点位、趋势切换即平；
  - 绝对利润阈值必须满足；不满足 → 不做。


## 6. 硬风控（引擎层）清单

- 已落地：
  - 最低保证金 ≥ 10U；最小名义/步长对齐；
  - 止损距离 ≥ max(1.0%, 0.8×3m ATR%)；
  - 保护单方向正确；
  - BTC 软化通道（强趋势严格、震荡期受限开放）。

- 待新增：
  - 预计绝对净利 ≥ max(手续费×5, 固定阈值)（新增“净利估算器”：名义×目标幅度×手续费模型→净额）；
  - 3m 动能冲突否决（短反抽/急跌模式识别）；
  - 策略特定入场结构硬条件（如 pullback_ema 的 EMA20 ±0.4% + 3m/15m MACD 同向）。


## 7. LLM 使用与频控

- 温度：0.0–0.1；多模板切换时控制 token，避免冗长；
- 调用触发：价格/ATR/结构“事件触发”为主（carry 阶段显著降频）；
- 注入数据：
  - 交易所规则（步长/最小名义）已注入“Exchange Rules”段；
  - 历史表现（交易所优先→日志回退）已统一口径；
  - 市场分型与证据摘要（新增注入），包含 `news_score` 与关键证据。


## 8. 配置与开关（Feature Flags）

环境变量（建议）
- NOFX_ENFORCE_MIN_SL=true|false（默认 true）
- NOFX_MIN_SL_PCT_BASE=1.0
- NOFX_MIN_SL_ATR_MULT=0.8
- NOFX_ENFORCE_PROTECT_ORDER_DIR=true|false（默认 true）
- NOFX_NEAR_EMA20_ENABLED=true|false（默认 false）
- NOFX_NEAR_EMA20_PCT=0.4
- NOFX_ABS_PROFIT_MIN_USD=1.5
- NOFX_PROFIT_FEE_MULTIPLIER=5
- AI_TEMPERATURE=0.0


## 9. 迭代与落地步骤

阶段 1（1–2 天）
1) 引擎：增加“绝对收益阈值”校验；
2) 引擎：新增“3m 动能冲突”否决；
3) 决策：增加 Regime 判别（先简单规则），在日志打印结果；
4) 模板：pullback_ema / trend_carry 初版（导入 v6.3 基座条目）。

阶段 2（2–4 天）
1) Router：根据 Regime+资金规模选择模板；
2) 引擎：策略特定入场结构硬条件（pullback_ema）；
3) LLM：降频（carry 阶段事件触发）；
4) 前端：标注“Regime/Route/理由摘要”。

阶段 3（灰度 1–2 周）
1) 覆盖更多标的；
2) 自适应阈值回调（根据 /api/dev/losses 最大亏损来源统计）；
3) 若数据支持，再引入 breakout_volexp / range_grid。


## 10. 验证指标与监控

- 单笔净利中位数 / 手续费比（>3）
- 交易成功率（有效下单/总提案）
- 探索单占比与其 PnL（控制在 <20% 且不拖累整体）
- Regime 切换频率与持仓时长分布
- 主要否决原因计数（便于优化阈值）


## 11. 风险与回滚

- 过严导致下单率过低 → 暂时放宽“绝对净利阈值”或 EMA20 贴合度；
- 震荡期网格误用 → 严格小仓且趋势切换即平，默认禁用，灰度再开；
- 提示词偏移 → 引擎硬风控优先，模板仅引导；随时可切回 v6.3 单模板。


## 12. 需要的代码改动清单（摘要）

- decision/regime.go（新）：实现 Regime 判别；
- decision/router.go（新）：路由选择 + 注入模板名；
- decision/engine.go（改）：
  - 在 buildSystemPromptWithCustom 前注入策略模板名；
  - validateDecision 中加入“绝对收益阈值”“动能冲突否决”“策略入场结构”校验；
  - ctx 中附带 Regime/Evidence（便于提示词）；
- news/（改）：提供 NewsScore(symbol/global, window) 轻量接口与来源/时效打分器；
- prompts/（新）：四套模板；
- trader/auto_trader.go（已部分落地）：
  - 保护单方向、止损距离、最低保证金/名义/步长（已）；
  - 预计净利估算器（名义×目标幅度×费率，考虑双边费用），用于硬校验；
- market/（已部分落地）：
  - Exchange Rules 注入（已）；
  - Regime 所需的指标若缺失则补齐（如 swing/结构检测，V1 可简化）。


## 13. 结语

通过“市场分型 × 策略路由 × 硬风控”的迭代，小资金场景将从“频繁小利、手续费蚕食”转向“低频高RR、绝对收益优先”，并通过引擎层硬约束与模板分治，降低 LLM 决策噪声与风控穿透风险。建议按阶段灰度，优先落地 pullback_ema / trend_carry 两条路径，然后再扩展到突破与网格。


## 14. 提示词目录重组（prompts/）

为配合“策略路由”，按策略维度重组 prompts/ 目录，分离基座/共享片段/策略模板，降低耦合与漂移：

目录建议
```
prompts/
  base/                      # 基座与通用片段
    adaptive_moderate_hist_v6_3.txt
    fragments_common.md      # 硬约束/输出格式/风险提示等可复用片段

  trend_carry/
    prompt_trend_carry.txt
    fragments_tpsl.md        # 分批/追踪止盈范式

  pullback_ema/
    prompt_pullback_ema.txt
    fragments_entry_structure.md  # EMA20贴近/3m15m同向等结构描述

  breakout_volexp/
    prompt_breakout_volexp.txt
    fragments_breakout_filters.md # 带宽/放量/假突破过滤

  range_grid/
    prompt_range_grid.txt
    fragments_grid_risk.md        # 小仓/绝对收益阈值/趋势切换即平

  shared/
    news_summary.md          # NewsScore 与摘要插槽
    exchange_rules.md        # 交易所规则说明（步长/最小名义）
```

拼装与路由
- Router 输出包含 `template` 路径（如 `prompts/pullback_ema/prompt_pullback_ema.txt`）；
- 引擎在构建 System Prompt 时，按顺序拼接：
  1) base/adaptive_moderate_hist_v6_3.txt（或更精简基座）；
  2) shared/exchange_rules.md（可选，按注入数据拼接）；
  3) shared/news_summary.md（可选，带 NewsScore/摘要）；
  4) 策略模板 prompt_xxx.txt；
  5) 策略子片段（fragments_*）；
- 硬风控仍在引擎层校验，提示词仅作为说明书与偏好引导。

## 15. 市价/限价策略（与路由绑定）

参考 docs/LIMIT_ORDER_DESIGN.md（限价单生命周期与风控细节）。本节定义“路由 × 下单方式”策略矩阵与校验要点：

路由到下单方式（默认）
- trend_carry：market 为主；仅在“回踩到关键均线/结构”的再入场可用 limit（小偏离、短有效期）。
- pullback_ema：limit 优先（贴近 3m/15m EMA20 ±0.4%）；若错过结构位且强确认，可临时 market。
- breakout_volexp：market 优先（避免错失爆发段）；失败重测再尝试 limit。
- range_grid：limit-only（轻仓、小网格；市价仅用于快速风险释放/平仓）。
- no_trade：不下单；若有挂单，取消。

限价单锚定与有效期（建议）
- 价格锚定：
  - 多：支撑/EMA20/VWAP 附近，且 limit_price ≤ 当前价，偏离不超过 ~2–3%；
  - 空：阻力/EMA20/VWAP 附近，且 limit_price ≥ 当前价，偏离不超过 ~2–3%；
- 有效期（expire_kbars，3mK线数量）：pullback_ema 6–12；range_grid 10–20；trend_carry 再入场 4–6；breakout 2–4；
- TIF：默认 GTC；breakout 可用短 IOC（若支持）避免残留。

全局挂单规则（引擎强制）
- 同一 symbol 同方向最多 1 笔活动限价单；存在未决挂单时禁止重复挂单（需先 cancel）。
- Regime/Route/结构失效时自动取消挂单（或 LLM 发出 cancel_limit）。
- 价格校验：
  - 做多：limit_price ≤ 当前价；做空：limit_price ≥ 当前价；偏离超阈拒绝；
  - 步长对齐后仍须满足最小名义（NOTIONAL/MIN_NOTIONAL）；不足 → wait；
- 小资金护栏：预计绝对净利 ≥ max(手续费×5, 固定U)；否则拒绝挂单；
- 与硬风控一致：RR、最小止损距离(ATR)、保护单方向、3m动能冲突否决、BTC软化通道等。

LLM 与规则分工
- LLM：基于已选 route 输出 order_type、limit_price（必要时）与简短理由；不得改变 Regime/Route/硬约束；
- 引擎：校验 order_type/limit_price（方向/偏离/步长/名义/绝对净利/结构贴合），管理限价单生命周期（部分成交、过期、取消）。

实现要点（集成）
- Decision 扩展：`order_type: "market"|"limit"`, `limit_price`, `expire_kbars`；
- validateDecision：按路由与全局规则校验 `order_type/limit_price`；
- Trader 层：支持限价下单、查询/取消、部分成交更新；
- 前端：展示挂单列表、已成交/部分成交、过期/取消；
	- Router：在 constraints 输出 `default_order_type`、`limit_anchor`（ema20/vwap/level）、`expire_kbars` 建议。
	
	## 15. 档位化配置（超参数预设）
	
	目标：仅向用户暴露一个“风险档位”旋钮，其余复杂阈值/频率/风控参数全部由档位映射生成（仍保留高级覆盖入口）。
	
	- 对外配置（唯一必填）
	  - config.json: `"profile": "balanced"`（ultra_safe | safe | balanced | bold | ultra_bold）
	
	- 内部映射（示例，落地可微调）
	  - ultra_safe（极稳）
	    - opp_score_min=10；rr_min=3.5；risk_pct_max=1.0%；ema_near_pct=0.3
	    - leverage_max=3；abs_profit_min_usd=2.0；profit_fee_multiplier=6
	    - symbol_min_interval=15m；global_max_per_scan=1；max_calls_hourly/daily=3/50
	    - btc_soft_channel=off；sl_min_base=1.2%；temperature=0.0
	  - safe（稳健）
	    - opp_score_min=9；rr_min=3.3；risk_pct_max=1.5%；ema_near_pct=0.35
	    - leverage_max=4；abs_profit_min_usd=1.8；profit_fee_multiplier=5.5
	    - symbol_min_interval=12m；global_max_per_scan=1；max_calls=6/100
	    - btc_soft_channel=cautious；sl_min_base=1.0%；temperature=0.0
	  - balanced（均衡，默认）
	    - opp_score_min=8；rr_min=3.0；risk_pct_max=2.0%；ema_near_pct=0.4
	    - leverage_max=5；abs_profit_min_usd=1.5；profit_fee_multiplier=5
	    - symbol_min_interval=10m；global_max_per_scan=1；max_calls=10/200
	    - btc_soft_channel=on（按文档条件）；sl_min_base=1.0%；temperature=0.0
	  - bold（积极）
	    - opp_score_min=7；rr_min=2.8；risk_pct_max=2.5%；ema_near_pct=0.5
	    - leverage_max=6；abs_profit_min_usd=1.2；profit_fee_multiplier=4.5
	    - symbol_min_interval=8m；global_max_per_scan=2；max_calls=20/400
	    - btc_soft_channel=on（略放松）；sl_min_base=0.8%；temperature=0.1
	  - ultra_bold（进取）
	    - opp_score_min=6；rr_min=2.5；risk_pct_max=3.0%；ema_near_pct=0.6
	    - leverage_max=8；abs_profit_min_usd=1.0；profit_fee_multiplier=4
	    - symbol_min_interval=6m；global_max_per_scan=3；max_calls=30/600
	    - btc_soft_channel=on（放松最多）；sl_min_base=0.8%；temperature=0.15
	
	- 固定硬约束（不随档位变化）
	  - 每单保证金 ≥ 10U（required_margin = position_size_usd / leverage）
	  - 名义 ≥ MIN_NOTIONAL 且数量按 MARKET_LOT_SIZE/LOT_SIZE 对齐（向下取整后仍需满足最小名义）
	  - 止损距离 ≥ max(1.0%, 0.8×3m ATR%)
	  - 保护单方向正确（LONG: SL<现价/TP>现价；SHORT 相反）
	
	- 路由与软化通道的档位影响
	  - 路由映射保持（trend_early/mid → pullback_ema|trend_carry；trend_late → no_trade；range → range_grid）
	  - 档位决定：是否启用软化通道、启用条件强弱、所需信心/RR、杠杆与风险上限、LLM 调用预算、限价单偏好与有效期范围。
	
	- 覆盖优先级（实现）
	  1) 显式高级覆盖（config.json 中具体字段）>
	  2) 档位派生值（ProfileToParams(profile)）>
	  3) 内置默认值
	
	- 前端与可观测
	- 前端：一个滑块/旋钮选择档位，并显示档位摘要；
	- 日志：打印“当前档位 + 派生参数摘要”，便于复盘与灰度对比。
	
	## 16. 降低 LLM 频次：事件驱动 + 机会阈值 + 去抖

目标：将 LLM 从固定扫描循环中解耦，仅在“真机会”出现时触发，减少费用与噪声决策。

事件驱动触发（任一命中才考虑唤起 LLM）
- Regime 切换：trend↔range、early→mid/late；
- 结构事件：3m/15m MACD 金/死叉、价格进入/离开 EMA20±阈值、触碰支撑/阻力/VWAP；
- 波动事件：3m ATR 跃迁、BB 带宽由窄→扩、VWAP 偏离回归；
- 资金/情绪：OI 4h 变动 > 阈值、资金费率翻转；
- 新闻：NewsScore |≥ 2（强正/负面）。

机会阈值（OpportunityScore，示例打分）
- 多周期一致性≥3/4 +3；贴近 EMA20（≤0.4%）+2；15m MACD 同向+2；RR 估算≥3.5 +3；  
  绝对净利≥max(手续费×5, 固定U) +3；BTC 支持/软化通道 +2；  
  动能冲突（-∞）；距离 EMA20 > 3%（-3）。  
- 仅当 Score ≥ 门槛（如 8/12）时触发 LLM。

去抖/节流
- per-symbol 最小重试间隔：≥ 6–12 分钟；全局每周期最多调用 N 次（如 1）；  
- 关键特征哈希：仅当（Regime/EMA接近/交叉方向/NewsScore 桶）变化时重试；  
- 否决后冷却：本 symbol 增加 6 分钟冷却，避免抖动。

Carry/Range 的“无 LLM 执行模式”
- trend_carry：追踪止损与分批完全规则化（事件触发更新），通常不唤起 LLM；  
- range_grid：小仓网格由规则驱动（价格栅格/绝对净利阈值/趋势切换即平），仅结构破坏时通知或直接规则平仓。

预算与频率（配置建议）
“预算与频率（配置建议）”统一到配置示例：

  - 更新：config.json.example
      - 新增 llm 段：
          - event_mode: true
          - opp_score_min: 8
          - ema_prox_pct: 0.4
          - oi_delta_pct: 5
          - bb_bw_min: 2.0
          - max_calls_hourly: 10
          - max_calls_daily: 200
          - symbol_min_interval_min: 10
          - global_max_per_scan: 1
          - temperature: 0.0
      - 新增 trading_thresholds 段：
          - abs_profit_min_usd: 1.5
          - profit_fee_multiplier: 5

  说明

  - 目前代码主要从环境变量读取这些阈值；此变更先统一示例配置，便于后续接入读取逻辑。
  - 如需我把这些 JSON 配置接入引擎读取（替代 env），我可以下一步改造 decision/engine 与
    main 配置同步逻辑。

引擎插点（实现顺序）
1) 在 decision/engine 主循环前增加 `preCheck()`：计算 Regime、事件命中、OpportunityScore；不达标则直接 `wait`（不调用 LLM）；  
2) 维护 per-symbol 最近特征哈希 + 上次调用时间，做去抖；  
3) Carry/Range 路由下，将“持仓管理/网格”规则化；  
4) 仅当路由 ∈ {pullback_ema, breakout_volexp} 且 preCheck 通过时调用 LLM。

## 17. 仓位管理与风险管理（规则化，非 LLM 轮询）

目标：日常持仓与风险控制完全规则化、事件触发；LLM 仅在少数需要判断的场景介入。

仓位状态机（单仓）
- Build：开仓即挂 SL/TP；禁止加码；记录入场价与 R（止损距离）；  
- Hold（事件触发，不唤起 LLM）：  
  - BE：浮盈≥+1R（或≥+3%）→ SL 移至入场；  
  - 分批：+2R/+3R（或+6%/+10%）→ 各减仓 30%/30%，SL 提至 +0.7R/+1.5R；  
  - 追踪：ATR 轨道（多：SL=max(结构SL, 价- k×ATR)；空：min(结构SL, 价+ k×ATR)），k≈1.5–2.0；  
  - 结构变更：跌破/收回 EMA20/50、3m/15m MACD 反向、关键位破/收回 → 规则平/减；  
- Exit：结构破坏/超时/强负面新闻（NewsScore ≤ -2）→ 平仓或切 route；

事件源（触发而非轮询）
- 价格/指标：触及 SL/TP、MACD 金/死叉、进入/离开 EMA20±阈值、关键位触碰；  
- 波动：ATR 跃迁、BB 带宽突变；  
- 执行：部分成交、挂单过期/取消、保护单触发；  
- 新闻：持仓符号或全局 BTC 的 NewsScore |≥ 2；

风险管理（规则）
- 单笔风险：风险预算=净值×risk_pct（小资金建议 0.5–1.0%）；  
  数量=风险预算/止损价差；RR≥3（或路由要求≥3.5）；  
  绝对净利阈值：预计净利 ≥ max(手续费×5, 固定U)；  
- 组合风险：单币本金≤30%，合计≤80%，最大持仓=3；保证金使用率≥70% 降速，≥80% 禁开；  
- 节律与止损：止损后冷却 6–12min；连亏 3/4/5 暂停 30min/12h/48h；日内亏损/回撤阈值触发停手；  
- 杠杆与路由：trend_carry ≤5x、pullback_ema ≤3x、breakout ≤5x、range_grid ≤2x；  
  BTC 软化通道生效：≤3x、risk≤1%；

LLM 参与边界（尽量少）
- Regime 逆转但规则不明确（反手/观望）；  
- 强新闻（NewsScore |≥ 2）+ 规则未覆盖的处置；  
- 跨仓位协调（换仓/再分配）；

实现蓝图
- PositionManager（goroutine）：订阅事件，按状态机更新 SL/TP/partial_close/close；更新节流（幅度≥0.3%或间隔≥30s）；  
- preCheck 门禁（engine）：机会达标才唤起 LLM；carry/range 轨道默认不唤起；  
	- 配置（config.json）：  
	  - 风险预算：risk_per_trade_pct、max_positions、max_total_margin_pct；  
	  - 追踪/分批：be_r、多级分批数组、atr_trail_k、update_throttle_sec；  
	  - 绝对利润阈值：abs_profit_min_usd、profit_fee_multiplier；  
	  - LLM 预算：llm.event_mode、opp_score_min、symbol_min_interval_min、max_calls_hourly/daily。

## 18. ReAct-lite Agent（Go 原生）与 smolagents 微服务（可选）

目标：在降频前提下，让 LLM 以“按需取数、分步验证”的方式做更稳健的决策；保持所有交易执行与硬风控在引擎侧，LLM 不直接下单。

- 模式选择（分阶段）
  - A. ReAct-lite（Go 内嵌，优先实施）
    - 受 preCheck 门禁：仅在 OpportunityScore 达标时启动
    - 步数上限：2-3 步；温度 0.0；总 token/步数双限
    - 只读工具（白名单）：
      - get_market(symbol, windows) → 简要快照（价格、EMA、MACD、ATR 等）
      - get_rules(symbol) → 交易所 MIN_NOTIONAL / MARKET_LOT_SIZE / LOT_SIZE
      - opp_score(symbol) → 结构/量能/OI/净利估算的统一分
      - news_score(symbol|global) → 新闻打分（如有）
    - 产出：受限 JSON 决策（不含执行），由引擎 validateDecision 后落单
    - 安全：禁网络、禁写；工具结果截断（≤2KB）；工具参数校验；失败回退单轮模板
  - B. smolagents 微服务（可选后续）
    - 独立 Python 服务；仅在 preCheck 通过时调用
    - 优点：工具生态更快扩展；缺点：新依赖与跨语言调试成本

- 集成流程（ReAct-lite）
  1) preCheck 命中 → Router 选择 route；
  2) 若 route.agent=true（如 pullback_ema 优先）→ 调用 AgentRunner
  3) Agent 执行最多 N 步：Thought → Action(tool+args) → Observation（由工具返回）→ 最终 JSON
  4) 引擎对 JSON 做硬校验（保证金≥10U、MIN_NOTIONAL/步长、RR、SL距离、净利阈值、方向保护等）
  5) 通过则执行下单/挂单（含限价生命周期管理）

- 频控与缓存
  - 仍受 llm.* 预算约束；同 symbol 最小间隔遵循档位
  - 工具级缓存：同 symbol 的 get_rules 缓存 1h；get_market 可短缓存（≤30s）以减负

- 实现清单（A 阶段）
  - decision/agent/runner.go（新）：有限步 ReAct 控制循环
  - decision/agent/tools.go（新）：只读工具实现与参数校验
  - prompts/agent/react_base.txt（新）：极简 ReAct 模板（要求输出最终 JSON）
  - decision/router.go：为特定 route 标记 agent=true
  - auto_trader：在调用 LLM 前检测 route.agent，分流到 AgentRunner
  - 日志：保存步骤轨迹（Thought/Action/Observation/Final）

- 配置项（建议）
  - llm.agent_enabled: true|false（默认 false，灰度）
  - llm.agent_max_steps: 3
  - llm.agent_step_token_limit: 512
  - llm.agent_run_token_limit: 2048
  - llm.agent_tools: ["get_market","get_rules","opp_score","news_score"]

	- 边界与保底
	  - 永不授予“下单类工具”；交易执行仍走引擎与硬风控
	  - ReAct-lite 失败或超限时回退到单轮模板（保持可用性）

### 18.1 工具清单与优先级

- V1 必备（建议首先落地）
  - get_market(symbol, windows=["3m","15m","1h","4h"])
    - 返回: price、ema20(各窗)、macd(3m/15m)、rsi(3m/15m)、atr_3m、bb_width、vwap、关键信号
  - get_rules(symbol)
    - 返回: min_notional、step_size_market、step_size_lot、tick_size
  - get_account()
    - 返回: equity、available_balance、margin_used_pct、position_count
  - get_positions(symbol?)
    - 返回: {side, qty, leverage, entry, mark, sl, tp, pnl}
  - estimate_trade(symbol, side, leverage, position_size_usd, stop_loss, take_profit)
    - 返回: rr、margin_needed、est_fees、net_profit_est、min_notional_ok、step_ok
  - opp_score(symbol)
    - 返回: score_total、breakdown（ema贴近/量能/OI/RR/净利/BTC支持等，与 preCheck 同口径）

- V1.1 增强（按需启用）
  - get_regime() → {type, confidence, evidence}
  - get_btc_state() → {dir_1h, dir_4h, adx, macd_sign}
  - get_news_score(symbol|global) → {score, sources, age_min}
  - get_open_orders(symbol) → 未决限价摘要（避免重复挂单）
  - get_losses(trader_id, lookback, limit, symbols?) → 仅亏损成交记录（来源=exchange）
  - get_perf_summary() → 连亏、最大回撤、最差币种、近期亏损标签

- 辅助校验（避免交易所报错/小额残留）
  - compute_quantity(symbol, position_size_usd, price) → qty_formatted（按步长向下取整）
  - check_partial_close_feasible(symbol, side, close_pct) → {ok, fallback="full_close"}（剩余价值>10U 且步长可下）

- 安全与输出
  - 全为只读；输出结构化且≤2KB；参数白名单校验；错误回退单轮模板。

### 18.2 技术选型与可替换后端

- 默认实现：LocalReActRunner（Go 原生）
  - 优点：零外部依赖、与引擎/风控同语言、可控性强
  - 实现：decision/agent/runner.go + tools.go；通过统一 AgentRunner 接口供 engine 调用

- 备选一：SmolagentsRunner（Python 微服务）
  - 优点：工具生态丰富、扩展快；缺点：部署与跨语言调试成本
  - 形态：HTTP/gRPC 服务，接收 symbols/上下文，返回 JSON 决策 + trace

- 备选二：Gemini/ADK/Tool‑Calling（Go SDK/HTTP）
  - 场景：若主力 LLM 切到 Gemini 且需要其原生工具调用能力
  - 做法：实现 GeminiRunner，映射工具 schema，仍通过 AgentRunner 接口接入

- 抽象接口（建议）
  - type AgentRunner interface { Run(ctx, symbols) (decisions, trace, error) }
  - 多实现并存：LocalReActRunner（默认）/SmolagentsRunner/GeminiRunner
