package config

import (
	"encoding/json"
	"log"
	"math"
	"strconv"
)

// AnomalyMode 异常监控模式
type AnomalyMode string

const (
	AnomalyModeOff        AnomalyMode = "off"        // 关闭
	AnomalyModeWatch      AnomalyMode = "watch"      // 观察（只提醒）
	AnomalyModeGuard      AnomalyMode = "guard"      // 防守（只减仓）
	AnomalyModeBalanced   AnomalyMode = "balanced"   // 平衡（可追涨杀跌）
	AnomalyModeAggressive AnomalyMode = "aggressive" // 激进
)

// AnomalySensitivity 灵敏度
type AnomalySensitivity string

const (
	SensitivityLow    AnomalySensitivity = "low"    // 低（5%+ 极端行情）
	SensitivityMedium AnomalySensitivity = "medium" // 中（3%+ 明显异常）
	SensitivityHigh   AnomalySensitivity = "high"   // 高（1.5%+ 早期信号）
)

// AnomalyConfig 异常监控配置
type AnomalyConfig struct {
	// 用户可见配置（3个核心拨片）
	Mode        AnomalyMode        `json:"mode"`        // 防护模式
	Sensitivity AnomalySensitivity `json:"sensitivity"` // 灵敏度
	UseLLM      bool               `json:"use_llm"`     // LLM决策

	// 搏一搏配置（第4个拨片）
	GambitEnabled bool `json:"gambit_enabled"` // 搏一搏开关

	// 内部映射参数（用户不可见，自动计算）
	internal struct {
		// 允许的操作
		AllowReduce bool // 允许减仓
		AllowChase  bool // 允许追涨杀跌
		AllowGambit bool // 允许搏一搏

		// 仓位限制
		MaxPositionPct float64 // 最大仓位百分比

		// 检测阈值
		PriceThresholdK       float64 // 价格变化阈值系数 k
		AbsMinPriceChangePct  float64 // 绝对最小价格变化阈值（%）
		VolumeMultiplierM     float64 // 成交量放大倍数 m
		ConsecutiveThreshold  float64 // 连续同向阈值
		MinVolumeUSDT         float64 // 最小成交量（USDT）
		CoolingPeriodMinutes  int     // 冷却期（分钟）

		// 杠杆策略
		ChaseLeverageMultiplier  float64 // 追涨杀跌杠杆倍数
		GambitLeverageMultiplier float64 // 搏一搏杠杆倍数
		SystemMaxLeverage        int     // 系统绝对杠杆上限

		// 搏一搏参数
		GambitMaxPositionPct float64 // 搏一搏最大仓位百分比
		GambitMaxAmount      float64 // 搏一搏最大绝对金额
		GambitMinAmount      float64 // 搏一搏最小绝对金额（避免过小单）
		GambitMinConfidence  float64 // 搏一搏最小置信度
		GambitMaxStopLoss    float64 // 搏一搏最大止损
		GambitCoolingMinutes int     // 搏一搏冷却期

		// LLM 超时
		LLMTimeoutSeconds    int    // LLM 超时时间（秒）
		LLMTimeoutAction     string // 超时默认动作
	}
}

// LoadAnomalyConfig 从数据库加载异常监控配置
func LoadAnomalyConfig(db *Database) (*AnomalyConfig, error) {
	config := &AnomalyConfig{}

	// 1. 加载核心配置（3个拨片）
	mode, _ := db.GetSystemConfig("anomaly_mode")
	if mode == "" {
		mode = string(AnomalyModeWatch) // 默认观察模式
	}
	config.Mode = AnomalyMode(mode)

	sensitivity, _ := db.GetSystemConfig("anomaly_sensitivity")
	if sensitivity == "" {
		sensitivity = string(SensitivityMedium) // 默认中等灵敏度
	}
	config.Sensitivity = AnomalySensitivity(sensitivity)

	useLLM, _ := db.GetSystemConfig("anomaly_use_llm")
	config.UseLLM = useLLM == "true" // 默认 false

	// 2. 加载搏一搏配置
	gambitEnabled, _ := db.GetSystemConfig("anomaly_gambit_enabled")
	config.GambitEnabled = gambitEnabled == "true" // 默认 false

	// 3. 应用配置映射
	config.applyModeMapping()
	config.applySensitivityMapping()
	config.applyGambitMapping(db)

	log.Printf("✅ 异常监控配置加载完成：模式=%s, 灵敏度=%s, LLM=%v, 搏一搏=%v",
		config.Mode, config.Sensitivity, config.UseLLM, config.GambitEnabled)

	return config, nil
}

