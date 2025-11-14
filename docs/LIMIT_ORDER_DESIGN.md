# 限价单支持与挂单生命周期管理技术方案

## 1. 目标与范围

### 1.1 目标

- 在现有全市价单的基础上，引入**限价下单能力**，并实现完整的挂单生命周期管理与风控闭环。
- 让 LLM：
  - 能显式选择 `市价/限价`；
  - 在决策时清楚“当前已持有仓位 + 已挂但未成交的限价单”所占用的风险与资金。
- 前端可以清晰展示：
  - 已成交仓位（市价/限价成交后的持仓）；
  - 未成交/部分成交的限价单（挂单队列）。

### 1.2 不在本轮范围内

- 不做复杂撮合策略（如冰山单、网格等），仅实现单价位限价单。
- 不在本轮实现“多级候补挂单队列”（一支撑位多档价格）；每个 symbol 先支持 0~N 笔简单限价单。

---

## 2. 核心概念与状态模型

### 2.1 新增概念

- **订单类型 `order_type`**
  - `"market"`：市价单（当前行为保持不变）。
  - `"limit"`：限价单（挂在指定价格，等待成交）。

- **限价单状态 `LimitOrder`**
  - 字段建议：
    - `id`：本地订单 ID（与交易所 `orderId` 关联，用于取消/跟踪）。
    - `symbol`：交易对，如 `ETHUSDT`。
    - `side`：`"long"` / `"short"`（语义同持仓方向）。
    - `price`：限价价格。
    - `quantity`：原始下单数量。
    - `filled_quantity`：已成交数量。
    - `status`：`pending` / `partially_filled` / `filled` / `canceled` / `expired` / `rejected`。
    - `created_at` / `updated_at`：时间戳。
    - `source`：`"llm"` / `"manual"` / `"anomaly"` 等。
    - `expire_kbars`：可选，挂单最多存活的 K 线数量（由系统或 LLM 决策）。

### 2.2 生命周期（状态机）

1. **创建（New）**
   - LLM 决策 `order_type="limit"`，提供 `limit_price`。
   - 后端调用交易所限价下单接口：
     - Binance: `LIMIT` + `GTC`/`IOC`；
     - Hyperliquid: `LimitOrderType` with `Gtc`；
   - 本地创建 `LimitOrder` 记录，状态 `pending`。

2. **部分成交（PartiallyFilled）**
   - 后端周期性查询 open orders 或通过成交推送更新；
   - `filled_quantity` > 0 且 `< quantity` → 状态 `partially_filled`；
   - 已成交部分计入持仓；剩余部分继续挂单。

3. **完全成交（Filled）**
   - `filled_quantity == quantity` → 状态 `filled`；
   - 本地挂单记录可保留一段历史，用于回溯。

4. **取消（Canceled）**
   - 触发方式：
     - LLM 决策明确发出 `cancel_limit` 或类似 action；
     - 用户在前端手动取消；
   - 后端调用交易所取消订单 API，更新状态为 `canceled`。

5. **过期（Expired）**
   - 系统配置：例如 `max_limit_order_kbars = 20`，代表超过 20 根 3m K 线还未完全成交 → 自动取消；
   - 逻辑：
     - 每个周期检查 `now - created_at` 对应 K 线数量；
     - 超过阈值 → 调用取消订单，状态 `expired`。

6. **拒绝（Rejected）**
   - 下单失败（例如价格/数量不符合交易所要求）；
   - 状态标记为 `rejected`，供调试使用。

---

## 3. Prompt & 决策结构扩展

### 3.1 `Decision` 结构体扩展（Go 层）

在 `decision/engine.go` 中扩展 `Decision`：

- 新增字段：
  - `OrderType string  "json:\"order_type,omitempty\""` // "market" | "limit"
  - `LimitPrice float64 "json:\"limit_price,omitempty\""` // 限价价格
  - 可选：`ExpireKBars int "json:\"expire_kbars,omitempty\""` // 该挂单最多存活的 K 线数

