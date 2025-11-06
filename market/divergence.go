package market

import (
	"math"
	"time"
)

// 背离检测参数
const (
	DivergenceLookback  = 50   // 回溯50根K线
	MinPeakDistance     = 5    // 峰值最小间隔（K线数量）
	DivergenceThreshold = 0.03 // 3%差异才算背离
	ConfirmationCandles = 2    // 需要2根K线确认
)

// AnalyzeDivergence 综合分析所有背离
func AnalyzeDivergence(data *Data, klines3m, klines4h []Kline) *DivergenceAnalysis {
	if len(klines4h) < DivergenceLookback {
		return &DivergenceAnalysis{
			HasDivergence: false,
			Signal:        "none",
			Summary:       "Insufficient data for divergence analysis",
		}
	}

	// 使用4小时K线检测背离（更可靠）
	recentKlines := klines4h
	if len(recentKlines) > DivergenceLookback {
		recentKlines = klines4h[len(klines4h)-DivergenceLookback:]
	}

	// 1. 检测RSI背离
	rsiDivergence := detectRSIDivergence(recentKlines, data.LongerTermContext.RSI14Values)

	// 2. 检测MACD背离
	macdDivergence := detectMACDDivergence(recentKlines, data.LongerTermContext.MACDValues)

	// 3. 检测OBV背离（价量背离）
	obvDivergence := detectOBVDivergence(recentKlines, data.OBV)

	// 综合评估
	hasDivergence := (rsiDivergence != nil) || (macdDivergence != nil) || (obvDivergence != nil)
	signal, confidence := evaluateDivergenceSignal(rsiDivergence, macdDivergence, obvDivergence)
	summary := generateDivergenceSummary(rsiDivergence, macdDivergence, obvDivergence)

	return &DivergenceAnalysis{
		HasDivergence:  hasDivergence,
		RSIDivergence:  rsiDivergence,
		MACDDivergence: macdDivergence,
		OBVDivergence:  obvDivergence,
		Signal:         signal,
		Confidence:     confidence,
		Summary:        summary,
	}
}

// detectRSIDivergence 检测RSI背离
func detectRSIDivergence(klines []Kline, rsiValues []float64) *Divergence {
	if len(klines) < 10 || len(rsiValues) < 10 {
		return nil
	}

	// 确保数据长度一致
	minLen := len(klines)
	if len(rsiValues) < minLen {
		minLen = len(rsiValues)
	}

	klines = klines[len(klines)-minLen:]
	rsiValues = rsiValues[len(rsiValues)-minLen:]

	// 查找价格峰值
	pricePeaks := findPricePeaks(klines, MinPeakDistance)
	if len(pricePeaks) < 2 {
		return nil
	}

	// 查找RSI峰值
	rsiPeaks := findIndicatorPeaks(rsiValues, MinPeakDistance, klines)
	if len(rsiPeaks) < 2 {
		return nil
	}

	// 比较最近的两个峰值
	return comparePeaksForDivergence(pricePeaks, rsiPeaks, "RSI")
}

// detectMACDDivergence 检测MACD背离
func detectMACDDivergence(klines []Kline, macdValues []float64) *Divergence {
	if len(klines) < 10 || len(macdValues) < 10 {
		return nil
	}

	minLen := len(klines)
	if len(macdValues) < minLen {
		minLen = len(macdValues)
	}

	klines = klines[len(klines)-minLen:]
	macdValues = macdValues[len(macdValues)-minLen:]

	pricePeaks := findPricePeaks(klines, MinPeakDistance)
	if len(pricePeaks) < 2 {
		return nil
	}

	macdPeaks := findIndicatorPeaks(macdValues, MinPeakDistance, klines)
	if len(macdPeaks) < 2 {
		return nil
	}

	return comparePeaksForDivergence(pricePeaks, macdPeaks, "MACD")
}

// detectOBVDivergence 检测OBV背离
func detectOBVDivergence(klines []Kline, obv float64) *Divergence {
	// OBV背离需要历史OBV数据，这里简化处理
	// 实际应用中需要存储历史OBV值
	return nil
}

