package market

import (
	"fmt"
	"math"
	"strings"
)

// AnalyzeSemantics 生成语义化的技术分析
func AnalyzeSemantics(data *Data) *SemanticAnalysis {
	analysis := &SemanticAnalysis{
		KeySignals: make([]string, 0),
	}

	// 1. 趋势方向和强度分析
	analyzeTrend(data, analysis)

	// 2. 动量分析
	analyzeMomentum(data, analysis)

	// 3. 波动性分析
	analyzeVolatility(data, analysis)

	// 4. 价格位置分析
	analyzePricePosition(data, analysis)

	// 5. 均线对齐分析
	analyzeEMAAlignment(data, analysis)

	// 6. 支撑阻力分析
	analyzeSupportResistance(data, analysis)

	// 7. 高级功能分析
	analyzeAdvancedFeatures(data, analysis)

	// 8. 综合交易建议
	generateTradeSetup(data, analysis)

	return analysis
}

// analyzeTrend 分析趋势方向和强度
func analyzeTrend(data *Data, analysis *SemanticAnalysis) {
	if data.ADX == nil {
		analysis.TrendDirection = "unknown"
		analysis.TrendStrength = "unknown"
		return
	}

	// ADX趋势强度判断
	adx := data.ADX.ADX
	if adx > 40 {
		analysis.TrendStrength = "strong"
	} else if adx > 25 {
		analysis.TrendStrength = "moderate"
	} else if adx > 20 {
		analysis.TrendStrength = "weak"
	} else {
		analysis.TrendStrength = "sideways"
		analysis.TrendDirection = "sideways"
		return
	}

	// 趋势方向判断（使用DI）
	if data.ADX.PlusDI > data.ADX.MinusDI {
		analysis.TrendDirection = "bullish"
		if data.ADX.PlusDI-data.ADX.MinusDI > 10 {
			analysis.KeySignals = append(analysis.KeySignals, "strong_bullish_momentum")
		}
	} else {
		analysis.TrendDirection = "bearish"
		if data.ADX.MinusDI-data.ADX.PlusDI > 10 {
			analysis.KeySignals = append(analysis.KeySignals, "strong_bearish_momentum")
		}
	}

	// EMA趋势确认
	if data.LongerTermContext != nil {
		if data.LongerTermContext.EMA20 > data.LongerTermContext.EMA50 {
			if analysis.TrendDirection == "bullish" {
				analysis.KeySignals = append(analysis.KeySignals, "trend_confirmed_by_ema")
			}
		} else if data.LongerTermContext.EMA20 < data.LongerTermContext.EMA50 {
			if analysis.TrendDirection == "bearish" {
				analysis.KeySignals = append(analysis.KeySignals, "trend_confirmed_by_ema")
			}
		}
	}
}

// analyzeMomentum 分析动量
func analyzeMomentum(data *Data, analysis *SemanticAnalysis) {
	// MACD动量分析
	macd := data.CurrentMACD
	if macd > 0 {
		if macd > 50 {
			analysis.MomentumStatus = "increasing"
			analysis.KeySignals = append(analysis.KeySignals, "strong_positive_macd")
		} else {
			analysis.MomentumStatus = "positive"
		}
	} else if macd < 0 {
		if macd < -50 {
			analysis.MomentumStatus = "decreasing"
			analysis.KeySignals = append(analysis.KeySignals, "strong_negative_macd")
		} else {
			analysis.MomentumStatus = "negative"
		}
	} else {
		analysis.MomentumStatus = "neutral"
	}

	// RSI动量确认
	rsi := data.CurrentRSI7
	if rsi > 70 && analysis.MomentumStatus == "increasing" {
		analysis.KeySignals = append(analysis.KeySignals, "momentum_overbought_warning")
	} else if rsi < 30 && analysis.MomentumStatus == "decreasing" {
		analysis.KeySignals = append(analysis.KeySignals, "momentum_oversold_opportunity")
	}
}

