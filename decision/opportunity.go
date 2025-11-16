package decision

import (
	"math"
	"nofx/market"
)

// SimpleOppResult 机会评分（简版，用于调试与可视化）
type SimpleOppResult struct {
	Symbol       string         `json:"symbol"`
	Score        int            `json:"score"`
	Breakdown    map[string]int `json:"breakdown"`
	EMANearPct3m float64        `json:"ema_near_pct_3m"`
	VolumeLast3m float64        `json:"volume_last_3m"`
	VolumeAvg3m  float64        `json:"volume_avg_3m"`
	OIDeltaPct   float64        `json:"oi_delta_pct"`
	BBWidthPct3m float64        `json:"bb_width_pct_3m"`
}

// ComputeSimpleOpportunityScore 基于单个 symbol 的简化机会评分（无方向、中性）
// 目的：提供 PreCheck/可视化的参考信号；不直接用于风控决策。
func ComputeSimpleOpportunityScore(data *market.Data, emaProxPct float64) SimpleOppResult {
	res := SimpleOppResult{
		Symbol:    data.Symbol,
		Breakdown: map[string]int{},
	}

	price := data.CurrentPrice
	ema := data.CurrentEMA20
	if price > 0 && ema > 0 {
		dist := math.Abs((price-ema)/ema) * 100.0
		res.EMANearPct3m = dist
		if dist <= emaProxPct {
			res.Score += 2
			res.Breakdown["ema_prox"] = 2
		}
	}

	// 成交量放大（3m）
	if data.IntradaySeries != nil && len(data.IntradaySeries.Volume) > 0 {
		vols := data.IntradaySeries.Volume
		last := vols[len(vols)-1]
		res.VolumeLast3m = last
		avg := 0.0
		n := len(vols)
		for i := 0; i < n; i++ {
			avg += vols[i]
		}
		avg = avg / float64(n)
		res.VolumeAvg3m = avg
		if avg > 0 && last >= 1.5*avg {
			res.Score += 1
			res.Breakdown["vol_expansion"] = 1
		}
	}

	// OI 变动（相对均值）
	if data.OpenInterest != nil && data.OpenInterest.Average > 0 {
		oiDeltaPct := math.Abs((data.OpenInterest.Latest-data.OpenInterest.Average)/data.OpenInterest.Average) * 100.0
		res.OIDeltaPct = oiDeltaPct
		if oiDeltaPct >= 5.0 {
			res.Score += 1
			res.Breakdown["oi_delta"] = 1
		}
	}

	// 布林带宽度（仅记录，不计分；留给后续调参）
	if data.BollingerBands != nil {
		res.BBWidthPct3m = data.BollingerBands.BandWidth
	}

	return res
}
