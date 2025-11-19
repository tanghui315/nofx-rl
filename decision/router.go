package decision

import (
	"fmt"
	"log"
	"math"
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

// AnalyzeRegime 分析市场状态并更新路由 (核心逻辑)
// 目前实现为简单规则，后续可接入 LLM Router Agent
func (r *Router) AnalyzeRegime(ctx *Context) map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := make(map[string]string)
	now := time.Now()

	// 遍历所有相关币种（持仓 + 候选）
	allSymbols := make([]string, 0)
	for _, p := range ctx.Positions {
		allSymbols = append(allSymbols, p.Symbol)
	}
	for _, c := range ctx.CandidateCoins {
		allSymbols = append(allSymbols, c.Symbol)
	}

	for _, symbol := range allSymbols {
		// 1. 检查缓存是否过期
		if route, ok := r.routes[symbol]; ok {
			if now.Sub(route.UpdateTime) < r.TTL {
				// 未过期，检查是否有强制触发条件
				// Trigger: 波动率剧烈 (BTC 1h > 2% 简单模拟)
				// 这里简单略过，假设未过期则沿用
				result[symbol] = route.StrategyCode
				continue
			}
		}

		// 2. 需要更新路由
		// TODO: 这里应该调用 LLM Router Agent 或执行复杂规则
		// 暂时使用简单规则模拟
		
		strategy := r.DefaultStrategy
		reason := "Default Refresh"

		// 示例简单规则：
		// 如果是 BTC/ETH，使用 trend_carry (假设有这个策略)
		// 实际上我们暂时只有 adaptive_moderate_v6_3
		
		// 获取市场数据
		if data, ok := ctx.MarketDataMap[symbol]; ok {
			// 如果波动率极低，可能切换到 range_grid
			if data.IntradaySeries != nil && data.IntradaySeries.ATR14 > 0 {
				// atrPct := (data.IntradaySeries.ATR14 / data.CurrentPrice) * 100
				// if atrPct < 0.5 {
				// 	strategy = "range_grid"
				// 	reason = "Low Volatility"
				// }
			}
			// 如果持仓量剧增，可能切换到 aggressive
			if oiData, ok2 := ctx.OITopDataMap[symbol]; ok2 {
				if math.Abs(oiData.OIDeltaPercent) > 10 {
					// strategy = "aggressive_breakout" 
					// reason = "High OI Surge"
				}
			}
		}

		// 更新缓存
		r.routes[symbol] = &StrategyRoute{
			Symbol:       symbol,
			StrategyCode: strategy,
			Reason:       reason,
			UpdateTime:   now,
		}
		result[symbol] = strategy
	}

	return result
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