// findPricePeaks 查找价格峰值和谷值
func findPricePeaks(klines []Kline, minDistance int) []Peak {
	if len(klines) < minDistance*2+1 {
		return nil
	}

	var peaks []Peak

	for i := minDistance; i < len(klines)-minDistance; i++ {
		current := klines[i]

		// 检查是否是局部最高点
		isHigh := true
		for j := i - minDistance; j <= i+minDistance; j++ {
			if j != i && klines[j].High > current.High {
				isHigh = false
				break
			}
		}

		if isHigh {
			peaks = append(peaks, Peak{
				Index: i,
				Value: current.High,
				Type:  "high",
				Time:  time.Unix(current.CloseTime/1000, 0),
			})
			continue
		}

		// 检查是否是局部最低点
		isLow := true
		for j := i - minDistance; j <= i+minDistance; j++ {
			if j != i && klines[j].Low < current.Low {
				isLow = false
				break
			}
		}

		if isLow {
			peaks = append(peaks, Peak{
				Index: i,
				Value: current.Low,
				Type:  "low",
				Time:  time.Unix(current.CloseTime/1000, 0),
			})
		}
	}

	return peaks
}

// findIndicatorPeaks 查找指标峰值
func findIndicatorPeaks(values []float64, minDistance int, klines []Kline) []Peak {
	if len(values) < minDistance*2+1 {
		return nil
	}

	var peaks []Peak

	for i := minDistance; i < len(values)-minDistance; i++ {
		current := values[i]

		// 检查是否是局部最高点
		isHigh := true
		for j := i - minDistance; j <= i+minDistance; j++ {
			if j != i && values[j] > current {
				isHigh = false
				break
			}
		}

		if isHigh {
			peakTime := time.Now()
			if i < len(klines) {
				peakTime = time.Unix(klines[i].CloseTime/1000, 0)
			}
			peaks = append(peaks, Peak{
				Index: i,
				Value: current,
				Type:  "high",
				Time:  peakTime,
			})
			continue
		}

		// 检查是否是局部最低点
		isLow := true
		for j := i - minDistance; j <= i+minDistance; j++ {
			if j != i && values[j] < current {
				isLow = false
				break
			}
		}

		if isLow {
			peakTime := time.Now()
			if i < len(klines) {
				peakTime = time.Unix(klines[i].CloseTime/1000, 0)
			}
			peaks = append(peaks, Peak{
				Index: i,
				Value: current,
				Type:  "low",
				Time:  peakTime,
			})
		}
	}

	return peaks
}

