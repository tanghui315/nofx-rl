package agent

import (
	"fmt"
	"math"
	"nofx/decision"
	"nofx/market"
)

// MarketSnap 简化的市场快照
type MarketSnap struct {
	Symbol     string
	Price      float64
	EMA20      float64
	MACD       float64
	RSI7       float64
	ATR3m      float64
	BBWidth3m  float64
	VWAP       float64
	Funding    float64
	OILatest   float64
	OIAverage  float64
}

// GetMarket 从上下文读取快照（不触网）
func GetMarket(ctx *decision.Context, symbol string) (*MarketSnap, error) {
	md, ok := ctx.MarketDataMap[symbol]
	if !ok || md == nil {
		return nil, fmt.Errorf("no market data for %s", symbol)
	}
	snap := &MarketSnap{
		Symbol:    symbol,
		Price:     md.CurrentPrice,
		EMA20:     md.CurrentEMA20,
		MACD:      md.CurrentMACD,
		RSI7:      md.CurrentRSI7,
		VWAP:      md.VWAP,
		Funding:   md.FundingRate,
		BBWidth3m: 0,
		ATR3m:     0,
	}
	if md.IntradaySeries != nil {
		snap.ATR3m = md.IntradaySeries.ATR14
	}
	if md.BollingerBands != nil {
		snap.BBWidth3m = md.BollingerBands.BandWidth
	}
	if md.OpenInterest != nil {
		snap.OILatest = md.OpenInterest.Latest
		snap.OIAverage = md.OpenInterest.Average
	}
	return snap, nil
}

// GetRules 读取交易所规则
func GetRules(symbol string) (*market.ExchangeRules, error) {
	r, err := market.GetSymbolExchangeRules(symbol)
	if err != nil {
		return nil, err
	}
	return r, nil
}

// GetAccount 从上下文读取账户简况
func GetAccount(ctx *decision.Context) (equity, available float64, marginUsedPct float64, positions int) {
	return ctx.Account.TotalEquity, ctx.Account.AvailableBalance, ctx.Account.MarginUsedPct, ctx.Account.PositionCount
}

// GetPositions 返回指定符号的持仓列表（若有）
func GetPositions(ctx *decision.Context, symbol string) []decision.PositionInfo {
	var out []decision.PositionInfo
	for _, p := range ctx.Positions {
		if p.Symbol == symbol {
			out = append(out, p)
		}
	}
	return out
}

// EstimateTrade 估算交易关键值（只读）
type EstimateOut struct {
	RR              float64
	MarginNeeded    float64
	FeeEstimate     float64
	NetProfitEst    float64
	MinNotionalOK   bool
	StepOK          bool
}

func EstimateTrade(symbol, side string, leverage int, positionSizeUSD, stopLoss, takeProfit, price float64) (*EstimateOut, error) {
	out := &EstimateOut{}
	if price <= 0 || stopLoss <= 0 || takeProfit <= 0 || positionSizeUSD <= 0 || leverage <= 0 {
		return nil, fmt.Errorf("invalid params")
	}
	// 粗略 RR
	entry := price
	var riskPct, rewardPct float64
	if side == "long" {
		riskPct = (entry - stopLoss) / entry * 100.0
		rewardPct = (takeProfit - entry) / entry * 100.0
	} else {
		riskPct = (stopLoss - entry) / entry * 100.0
		rewardPct = (entry - takeProfit) / entry * 100.0
	}
	if riskPct > 0 {
		out.RR = rewardPct / riskPct
	}
	out.MarginNeeded = positionSizeUSD / float64(leverage)
	// 手续费估算（双边 taker 0.04%）
	out.FeeEstimate = positionSizeUSD * 0.0004 * 2.0
	gross := positionSizeUSD * (rewardPct / 100.0)
	out.NetProfitEst = gross - out.FeeEstimate
	// 名义与步长检查
	rules, _ := GetRules(symbol)
	minNotional := 0.0
	step := 0.0
	if rules != nil {
		minNotional = rules.MinNotional
		if rules.StepSizeMarket > 0 {
			step = rules.StepSizeMarket
		} else {
			step = rules.StepSizeLot
		}
	}
	out.MinNotionalOK = (minNotional <= 0 || positionSizeUSD >= minNotional)
	qty := positionSizeUSD / price
	out.StepOK = true
	if step > 0 {
		steps := math.Floor(qty/step + 1e-12)
		out.StepOK = steps > 0
	}
	return out, nil
}

// OppScore 机会分
func OppScore(ctx *decision.Context, symbol string, emaProxPct float64) (int, error) {
	md, ok := ctx.MarketDataMap[symbol]
	if !ok || md == nil {
		return 0, fmt.Errorf("no market data")
	}
	res := decision.ComputeSimpleOpportunityScore(md, emaProxPct)
	return res.Score, nil
}