约束：

- 当 `action == "open_long" / "open_short"` 时：
  - 若未提供 `order_type`，默认 `"market"`（与当前行为保持一致）。
  - 若 `order_type == "limit"`，必须提供 `limit_price`，且验证：
    - 做多：`limit_price <= 当前价`（挂在下方回踩）；
    - 做空：`limit_price >= 当前价`（挂在上方反抽）；
    - 与当前价的偏离不能过大（例如 ≤3~5%，具体规则可放在 prompt + validateDecision 中）。

### 3.2 Prompt 中的规则调整

在 v6.3 提示词中增加一个子章节（只需一个模板，先不拆市价/限价双模板）：

> **入场方式（市价 vs 限价）**
> - 当出现强势突破 / 急跌急拉 / 异常波动时：
>   - 优先使用 `order_type = "market"`，避免错失关键行情；
> - 当计划在重要结构位（支撑/阻力/EMA20/VWAP/区间边界）“回踩/反抽”入场时：
>   - 可以使用 `order_type = "limit"`，并给出 `limit_price`：
>     - 多单：挂在支撑/EMA20 附近，且 `limit_price ≤ 当前价`，偏离不超过 ~2–3%；
>     - 空单：挂在阻力/EMA20 上方，且 `limit_price ≥ 当前价`，偏离不超过 ~2–3%；
> - 每次决策最多对同一 symbol 给出一笔未成交挂单，禁止“密集加仓型挂单”；
> - 若已有未成交挂单且逻辑已失效（结构破位 / 趋势反转），应优先发出 `cancel_limit` 决策，而不是继续加挂新单。

同时在输出格式说明中增加字段解释：

- `order_type`: `"market"` | `"limit"`；
- `limit_price`: 仅当 `order_type == "limit"` 时使用；
- （可选）`expire_kbars`: 该挂单最多等待的 K 线数量；若缺省，则使用系统默认。

### 3.3 限价挂单关闭策略（策略+系统双层）

双层机制：

1. **系统级超时配置（硬规则）**
   - 在 `system_config` 或 `config.json` 增加：
     - `limit_order_expire_kbars`：整数，例如 20（约等于 1 小时的 3m K 线）。
   - 挂单存活超过此阈值，系统自动取消，LLM 不得否决。

2. **Prompt 中要求 LLM 主动管理**
   - 在“持仓阶段行为”旁增加“挂单阶段行为”：
     - 若价格已经远离挂单价位、结构失效、或出现更优机会：
       - LLM 应明确给出 `cancel_limit` 决策，收回这笔挂单；
     - 不允许长期遗忘挂单，使得系统持有大量积尘挂单。

---

## 4. 后端实现概要（不立即开发）

> 本节是未来实现时的参考，不在本次改动中执行。

### 4.1 Trader 接口扩展

在 `trader/interface.go` 中增加：

- `OpenLongLimit(symbol string, quantity float64, leverage int, price float64) (map[string]interface{}, error)`
- `OpenShortLimit(symbol string, quantity float64, leverage int, price float64) (map[string]interface{}, error)`
- `GetOpenOrders(symbol string) ([]map[string]interface{}, error)`
- `CancelOrder(symbol string, orderID interface{}) error`

币安/Hype rliquid 的具体实现内部使用交易所的 LIMIT / GTC / IOC 接口。

### 4.2 AutoTrader 中的挂单管理

- AutoTrader 结构中增加：
  - `pendingOrders []LimitOrder` 或映射 `map[string][]LimitOrder`。
- 每轮 `runCycle` 时：
  - 刷新 open orders（从交易所拉取）；  
  - 同步状态到 `pendingOrders`（处理 filled / partially_filled / canceled）；  
  - 更新“已占用保证金/预期风险”。
- 在构建 LLM 上下文 (`buildTradingContext`) 时：
  - 添加一个简化的 `pending_orders` 段：
    - 每个 symbol 列出：
      - `side` / `price` / `remaining_qty` / `age_kbars` 等；
  - 供 LLM 在决策时考虑是否继续保留/取消挂单。