// analyzeVolatility 分析波动性
func analyzeVolatility(data *Data, analysis *SemanticAnalysis) {
	if data.LongerTermContext == nil || data.BollingerBands == nil {
		analysis.VolatilityLevel = "unknown"
		return
	}

	// ATR波动性判断
	atr := data.LongerTermContext.ATR14
	atr3 := data.LongerTermContext.ATR3
	
	// 布林带宽度判断
	bbWidth := data.BollingerBands.BandWidth

	if atr3 > atr*1.5 || bbWidth > 5 {
		analysis.VolatilityLevel = "high"
		analysis.KeySignals = append(analysis.KeySignals, "high_volatility")
	} else if atr3 < atr*0.7 || bbWidth < 2 {
		analysis.VolatilityLevel = "low"
		analysis.KeySignals = append(analysis.KeySignals, "low_volatility_squeeze")
	} else {
		analysis.VolatilityLevel = "normal"
	}
}

// analyzePricePosition 分析价格位置
func analyzePricePosition(data *Data, analysis *SemanticAnalysis) {
	rsi := data.CurrentRSI7

	if rsi > 70 {
		analysis.PricePosition = "overbought"
		analysis.KeySignals = append(analysis.KeySignals, "rsi_overbought")
	} else if rsi < 30 {
		analysis.PricePosition = "oversold"
		analysis.KeySignals = append(analysis.KeySignals, "rsi_oversold")
	} else if rsi > 55 {
		analysis.PricePosition = "bullish_zone"
	} else if rsi < 45 {
		analysis.PricePosition = "bearish_zone"
	} else {
		analysis.PricePosition = "neutral"
	}

	// 布林带位置分析
	if data.BollingerBands != nil {
		percentB := data.BollingerBands.PercentB
		if percentB > 0.8 {
			analysis.KeySignals = append(analysis.KeySignals, "price_near_bb_upper")
		} else if percentB < 0.2 {
			analysis.KeySignals = append(analysis.KeySignals, "price_near_bb_lower")
		}
	}

	// VWAP位置分析
	if data.VWAP > 0 {
		priceVsVWAP := ((data.CurrentPrice - data.VWAP) / data.VWAP) * 100
		if priceVsVWAP > 1 {
			analysis.KeySignals = append(analysis.KeySignals, "price_above_vwap")
		} else if priceVsVWAP < -1 {
			analysis.KeySignals = append(analysis.KeySignals, "price_below_vwap")
		}
	}
}

// analyzeEMAAlignment 分析均线对齐
func analyzeEMAAlignment(data *Data, analysis *SemanticAnalysis) {
	if data.MultipleEMAs == nil {
		analysis.EMAAlignment = "unknown"
		return
	}

	emas := data.MultipleEMAs
	
	// 检查多头排列（短期均线 > 长期均线）
	bullishCount := 0
	bearishCount := 0

	if emas.EMA5 > emas.EMA10 {
		bullishCount++
	} else {
		bearishCount++
	}

	if emas.EMA10 > emas.EMA20 {
		bullishCount++
	} else {
		bearishCount++
	}

	if emas.EMA20 > emas.EMA50 {
		bullishCount++
	} else {
		bearishCount++
	}

	if emas.EMA50 > emas.EMA100 {
		bullishCount++
	} else {
		bearishCount++
	}

	if bullishCount >= 3 {
		analysis.EMAAlignment = "bullish_alignment"
		analysis.KeySignals = append(analysis.KeySignals, "bullish_ema_alignment")
	} else if bearishCount >= 3 {
		analysis.EMAAlignment = "bearish_alignment"
		analysis.KeySignals = append(analysis.KeySignals, "bearish_ema_alignment")
	} else {
		analysis.EMAAlignment = "mixed"
	}

	// 金叉/死叉检测（简化版）
	if data.LongerTermContext != nil {
		ema20 := data.LongerTermContext.EMA20
		ema50 := data.LongerTermContext.EMA50
		
		// 金叉：EMA20刚穿越EMA50向上
		if ema20 > ema50 && (ema20-ema50)/ema50 < 0.01 { // 差距小于1%，可能刚交叉
			analysis.KeySignals = append(analysis.KeySignals, "potential_golden_cross")
		}
		
		// 死叉：EMA20刚穿越EMA50向下
		if ema20 < ema50 && (ema50-ema20)/ema50 < 0.01 {
			analysis.KeySignals = append(analysis.KeySignals, "potential_death_cross")
		}
	}
}

