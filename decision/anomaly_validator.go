package decision

import (
	"fmt"
	"log"
	"math"
	"nofx/config"
	"nofx/market"
	"strings"
)

// ValidateAnomalyDecision 验证异常决策的合理性和安全性（4层验证）
func ValidateAnomalyDecision(
	decision *AnomalyDecision,
	event *market.AnomalyEvent,
	ctx *Context,
	anomalyConfig *config.AnomalyConfig,
) error {
	log.Printf("🔍 开始4层决策验证：%s %s", event.Symbol, decision.Action)
	
	// Layer 1: LLM响应基础验证（已在parseAnomalyDecision中完成）
	// 这里只需确认decision非空
	if decision == nil {
		return fmt.Errorf("决策对象为空")
	}
	
	// Layer 2: 风控策略验证
	if err := validateRiskStrategy(decision, event, ctx, anomalyConfig); err != nil {
		return fmt.Errorf("L2-风控策略验证失败: %v", err)
	}
	
	// Layer 3: 账户安全验证
	if err := validateAccountSafety(decision, ctx, anomalyConfig); err != nil {
		return fmt.Errorf("L3-账户安全验证失败: %v", err)
	}
	
	// Layer 4: 市场状态验证
	if err := validateMarketCondition(decision, event, ctx); err != nil {
		return fmt.Errorf("L4-市场状态验证失败: %v", err)
	}
	
	log.Printf("✅ 决策验证通过：%s %s", event.Symbol, decision.Action)
	return nil
}

// ========================================
// Layer 2: 风控策略验证
// ========================================

func validateRiskStrategy(
	decision *AnomalyDecision,
	event *market.AnomalyEvent,
	ctx *Context,
	anomalyConfig *config.AnomalyConfig,
) error {
	// 2.1 模式权限检查
	if err := validateModePermission(decision, anomalyConfig); err != nil {
		return err
	}
	
	// 2.2 搏一搏约束验证（如果适用）
	if isGambitDecision(decision, event, anomalyConfig) {
		if err := validateGambitConstraints(decision, event, ctx, anomalyConfig); err != nil {
			return err
		}
	}
	
	// 2.3 仓位限制验证
	if decision.ShouldAct && (decision.Action == "chase_long" || decision.Action == "chase_short") {
		if err := validatePositionSize(decision, ctx, anomalyConfig); err != nil {
			return err
		}
	}
	
    // 2.4 杠杆验证
    if decision.ShouldAct && decision.Leverage > 0 {
        if err := validateLeverage(decision, event, anomalyConfig, ctx); err != nil {
            return err
        }
    }
	
	// 2.5 止损验证
	if decision.ShouldAct && (decision.Action == "chase_long" || decision.Action == "chase_short") {
		if err := validateStopLoss(decision, event, anomalyConfig); err != nil {
			return err
		}
	}
	
	return nil
}

// validateModePermission 验证当前模式是否允许该操作
func validateModePermission(decision *AnomalyDecision, config *config.AnomalyConfig) error {
	if !decision.ShouldAct {
		return nil // 不操作，总是允许
	}
	
	switch config.Mode {
	case "off":
		return fmt.Errorf("异常监控已关闭，不允许任何操作")
		
	case "watch":
		return fmt.Errorf("观察模式不允许自动操作")
		
	case "guard":
		// 只允许减仓和平仓
		if !strings.HasPrefix(decision.Action, "reduce_") && decision.Action != "close_all" {
			return fmt.Errorf("防守模式只允许减仓/平仓操作，不允许: %s", decision.Action)
		}
		
	case "balanced", "aggressive":
		// 允许所有操作
		return nil
		
	default:
		return fmt.Errorf("未知的防护模式: %s", config.Mode)
	}
	
	return nil
}

