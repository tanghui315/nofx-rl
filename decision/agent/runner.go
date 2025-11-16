package agent

import (
	"fmt"
	"math"
	"nofx/decision"
	"os"
	"strconv"
	"strings"
	"time"
)

// Runner 是 ReAct-lite 的统一接口（可替换实现）
type Runner interface {
	// Run 接收上下文与候选符号，返回 FullDecision（与 LLM 路径一致的承载结构）
	Run(ctx *decision.Context, symbols []string) (*decision.FullDecision, error)
}

// LocalReActRunner 是 Go 原生的最小可用实现（只读工具，受限步数）
type LocalReActRunner struct {
	MaxSteps int
}

func NewLocalReActRunner() *LocalReActRunner {
	return &LocalReActRunner{MaxSteps: 3}
}

// Run：当前占位实现（演示 ReAct-lite 流程）。策略：
// - 读取 symbols（若为空则使用 ctx.CandidateCoins 前若干个）
// - 计算每个 symbol 的机会分（与 PreCheck 同口径）
// - 仅输出 wait 决策与 CoT 轨迹（后续再扩展 tools 与开仓逻辑）
func (r *LocalReActRunner) Run(ctx *decision.Context, symbols []string) (*decision.FullDecision, error) {
	trace := &strings.Builder{}
	fmt.Fprintf(trace, "ReAct-lite (local) start; max_steps=%d\n", r.MaxSteps)

	picks := symbols
	if len(picks) == 0 {
		limit := len(ctx.CandidateCoins)
		if limit > 10 {
			limit = 10
		}
		for i := 0; i < limit; i++ {
			picks = append(picks, ctx.CandidateCoins[i].Symbol)
		}
	}
	if len(picks) == 0 {
		fmt.Fprintln(trace, "No symbols to evaluate; return wait")
		return &decision.FullDecision{
			CoTTrace:  trace.String(),
			Decisions: []decision.Decision{{Symbol: "ALL", Action: "wait", Reasoning: "agent: no symbols"}},
			Timestamp: time.Now(),
		}, nil
	}

	// 评估机会（简单评分）
	type pair struct{ sym string; score int }
	var ranking []pair
	for _, s := range picks {
		if md, ok := ctx.MarketDataMap[s]; ok && md != nil {
			opp := decision.ComputeSimpleOpportunityScore(md, 0.4)
			ranking = append(ranking, pair{sym: s, score: opp.Score})
			fmt.Fprintf(trace, "check %s: opp_score=%d\n", s, opp.Score)
		} else {
			fmt.Fprintf(trace, "skip %s: no market data\n", s)
		}
	}

	// 选择最高分
	if len(ranking) == 0 {
		fmt.Fprintln(trace, "No scoring result; wait")
		return &decision.FullDecision{CoTTrace: trace.String(), Decisions: []decision.Decision{{Symbol: "ALL", Action: "wait"}}, Timestamp: time.Now()}, nil
	}
	best := ranking[0]
	for _, p := range ranking {
		if p.score > best.score {
			best = p
		}
	}
	sym := best.sym
	snap, err := GetMarket(ctx, sym)
	if err != nil || snap.Price <= 0 {
		fmt.Fprintf(trace, "GetMarket fail: %v; wait\n", err)
		return &decision.FullDecision{CoTTrace: trace.String(), Decisions: []decision.Decision{{Symbol: "ALL", Action: "wait"}}, Timestamp: time.Now()}, nil
	}

	// 基于 EMA/MACD 粗略定向
	side := ""
	if snap.Price > snap.EMA20 && snap.MACD > 0 {
		side = "long"
	} else if snap.Price < snap.EMA20 && snap.MACD < 0 {
		side = "short"
	} else {
		fmt.Fprintln(trace, "Direction unclear (EMA/MACD not aligned); wait")
		return &decision.FullDecision{CoTTrace: trace.String(), Decisions: []decision.Decision{{Symbol: sym, Action: "wait", Reasoning: "agent: direction unclear"}}, Timestamp: time.Now()}, nil
	}

	// 风险距离（≥ max(1.0%, 0.8*ATR%)）
	atrPct := 0.0
	if snap.ATR3m > 0 {
		atrPct = (snap.ATR3m / snap.Price) * 100.0
	}
	minRisk := math.Max(1.0, atrPct*0.8)
	// 贴近 EMA20（用于 pullback_ema 路由约束）
	requireEmaNear := false
	if v := os.Getenv("NOFX_AGENT_REQUIRE_EMA_NEAR"); v != "" {
		requireEmaNear = (v == "1" || strings.ToLower(v) == "true")
	}
	emaNearPct := 0.4
	if v := os.Getenv("NOFX_AGENT_EMA_PROX_PCT"); v != "" {
		if f, e := strconv.ParseFloat(v, 64); e == nil && f > 0 {
			emaNearPct = f
		}
	}
	if snap.EMA20 > 0 && requireEmaNear {
		dist := math.Abs((snap.Price-snap.EMA20)/snap.EMA20) * 100.0
		if dist > emaNearPct {
			fmt.Fprintf(trace, "Price not near EMA20 (dist=%.2f%% > %.2f%%); wait\n", dist, emaNearPct)
			return &decision.FullDecision{CoTTrace: trace.String(), Decisions: []decision.Decision{{Symbol: sym, Action: "wait", Reasoning: "agent: not near EMA20"}}, Timestamp: time.Now()}, nil
		}
	}
	riskPct := minRisk * 1.2 // 略放大保证通过
	rrMin := 3.0
	if v := os.Getenv("NOFX_AGENT_RR_MIN"); v != "" {
		if f, e := strconv.ParseFloat(v, 64); e == nil && f > 0 {
			rrMin = f
		}
	}
	rewardPct := rrMin * riskPct

	// 构造 SL/TP
	var sl, tp float64
	if side == "long" {
		sl = snap.Price * (1.0 - riskPct/100.0)
		tp = snap.Price * (1.0 + rewardPct/100.0)
	} else {
		sl = snap.Price * (1.0 + riskPct/100.0)
		tp = snap.Price * (1.0 - rewardPct/100.0)
	}

	// 选择杠杆与名义
	lev := 3
	if sym == "BTCUSDT" || sym == "ETHUSDT" {
		if ctx.BTCETHLeverage > 0 {
			lev = ctx.BTCETHLeverage
		} else {
			lev = 3
		}
	} else if ctx.AltcoinLeverage > 0 {
		lev = ctx.AltcoinLeverage
	}
	if lev > 5 {
		lev = 5 // Agent保守
	}

	// 避免与当前持仓重复（同symbol任意方向都暂避）
	if len(decision.GetOpenPositionsForSymbol(ctx, sym)) > 0 {
		fmt.Fprintln(trace, "symbol already has position; wait")
		return &decision.FullDecision{CoTTrace: trace.String(), Decisions: []decision.Decision{{Symbol: sym, Action: "wait", Reasoning: "agent: symbol already has position"}}, Timestamp: time.Now()}, nil
	}

	// 名义：保证 required_margin ≥ 10U，且满足最小名义
	marginUSD := 12.0 // 保守
	posUSD := marginUSD * float64(lev)
	if rules, _ := GetRules(sym); rules != nil && rules.MinNotional > 0 && posUSD < rules.MinNotional {
		posUSD = rules.MinNotional
		marginUSD = posUSD / float64(lev)
	}

	// 估算交易
	est, err := EstimateTrade(sym, side, lev, posUSD, sl, tp, snap.Price)
	if err != nil || !est.MinNotionalOK || !est.StepOK || est.NetProfitEst <= 0 {
		fmt.Fprintf(trace, "Estimate fail or not profitable: err=%v minNotionalOK=%v stepOK=%v netProfit=%.3f; wait\n",
			err, est.MinNotionalOK, est.StepOK, est.NetProfitEst)
		return &decision.FullDecision{CoTTrace: trace.String(), Decisions: []decision.Decision{{Symbol: sym, Action: "wait", Reasoning: "agent: estimate not ok"}}, Timestamp: time.Now()}, nil
	}

	// 产出开仓
	action := "open_long"
	if side == "short" {
		action = "open_short"
	}
	dec := decision.Decision{
		Symbol:          sym,
		Action:          action,
		Leverage:        lev,
		PositionSizeUSD: posUSD,
		StopLoss:        sl,
		TakeProfit:      tp,
		Confidence:      88,
		RiskUSD:         marginUSD, // 近似占位
		Reasoning:       fmt.Sprintf("agent: EMA/MACD aligned; risk≈%.2f%% rr>=%.1f; opp=%d", riskPct, rrMin, best.score),
	}
	return &decision.FullDecision{
		CoTTrace:  trace.String(),
		Decisions: []decision.Decision{dec},
		Timestamp: time.Now(),
	}, nil
}
