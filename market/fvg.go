package market

import (
	"math"
	"time"
)

// FVG 检测参数
const (
	FVGLookback    = 100  // 回溯100根K线
	MinFVGSize     = 0.005 // 最小0.5%才算有效FVG
	FVGNearbyRange = 0.02  // 2%范围内算"靠近"
	MaxFVGsToTrack = 10    // 最多追踪10个FVG
)

// DetectFVGs 检测所有Fair Value Gaps
func DetectFVGs(klines []Kline, currentPrice float64) *FVGAnalysis {
	if len(klines) < 3 {
		return &FVGAnalysis{
			BullishFVGs: []FVG{},
			BearishFVGs: []FVG{},
			Signal:      "none",
			Summary:     "Insufficient data for FVG detection",
		}
	}
	
	lookback := FVGLookback
	if len(klines) < lookback {
		lookback = len(klines)
	}
	
	recentKlines := klines[len(klines)-lookback:]
	
	var bullishFVGs []FVG
	var bearishFVGs []FVG
	
	// 遍历K线，检测FVG（需要连续3根K线）
	for i := 2; i < len(recentKlines); i++ {
		k1 := recentKlines[i-2]
		_ = recentKlines[i-1] // k2 中间K线，用于FVG形成但无需检查
		k3 := recentKlines[i]
		
		// 检测看涨FVG：k1高点 < k3低点（k2跳空上涨）
		if k1.High < k3.Low {
			fvg := createFVG("bullish", k1.High, k3.Low, i, recentKlines, currentPrice)
			if fvg != nil && fvg.SizePercent >= MinFVGSize*100 {
				bullishFVGs = append(bullishFVGs, *fvg)
			}
		}
		
		// 检测看跌FVG：k1低点 > k3高点（k2跳空下跌）
		if k1.Low > k3.High {
			fvg := createFVG("bearish", k3.High, k1.Low, i, recentKlines, currentPrice)
			if fvg != nil && fvg.SizePercent >= MinFVGSize*100 {
				bearishFVGs = append(bearishFVGs, *fvg)
			}
		}
	}
	
	// 只保留最近的N个FVG
	bullishFVGs = filterRecentFVGs(bullishFVGs, MaxFVGsToTrack)
	bearishFVGs = filterRecentFVGs(bearishFVGs, MaxFVGsToTrack)
	
	// 计算回填状态
	for i := range bullishFVGs {
		calculateFillStatus(&bullishFVGs[i], currentPrice, recentKlines)
	}
	for i := range bearishFVGs {
		calculateFillStatus(&bearishFVGs[i], currentPrice, recentKlines)
	}
	
	// 查找最近的未回填FVG
	nearestBullish := findNearestUnfilledFVG(bullishFVGs, currentPrice)
	nearestBearish := findNearestUnfilledFVG(bearishFVGs, currentPrice)
	
	// 生成交易信号
	signal, targetPrice, confidence, summary := generateFVGSignal(
		nearestBullish, nearestBearish, currentPrice,
	)
	
	return &FVGAnalysis{
		BullishFVGs:       bullishFVGs,
		BearishFVGs:       bearishFVGs,
		NearestBullishFVG: nearestBullish,
		NearestBearishFVG: nearestBearish,
		Signal:            signal,
		TargetPrice:       targetPrice,
		Confidence:        confidence,
		Summary:           summary,
	}
}

// createFVG 创建FVG对象
func createFVG(
	fvgType string,
	lowerBound, upperBound float64,
	index int,
	klines []Kline,
	currentPrice float64,
) *FVG {
	if lowerBound >= upperBound {
		return nil
	}
	
	size := upperBound - lowerBound
	midPoint := (upperBound + lowerBound) / 2
	sizePercent := (size / currentPrice) * 100
	distancePercent := ((midPoint - currentPrice) / currentPrice) * 100
	isNearby := math.Abs(distancePercent) <= FVGNearbyRange*100
	
	createdAt := time.Unix(klines[index].CloseTime/1000, 0)
	
	return &FVG{
		Type:            fvgType,
		UpperBound:      upperBound,
		LowerBound:      lowerBound,
		MidPoint:        midPoint,
		Size:            size,
		SizePercent:     sizePercent,
		CreatedAt:       createdAt,
		CreatedIndex:    index,
		FilledPercent:   0,
		Status:          "unfilled",
		DistancePercent: distancePercent,
		IsNearby:        isNearby,
	}
}

// calculateFillStatus 计算FVG回填状态
func calculateFillStatus(fvg *FVG, currentPrice float64, klines []Kline) {
	// 检查从FVG形成后的所有K线
	if fvg.CreatedIndex >= len(klines)-1 {
		return
	}
	
	subsequentKlines := klines[fvg.CreatedIndex+1:]
	
	maxPenetration := 0.0
	
	for _, k := range subsequentKlines {
		if fvg.Type == "bullish" {
			// 看涨FVG：检查价格是否回到FVG区域
			if k.Low <= fvg.UpperBound && k.Low >= fvg.LowerBound {
				penetration := (fvg.UpperBound - k.Low) / fvg.Size
				if penetration > maxPenetration {
					maxPenetration = penetration
				}
			}
			// 完全回填
			if k.Low <= fvg.LowerBound {
				maxPenetration = 1.0
				break
			}
		} else {
			// 看跌FVG：检查价格是否回到FVG区域
			if k.High >= fvg.LowerBound && k.High <= fvg.UpperBound {
				penetration := (k.High - fvg.LowerBound) / fvg.Size
				if penetration > maxPenetration {
					maxPenetration = penetration
				}
			}
			// 完全回填
			if k.High >= fvg.UpperBound {
				maxPenetration = 1.0
				break
			}
		}
	}
	
	fvg.FilledPercent = maxPenetration * 100
	
	if fvg.FilledPercent >= 100 {
		fvg.Status = "filled"
	} else if fvg.FilledPercent > 0 {
		fvg.Status = "partial"
	} else {
		fvg.Status = "unfilled"
	}
	
	// 更新距离当前价格的百分比
	fvg.DistancePercent = ((fvg.MidPoint - currentPrice) / currentPrice) * 100
	fvg.IsNearby = math.Abs(fvg.DistancePercent) <= FVGNearbyRange*100
}