// isGambitDecision 判断是否为搏一搏决策
func isGambitDecision(decision *AnomalyDecision, event *market.AnomalyEvent, config *config.AnomalyConfig) bool {
	if !config.GambitEnabled || !config.CanGambit() {
		return false
	}
	
	// 搏一搏的特征：
	// 1. 高置信度 (≥0.8)
	// 2. 高严重程度 (HIGH)
	// 3. 极端价格变化 (≥5%)
	// 4. 高成交量（根据灵敏度动态调整）
	
	isHighConfidence := decision.Confidence >= 0.8
	isHighSeverity := event.Severity == market.SeverityHigh
	isExtremePriceChange := math.Abs(event.PriceChange3m) >= 5.0
	
	// ✅ 使用动态阈值（与灵敏度关联）
	gambitVolumeThreshold := config.GetGambitVolumeThreshold()
	isHighVolume := event.VolumeRatio >= gambitVolumeThreshold
	
	return isHighConfidence && isHighSeverity && isExtremePriceChange && isHighVolume
}

// validateGambitConstraints 验证搏一搏约束
func validateGambitConstraints(
	decision *AnomalyDecision,
	event *market.AnomalyEvent,
	ctx *Context,
	config *config.AnomalyConfig,
) error {
	log.Printf("⚡ 检测到搏一搏决策，执行严格验证")
	
	gambitConfig := config.GetGambitConfig()
	
	// 1. 严重程度必须HIGH
	if event.Severity != market.SeverityHigh {
		return fmt.Errorf("搏一搏要求严重程度HIGH，当前: %s", event.Severity)
	}
	
	// 2. 价格变化 ≥ 5%
	if math.Abs(event.PriceChange3m) < 5.0 {
		return fmt.Errorf("搏一搏要求价格变化≥5%%，当前: %.2f%%", event.PriceChange3m)
	}
	
	// 3. 成交量倍数（根据灵敏度动态调整）
	gambitVolumeThreshold := config.GetGambitVolumeThreshold()
	if event.VolumeRatio < gambitVolumeThreshold {
		return fmt.Errorf("搏一搏要求成交量≥%.1fx（灵敏度：%s），当前: %.1fx", 
			gambitVolumeThreshold, config.Sensitivity, event.VolumeRatio)
	}
	
	// 4. LLM置信度 ≥ 配置的最小值（默认0.8）
	minConfidence := gambitConfig["min_confidence"].(float64)
	if decision.Confidence < minConfidence {
		return fmt.Errorf("搏一搏要求置信度≥%.0f%%，当前: %.0f%%", 
			minConfidence*100, decision.Confidence*100)
	}
	
	// 5. 仓位限制（在validatePositionSize中检查，这里只记录）
	log.Printf("⚡ 搏一搏仓位限制: %.1f%% 或 $%.0f", 
		gambitConfig["max_position_pct"].(float64)*100,
		gambitConfig["max_amount"].(float64))
	
	// 6. 止损要求
	maxStopLoss := gambitConfig["max_stop_loss"].(float64)
	actualStopLoss := calculateStopLossPercentage(decision.StopLoss, event.CurrentPrice, decision.Action)
	if actualStopLoss > maxStopLoss {
		return fmt.Errorf("搏一搏止损过大: %.2f%% (最大: %.0f%%)", 
			actualStopLoss*100, maxStopLoss*100)
	}
	
	log.Printf("✅ 搏一搏约束验证通过")
	return nil
}

