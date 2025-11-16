package decision

import (
	"nofx/market"
	"nofx/news"
	"time"
)

// RegimeType 市场分型类型
type RegimeType string

const (
	RegimeTrendEarly RegimeType = "trend_early"
	RegimeTrendMid   RegimeType = "trend_mid"
	RegimeTrendLate  RegimeType = "trend_late"
	RegimeRange      RegimeType = "range"
)

// Regime 分型结果
type Regime struct {
	Type       RegimeType          `json:"type"`
	Confidence int                 `json:"confidence"`
	Evidence   map[string]float64  `json:"evidence,omitempty"`
	Notes      []string            `json:"notes,omitempty"`
	Symbols    []string            `json:"symbols,omitempty"`
}

// DetectRegime 轻量分型（V1 占位实现）
// 说明：这是一个保守的占位实现，用于日志与后续路由；逻辑尽量简单，避免引入噪声。
func DetectRegime(md map[string]*market.Data) Regime {
	// 默认：震荡
	r := Regime{
		Type:       RegimeRange,
		Confidence: 50,
		Evidence:   map[string]float64{},
		Notes:      []string{},
	}

	// 以 BTC 为代表进行简单判断（可扩展为多币加权）
	if btc, ok := md["BTCUSDT"]; ok && btc != nil {
		adx1h := 0.0
		adx4h := 0.0
		if btc.ADX != nil {
			adx1h = btc.ADX.ADX
		}
		if btc.LongerTermContext != nil {
			// 4h 级别强弱可用 ATR/带宽近似；真正 ADX_4h 可在数据层补齐
			adx4h = (btc.LongerTermContext.ATR14 / btc.CurrentPrice) * 100 * 2 // 粗略映射
		}
		r.Evidence["adx_1h"] = adx1h
		r.Evidence["adx_4h_proxy"] = adx4h

		switch {
		case adx1h >= 25 && adx4h >= 20:
			r.Type = RegimeTrendMid
			r.Confidence = 70
		case adx1h >= 20 && adx4h >= 15:
			r.Type = RegimeTrendEarly
			r.Confidence = 60
		default:
			r.Type = RegimeRange
			r.Confidence = 55
		}
	}
	// 融合新闻分（BTC 全局）
	ns := news.Score("BTCUSDT", 24*time.Hour) // 24h 窗口
	r.Evidence["news_score_btc"] = ns.Score
	if ns.Score <= -2 {
		// 强负面 → 降级为 range，并加注
		r.Type = RegimeRange
		if r.Confidence > 60 {
			r.Confidence = 60
		}
		r.Notes = append(r.Notes, "news:risk_off")
	} else if ns.Score >= 2 {
		// 强正面 → 若非趋势，提升为 trend_early（但不超过中等置信）
		if r.Type == RegimeRange {
			r.Type = RegimeTrendEarly
			r.Confidence = 60
		}
		r.Notes = append(r.Notes, "news:risk_on")
	}
	return r
}
