package market

import (
	"log"
	"math"
	"sync"
	"time"
)

// AnomalyType 异常类型
type AnomalyType string

const (
	AnomalyPriceSpike      AnomalyType = "PRICE_SPIKE"      // 价格暴涨
	AnomalyPriceCrash      AnomalyType = "PRICE_CRASH"      // 价格暴跌
	AnomalyVolumeSpike     AnomalyType = "VOLUME_SPIKE"     // 成交量突增
	AnomalyVolatilitySpike AnomalyType = "VOLATILITY_SPIKE" // 波动率突增
	AnomalyConsecutiveUp   AnomalyType = "CONSECUTIVE_UP"   // 连续上涨
	AnomalyConsecutiveDown AnomalyType = "CONSECUTIVE_DOWN" // 连续下跌
)

// AnomalySeverity 严重程度
type AnomalySeverity string

const (
	SeverityLow      AnomalySeverity = "LOW"
	SeverityMedium   AnomalySeverity = "MEDIUM"
	SeverityHigh     AnomalySeverity = "HIGH"
	SeverityCritical AnomalySeverity = "CRITICAL"
)

// AnomalyEvent 异常事件
type AnomalyEvent struct {
	ID        string          `json:"id"`
	Symbol    string          `json:"symbol"`
	Type      AnomalyType     `json:"type"`
	Severity  AnomalySeverity `json:"severity"`
	Timestamp time.Time       `json:"timestamp"`

	// 异常详情
	PriceChange3m  float64 `json:"price_change_3m"`
	PriceChange15m float64 `json:"price_change_15m"`
	VolumeRatio    float64 `json:"volume_ratio"`
	ATRRatio       float64 `json:"atr_ratio"`
	Direction      string  `json:"direction"` // UP, DOWN

	// 市场快照
	CurrentPrice  float64 `json:"current_price"`
	CurrentVolume float64 `json:"current_volume"`

	// 内部使用
	Kline        *Kline  `json:"-"`
	RecentKlines []Kline `json:"-"`
}

// AnomalyDetector 异常检测器
type AnomalyDetector struct {
    symbol               string
    priceThresholdK      float64 // ATR 标准化系数
    volumeMultiplierM    float64 // 成交量放大倍数
    consecutiveThreshold float64 // 连续同向阈值
    minVolumeUSDT        float64 // 最小成交量
    absMinPriceChangePct float64 // 绝对最小价格变化阈值（百分比）

	// 冷却期管理
	coolingPeriod int                       // 冷却期（分钟）
	lastEventTime map[AnomalyType]time.Time // 上次事件时间
	cooldownMutex sync.RWMutex
}

// NewAnomalyDetector 创建异常检测器
func NewAnomalyDetector(
    symbol string,
    priceThresholdK float64,
    volumeMultiplierM float64,
    consecutiveThreshold float64,
    minVolumeUSDT float64,
    coolingPeriod int,
    absMinPriceChangePct float64,
) *AnomalyDetector {
    return &AnomalyDetector{
        symbol:               symbol,
        priceThresholdK:      priceThresholdK,
        volumeMultiplierM:    volumeMultiplierM,
        consecutiveThreshold: consecutiveThreshold,
        minVolumeUSDT:        minVolumeUSDT,
        absMinPriceChangePct: absMinPriceChangePct,
        coolingPeriod:        coolingPeriod,
        lastEventTime:        make(map[AnomalyType]time.Time),
    }
}

// Detect 检测异常
func (d *AnomalyDetector) Detect(currentKline *Kline, recentKlines []Kline) []*AnomalyEvent {
	var events []*AnomalyEvent

	// 需要至少 20 根 K 线进行分析
	if len(recentKlines) < 20 {
		return events
	}

	// 1. 价格异常检测（ATR 标准化）
	if event := d.detectPriceAnomaly(currentKline, recentKlines); event != nil {
		events = append(events, event)
	}

	// 2. 成交量异常检测
	if event := d.detectVolumeAnomaly(currentKline, recentKlines); event != nil {
		events = append(events, event)
	}

	// 3. 连续同向 K 线检测
	if event := d.detectConsecutiveMove(currentKline, recentKlines); event != nil {
		events = append(events, event)
	}

	return events
}