// analyzeSupportResistance 分析支撑阻力
func analyzeSupportResistance(data *Data, analysis *SemanticAnalysis) {
	currentPrice := data.CurrentPrice

	// 使用布林带作为动态支撑阻力
	if data.BollingerBands != nil {
		analysis.NearestSupport = data.BollingerBands.Lower
		analysis.NearestResistance = data.BollingerBands.Upper

		analysis.DistanceToSupport = ((currentPrice - analysis.NearestSupport) / currentPrice) * 100
		analysis.DistanceToResistance = ((analysis.NearestResistance - currentPrice) / currentPrice) * 100
	} else {
		// 简单估算
		analysis.NearestSupport = currentPrice * 0.97
		analysis.NearestResistance = currentPrice * 1.03
		analysis.DistanceToSupport = 3.0
		analysis.DistanceToResistance = 3.0
	}

	// 检查是否接近支撑或阻力
	if analysis.DistanceToResistance < 1 {
		analysis.KeySignals = append(analysis.KeySignals, "approaching_resistance")
	}
	if analysis.DistanceToSupport < 1 {
		analysis.KeySignals = append(analysis.KeySignals, "approaching_support")
	}
}

// generateTradeSetup 生成交易建议
func generateTradeSetup(data *Data, analysis *SemanticAnalysis) {
	// 综合评分系统
	bullishScore := 0
	bearishScore := 0

	// 1. 趋势得分（权重最高）
	if analysis.TrendDirection == "bullish" {
		bullishScore += 3
		if analysis.TrendStrength == "strong" {
			bullishScore += 2
		}
	} else if analysis.TrendDirection == "bearish" {
		bearishScore += 3
		if analysis.TrendStrength == "strong" {
			bearishScore += 2
		}
	}

	// 2. EMA对齐得分
	if analysis.EMAAlignment == "bullish_alignment" {
		bullishScore += 2
	} else if analysis.EMAAlignment == "bearish_alignment" {
		bearishScore += 2
	}

	// 3. 动量得分
	if analysis.MomentumStatus == "increasing" {
		bullishScore += 2
	} else if analysis.MomentumStatus == "decreasing" {
		bearishScore += 2
	}

	// 4. 价格位置得分
	if analysis.PricePosition == "oversold" {
		bullishScore += 1
	} else if analysis.PricePosition == "overbought" {
		bearishScore += 1
	}

	// 5. 关键信号加分
	for _, signal := range analysis.KeySignals {
		if strings.Contains(signal, "bullish") || strings.Contains(signal, "golden") || 
		   strings.Contains(signal, "oversold") || strings.Contains(signal, "above_vwap") {
			bullishScore++
		}
		if strings.Contains(signal, "bearish") || strings.Contains(signal, "death") || 
		   strings.Contains(signal, "overbought") || strings.Contains(signal, "below_vwap") {
			bearishScore++
		}
	}

	// 计算总分
	totalScore := bullishScore + bearishScore
	if totalScore == 0 {
		totalScore = 1 // 避免除以0
	}

	// 生成建议
	if bullishScore > bearishScore+2 {
		analysis.TradeSetup = "long"
		analysis.ConfidenceScore = math.Min(float64(bullishScore)/float64(totalScore)*100, 95)
	} else if bearishScore > bullishScore+2 {
		analysis.TradeSetup = "short"
		analysis.ConfidenceScore = math.Min(float64(bearishScore)/float64(totalScore)*100, 95)
	} else {
		analysis.TradeSetup = "wait"
		analysis.ConfidenceScore = 50
	}

	// 风险评估
	if analysis.VolatilityLevel == "high" {
		analysis.RiskLevel = "high"
	} else if analysis.PricePosition == "overbought" || analysis.PricePosition == "oversold" {
		analysis.RiskLevel = "medium"
	} else {
		analysis.RiskLevel = "low"
	}
}