// comparePeaksForDivergence 比较峰值判断背离
func comparePeaksForDivergence(pricePeaks, indicatorPeaks []Peak, indicator string) *Divergence {
	if len(pricePeaks) < 2 || len(indicatorPeaks) < 2 {
		return nil
	}

	// 获取最近的两个高点
	var recentPriceHighs []Peak
	var recentIndicatorHighs []Peak

	for i := len(pricePeaks) - 1; i >= 0 && len(recentPriceHighs) < 2; i-- {
		if pricePeaks[i].Type == "high" {
			recentPriceHighs = append([]Peak{pricePeaks[i]}, recentPriceHighs...)
		}
	}

	for i := len(indicatorPeaks) - 1; i >= 0 && len(recentIndicatorHighs) < 2; i-- {
		if indicatorPeaks[i].Type == "high" {
			recentIndicatorHighs = append([]Peak{indicatorPeaks[i]}, recentIndicatorHighs...)
		}
	}

	// 检测看跌背离：价格更高，指标更低
	if len(recentPriceHighs) >= 2 && len(recentIndicatorHighs) >= 2 {
		priceChange := (recentPriceHighs[1].Value - recentPriceHighs[0].Value) / recentPriceHighs[0].Value
		indicatorChange := (recentIndicatorHighs[1].Value - recentIndicatorHighs[0].Value) / recentIndicatorHighs[0].Value

		// 价格上涨但指标下降 = 看跌背离
		if priceChange > DivergenceThreshold && indicatorChange < -DivergenceThreshold {
			strength := math.Min((math.Abs(priceChange)+math.Abs(indicatorChange))*100, 100)

			return &Divergence{
				Type:           "bearish",
				Indicator:      indicator,
				Strength:       strength,
				PricePeaks:     recentPriceHighs,
				IndicatorPeaks: recentIndicatorHighs,
				DetectedAt:     time.Now(),
				Description:    "Bearish divergence: Price making higher highs while indicator making lower highs",
			}
		}
	}

	// 获取最近的两个低点
	var recentPriceLows []Peak
	var recentIndicatorLows []Peak

	for i := len(pricePeaks) - 1; i >= 0 && len(recentPriceLows) < 2; i-- {
		if pricePeaks[i].Type == "low" {
			recentPriceLows = append([]Peak{pricePeaks[i]}, recentPriceLows...)
		}
	}

	for i := len(indicatorPeaks) - 1; i >= 0 && len(recentIndicatorLows) < 2; i-- {
		if indicatorPeaks[i].Type == "low" {
			recentIndicatorLows = append([]Peak{indicatorPeaks[i]}, recentIndicatorLows...)
		}
	}

	// 检测看涨背离：价格更低，指标更高
	if len(recentPriceLows) >= 2 && len(recentIndicatorLows) >= 2 {
		priceChange := (recentPriceLows[1].Value - recentPriceLows[0].Value) / recentPriceLows[0].Value
		indicatorChange := (recentIndicatorLows[1].Value - recentIndicatorLows[0].Value) / math.Abs(recentIndicatorLows[0].Value)

		// 价格下跌但指标上升 = 看涨背离
		if priceChange < -DivergenceThreshold && indicatorChange > DivergenceThreshold {
			strength := math.Min((math.Abs(priceChange)+math.Abs(indicatorChange))*100, 100)

			return &Divergence{
				Type:           "bullish",
				Indicator:      indicator,
				Strength:       strength,
				PricePeaks:     recentPriceLows,
				IndicatorPeaks: recentIndicatorLows,
				DetectedAt:     time.Now(),
				Description:    "Bullish divergence: Price making lower lows while indicator making higher lows",
			}
		}
	}

	return nil
}

// evaluateDivergenceSignal 评估背离信号
func evaluateDivergenceSignal(rsi, macd, obv *Divergence) (signal string, confidence float64) {
	if rsi == nil && macd == nil && obv == nil {
		return "none", 0
	}

	bearishCount := 0
	bullishCount := 0
	totalStrength := 0.0
	count := 0

	if rsi != nil {
		if rsi.Type == "bearish" {
			bearishCount++
		} else if rsi.Type == "bullish" {
			bullishCount++
		}
		totalStrength += rsi.Strength
		count++
	}

	if macd != nil {
		if macd.Type == "bearish" {
			bearishCount++
		} else if macd.Type == "bullish" {
			bullishCount++
		}
		totalStrength += macd.Strength
		count++
	}

	if obv != nil {
		if obv.Type == "bearish" {
			bearishCount++
		} else if obv.Type == "bullish" {
			bullishCount++
		}
		totalStrength += obv.Strength
		count++
	}

	avgStrength := totalStrength / float64(count)

	// 多个指标确认
	if bearishCount >= 2 {
		return "strong_reversal", math.Min(avgStrength+20, 100)
	} else if bearishCount == 1 {
		return "reversal_warning", avgStrength
	} else if bullishCount >= 2 {
		return "strong_reversal", math.Min(avgStrength+20, 100)
	} else if bullishCount == 1 {
		return "reversal_warning", avgStrength
	}

	return "none", 0
}

// generateDivergenceSummary 生成背离总结
func generateDivergenceSummary(rsi, macd, obv *Divergence) string {
	if rsi == nil && macd == nil && obv == nil {
		return "No divergence detected"
	}

	summary := "Divergence detected: "
	divergences := []string{}

	if rsi != nil {
		divergences = append(divergences, rsi.Indicator+" "+rsi.Type)
	}
	if macd != nil {
		divergences = append(divergences, macd.Indicator+" "+macd.Type)
	}
	if obv != nil {
		divergences = append(divergences, obv.Indicator+" "+obv.Type)
	}

	for i, d := range divergences {
		if i > 0 {
			summary += ", "
		}
		summary += d
	}

	return summary + ". Trend reversal may be imminent."
}