// detectPriceAnomaly 检测价格异常（ATR 标准化）
func (d *AnomalyDetector) detectPriceAnomaly(current *Kline, recent []Kline) *AnomalyEvent {
	if len(recent) < 15 {
		return nil
	}

	// 计算 ATR (14 周期)
	atr := d.calculateATR(recent, 14)
	if atr == 0 {
		return nil
	}

	// ATR 标准化的价格变化百分比
	atrPct := atr / current.Close * 100

    // 获取前一根 K 线（注意 recent 不包含 current，本应取倒数第二根收盘价）
    if len(recent) < 2 {
        return nil
    }
    prevClose := recent[len(recent)-2].Close

	// 价格变化百分比
	priceChangePct := math.Abs((current.Close - prevClose) / prevClose * 100)

    // 检测阈值：取 ATR 标准化与绝对变化的更大者
    threshold := d.priceThresholdK * atrPct
    if d.absMinPriceChangePct > 0 && threshold < d.absMinPriceChangePct {
        threshold = d.absMinPriceChangePct
    }

	if priceChangePct >= threshold {
		// 判断方向
		direction := "UP"
		anomalyType := AnomalyPriceSpike
		if current.Close < prevClose {
			direction = "DOWN"
			anomalyType = AnomalyPriceCrash
		}

		// 检查冷却期
		if !d.checkCoolingPeriod(anomalyType) {
			return nil
		}

		// 计算严重程度
		severity := d.calculateSeverity(priceChangePct, threshold)

		event := &AnomalyEvent{
			ID:            generateEventID(),
			Symbol:        d.symbol,
			Type:          anomalyType,
			Severity:      severity,
			Timestamp:     time.Now(),
			PriceChange3m: priceChangePct * sign(current.Close-prevClose),
			VolumeRatio:   0,
			ATRRatio:      priceChangePct / atrPct,
			Direction:     direction,
			CurrentPrice:  current.Close,
			CurrentVolume: current.Volume,
			Kline:         current,
			RecentKlines:  recent,
		}

		// 更新冷却期
		d.updateLastEventTime(anomalyType)

		log.Printf("🚨 [%s] 检测到价格异常：%s %.2f%% (阈值 %.2f%%, ATR %.2f%%)",
			d.symbol, direction, priceChangePct, threshold, atrPct)

		return event
	}

	return nil
}

// detectVolumeAnomaly 检测成交量异常
func (d *AnomalyDetector) detectVolumeAnomaly(current *Kline, recent []Kline) *AnomalyEvent {
	if len(recent) < 20 {
		return nil
	}

	// 计算最近 20 根 K 线的平均成交量
	avgVolume := d.calculateAverageVolume(recent, 20)
	if avgVolume == 0 {
		return nil
	}

	// 成交量比率
	volumeRatio := current.Volume / avgVolume

    // 检查是否超过阈值且满足最小成交量要求（优先用报价量）
    currentVolumeUSDT := current.QuoteVolume

	if volumeRatio >= d.volumeMultiplierM && currentVolumeUSDT >= d.minVolumeUSDT {
		// 检查冷却期
		if !d.checkCoolingPeriod(AnomalyVolumeSpike) {
			return nil
		}

		// 计算价格变化
		prevClose := recent[len(recent)-1].Close
		priceChangePct := (current.Close - prevClose) / prevClose * 100
		direction := "UP"
		if priceChangePct < 0 {
			direction = "DOWN"
		}

		// 计算严重程度
		severity := SeverityMedium
		if volumeRatio >= d.volumeMultiplierM*2 {
			severity = SeverityHigh
		}
		if volumeRatio >= d.volumeMultiplierM*3 {
			severity = SeverityCritical
		}

		event := &AnomalyEvent{
			ID:            generateEventID(),
			Symbol:        d.symbol,
			Type:          AnomalyVolumeSpike,
			Severity:      severity,
			Timestamp:     time.Now(),
			PriceChange3m: priceChangePct,
			VolumeRatio:   volumeRatio,
			Direction:     direction,
			CurrentPrice:  current.Close,
			CurrentVolume: current.Volume,
			Kline:         current,
			RecentKlines:  recent,
		}

		// 更新冷却期
		d.updateLastEventTime(AnomalyVolumeSpike)

		log.Printf("🚨 [%s] 检测到成交量异常：%.1fx (平均 %.0f, 当前 %.0f)",
			d.symbol, volumeRatio, avgVolume, current.Volume)

		return event
	}

	return nil
}