// FormatSemanticAnalysis 格式化语义化分析为文本
func FormatSemanticAnalysis(data *Data) string {
	if data.Semantics == nil {
		return ""
	}

	var sb strings.Builder
	s := data.Semantics

	sb.WriteString("## 📊 Technical Analysis Summary\n\n")

	// 趋势分析
	sb.WriteString("**Trend Analysis:**\n")
	sb.WriteString(fmt.Sprintf("- Direction: %s\n", formatTrendDirection(s.TrendDirection)))
	sb.WriteString(fmt.Sprintf("- Strength: %s", formatTrendStrength(s.TrendStrength)))
	if data.ADX != nil {
		sb.WriteString(fmt.Sprintf(" (ADX: %.1f)\n", data.ADX.ADX))
	} else {
		sb.WriteString("\n")
	}

	// 动量分析
	sb.WriteString(fmt.Sprintf("- Momentum: %s (MACD: %.2f, RSI: %.1f)\n", 
		formatMomentum(s.MomentumStatus), data.CurrentMACD, data.CurrentRSI7))

	// EMA状态
	sb.WriteString(fmt.Sprintf("- EMA Alignment: %s\n", formatEMAAlignment(s.EMAAlignment)))

	sb.WriteString("\n")

	// 价格位置
	sb.WriteString("**Price Position:**\n")
	sb.WriteString(fmt.Sprintf("- Status: %s\n", formatPricePosition(s.PricePosition)))
	sb.WriteString(fmt.Sprintf("- Volatility: %s\n", s.VolatilityLevel))
	
	if data.VWAP > 0 {
		vwapDiff := ((data.CurrentPrice - data.VWAP) / data.VWAP) * 100
		vwapStatus := "neutral"
		if vwapDiff > 0 {
			vwapStatus = "above (bullish)"
		} else if vwapDiff < 0 {
			vwapStatus = "below (bearish)"
		}
		sb.WriteString(fmt.Sprintf("- VWAP: %.2f (%s, %.2f%%)\n", data.VWAP, vwapStatus, vwapDiff))
	}

	sb.WriteString("\n")

	// 支撑阻力
	sb.WriteString("**Support & Resistance:**\n")
	sb.WriteString(fmt.Sprintf("- Nearest Resistance: %.2f (+%.2f%%)\n", 
		s.NearestResistance, s.DistanceToResistance))
	sb.WriteString(fmt.Sprintf("- Current Price: %.2f\n", data.CurrentPrice))
	sb.WriteString(fmt.Sprintf("- Nearest Support: %.2f (-%.2f%%)\n", 
		s.NearestSupport, s.DistanceToSupport))

	sb.WriteString("\n")

	// 关键信号
	if len(s.KeySignals) > 0 {
		sb.WriteString("**Key Signals:**\n")
		for _, signal := range s.KeySignals {
			sb.WriteString(fmt.Sprintf("✓ %s\n", formatSignal(signal)))
		}
		sb.WriteString("\n")
	}

	// 交易建议
	sb.WriteString("**Trade Setup:**\n")
	sb.WriteString(fmt.Sprintf("- Recommendation: %s\n", strings.ToUpper(s.TradeSetup)))
	sb.WriteString(fmt.Sprintf("- Confidence: %.0f%%\n", s.ConfidenceScore))
	sb.WriteString(fmt.Sprintf("- Risk Level: %s\n", strings.ToUpper(s.RiskLevel)))

	sb.WriteString("\n")

	return sb.String()
}