// validatePositionSize 验证仓位大小（实现双重限制）
func validatePositionSize(
	decision *AnomalyDecision,
	ctx *Context,
	config *config.AnomalyConfig,
) error {
	if decision.PositionSizeUSD <= 0 {
		return fmt.Errorf("仓位大小必须>0: $%.2f", decision.PositionSizeUSD)
	}
	
	gambitConfig := config.GetGambitConfig()
	maxAmount := gambitConfig["max_amount"].(float64)
	
	// 如果是搏一搏，使用双重限制：min(账户余额 × 百分比, 绝对金额上限)
	if config.CanGambit() && decision.Confidence >= gambitConfig["min_confidence"].(float64) {
		maxPositionPct := gambitConfig["max_position_pct"].(float64)
		
		// 方法1：按百分比计算
		positionByPct := ctx.Account.TotalEquity * maxPositionPct
		
		// 方法2：绝对上限
		positionByMax := maxAmount
		
		// ✅ 双重限制：取两者较小值
		actualMaxPosition := math.Min(positionByPct, positionByMax)
		
		if decision.PositionSizeUSD > actualMaxPosition {
			return fmt.Errorf("搏一搏仓位超限: $%.2f > $%.2f (账户 $%.2f × %.1f%% = $%.2f, 上限 $%.2f)", 
				decision.PositionSizeUSD, actualMaxPosition,
				ctx.Account.TotalEquity, maxPositionPct*100, positionByPct, maxAmount)
		}
		
		log.Printf("✅ 搏一搏仓位验证：账户$%.2f, 百分比$%.2f (%.1f%%), 上限$%.2f, 实际最大$%.2f, 请求$%.2f",
			ctx.Account.TotalEquity, positionByPct, maxPositionPct*100, maxAmount, actualMaxPosition, decision.PositionSizeUSD)
	} else {
		// 正常追涨杀跌：使用较小的限制（搏一搏的5倍作为正常上限）
		normalMaxAmount := maxAmount * 5.0
		if decision.PositionSizeUSD > normalMaxAmount {
			return fmt.Errorf("仓位过大: $%.2f (建议最大: $%.0f)", 
				decision.PositionSizeUSD, normalMaxAmount)
		}
	}
	
	return nil
}

// validateLeverage 验证杠杆倍数
func validateLeverage(
    decision *AnomalyDecision,
    event *market.AnomalyEvent,
    cfg *config.AnomalyConfig,
    ctx *Context,
) error {
	if decision.Leverage <= 0 {
		return fmt.Errorf("杠杆倍数必须>0: %dx", decision.Leverage)
	}
	
	// 系统绝对上限
    const systemMaxLeverage = 50
    if decision.Leverage > systemMaxLeverage {
        return fmt.Errorf("杠杆超过系统上限: %dx (最大: %dx)", 
            decision.Leverage, systemMaxLeverage)
    }

    // 基于交易员配置的硬上限（与模式倍数绑定）
    base := ctx.AltcoinLeverage
    if isBTCETH(event.Symbol) {
        base = ctx.BTCETHLeverage
    }

    // 模式/动作倍数
    mult := 1.0
    if decision.Action == "chase_long" || decision.Action == "chase_short" {
        mult = 1.2
        if isGambitDecision(decision, event, cfg) {
            mult = 2.0
        }
    }

    allowed := int(float64(base) * mult)
    if allowed > systemMaxLeverage {
        allowed = systemMaxLeverage
    }

    if decision.Leverage > allowed {
        return fmt.Errorf("杠杆超过允许上限: %dx (币种上限%dx × 倍数%.1f → %dx)", decision.Leverage, base, mult, allowed)
    }
    
    return nil
}

// validateStopLoss 验证止损设置
func validateStopLoss(
	decision *AnomalyDecision,
	event *market.AnomalyEvent,
	config *config.AnomalyConfig,
) error {
	if decision.StopLoss <= 0 {
		return fmt.Errorf("止损价格必须设置且>0: %.2f", decision.StopLoss)
	}
	
	// 计算止损百分比
	stopLossPct := calculateStopLossPercentage(decision.StopLoss, event.CurrentPrice, decision.Action)
	
	// 止损不能太小（<0.5%）
	if stopLossPct < 0.005 {
		return fmt.Errorf("止损过小: %.2f%% (最小建议: 0.5%%)", stopLossPct*100)
	}
	
	// 根据模式检查止损上限
	gambitConfig := config.GetGambitConfig()
	
	if config.CanGambit() && decision.Confidence >= gambitConfig["min_confidence"].(float64) {
		// 搏一搏模式：止损≤3%
		maxStopLoss := gambitConfig["max_stop_loss"].(float64)
		if stopLossPct > maxStopLoss {
			return fmt.Errorf("搏一搏止损过大: %.2f%% (最大: %.0f%%)", 
				stopLossPct*100, maxStopLoss*100)
		}
	} else {
		// 正常模式：止损建议≤10%
		if stopLossPct > 0.10 {
			log.Printf("⚠️  止损较大: %.2f%% (建议≤10%%)", stopLossPct*100)
		}
	}
	
	return nil
}