// applyModeMapping 根据模式映射内部参数
func (c *AnomalyConfig) applyModeMapping() {
	switch c.Mode {
	case AnomalyModeOff:
		c.internal.AllowReduce = false
		c.internal.AllowChase = false
		c.internal.AllowGambit = false
		c.internal.MaxPositionPct = 0.0

	case AnomalyModeWatch:
		c.internal.AllowReduce = false
		c.internal.AllowChase = false
		c.internal.AllowGambit = false
		c.internal.MaxPositionPct = 0.0
		// 只记录和通知

	case AnomalyModeGuard:
		c.internal.AllowReduce = true
		c.internal.AllowChase = false
		c.internal.AllowGambit = false
		c.internal.MaxPositionPct = 0.02         // 2%
		c.internal.CoolingPeriodMinutes = 15

	case AnomalyModeBalanced:
		c.internal.AllowReduce = true
		c.internal.AllowChase = true
		c.internal.AllowGambit = c.GetGambitEnabled() // 可选搏一搏
		c.internal.MaxPositionPct = 0.05         // 5%
		c.internal.CoolingPeriodMinutes = 15

	case AnomalyModeAggressive:
		c.internal.AllowReduce = true
		c.internal.AllowChase = true
		c.internal.AllowGambit = c.GetGambitEnabled() // 可选搏一搏
		c.internal.MaxPositionPct = 0.10         // 10%
		c.internal.CoolingPeriodMinutes = 5
	}
}

// applySensitivityMapping 根据灵敏度映射检测阈值
func (c *AnomalyConfig) applySensitivityMapping() {
    switch c.Sensitivity {
    case SensitivityLow:
        c.internal.PriceThresholdK = 3.0       // ATR 标准化系数
        c.internal.AbsMinPriceChangePct = 5.0  // 绝对阈值 5%
        c.internal.VolumeMultiplierM = 2.5     // 成交量倍数
        c.internal.ConsecutiveThreshold = 0.09 // 9%
        c.internal.MinVolumeUSDT = 2000000     // 2M USDT

    case SensitivityMedium:
        c.internal.PriceThresholdK = 2.5
        c.internal.AbsMinPriceChangePct = 3.0  // 绝对阈值 3%
        c.internal.VolumeMultiplierM = 2.0
        c.internal.ConsecutiveThreshold = 0.07 // 7%
        c.internal.MinVolumeUSDT = 1000000     // 1M USDT

    case SensitivityHigh:
        c.internal.PriceThresholdK = 2.0
        c.internal.AbsMinPriceChangePct = 1.5  // 绝对阈值 1.5%
        c.internal.VolumeMultiplierM = 1.6
        c.internal.ConsecutiveThreshold = 0.05 // 5%
        c.internal.MinVolumeUSDT = 500000      // 500K USDT
    }
}

// applyGambitMapping 应用搏一搏配置映射
func (c *AnomalyConfig) applyGambitMapping(db *Database) {
	// 杠杆倍数
	c.internal.ChaseLeverageMultiplier = 1.2
	c.internal.GambitLeverageMultiplier = 2.0
	c.internal.SystemMaxLeverage = 50

	// 搏一搏参数（从数据库读取或使用默认值）
	c.internal.GambitMaxPositionPct = 0.02  // 2%
	c.internal.GambitMaxAmount = 5000.0     // $5,000
	c.internal.GambitMinAmount = 50.0       // $50（默认最小金额）
	c.internal.GambitMinConfidence = 0.8    // 80%
	c.internal.GambitMaxStopLoss = 0.03     // 3%
	c.internal.GambitCoolingMinutes = 60    // 60分钟

	// 从数据库读取（如果有自定义值）
	if val, err := db.GetSystemConfig("anomaly_gambit_max_position"); err == nil {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			c.internal.GambitMaxPositionPct = f
		}
	}

	if val, err := db.GetSystemConfig("anomaly_gambit_max_amount"); err == nil {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			c.internal.GambitMaxAmount = f
		}
	}

	if val, err := db.GetSystemConfig("anomaly_gambit_min_amount"); err == nil {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			c.internal.GambitMinAmount = f
		}
	}

	if val, err := db.GetSystemConfig("anomaly_gambit_min_confidence"); err == nil {
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			c.internal.GambitMinConfidence = f
		}
	}

	// LLM 超时配置
	c.internal.LLMTimeoutSeconds = 30
	c.internal.LLMTimeoutAction = "hold"
}