// detectConsecutiveMove 检测连续同向 K 线
func (d *AnomalyDetector) detectConsecutiveMove(current *Kline, recent []Kline) *AnomalyEvent {
	if len(recent) < 3 {
		return nil
	}

	// 检查最近 3 根 K 线（包括当前）
	last3 := append(recent[len(recent)-2:], *current)

	// 计算累计变化
	startPrice := last3[0].Open
	endPrice := last3[len(last3)-1].Close
	totalChange := (endPrice - startPrice) / startPrice

	// 检查是否连续同向
	allUp := true
	allDown := true

	for _, k := range last3 {
		if k.Close <= k.Open {
			allUp = false
		}
		if k.Close >= k.Open {
			allDown = false
		}
	}

	// 判断是否满足条件
	absChange := math.Abs(totalChange)
	if absChange >= d.consecutiveThreshold && (allUp || allDown) {
		anomalyType := AnomalyConsecutiveUp
		direction := "UP"
		if allDown {
			anomalyType = AnomalyConsecutiveDown
			direction = "DOWN"
		}

		// 检查冷却期
		if !d.checkCoolingPeriod(anomalyType) {
			return nil
		}

		// 计算严重程度
		severity := SeverityMedium
		if absChange >= d.consecutiveThreshold*1.5 {
			severity = SeverityHigh
		}

		event := &AnomalyEvent{
			ID:            generateEventID(),
			Symbol:        d.symbol,
			Type:          anomalyType,
			Severity:      severity,
			Timestamp:     time.Now(),
			PriceChange3m: totalChange * 100,
			Direction:     direction,
			CurrentPrice:  current.Close,
			CurrentVolume: current.Volume,
			Kline:         current,
			RecentKlines:  recent,
		}

		// 更新冷却期
		d.updateLastEventTime(anomalyType)

		log.Printf("🚨 [%s] 检测到连续%s：3根K线累计 %.2f%%",
			d.symbol, direction, totalChange*100)

		return event
	}

	return nil
}

// 辅助函数

// calculateATR 计算平均真实波幅
func (d *AnomalyDetector) calculateATR(klines []Kline, period int) float64 {
	if len(klines) < period+1 {
		return 0
	}

	var trSum float64
	startIdx := len(klines) - period

	for i := startIdx; i < len(klines); i++ {
		tr := d.calculateTR(klines[i], klines[i-1])
		trSum += tr
	}

	return trSum / float64(period)
}

// calculateTR 计算真实波幅
func (d *AnomalyDetector) calculateTR(current, previous Kline) float64 {
	highLow := current.High - current.Low
	highClose := math.Abs(current.High - previous.Close)
	lowClose := math.Abs(current.Low - previous.Close)

	return math.Max(highLow, math.Max(highClose, lowClose))
}

// calculateAverageVolume 计算平均成交量
func (d *AnomalyDetector) calculateAverageVolume(klines []Kline, period int) float64 {
	if len(klines) < period {
		return 0
	}

	var volumeSum float64
	startIdx := len(klines) - period

	for i := startIdx; i < len(klines); i++ {
		volumeSum += klines[i].Volume
	}

	return volumeSum / float64(period)
}

// calculateSeverity 计算严重程度
func (d *AnomalyDetector) calculateSeverity(value, threshold float64) AnomalySeverity {
	ratio := value / threshold

	if ratio >= 3.0 {
		return SeverityCritical
	} else if ratio >= 2.0 {
		return SeverityHigh
	} else if ratio >= 1.5 {
		return SeverityMedium
	}
	return SeverityLow
}

// checkCoolingPeriod 检查冷却期
func (d *AnomalyDetector) checkCoolingPeriod(eventType AnomalyType) bool {
	d.cooldownMutex.RLock()
	defer d.cooldownMutex.RUnlock()

	lastTime, exists := d.lastEventTime[eventType]
	if !exists {
		return true
	}

	elapsed := time.Since(lastTime)
	coolingDuration := time.Duration(d.coolingPeriod) * time.Minute

	if elapsed < coolingDuration {
		log.Printf("⏸️  [%s] %s 在冷却期内，忽略 (已过 %.1f 分钟，需要 %d 分钟)",
			d.symbol, eventType, elapsed.Minutes(), d.coolingPeriod)
		return false
	}

	return true
}

// updateLastEventTime 更新最后事件时间
func (d *AnomalyDetector) updateLastEventTime(eventType AnomalyType) {
	d.cooldownMutex.Lock()
	defer d.cooldownMutex.Unlock()

	d.lastEventTime[eventType] = time.Now()
}

// 辅助函数
func sign(x float64) float64 {
	if x >= 0 {
		return 1
	}
	return -1
}

func generateEventID() string {
	return time.Now().Format("20060102150405") + randomString(6)
}

func randomString(n int) string {
	const letters = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
	}
	return string(b)
}