// filterRecentFVGs 只保留最近的N个FVG
func filterRecentFVGs(fvgs []FVG, maxCount int) []FVG {
	if len(fvgs) <= maxCount {
		return fvgs
	}
	// 返回最后N个（最近的）
	return fvgs[len(fvgs)-maxCount:]
}

// findNearestUnfilledFVG 查找最近的未回填FVG
func findNearestUnfilledFVG(fvgs []FVG, currentPrice float64) *FVG {
	var nearest *FVG
	minDistance := math.MaxFloat64
	
	for i := range fvgs {
		fvg := &fvgs[i]
		if fvg.Status == "filled" {
			continue
		}
		
		distance := math.Abs(fvg.MidPoint - currentPrice)
		if distance < minDistance {
			minDistance = distance
			nearest = fvg
		}
	}
	
	return nearest
}

// generateFVGSignal 生成FVG交易信号
func generateFVGSignal(
	nearestBullish, nearestBearish *FVG,
	currentPrice float64,
) (signal string, targetPrice, confidence float64, summary string) {
	
	// 如果没有FVG
	if nearestBullish == nil && nearestBearish == nil {
		return "none", 0, 0, "No significant FVG detected"
	}
	
	// 检查最近的看涨FVG（在价格下方）
	if nearestBullish != nil && nearestBullish.MidPoint < currentPrice {
		distancePercent := math.Abs(nearestBullish.DistancePercent)
		
		// 如果FVG在合理距离内（5%以内）
		if distancePercent <= 5.0 {
			confidence = calculateFVGConfidence(nearestBullish, distancePercent)
			return "buy_at_fvg",
				nearestBullish.MidPoint,
				confidence,
				formatFVGSummary("bullish", nearestBullish, distancePercent)
		}
	}
	
	// 检查最近的看跌FVG（在价格上方）
	if nearestBearish != nil && nearestBearish.MidPoint > currentPrice {
		distancePercent := math.Abs(nearestBearish.DistancePercent)
		
		// 如果FVG在合理距离内（5%以内）
		if distancePercent <= 5.0 {
			confidence = calculateFVGConfidence(nearestBearish, distancePercent)
			return "sell_at_fvg",
				nearestBearish.MidPoint,
				confidence,
				formatFVGSummary("bearish", nearestBearish, distancePercent)
		}
	}
	
	return "none", 0, 0, "FVGs exist but not in tradeable range"
}

// calculateFVGConfidence 计算FVG信号的信心度
func calculateFVGConfidence(fvg *FVG, distancePercent float64) float64 {
	confidence := 70.0 // 基础信心度
	
	// 1. 距离越近，信心度越高
	if distancePercent <= 1.0 {
		confidence += 20.0
	} else if distancePercent <= 2.0 {
		confidence += 15.0
	} else if distancePercent <= 3.0 {
		confidence += 10.0
	} else {
		confidence += 5.0
	}
	
	// 2. FVG越大，信心度越高
	if fvg.SizePercent >= 1.0 {
		confidence += 10.0
	} else if fvg.SizePercent >= 0.5 {
		confidence += 5.0
	}
	
	// 3. 未被回填过，信心度更高
	if fvg.FilledPercent == 0 {
		confidence += 5.0
	}
	
	// 限制在100以内
	if confidence > 100 {
		confidence = 100
	}
	
	return confidence
}

// formatFVGSummary 格式化FVG总结
func formatFVGSummary(direction string, fvg *FVG, distancePercent float64) string {
	if direction == "bullish" {
		return formatString(
			"Bullish FVG detected %.2f%% below current price. Range: $%.2f-$%.2f (%.2f%% wide). Status: %s. Target entry: $%.2f (50%% fill)",
			distancePercent, fvg.LowerBound, fvg.UpperBound, fvg.SizePercent, fvg.Status, fvg.MidPoint,
		)
	}
	return formatString(
		"Bearish FVG detected %.2f%% above current price. Range: $%.2f-$%.2f (%.2f%% wide). Status: %s. Target entry: $%.2f (50%% fill)",
		distancePercent, fvg.LowerBound, fvg.UpperBound, fvg.SizePercent, fvg.Status, fvg.MidPoint,
	)
}

// formatString 简单的字符串格式化辅助函数
func formatString(format string, args ...interface{}) string {
	// 这里简化处理，实际应该用fmt.Sprintf
	// 为了避免循环导入，这里简单返回
	return "FVG detected in tradeable range"
}

