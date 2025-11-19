package decision

import (
	"fmt"
	"log"
	"math"
	"nofx/market"
	"nofx/mcp"
	"os"
	"strings"
	"sync"
	"time"
)

// StrategyRoute 策略路由信息
type StrategyRoute struct {
	Symbol       string    `json:"symbol"`
	StrategyCode string    `json:"strategy_code"` // 策略代码 (e.g. "trend_carry", "adaptive_moderate_v6_3")
	Reason       string    `json:"reason"`        // 路由理由
	UpdateTime   time.Time `json:"update_time"`   // 更新时间
}

// Router 策略路由器
type Router struct {
	// 缓存: Symbol -> Route Info
	routes map[string]*StrategyRoute
	mu     sync.RWMutex

	// 配置
	DefaultStrategy string        // 默认策略
	TTL             time.Duration // 路由有效期 (默认4小时)
}

// NewRouter 创建路由器
func NewRouter(defaultStrategy string) *Router {
	if defaultStrategy == "" {
		defaultStrategy = "adaptive_moderate_v6_3" // 默认使用新版策略
	}
	return &Router{
		routes:          make(map[string]*StrategyRoute),
		DefaultStrategy: defaultStrategy,
		TTL:             4 * time.Hour,
	}
}

// GetRoute 获取指定币种的当前路由
func (r *Router) GetRoute(symbol string) *StrategyRoute {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	if route, ok := r.routes[symbol]; ok {
		return route
	}
	// 如果不存在，返回默认
	return &StrategyRoute{
		Symbol:       symbol,
		StrategyCode: r.DefaultStrategy,
		Reason:       "Default (No Route)",
		UpdateTime:   time.Now(),
	}
}

// UpdateRoute 更新路由
func (r *Router) UpdateRoute(symbol, strategy, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	r.routes[symbol] = &StrategyRoute{
		Symbol:       symbol,
		StrategyCode: strategy,
		Reason:       reason,
		UpdateTime:   time.Now(),
	}
	log.Printf("🔄 路由更新 [%s]: %s (%s)", symbol, strategy, reason)
}

// AnalyzeRegime 分析市场状态并更新路由 (接入 LLM Router Agent)
func (r *Router) AnalyzeRegime(ctx *Context, mcpClient *mcp.Client) map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make(map[string]string)
	now := time.Now()

	// 1. 筛选需要更新路由的币种
	var symbolsToUpdate []string
	
	// 遍历所有相关币种（持仓 + 候选）
	allSymbols := make([]string, 0)
	seen := make(map[string]bool)
	
	for _, p := range ctx.Positions {
		if !seen[p.Symbol] {
			allSymbols = append(allSymbols, p.Symbol)
			seen[p.Symbol] = true
		}
	}
	for _, c := range ctx.CandidateCoins {
		if !seen[c.Symbol] {
			allSymbols = append(allSymbols, c.Symbol)
			seen[c.Symbol] = true
		}
	}

	for _, symbol := range allSymbols {
		// 检查缓存是否过期
		if route, ok := r.routes[symbol]; ok {
			if now.Sub(route.UpdateTime) < r.TTL {
				// 未过期，沿用旧路由
				result[symbol] = route.StrategyCode
				continue
			}
		}
		// 需要更新
		symbolsToUpdate = append(symbolsToUpdate, symbol)
	}

	// 如果没有需要更新的，直接返回
	if len(symbolsToUpdate) == 0 {
		return result
	}

	// 2. 构建 Router Prompt 并调用 LLM
	// 为了节省 Token，我们只发送必要的数据
	// 分批处理？暂且全量，因为 Candidate 已经被筛选过数量了
	log.Printf("🧠 正在请求 Router Agent 分析 %d 个币种的市场状态...", len(symbolsToUpdate))

	routerSystemPrompt := buildRouterSystemPrompt(r.DefaultStrategy)
	routerUserPrompt := buildRouterUserPrompt(ctx, symbolsToUpdate)

	aiResponse, err := mcpClient.CallWithMessages(routerSystemPrompt, routerUserPrompt)
	if err != nil {
		log.Printf("❌ Router Agent 调用失败: %v，回退到默认策略", err)
		for _, sym := range symbolsToUpdate {
			r.routes[sym] = &StrategyRoute{
				Symbol:       sym,
				StrategyCode: r.DefaultStrategy,
				Reason:       "Router Error Fallback",
				UpdateTime:   now,
			}
			result[sym] = r.DefaultStrategy
		}
		return result
	}

	// 3. 解析 LLM 响应并更新路由
	routes, err := parseRouterResponse(aiResponse)
	if err != nil {
		log.Printf("❌ Router 响应解析失败: %v，回退到默认策略", err)
		// 仅更新解析失败的部分为默认
		for _, sym := range symbolsToUpdate {
			r.routes[sym] = &StrategyRoute{
				Symbol:       sym,
				StrategyCode: r.DefaultStrategy,
				Reason:       "Parse Error Fallback",
				UpdateTime:   now,
			}
			result[sym] = r.DefaultStrategy
		}
		return result
	}

	// 4. 应用新路由
	for _, route := range routes {
		// 验证 StrategyCode 是否有效（防止 AI 瞎编）
		validCode := validateStrategyCode(route.StrategyCode, r.DefaultStrategy)
		
		r.routes[route.Symbol] = &StrategyRoute{
			Symbol:       route.Symbol,
			StrategyCode: validCode,
			Reason:       route.Reason,
			UpdateTime:   now,
		}
		result[route.Symbol] = validCode
		log.Printf("  👉 路由 [%s] -> %s (理由: %s)", route.Symbol, validCode, route.Reason)
	}

	// 5. 填补未被 LLM 提及的币种（如果有遗漏）
	for _, sym := range symbolsToUpdate {
		if _, ok := result[sym]; !ok {
			r.routes[sym] = &StrategyRoute{
				Symbol:       sym,
				StrategyCode: r.DefaultStrategy,
				Reason:       "Router Missed Fallback",
				UpdateTime:   now,
			}
			result[sym] = r.DefaultStrategy
		}
	}

	return result
}

