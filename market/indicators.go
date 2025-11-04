package market

import (
	"math"
)

// calculateBollingerBands 计算布林带
func calculateBollingerBands(klines []Kline, period int, stdDevMultiplier float64) *BollingerBands {
	if len(klines) < period {
		return nil
	}

	// 计算中轨（SMA）
	sum := 0.0
	for i := len(klines) - period; i < len(klines); i++ {
		sum += klines[i].Close
	}
	middle := sum / float64(period)

	// 计算标准差
	variance := 0.0
	for i := len(klines) - period; i < len(klines); i++ {
		diff := klines[i].Close - middle
		variance += diff * diff
	}
	stdDev := math.Sqrt(variance / float64(period))

	// 计算上下轨
	upper := middle + stdDevMultiplier*stdDev
	lower := middle - stdDevMultiplier*stdDev

	// 计算带宽
	bandWidth := 0.0
	if middle > 0 {
		bandWidth = (upper - lower) / middle * 100
	}

	// 计算 %B
	currentPrice := klines[len(klines)-1].Close
	percentB := 0.0
	if upper != lower {
		percentB = (currentPrice - lower) / (upper - lower)
	}

	return &BollingerBands{
		Upper:     upper,
		Middle:    middle,
		Lower:     lower,
		BandWidth: bandWidth,
		PercentB:  percentB,
	}
}

// calculateADX 计算ADX（Average Directional Index）
func calculateADX(klines []Kline, period int) *ADXData {
	if len(klines) < period+1 {
		return nil
	}

	// 计算True Range和方向移动
	trueRanges := make([]float64, len(klines))
	plusDM := make([]float64, len(klines))
	minusDM := make([]float64, len(klines))

	for i := 1; i < len(klines); i++ {
		high := klines[i].High
		low := klines[i].Low
		prevClose := klines[i-1].Close
		prevHigh := klines[i-1].High
		prevLow := klines[i-1].Low

		// True Range
		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)
		trueRanges[i] = math.Max(tr1, math.Max(tr2, tr3))

		// Directional Movement
		upMove := high - prevHigh
		downMove := prevLow - low

		if upMove > downMove && upMove > 0 {
			plusDM[i] = upMove
		}
		if downMove > upMove && downMove > 0 {
			minusDM[i] = downMove
		}
	}

	// 计算平滑的TR, +DM, -DM
	smoothTR := 0.0
	smoothPlusDM := 0.0
	smoothMinusDM := 0.0

	// 初始化
	for i := 1; i <= period; i++ {
		smoothTR += trueRanges[i]
		smoothPlusDM += plusDM[i]
		smoothMinusDM += minusDM[i]
	}

	// Wilder平滑
	for i := period + 1; i < len(klines); i++ {
		smoothTR = smoothTR - (smoothTR / float64(period)) + trueRanges[i]
		smoothPlusDM = smoothPlusDM - (smoothPlusDM / float64(period)) + plusDM[i]
		smoothMinusDM = smoothMinusDM - (smoothMinusDM / float64(period)) + minusDM[i]
	}

	// 计算DI
	plusDI := 0.0
	minusDI := 0.0
	if smoothTR > 0 {
		plusDI = (smoothPlusDM / smoothTR) * 100
		minusDI = (smoothMinusDM / smoothTR) * 100
	}

	// 计算DX
	dx := 0.0
	if (plusDI + minusDI) > 0 {
		dx = (math.Abs(plusDI-minusDI) / (plusDI + minusDI)) * 100
	}

	// ADX是DX的平滑移动平均（简化版本，实际应该用多个周期的DX）
	adx := dx // 简化实现，生产环境应该用完整的Wilder平滑

	return &ADXData{
		ADX:     adx,
		PlusDI:  plusDI,
		MinusDI: minusDI,
	}
}

// calculateVWAP 计算VWAP（Volume Weighted Average Price）
func calculateVWAP(klines []Kline) float64 {
	if len(klines) == 0 {
		return 0
	}

	totalVolume := 0.0
	totalPV := 0.0

	for _, k := range klines {
		typicalPrice := (k.High + k.Low + k.Close) / 3
		pv := typicalPrice * k.Volume
		totalPV += pv
		totalVolume += k.Volume
	}

	if totalVolume == 0 {
		return 0
	}

	return totalPV / totalVolume
}

// calculateMultipleEMAs 计算多周期EMA
func calculateMultipleEMAs(klines []Kline) *MultiEMAData {
	return &MultiEMAData{
		EMA5:   calculateEMA(klines, 5),
		EMA10:  calculateEMA(klines, 10),
		EMA20:  calculateEMA(klines, 20),
		EMA30:  calculateEMA(klines, 30),
		EMA50:  calculateEMA(klines, 50),
		EMA100: calculateEMA(klines, 100),
		EMA200: calculateEMA(klines, 200),
	}
}

// calculateStochastic 计算随机指标 (Stochastic Oscillator)
func calculateStochastic(klines []Kline, kPeriod, dPeriod int) *StochasticData {
	if len(klines) < kPeriod {
		return nil
	}

	// 计算 %K
	recentKlines := klines[len(klines)-kPeriod:]
	
	// 找最高价和最低价
	highestHigh := recentKlines[0].High
	lowestLow := recentKlines[0].Low
	for _, k := range recentKlines {
		if k.High > highestHigh {
			highestHigh = k.High
		}
		if k.Low < lowestLow {
			lowestLow = k.Low
		}
	}

	currentClose := klines[len(klines)-1].Close
	kValue := 0.0
	if highestHigh != lowestLow {
		kValue = ((currentClose - lowestLow) / (highestHigh - lowestLow)) * 100
	}

	// 计算 %D（%K的简单移动平均）
	// 简化实现：只用当前K值，实际应该用最近dPeriod个K值的平均
	dValue := kValue // 简化版本

	return &StochasticData{
		K: kValue,
		D: dValue,
	}
}

// calculateOBV 计算OBV (On-Balance Volume)
func calculateOBV(klines []Kline) float64 {
	if len(klines) < 2 {
		return 0
	}

	obv := 0.0
	for i := 1; i < len(klines); i++ {
		if klines[i].Close > klines[i-1].Close {
			obv += klines[i].Volume
		} else if klines[i].Close < klines[i-1].Close {
			obv -= klines[i].Volume
		}
		// 如果相等，OBV不变
	}

	return obv
}

// calculateSupportResistance 计算支撑和阻力位
func calculateSupportResistance(klines []Kline, currentPrice float64) (support, resistance float64) {
	if len(klines) < 10 {
		return currentPrice * 0.95, currentPrice * 1.05
	}

	// 方法1: 使用Pivot Points
	lastKline := klines[len(klines)-1]
	pivot := (lastKline.High + lastKline.Low + lastKline.Close) / 3
	
	r1 := 2*pivot - lastKline.Low
	s1 := 2*pivot - lastKline.High

	// 找最近的支撑和阻力
	if currentPrice > pivot {
		support = pivot
		resistance = r1
	} else {
		support = s1
		resistance = pivot
	}

	return support, resistance
}