// 辅助格式化函数
func formatTrendDirection(direction string) string {
	switch direction {
	case "bullish":
		return "🟢 Bullish (Uptrend)"
	case "bearish":
		return "🔴 Bearish (Downtrend)"
	case "sideways":
		return "🟡 Sideways (Ranging)"
	default:
		return direction
	}
}

func formatTrendStrength(strength string) string {
	switch strength {
	case "strong":
		return "STRONG"
	case "moderate":
		return "Moderate"
	case "weak":
		return "Weak"
	case "sideways":
		return "No clear trend"
	default:
		return strength
	}
}

func formatMomentum(momentum string) string {
	switch momentum {
	case "increasing":
		return "📈 Increasing (Bullish)"
	case "decreasing":
		return "📉 Decreasing (Bearish)"
	case "positive":
		return "Positive"
	case "negative":
		return "Negative"
	default:
		return "Neutral"
	}
}

func formatEMAAlignment(alignment string) string {
	switch alignment {
	case "bullish_alignment":
		return "✓ Bullish (All EMAs aligned upward)"
	case "bearish_alignment":
		return "✗ Bearish (All EMAs aligned downward)"
	case "mixed":
		return "Mixed (No clear alignment)"
	default:
		return alignment
	}
}

func formatPricePosition(position string) string {
	switch position {
	case "overbought":
		return "⚠️  Overbought (RSI > 70)"
	case "oversold":
		return "💡 Oversold (RSI < 30)"
	case "bullish_zone":
		return "Bullish Zone (RSI 55-70)"
	case "bearish_zone":
		return "Bearish Zone (RSI 30-45)"
	default:
		return "Neutral (RSI 45-55)"
	}
}

func formatSignal(signal string) string {
	signalMap := map[string]string{
		"strong_bullish_momentum":      "Strong bullish momentum detected",
		"strong_bearish_momentum":      "Strong bearish momentum detected",
		"trend_confirmed_by_ema":       "Trend confirmed by EMA crossover",
		"strong_positive_macd":         "MACD showing strong positive momentum",
		"strong_negative_macd":         "MACD showing strong negative momentum",
		"momentum_overbought_warning":  "Momentum overbought - potential reversal",
		"momentum_oversold_opportunity": "Momentum oversold - potential bounce",
		"high_volatility":              "High volatility detected",
		"low_volatility_squeeze":       "Low volatility - potential breakout setup",
		"rsi_overbought":               "RSI overbought (>70)",
		"rsi_oversold":                 "RSI oversold (<30)",
		"price_near_bb_upper":          "Price testing Bollinger upper band",
		"price_near_bb_lower":          "Price testing Bollinger lower band",
		"price_above_vwap":             "Price trading above VWAP (bullish)",
		"price_below_vwap":             "Price trading below VWAP (bearish)",
		"bullish_ema_alignment":        "Bullish EMA alignment confirmed",
		"bearish_ema_alignment":        "Bearish EMA alignment confirmed",
		"potential_golden_cross":       "Potential Golden Cross forming",
		"potential_death_cross":        "Potential Death Cross forming",
		"approaching_resistance":       "Approaching resistance level",
		"approaching_support":          "Approaching support level",
	}

	if formatted, ok := signalMap[signal]; ok {
		return formatted
	}
	return signal
}

// ========================================
// 高级功能语义化分析
// ========================================

// analyzeAdvancedFeatures 分析高级功能（Ichimoku, FVG, Divergence）
func analyzeAdvancedFeatures(data *Data, analysis *SemanticAnalysis) {
	// 1. Ichimoku Cloud 分析
	if data.Ichimoku != nil {
		analyzeIchimokuSignals(data.Ichimoku, analysis)
	}
	
	// 2. FVG 分析
	if data.FVG != nil {
		analyzeFVGSignals(data.FVG, analysis)
	}
	
	// 3. Divergence 分析
	if data.Divergence != nil && data.Divergence.HasDivergence {
		analyzeDivergenceSignals(data.Divergence, analysis)
	}
}

