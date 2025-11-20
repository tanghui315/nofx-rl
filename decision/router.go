package decision

import (
	"encoding/json"
	"fmt"
	"log"
	"nofx/mcp"
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

// RouterTrace 用于记录本次 Router Agent 调用所使用的提示词与原始输出，便于写入决策日志
type RouterTrace struct {
	SystemPrompt string
	UserPrompt   string
	RawOutput    string
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
// 返回值：
//   - map[string]string: symbol -> strategy_code 的路由结果
//   - *RouterTrace: 本次 Router 调用所使用的 system/user prompt 与原始 LLM 输出（用于日志记录）
func (r *Router) AnalyzeRegime(ctx *Context, mcpClient *mcp.Client) (map[string]string, *RouterTrace) {
	// 0. 为 Router 加载市场数据，与主交易决策使用同一数据管线
	//    这样 Router 和主策略 Agent 看到的是同一份技术面/情绪/流动性信息
	if err := fetchMarketDataForContext(ctx); err != nil {
		log.Printf("⚠️ Router 获取市场数据失败: %v", err)
	}

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
		return result, nil
	}

	// 2. 构建 Router Prompt 并调用 LLM
	// 为了节省 Token，我们只发送必要的数据
	// 分批处理？暂且全量，因为 Candidate 已经被筛选过数量了
	log.Printf("🧠 正在请求 Router Agent 分析 %d 个币种的市场状态...", len(symbolsToUpdate))

	routerSystemPrompt := buildRouterSystemPrompt(r.DefaultStrategy)
	routerUserPrompt := buildRouterUserPrompt(ctx, symbolsToUpdate)

	// 预先构造 RouterTrace，便于即使调用失败也能记录提示词
	trace := &RouterTrace{
		SystemPrompt: routerSystemPrompt,
		UserPrompt:   routerUserPrompt,
		RawOutput:    "",
	}

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
		return result, trace
	}

	// 记录原始 LLM 输出，便于日志分析
	trace.RawOutput = aiResponse

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
		return result, trace
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

	return result, trace
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
4. **breakout_volexp** (放量突破):
   - 适用: 布林带长期收口后，突然伴随成交量剧增突破关键位。
   - 行为: 右侧追突破（Stop-Market 或 Market），目标是捕捉爆发性行情。
5. **no_trade** (观望):
   - 适用: 趋势末期、重大风险事件前夕、流动性枯竭或指标互相冲突。
   - 行为: 强制空仓观望，不进行任何交易。
6. **%s** (默认策略/通用):
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
// 这里复用与主决策 Agent 相同的数据管线，但采用更精简的多因子特征表，专注于“选策略”所需的信息
func buildRouterUserPrompt(ctx *Context, symbols []string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Current Time: %s\n", ctx.CurrentTime))
	sb.WriteString("You are the strategy router. For each symbol, analyze the market regime and choose the most appropriate strategy.\n\n")

	// 新闻速览（全局情绪与事件背景，控制条数避免过长）
	if len(ctx.News) > 0 {
		sb.WriteString("## Recent News (top 6)\n")
		shown := 0
		for _, n := range ctx.News {
			if shown >= 6 {
				break
			}
			line := n.Title
			if n.Source != "" {
				line += " — " + n.Source
			}
			if n.PublishedAt != "" {
				line += " (" + n.PublishedAt + ")"
			}
			if n.Symbol != "" {
				line = "[" + n.Symbol + "] " + line
			}
			sb.WriteString("- " + line + "\n")
			shown++
		}
		sb.WriteString("\n")
	}

	sb.WriteString("## Symbols\n\n")

	for _, sym := range symbols {
		data, ok := ctx.MarketDataMap[sym]
		if !ok || data == nil {
			continue
		}

		// 基本价格与涨跌幅
		sb.WriteString(fmt.Sprintf("### %s\n", sym))
		sb.WriteString(fmt.Sprintf("- Price: %.4f | 1h: %+.2f%% | 4h: %+.2f%%\n",
			data.CurrentPrice, data.PriceChange1h, data.PriceChange4h))

		// 波动率与成交量特征（区分趋势/震荡/爆发）
		bbWidth := 0.0
		if data.BollingerBands != nil {
			bbWidth = data.BollingerBands.BandWidth
		}
		atrPct := 0.0
		if data.IntradaySeries != nil && data.CurrentPrice > 0 {
			atrPct = (data.IntradaySeries.ATR14 / data.CurrentPrice) * 100
		}
		volRatio := 0.0
		if data.LongerTermContext != nil && data.LongerTermContext.AverageVolume > 0 {
			volRatio = data.LongerTermContext.CurrentVolume / data.LongerTermContext.AverageVolume
		}
		sb.WriteString(fmt.Sprintf("- Volatility: BBWidth %.2f%% | ATR(3m,14) %.2f%% | Volume %.1fx avg\n",
			bbWidth, atrPct, volRatio))

		// 趋势强度 + OI / Funding / OI Top（流动性与杠杆情绪）
		adx := 0.0
		if data.ADX != nil {
			adx = data.ADX.ADX
		}
		oiLatest := 0.0
		if data.OpenInterest != nil {
			oiLatest = data.OpenInterest.Latest
		}
		// OI Top 变化率（如有），用于识别主力增减仓
		oiTopDelta := 0.0
		if ctx.OITopDataMap != nil {
			if oiTop, ok := ctx.OITopDataMap[sym]; ok && oiTop != nil {
				oiTopDelta = oiTop.OIDeltaPercent
			}
		}
		sb.WriteString(fmt.Sprintf("- Trend/OI: ADX %.1f | OI %.2f | OI_top Δ%.2f%% | Funding %.4f\n",
			adx, oiLatest, oiTopDelta, data.FundingRate))

		// 语义化技术分析摘要（简化版）
		if data.Semantics != nil {
			signals := ""
			if len(data.Semantics.KeySignals) > 0 {
				signals = strings.Join(data.Semantics.KeySignals, ", ")
				if len(signals) > 120 {
					signals = signals[:120] + "…"
				}
			}
			sb.WriteString(fmt.Sprintf("- Semantic: trend %s/%s, momentum %s, vol %s\n",
				data.Semantics.TrendDirection,
				data.Semantics.TrendStrength,
				data.Semantics.MomentumStatus,
				data.Semantics.VolatilityLevel,
			))
			if signals != "" {
				sb.WriteString(fmt.Sprintf("  Key signals: %s\n", signals))
			}
		}

		sb.WriteString("\n")
	}

	sb.WriteString("Return a pure JSON array of {symbol, strategy_code, reason}.\n")
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
		"trend_carry":            true,
		"range_grid":             true,
		"pullback_ema":           true,
		"breakout_volexp":        true,
		"no_trade":               true,
		defaultStrat:             true,
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