### 4.3 风控与仓位计算整合

- 现有仓位管理主要基于“已成交仓位”；引入挂单后需要：
  - 将挂单“潜在名义价值”纳入总风险预算，例如：
    - `effective_position_value = current_positions_value + pending_limit_value`；
  - 在校验单币上限、总仓位上限、保证金使用率时考虑挂单。
- 在 `validateDecision` 中新增检查：
  - 若当前挂单 + 持仓会超出币种/总仓位上限，拒绝新的 `open_*` 决策；
  - 记录详细错误信息，提示 LLM 调整（如取消旧挂单 / 减少新单 size）。

---

## 5. 前端展示与交互

### 5.1 数据结构扩展

API 层（`api/server.go`）新增：

- `GET /api/open-orders`（可带 `trader_id` / `symbol`）：
  - 返回当前未成交/部分成交的限价单列表：
    - `order_id` / `symbol` / `side` / `price` / `quantity` / `filled_quantity` / `status` / `created_at` 等。

### 5.2 UI 展示建议

- 在 Trader 详情页顶部保留“当前持仓”卡片，显示**实际持仓**；
- 增加一个“挂单队列”面板：
  - 列出当前所有 `pending` / `partially_filled` 的限价单；
  - 标明：
    - 是否来自 LLM；  
    - 已持仓+挂单的总风险占比；
  - 提供“取消”按钮（调用 `CancelOrder`）。

这样人可以很清楚看到：

- 目前有哪些“已在场”仓位（市价+限价成交后的持仓）；  
- 有哪些“排队中”的限价单，还没有真正成交。

---

## 6. 配置与参数

### 6.1 系统级配置（建议项）

- `limit_order_expire_kbars`：挂单最大存活 K 线数（如 20）；
- `limit_order_max_pending_per_symbol`：每个 symbol 允许的挂单上限数（例如 1~3）；
- `limit_order_max_slippage_pct`：限价价位相对当前价的最大偏离（例如 3%）。

### 6.2 Prompt 中的参数说明

- 对于上述系统参数，prompt 中可以这样表述：
  - “系统会在超过 N 根 3m K 线未成交时自动取消挂单”；  
  - “限价价格不得距离当前价格超过 3%”；  
  - “同一币种最多允许 1 笔未成交挂单”。

---

## 7. 演进路线建议

1. **阶段 1（Prompt + 结构扩展）**
   - 在 Decision 中加入 `order_type` / `limit_price` 字段；
   - 更新 v6.3 prompt，增加“何时用市价/限价”的规则；
   - Go 层暂时只接受 `market`，对 `limit` 给出错误提示（用于观察 LLM 行为）。

2. **阶段 2（挂单执行与生命周期）**
   - 实现 Trader 层 `OpenLongLimit/OpenShortLimit` 和挂单查询/取消；
   - 在 AutoTrader 中维护 `pendingOrders`，并纳入风险计算；
   - 开启对 `order_type="limit"` 决策的实际执行。

3. **阶段 3（优化与风格细分）**
   - 根据实盘表现微调 prompt 中市价/限价策略；
   - 如有需要，再考虑你最初提到的“两阶段 + 不同模板”的风格分离（例如“专门的回踩限价策略模板”）。

---

## 8. 小结

这份方案的核心是：

- 不直接引入“第二套完全独立的限价模板”，而是在**统一的决策模板**中让 LLM 显式选择 `order_type` 并给出 `limit_price`；
- 后端负责：
  - 正确地执行限价下单；
  - 维护清晰的挂单生命周期；
  - 将挂单风险纳入总风控；
  - 清晰地向 LLM 和前端暴露“未成交挂单”的状态；
- 系统通过“超时自动取消 + LLM 主动取消”的双层机制，避免挂单无限期遗忘。

在不急于实现的前提下，这个设计可以作为后续开发的蓝本，先从 prompt/结构扩展开始，小步前进。***
