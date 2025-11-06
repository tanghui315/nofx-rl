package market

import (
	"math"
)

// Ichimoku Cloud 参数
const (
	TenkanPeriod  = 9  // 转换线周期
	KijunPeriod   = 26 // 基准线周期
	SenkouBPeriod = 52 // 先行带B周期
	Displacement  = 26 // 偏移量

	ThickCloudThreshold = 0.015 // 1.5%以上算厚云
)

// CalculateIchimoku 计算一目均衡表
func CalculateIchimoku(klines []Kline, currentPrice float64) *IchimokuCloud {
	if len(klines) < SenkouBPeriod {
		return nil // 数据不足
	}

	// 1. 计算转换线 Tenkan-sen (9期)
	tenkan := calculateTenkan(klines, TenkanPeriod)

	// 2. 计算基准线 Kijun-sen (26期)
	kijun := calculateKijun(klines, KijunPeriod)

	// 3. 计算先行带 Senkou Span A & B
	senkouA, senkouB := calculateSenkou(klines, tenkan, kijun)

	// 4. 计算未来云边界（向未来偏移26期）
	futureSenkouA, futureSenkouB := calculateFutureSenkou(klines)

	// 5. 计算延迟线 Chikou Span（当前收盘价，向过去偏移26期）
	chikouSpan := currentPrice

	// 6. 分析云的属性
	cloudColor, cloudTop, cloudBottom := analyzeCloudColor(senkouA, senkouB)
	cloudThickness := math.Abs(cloudTop - cloudBottom)
	cloudPercent := (cloudThickness / currentPrice) * 100

	// 7. 分析价格与云的关系
	pricePosition, priceToCloud := analyzeIchimokuPricePosition(currentPrice, cloudTop, cloudBottom)

	// 8. 检测TK交叉
	tkCross, tkDistance, tkPercent := analyzeTKCross(tenkan, kijun, currentPrice)

	// 9. 分析Chikou Span
	chikouPosition := analyzeChikouPosition(klines, chikouSpan)

	// 10. 生成综合信号
	signal, strength, confidence := generateIchimokuSignal(
		pricePosition, cloudColor, tkCross, chikouPosition, cloudPercent,
	)

	return &IchimokuCloud{
		Tenkan:         tenkan,
		Kijun:          kijun,
		SenkouSpanA:    senkouA,
		SenkouSpanB:    senkouB,
		ChikouSpan:     chikouSpan,
		FutureSenkouA:  futureSenkouA,
		FutureSenkouB:  futureSenkouB,
		CloudColor:     cloudColor,
		CloudTop:       cloudTop,
		CloudBottom:    cloudBottom,
		CloudThickness: cloudThickness,
		CloudPercent:   cloudPercent,
		PricePosition:  pricePosition,
		PriceToCloud:   priceToCloud,
		TKCross:        tkCross,
		TKDistance:     tkDistance,
		TKPercent:      tkPercent,
		ChikouPosition: chikouPosition,
		Signal:         signal,
		Strength:       strength,
		Confidence:     confidence,
	}
}

// calculateTenkan 计算转换线 (9期最高+最低)/2
func calculateTenkan(klines []Kline, period int) float64 {
	if len(klines) < period {
		return 0
	}

	recent := klines[len(klines)-period:]
	high, low := getHighLow(recent)
	return (high + low) / 2
}

// calculateKijun 计算基准线 (26期最高+最低)/2
func calculateKijun(klines []Kline, period int) float64 {
	if len(klines) < period {
		return 0
	}

	recent := klines[len(klines)-period:]
	high, low := getHighLow(recent)
	return (high + low) / 2
}

// calculateSenkou 计算先行带A和B（当前）
func calculateSenkou(klines []Kline, tenkan, kijun float64) (float64, float64) {
	// Senkou Span A = (Tenkan + Kijun) / 2
	senkouA := (tenkan + kijun) / 2

	// Senkou Span B = (52期最高 + 52期最低) / 2
	senkouB := 0.0
	if len(klines) >= SenkouBPeriod {
		recent := klines[len(klines)-SenkouBPeriod:]
		high, low := getHighLow(recent)
		senkouB = (high + low) / 2
	}

	return senkouA, senkouB
}