// IsEnabled 是否启用异常监控
func (c *AnomalyConfig) IsEnabled() bool {
	return c.Mode != AnomalyModeOff
}

// ShouldTakeAction 是否应该执行操作（不仅仅是观察）
func (c *AnomalyConfig) ShouldTakeAction() bool {
	return c.Mode != AnomalyModeOff && c.Mode != AnomalyModeWatch
}

// CanReducePosition 是否允许减仓
func (c *AnomalyConfig) CanReducePosition() bool {
	return c.internal.AllowReduce
}

// CanChaseRally 是否允许追涨杀跌
func (c *AnomalyConfig) CanChaseRally() bool {
	return c.internal.AllowChase
}

// CanGambit 是否允许搏一搏
func (c *AnomalyConfig) CanGambit() bool {
	return c.internal.AllowGambit && c.GetGambitEnabled()
}

// GetMode 获取模式（字符串形式，用于接口）
func (c *AnomalyConfig) GetMode() string {
	return string(c.Mode)
}

// GetSensitivity 获取灵敏度（字符串形式，用于接口）
func (c *AnomalyConfig) GetSensitivity() string {
	return string(c.Sensitivity)
}

// GetUseLLM 获取是否使用LLM（用于接口，避免与字段名冲突）
func (c *AnomalyConfig) GetUseLLM() bool {
	return c.UseLLM
}

// GetGambitEnabled 获取是否启用搏一搏（用于接口，避免与字段名冲突）
func (c *AnomalyConfig) GetGambitEnabled() bool {
	return c.GambitEnabled
}

// GetPriceThreshold 获取价格异常阈值系数
func (c *AnomalyConfig) GetPriceThreshold() float64 {
	return c.internal.PriceThresholdK
}

// GetVolumeMultiplier 获取成交量放大倍数
func (c *AnomalyConfig) GetVolumeMultiplier() float64 {
    return c.internal.VolumeMultiplierM
}

// GetAbsMinPriceChangePct 获取绝对最小价格变化阈值（百分比）
func (c *AnomalyConfig) GetAbsMinPriceChangePct() float64 {
    return c.internal.AbsMinPriceChangePct
}

// GetGambitVolumeThreshold 获取搏一搏模式的成交量阈值（与灵敏度关联）
// 计算公式：普通检测阈值 × 搏一搏倍数
// - 低灵敏度：2.5 × 8.0 = 20x（保守，只捕捉极端极端行情）
// - 中灵敏度：2.0 × 7.5 = 15x（平衡，捕捉明显极端行情）
// - 高灵敏度：1.6 × 7.0 = 11.2x（激进，捕捉早期极端信号）
func (c *AnomalyConfig) GetGambitVolumeThreshold() float64 {
    // 统一常量：low=20x, medium=15x, high=12x
    switch c.Sensitivity {
    case SensitivityLow:
        return 20.0
    case SensitivityMedium:
        return 15.0
    case SensitivityHigh:
        return 12.0
    default:
        return 15.0
    }
}

// GetConsecutiveThreshold 获取连续同向阈值
func (c *AnomalyConfig) GetConsecutiveThreshold() float64 {
	return c.internal.ConsecutiveThreshold
}

// GetMinVolumeUSDT 获取最小成交量
func (c *AnomalyConfig) GetMinVolumeUSDT() float64 {
	return c.internal.MinVolumeUSDT
}

// GetCoolingPeriodMinutes 获取冷却期
func (c *AnomalyConfig) GetCoolingPeriodMinutes() int {
	return c.internal.CoolingPeriodMinutes
}