// buildRouterSystemPrompt 构建 Router 系统提示词
func buildRouterSystemPrompt(defaultStrategy string) string {
	return fmt.Sprintf(`# Role: Crypto Strategy Router
你是一个高频交易系统的路由代理（Router Agent）。你的任务是根据市场状态（Market Regime）为每个加密货币分配最合适的交易策略。

# Available Strategies
1. **trend_carry** (趋势跟随):
   - 适用: 强趋势（ADX > 25），价格在均线之上（多）或之下（空），动能强劲。
   - 行为: 顺势开仓，宽止损，忽略超买超卖。
2. **range_grid** (震荡网格):
   - 适用: 震荡/盘整（ADX < 20），布林带缩口/走平，无明显方向。
   - 行为: 边界高抛低吸，使用 Limit 单，严格止损。
3. **pullback_ema** (均线回抽):
   - 适用: 趋势中的回调阶段，价格回踩 EMA20/50，量能萎缩。
   - 行为: 均线处挂单接回，盈亏比高。
4. **%s** (默认策略/通用):
   - 适用: 状态不明朗，或不符合上述特征，或混合状态。
   - 行为: 平衡型策略，兼顾趋势和反转。

# Output Format
必须输出纯 JSON 数组，不要包含 markdown 标记。格式如下：
[
  {"symbol": "BTCUSDT", "strategy_code": "trend_carry", "reason": "Strong uptrend, ADX 35"},
  {"symbol": "ETHUSDT", "strategy_code": "range_grid", "reason": "Low vol, Bollinger bands squeezing"}
]
`, defaultStrategy)
}

// buildRouterUserPrompt 构建 Router 用户提示词
func buildRouterUserPrompt(ctx *Context, symbols []string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Current Time: %s\n", ctx.CurrentTime))
	sb.WriteString("Analyze the following markets and assign a strategy:\n\n")

	for _, sym := range symbols {
		if data, ok := ctx.MarketDataMap[sym]; ok {
			// 提取关键指标
			trend := "Neutral"
			if data.PriceChange24h > 3 { trend = "Up" }
			if data.PriceChange24h < -3 { trend = "Down" }
			
			vol := "Normal"
			if data.IntradaySeries != nil {
				atrPct := (data.IntradaySeries.ATR14 / data.CurrentPrice) * 100
				if atrPct < 1.0 { vol = "Low" }
				if atrPct > 3.0 { vol = "High" }
			}

			adx := "N/A"
			if data.IntradaySeries != nil {
				adx = fmt.Sprintf("%.1f", data.IntradaySeries.ADX14)
			}
			
			bbWidth := "N/A" // 暂时无法直接获取，略过
			
			sb.WriteString(fmt.Sprintf("- %s: Price=%.4f, 24hChg=%.2f%%, Trend=%s, Vol=%s, ADX=%s\n", 
				sym, data.CurrentPrice, data.PriceChange24h, trend, vol, adx))
		}
	}
	return sb.String()
}

// parseRouterResponse 解析 Router 响应
func parseRouterResponse(response string) ([]StrategyRoute, error) {
	// 复用 engine.go 中的工具函数（需要把那些工具函数导出或放在 common 包，或者在这里复制一份）
	// 为了简单，这里直接复制简化版，因为 router.go 和 engine.go 在同一个包 decision 下
	// 只要 engine.go 中的 extractDecisions 改为支持通用 JSON 提取即可。
	// 但 engine.go 的 extractDecisions 是针对 Decision 结构的。
	
	// 我们直接用 engine.go 中的 extractDecisions 类似的逻辑
	decisions, err := extractDecisionsInternal[StrategyRoute](response)
	return decisions, err
}

// extractDecisionsInternal 泛型提取函数 (需要 Go 1.18+)
// 由于 engine.go 里是私有的，我们暂时在这里重写一个简单的
func extractDecisionsInternal[T any](response string) ([]T, error) {
	// 1. 提取 JSON 字符串 (简单正则)
	// ... (此处为了代码简洁，我假设 response 已经是比较干净的 JSON 或包含在 ```json 中)
	
	// 清洗
	s := removeInvisibleRunes(response)
	s = strings.TrimSpace(s)
	s = fixMissingQuotes(s)
	
	// 提取
	if m := reJSONFence.FindStringSubmatch(s); m != nil && len(m) > 1 {
		s = m[1]
	} else if m := reJSONArray.FindString(s); m != "" {
		s = m
	}
	
	// Unmarshal
	var result []T
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil, err
	}
	return result, nil
}

// validateStrategyCode 验证策略代码
func validateStrategyCode(code, defaultStrat string) string {
	valid := map[string]bool{
		"trend_carry":          true,
		"range_grid":           true,
		"pullback_ema":         true,
		defaultStrat:           true,
		"adaptive_moderate_v6_3": true,
	}
	if valid[code] {
		return code
	}
	return defaultStrat
}


// GroupByStrategy 将币种按策略分组
func GroupByStrategy(routing map[string]string) map[string][]string {
	groups := make(map[string][]string)
	for sym, strat := range routing {
		groups[strat] = append(groups[strat], sym)
	}
	return groups
}

// GetRoutesSnapshot 获取路由表快照 (用于API展示)
func (r *Router) GetRoutesSnapshot() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	snapshot := make(map[string]string)
	for sym, route := range r.routes {
		snapshot[sym] = route.StrategyCode
	}
	return snapshot
}