// calculateFutureSenkou 计算未来云边界（26期后）
func calculateFutureSenkou(klines []Kline) (float64, float64) {
	// 这里简化处理：使用当前的Tenkan/Kijun计算
	// 实际应用中，未来云是已知的（已经计算好的）
	if len(klines) < SenkouBPeriod {
		return 0, 0
	}

	// 使用最近的数据预测
	tenkan := calculateTenkan(klines, TenkanPeriod)
	kijun := calculateKijun(klines, KijunPeriod)
	futureSenkouA := (tenkan + kijun) / 2

	recent := klines[len(klines)-SenkouBPeriod:]
	high, low := getHighLow(recent)
	futureSenkouB := (high + low) / 2

	return futureSenkouA, futureSenkouB
}

// analyzeCloudColor 分析云的颜色和边界
func analyzeCloudColor(senkouA, senkouB float64) (color string, top, bottom float64) {
	if senkouA > senkouB {
		return "green", senkouA, senkouB // 绿云（看涨）
	} else if senkouA < senkouB {
		return "red", senkouB, senkouA // 红云（看跌）
	}
	return "neutral", senkouA, senkouB
}

// analyzeIchimokuPricePosition 分析价格与云的关系
func analyzeIchimokuPricePosition(price, cloudTop, cloudBottom float64) (position string, distance float64) {
	if price > cloudTop {
		distance = ((price - cloudTop) / price) * 100
		return "above_cloud", distance
	} else if price < cloudBottom {
		distance = ((cloudBottom - price) / price) * 100
		return "below_cloud", -distance
	}
	return "in_cloud", 0
}

// analyzeTKCross 分析TK交叉
func analyzeTKCross(tenkan, kijun, currentPrice float64) (cross string, distance, percent float64) {
	distance = math.Abs(tenkan - kijun)
	percent = (distance / currentPrice) * 100

	if tenkan > kijun {
		return "bullish", distance, percent
	} else if tenkan < kijun {
		return "bearish", -distance, percent
	}
	return "neutral", 0, 0
}

// analyzeChikouPosition 分析Chikou Span位置
func analyzeChikouPosition(klines []Kline, chikouSpan float64) string {
	if len(klines) < Displacement {
		return "neutral"
	}

	// 获取26期前的价格
	historicalPrice := klines[len(klines)-Displacement].Close

	if chikouSpan > historicalPrice {
		return "above_price" // 看涨
	} else if chikouSpan < historicalPrice {
		return "below_price" // 看跌
	}
	return "neutral"
}

// generateIchimokuSignal 生成综合信号
func generateIchimokuSignal(
	pricePosition, cloudColor, tkCross, chikouPosition string,
	cloudPercent float64,
) (signal string, strength, confidence float64) {

	score := 0.0
	maxScore := 5.0

	// 1. 价格位置（最重要，权重2）
	if pricePosition == "above_cloud" && cloudColor == "green" {
		score += 2.0
	} else if pricePosition == "below_cloud" && cloudColor == "red" {
		score -= 2.0
	} else if pricePosition == "in_cloud" {
		score += 0.0 // 震荡，无信号
	}

	// 2. TK交叉（权重1.5）
	if tkCross == "bullish" {
		score += 1.5
	} else if tkCross == "bearish" {
		score -= 1.5
	}

	// 3. Chikou确认（权重1）
	if chikouPosition == "above_price" {
		score += 1.0
	} else if chikouPosition == "below_price" {
		score -= 1.0
	}

	// 4. 云厚度（权重0.5）
	if cloudPercent > ThickCloudThreshold*100 {
		if cloudColor == "green" {
			score += 0.5
		} else if cloudColor == "red" {
			score -= 0.5
		}
	}

	// 计算信号强度和信心度
	strength = math.Abs(score) / maxScore * 100
	confidence = strength

	// 确定信号类型
	if score >= 4.0 {
		return "strong_buy", strength, confidence
	} else if score >= 2.0 {
		return "buy", strength, confidence
	} else if score <= -4.0 {
		return "strong_sell", strength, confidence
	} else if score <= -2.0 {
		return "sell", strength, confidence
	}
	return "neutral", strength, confidence
}

// getHighLow 获取K线数组的最高和最低价
func getHighLow(klines []Kline) (high, low float64) {
	if len(klines) == 0 {
		return 0, 0
	}

	high = klines[0].High
	low = klines[0].Low

	for _, k := range klines {
		if k.High > high {
			high = k.High
		}
		if k.Low < low {
			low = k.Low
		}
	}

	return high, low
}