// CalculateLeverage 计算杠杆（基于交易员配置）
func (c *AnomalyConfig) CalculateLeverage(
	traderBTCETHLeverage int,
	traderAltcoinLeverage int,
	tradeType string, // "normal", "chase", "gambit"
	symbol string,
) int {
	// 1. 获取交易员基础杠杆
	baseLeverage := c.getBaseLeverage(traderBTCETHLeverage, traderAltcoinLeverage, symbol)

	// 2. 根据交易类型调整
	var multiplier float64
	switch tradeType {
	case "normal":
		multiplier = 1.0
	case "chase":
		multiplier = c.internal.ChaseLeverageMultiplier
	case "gambit":
		multiplier = c.internal.GambitLeverageMultiplier
	default:
		multiplier = 1.0
	}

	adjustedLeverage := int(math.Round(float64(baseLeverage) * multiplier))

	// 3. 应用系统上限
	finalLeverage := min(adjustedLeverage, c.internal.SystemMaxLeverage)

	log.Printf("杠杆计算 [%s, %s]: 交易员基础=%dx, 倍数=%.1f, 调整后=%dx, 最终=%dx",
		symbol, tradeType, baseLeverage, multiplier, adjustedLeverage, finalLeverage)

	return finalLeverage
}

// CalculateGambitPosition 计算搏一搏实际仓位（双重限制）
func (c *AnomalyConfig) CalculateGambitPosition(accountBalance float64, requestedPct float64) float64 {
	// 方法1：按百分比计算
	positionByPct := accountBalance * requestedPct

	// 方法2：取绝对上限
	positionByMax := c.internal.GambitMaxAmount

	// 候选：不超过上限
	candidate := math.Min(positionByPct, positionByMax)

	// 若低于最小金额，则返回0（放弃执行，避免超越风险比例而强行抬升）
	if candidate < c.internal.GambitMinAmount {
		log.Printf("⚠️  搏一搏候选仓位过小：$%.2f < 最小金额$%.2f，放弃执行", candidate, c.internal.GambitMinAmount)
		return 0
	}

	actualPosition := candidate

	log.Printf("搏一搏仓位计算：账户$%.2f × %.2f%% = $%.2f, 上限$%.2f, 实际$%.2f",
		accountBalance, requestedPct*100, positionByPct, positionByMax, actualPosition)

	return actualPosition
}

// GetGambitConfig 获取搏一搏配置
func (c *AnomalyConfig) GetGambitConfig() map[string]interface{} {
	return map[string]interface{}{
		"enabled":            c.GetGambitEnabled(),
		"max_position_pct":   c.internal.GambitMaxPositionPct,
		"max_amount":         c.internal.GambitMaxAmount,
		"min_amount":         c.internal.GambitMinAmount,
		"min_confidence":     c.internal.GambitMinConfidence,
		"max_stop_loss":      c.internal.GambitMaxStopLoss,
		"cooling_minutes":    c.internal.GambitCoolingMinutes,
		"leverage_multiplier": c.internal.GambitLeverageMultiplier,
	}
}

// ToJSON 转换为 JSON（用于 API 返回）
func (c *AnomalyConfig) ToJSON() (string, error) {
	data := map[string]interface{}{
		"mode":           c.Mode,
		"sensitivity":    c.Sensitivity,
		"use_llm":        c.GetUseLLM(),
		"gambit_enabled": c.GetGambitEnabled(),
		"is_enabled":     c.IsEnabled(),
		"can_take_action": c.ShouldTakeAction(),
	}
	
	bytes, err := json.Marshal(data)
	return string(bytes), err
}

// 辅助函数
func (c *AnomalyConfig) getBaseLeverage(traderBTCETHLeverage, traderAltcoinLeverage int, symbol string) int {
	coinType := getCoinType(symbol)
	if coinType == "BTC" || coinType == "ETH" {
		return traderBTCETHLeverage
	}
	return traderAltcoinLeverage
}

func getCoinType(symbol string) string {
	// 简化版：提取币种类型
	if len(symbol) >= 3 {
		coin := symbol[:len(symbol)-4] // 去掉 USDT
		switch coin {
		case "BTC":
			return "BTC"
		case "ETH":
			return "ETH"
		case "SOL", "BNB", "XRP":
			return "MAJOR_ALT"
		default:
			return "MINOR_ALT"
		}
	}
	return "UNKNOWN"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
