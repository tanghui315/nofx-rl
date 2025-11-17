package decision

// StrategyRoute 策略路由
type StrategyRoute string

const (
	RouteTrendCarry   StrategyRoute = "trend_carry"
	RoutePullbackEMA  StrategyRoute = "pullback_ema"
	RouteBreakoutVOE  StrategyRoute = "breakout_volexp"
	RouteRangeGrid    StrategyRoute = "range_grid"
	RouteNoTrade      StrategyRoute = "no_trade"
)

// RouteConstraints 路由约束（供提示词与引擎校验参考）
type RouteConstraints struct {
	RRMin             float64 `json:"rr_min,omitempty"`
	LeverageMax       int     `json:"leverage_max,omitempty"`
	RiskPctMax        float64 `json:"risk_pct_max,omitempty"`
	EMANearPct        float64 `json:"ema_near_pct,omitempty"`
	ExpireKBars       int     `json:"expire_kbars,omitempty"`
	DefaultOrderType  string  `json:"default_order_type,omitempty"` // "market"|"limit"
	BTCSoftChannel    bool    `json:"btc_soft_channel,omitempty"`
	AbsProfitMinUSD   float64 `json:"abs_profit_min_usd,omitempty"`
	ProfitFeeMul      float64 `json:"profit_fee_multiplier,omitempty"`
}

// RouteResult 路由结果
type RouteResult struct {
	Route        StrategyRoute   `json:"route"`
	TemplatePath string          `json:"template"`
	Constraints  RouteConstraints `json:"constraints"`
	Agent        bool            `json:"agent"` // 是否建议使用 ReAct-lite Agent
}

// RouteStrategy 简单路由（V1 占位实现）
// - 小资金优先 pullback_ema
// - 趋势晚期/震荡 → no_trade 或 range_grid（保守）
func RouteStrategy(regime Regime, accountEquity float64, smallCapital bool) RouteResult {
	res := RouteResult{
		Route:        RouteNoTrade,
		TemplatePath: "adaptive_moderate_hist_v6_3", // 默认使用平衡基座
		Constraints: RouteConstraints{
			RRMin:            3.0,
			LeverageMax:      5,
			RiskPctMax:       1.5,
			EMANearPct:       0.4,
			ExpireKBars:      8,
			DefaultOrderType: "market",
			BTCSoftChannel:   true,
			AbsProfitMinUSD:  1.5,
			ProfitFeeMul:     5,
		},
		Agent: false,
	}

	switch regime.Type {
	case RegimeTrendEarly, RegimeTrendMid:
		if smallCapital {
			res.Route = RoutePullbackEMA
			res.TemplatePath = "pullback_ema/prompt_pullback_ema"
			res.Constraints.DefaultOrderType = "limit"
			res.Constraints.RRMin = 3.5
			res.Agent = true // 小资金 + 回抽结构，优先使用 ReAct-lite 工具化执行
		} else {
			res.Route = RouteTrendCarry
			res.TemplatePath = "trend_carry/prompt_trend_carry"
			res.Constraints.RRMin = 3.0
			res.Constraints.DefaultOrderType = "market"
			res.Agent = false
		}
	case RegimeTrendLate:
		res.Route = RouteNoTrade
		res.TemplatePath = "adaptive_moderate_hist_v6_3"
		res.Agent = false
	case RegimeRange:
		res.Route = RouteRangeGrid
		res.TemplatePath = "range_grid/prompt_range_grid"
		res.Constraints.DefaultOrderType = "limit"
		res.Agent = false
	}
	return res
}