// analyzeIchimokuSignals 分析Ichimoku信号
func analyzeIchimokuSignals(ichimoku *IchimokuCloud, analysis *SemanticAnalysis) {
	// 价格在云上方 = 强势
	if ichimoku.PricePosition == "above_cloud" {
		if ichimoku.CloudColor == "green" {
			analysis.KeySignals = append(analysis.KeySignals, "ichimoku_bullish_cloud")
		}
	} else if ichimoku.PricePosition == "below_cloud" {
		if ichimoku.CloudColor == "red" {
			analysis.KeySignals = append(analysis.KeySignals, "ichimoku_bearish_cloud")
		}
	}
	
	// TK交叉信号
	if ichimoku.TKCross == "bullish" && ichimoku.TKPercent > 0.3 {
		analysis.KeySignals = append(analysis.KeySignals, "ichimoku_tk_golden_cross")
	} else if ichimoku.TKCross == "bearish" && ichimoku.TKPercent > 0.3 {
		analysis.KeySignals = append(analysis.KeySignals, "ichimoku_tk_death_cross")
	}
	
	// 厚云提供强支撑/阻力
	if ichimoku.CloudPercent > 1.5 {
		if ichimoku.PricePosition == "above_cloud" {
			analysis.KeySignals = append(analysis.KeySignals, "ichimoku_strong_support_below")
		} else if ichimoku.PricePosition == "below_cloud" {
			analysis.KeySignals = append(analysis.KeySignals, "ichimoku_strong_resistance_above")
		}
	}
}

// analyzeFVGSignals 分析FVG信号
func analyzeFVGSignals(fvg *FVGAnalysis, analysis *SemanticAnalysis) {
	// 检查最近的看涨FVG
	if fvg.NearestBullishFVG != nil && fvg.NearestBullishFVG.IsNearby {
		if fvg.NearestBullishFVG.Status == "unfilled" {
			analysis.KeySignals = append(analysis.KeySignals, "fvg_bullish_nearby")
		}
	}
	
	// 检查最近的看跌FVG
	if fvg.NearestBearishFVG != nil && fvg.NearestBearishFVG.IsNearby {
		if fvg.NearestBearishFVG.Status == "unfilled" {
			analysis.KeySignals = append(analysis.KeySignals, "fvg_bearish_nearby")
		}
	}
	
	// FVG交易信号
	if fvg.Signal == "buy_at_fvg" {
		analysis.KeySignals = append(analysis.KeySignals, "fvg_buy_opportunity")
	} else if fvg.Signal == "sell_at_fvg" {
		analysis.KeySignals = append(analysis.KeySignals, "fvg_sell_opportunity")
	}
}

// analyzeDivergenceSignals 分析背离信号
func analyzeDivergenceSignals(divergence *DivergenceAnalysis, analysis *SemanticAnalysis) {
	// RSI背离
	if divergence.RSIDivergence != nil {
		if divergence.RSIDivergence.Type == "bearish" {
			analysis.KeySignals = append(analysis.KeySignals, "rsi_bearish_divergence")
		} else if divergence.RSIDivergence.Type == "bullish" {
			analysis.KeySignals = append(analysis.KeySignals, "rsi_bullish_divergence")
		}
	}
	
	// MACD背离
	if divergence.MACDDivergence != nil {
		if divergence.MACDDivergence.Type == "bearish" {
			analysis.KeySignals = append(analysis.KeySignals, "macd_bearish_divergence")
		} else if divergence.MACDDivergence.Type == "bullish" {
			analysis.KeySignals = append(analysis.KeySignals, "macd_bullish_divergence")
		}
	}
	
	// 综合背离警告
	if divergence.Signal == "strong_reversal" {
		analysis.KeySignals = append(analysis.KeySignals, "strong_reversal_warning")
	} else if divergence.Signal == "reversal_warning" {
		analysis.KeySignals = append(analysis.KeySignals, "reversal_warning")
	}
}

