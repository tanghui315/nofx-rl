# 策略路由与限价单落地实施路线图 (Implementation Roadmap)

**版本**: v1.0  
**日期**: 2025-05-20  
**状态**: 规划中 (Planning)

---

## 1. 后端改造 (Backend Refactoring)

### 1.1 决策引擎升级 (`decision/`)
**目标**: 支持 LLM 输出限价单指令和策略标签。

1.  **修改 `decision/engine.go`**:
    *   更新 `Decision` 结构体：
        ```go
        type Decision struct {
            // ... existing fields
            OrderType   string  `json:"order_type,omitempty"` // "market" or "limit"
            LimitPrice  float64 `json:"limit_price,omitempty"`
            TimeInForce string  `json:"time_in_force,omitempty"` // "GTC", "IOC", "FOK"
            Strategy    string  `json:"strategy,omitempty"`      // 当前使用的策略标签 (仅用于展示)
        }
        ```
    *   更新 `validateDecision` 函数：
        *   如果 `OrderType == "limit"`，校验 `LimitPrice > 0`。
        *   校验 `TimeInForce` 有效性。

### 1.2 交易接口扩展 (`trader/`)
**目标**: 统一“下单”接口，支持全功能的订单参数。

1.  **定义通用接口 (`trader/interface.go`)**:
    *   新增 `CreateOrder(...)` 方法，参数包含 `OrderType`, `Price`, `TimeInForce`, `PostOnly`。
    *   新增 `GetOpenOrders(symbol)` 方法，返回所有挂单（不仅是 SL/TP）。
    *   新增 `CancelOrder(symbol, orderID)` 方法。

2.  **改造 `trader/binance_futures.go` & `trader/aster_trader.go`**:
    *   实现 `CreateOrder`：
        *   映射 `OrderType` 到交易所 SDK 的枚举 (e.g., `futures.OrderTypeLimit`).
        *   映射 `TimeInForce`。
    *   实现 `GetOpenOrders`：调用 `/fapi/v1/openOrders`，返回标准化结构。
    *   重构现有的 `OpenLong/Short`：改为调用底层的 `CreateOrder`，保持向后兼容。

3.  **更新 `trader/auto_trader.go`**:
    *   在 `executeDecision` 中，根据 `d.OrderType` 决定调用逻辑。
    *   如果是 `LIMIT` 单，调用 `trader.CreateOrder`。
    *   **新增挂单监控**：在 `runCycle` 或独立 goroutine 中，定期检查 `GetOpenOrders`。
        *   *超时策略*：如果挂单 N 分钟未成交，自动撤单或追单（根据 LLM 配置）。

### 1.3 路由代理 (`decision/router.go`)
**目标**: 实现策略分发逻辑。

1.  **新建 `decision/router.go`**:
    *   定义 `RouterResponse` 结构体。
    *   实现 `AnalyzeRegime(ctx *Context)` 函数：
        *   调用 LLM (Router Prompt) 分析市场状态。
        *   返回 map: `Symbol -> StrategyCode`.
2.  **集成到 `AutoTrader`**:
    *   在 `AutoTrader` 中维护 `strategyCache map[string]string`。
    *   在 `runCycle` 开始时，检查是否触发“重路由” (Re-route)：
        *   触发条件：BTC 波动剧烈 OR 连续亏损 OR 定时 (每 4h)。
    *   如果未触发，沿用 Cache 中的策略。
    *   根据 StrategyCode 加载对应的 Prompt 文件（拼接到 Base Prompt）。

---

## 2. Prompt 工程 (Prompt Engineering)

### 2.1 文件拆分
将 `prompts/adaptive_moderate_hist_v6_3.txt` 拆分为：

1.  **`prompts/base/instruction.txt`**:
    *   核心风控、JSON 格式、输出要求。
2.  **`prompts/router/analyze.txt`**:
    *   专门用于 Router Agent，教它如何看盘面定策略。
3.  **`prompts/strategies/*.txt`**:
    *   `trend_carry.txt`: 趋势跟随。
    *   `pullback_ema.txt`: 均线回抽 (强制 Limit 单)。
    *   `range_grid.txt`: 震荡网格 (Limit Buy Low / Sell High)。
    *   `breakout.txt`: 突破交易。

---

## 3. 前端适配 (Frontend)

### 3.1 挂单管理 (`web/src/components/AITradersPage.tsx`)
1.  **API 增加字段**:
    *   后端 `/api/trader/{id}/open-orders` 接口（需新增）。
2.  **界面改造**:
    *   在“持仓 (Positions)” 旁边增加 “挂单 (Open Orders)” Tab。
    *   列表展示：时间、币种、方向、类型 (Limit)、价格、数量、状态。
    *   操作列：增加“撤单 (Cancel)” 按钮。

### 3.2 决策卡片优化
1.  **展示策略标签**:
    *   在决策卡片顶部，高亮显示当前策略，例如 `<Badge>Strategy: Trend Carry</Badge>`。
2.  **限价单反馈**:
    *   如果 Action 是挂单，显示“挂单中 @ 65000”，而不是“已开仓”。

---

## 4. 执行计划 (Action Plan)

1.  **Step 1 (Backend Base)**: 修改 `Decision` 结构体，扩展 `trader` 接口 (CreateOrder/GetOpenOrders)。
2.  **Step 2 (Prompt Split)**: 完成 Prompt 文件的物理拆分和整理。
3.  **Step 3 (Router Logic)**: 实现 `AutoTrader` 中的 Prompt 动态拼接逻辑。
4.  **Step 4 (Frontend)**: 开发挂单展示与撤单功能。
5.  **Step 5 (Testing)**: 在 Testnet 进行全链路测试，特别是 `pullback_ema` 策略的挂单与撤单流程。