// ========================================
// Layer 3: 账户安全验证
// ========================================

func validateAccountSafety(
	decision *AnomalyDecision,
	ctx *Context,
	config *config.AnomalyConfig,
) error {
	if !decision.ShouldAct {
		return nil // 不操作，跳过
	}
	
	// 3.1 保证金使用率检查
	if ctx.Account.MarginUsedPct >= 80.0 {
		return fmt.Errorf("保证金使用率过高: %.1f%% (≥80%%)", ctx.Account.MarginUsedPct)
	}
	
	// 3.2 可用余额检查
	if decision.Action == "chase_long" || decision.Action == "chase_short" {
		requiredMargin := decision.PositionSizeUSD / float64(decision.Leverage)
		if requiredMargin > ctx.Account.AvailableBalance {
			return fmt.Errorf("可用余额不足: 需要$%.2f, 可用$%.2f", 
				requiredMargin, ctx.Account.AvailableBalance)
		}
		
		// 开仓后保证金使用率不能超过85%
		newMarginUsed := ctx.Account.MarginUsed + requiredMargin
		newMarginUsedPct := (newMarginUsed / ctx.Account.TotalEquity) * 100
		if newMarginUsedPct > 85.0 {
			return fmt.Errorf("开仓后保证金使用率过高: %.1f%% (>85%%)", newMarginUsedPct)
		}
	}
	
	// 3.3 持仓数量限制（建议≤8个）
	const maxPositionCount = 8
	if decision.Action == "chase_long" || decision.Action == "chase_short" {
		if ctx.Account.PositionCount >= maxPositionCount {
			return fmt.Errorf("持仓数量已达上限: %d (≥%d)", 
				ctx.Account.PositionCount, maxPositionCount)
		}
	}
	
	// 3.4 单币种风险暴露检查
	if decision.Action == "chase_long" || decision.Action == "chase_short" {
		symbolExposure := decision.PositionSizeUSD / ctx.Account.TotalEquity
		if symbolExposure > 0.30 {
			return fmt.Errorf("单币种风险暴露过高: %.1f%% (>30%%)", symbolExposure*100)
		}
	}
	
	return nil
}

// ========================================
// Layer 4: 市场状态验证
// ========================================

func validateMarketCondition(
	decision *AnomalyDecision,
	event *market.AnomalyEvent,
	ctx *Context,
) error {
	if !decision.ShouldAct {
		return nil // 不操作，跳过
	}
	
	// 4.1 价格偏离ATR检查（如果>5x ATR，给出警告）
	if event.ATRRatio > 5.0 {
		log.Printf("⚠️  价格变化极端: %.1fx ATR (>5x)", event.ATRRatio)
	}
	
	// 4.2 成交量检查（通过异常事件中的成交量比率推断）
	// 如果成交量倍数很高，说明成交量足够
	// 如果倍数很低（<0.3），可能成交量不足
	if event.VolumeRatio < 0.3 {
		log.Printf("⚠️  成交量较低: %.2fx 平均值", event.VolumeRatio)
	}
	
	// 4.3 冷却期检查（防止短时间内重复操作同一币种）
	// 注：冷却期检查应该在更高层级（monitor）完成，这里只记录
	log.Printf("💡 建议实施冷却期：同一币种30-60分钟内避免重复异常操作")
	
	return nil
}

// ========================================
// Helper Functions
// ========================================

// calculateStopLossPercentage 计算止损百分比
func calculateStopLossPercentage(stopLoss, currentPrice float64, action string) float64 {
	if action == "chase_long" {
		// 做多：止损在下方
		return (currentPrice - stopLoss) / currentPrice
	} else if action == "chase_short" {
		// 做空：止损在上方
		return (stopLoss - currentPrice) / currentPrice
	}
	return 0
}

// isBTCETH 判断是否为BTC或ETH
func isBTCETH(symbol string) bool {
	return symbol == "BTCUSDT" || symbol == "ETHUSDT"
}
