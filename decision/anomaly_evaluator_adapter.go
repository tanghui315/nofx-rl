package decision

import (
	"fmt"
	"nofx/config"
	"nofx/market"
	"nofx/mcp"
)

// AnomalyEvaluatorAdapter LLM评估器适配器
// 实现 market.AnomalyLLMEvaluatorInterface 接口，连接 market 和 decision 包
type AnomalyEvaluatorAdapter struct {
	mcpClient *mcp.Client
}

// NewAnomalyEvaluatorAdapter 创建LLM评估器适配器
func NewAnomalyEvaluatorAdapter(mcpClient *mcp.Client) *AnomalyEvaluatorAdapter {
	return &AnomalyEvaluatorAdapter{
		mcpClient: mcpClient,
	}
}

// Evaluate 实现 market.AnomalyLLMEvaluatorInterface 接口
// 将 map 格式的参数转换为 decision 包的类型，调用 EvaluateAnomalyEvent，然后转换回 map
func (a *AnomalyEvaluatorAdapter) Evaluate(
	event *market.AnomalyEvent,
	ctxMap map[string]interface{},
	anomalyConfig interface{},
	aiModel interface{},
) (map[string]interface{}, error) {
	// 类型断言：转换配置对象
	cfg, ok := anomalyConfig.(*config.AnomalyConfig)
	if !ok {
		return nil, fmt.Errorf("anomalyConfig 类型错误，期望 *config.AnomalyConfig")
	}

	aiModelCfg, ok := aiModel.(*config.AIModelConfig)
	if !ok {
		return nil, fmt.Errorf("aiModel 类型错误，期望 *config.AIModelConfig")
	}

	// 将 ctxMap 转换为 decision.Context
	ctx, err := a.convertContextMapToContext(ctxMap)
	if err != nil {
		return nil, fmt.Errorf("转换上下文失败: %w", err)
	}

	// 调用 decision.EvaluateAnomalyEvent
    decision, prompt, rawResp, err := EvaluateAnomalyEvent(event, ctx, cfg, aiModelCfg, a.mcpClient)
    if err != nil {
        return nil, fmt.Errorf("LLM评估失败: %w", err)
    }

    // 将 decision 转换为 map
    m := a.convertDecisionToMap(decision)
    // 附加调试/可视化字段：输入提示与原始响应，供上层写入日志（前端可展开查看）
    m["input_prompt"] = prompt
    m["cot_trace"] = rawResp
    return m, nil
}

// convertContextMapToContext 将 map 格式的上下文转换为 decision.Context
func (a *AnomalyEvaluatorAdapter) convertContextMapToContext(ctxMap map[string]interface{}) (*Context, error) {
	ctx := &Context{}

	// 提取基本信息
	if currentTime, ok := ctxMap["current_time"].(string); ok {
		ctx.CurrentTime = currentTime
	}
	if runtimeMinutes, ok := ctxMap["runtime_minutes"].(int); ok {
		ctx.RuntimeMinutes = runtimeMinutes
	}
	if callCount, ok := ctxMap["call_count"].(int); ok {
		ctx.CallCount = callCount
	}

	// 提取账户信息
	if accountMap, ok := ctxMap["account"].(map[string]interface{}); ok {
		ctx.Account = AccountInfo{
			TotalEquity:      getFloat64(accountMap, "total_equity"),
			AvailableBalance: getFloat64(accountMap, "available_balance"),
			MarginUsed:       getFloat64(accountMap, "margin_used"),
			MarginUsedPct:    getFloat64(accountMap, "margin_used_pct"),
			PositionCount:    getInt(accountMap, "position_count"),
		}
	}

	// 提取持仓信息
	if positionsList, ok := ctxMap["positions"].([]map[string]interface{}); ok {
		ctx.Positions = make([]PositionInfo, 0, len(positionsList))
		for _, posMap := range positionsList {
			pos := PositionInfo{
				Symbol:           getString(posMap, "symbol"),
				Side:             getString(posMap, "side"),
				EntryPrice:       getFloat64(posMap, "entry_price"),
				MarkPrice:        getFloat64(posMap, "mark_price"),
				Quantity:         getFloat64(posMap, "quantity"),
				Leverage:         getInt(posMap, "leverage"),
				UnrealizedPnL:    getFloat64(posMap, "unrealized_pnl"),
				UnrealizedPnLPct: getFloat64(posMap, "unrealized_pnl_pct"),
			}
			ctx.Positions = append(ctx.Positions, pos)
		}
	}

	// 提取市场数据
	if marketDataMap, ok := ctxMap["market_data_map"].(map[string]*market.Data); ok {
		ctx.MarketDataMap = marketDataMap
	}

	// 提取杠杆配置
	if btcEthLeverage, ok := ctxMap["btc_eth_leverage"].(int); ok {
		ctx.BTCETHLeverage = btcEthLeverage
	}
	if altcoinLeverage, ok := ctxMap["altcoin_leverage"].(int); ok {
		ctx.AltcoinLeverage = altcoinLeverage
	}

	return ctx, nil
}

// convertDecisionToMap 将 AnomalyDecision 转换为 map
func (a *AnomalyEvaluatorAdapter) convertDecisionToMap(decision *AnomalyDecision) map[string]interface{} {
	result := make(map[string]interface{})
	result["should_act"] = decision.ShouldAct
	result["confidence"] = decision.Confidence
	result["action"] = decision.Action
	result["urgency"] = decision.Urgency
	result["reasoning"] = decision.Reasoning
	result["position_size_usd"] = decision.PositionSizeUSD
	result["leverage"] = decision.Leverage
	result["stop_loss"] = decision.StopLoss
	result["take_profit"] = decision.TakeProfit
	result["close_percentage"] = decision.ClosePercentage
	result["risk_factors"] = decision.RiskFactors
	result["key_indicators"] = decision.KeyIndicators
	result["market_context"] = decision.MarketContext
	return result
}

// 辅助函数：从 map 中安全提取值
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getFloat64(m map[string]interface{}, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	if v, ok := m[key].(int); ok {
		return float64(v)
	}
	return 0.0
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key].(int); ok {
		return v
	}
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

// Validate 实现 market.AnomalyLLMEvaluatorInterface 接口
// 验证决策的合理性和安全性（4层验证）
func (a *AnomalyEvaluatorAdapter) Validate(
	decisionMap map[string]interface{},
	event *market.AnomalyEvent,
	ctxMap map[string]interface{},
	anomalyConfig interface{},
) error {
	// 类型断言：转换配置对象
	cfg, ok := anomalyConfig.(*config.AnomalyConfig)
	if !ok {
		return fmt.Errorf("anomalyConfig 类型错误，期望 *config.AnomalyConfig")
	}

	// 将 decisionMap 转换为 AnomalyDecision
	decision := a.convertMapToDecision(decisionMap)
	if decision == nil {
		return fmt.Errorf("决策对象转换失败")
	}

	// 将 ctxMap 转换为 decision.Context
	ctx, err := a.convertContextMapToContext(ctxMap)
	if err != nil {
		return fmt.Errorf("转换上下文失败: %w", err)
	}

	// 调用 decision.ValidateAnomalyDecision
	return ValidateAnomalyDecision(decision, event, ctx, cfg)
}

// convertMapToDecision 将 map 转换为 AnomalyDecision
func (a *AnomalyEvaluatorAdapter) convertMapToDecision(decisionMap map[string]interface{}) *AnomalyDecision {
	decision := &AnomalyDecision{}

	if shouldAct, ok := decisionMap["should_act"].(bool); ok {
		decision.ShouldAct = shouldAct
	}
	if confidence, ok := decisionMap["confidence"].(float64); ok {
		decision.Confidence = confidence
	}
	if action, ok := decisionMap["action"].(string); ok {
		decision.Action = action
	}
	if urgency, ok := decisionMap["urgency"].(string); ok {
		decision.Urgency = urgency
	}
	if reasoning, ok := decisionMap["reasoning"].(string); ok {
		decision.Reasoning = reasoning
	}
	if positionSizeUSD, ok := decisionMap["position_size_usd"].(float64); ok {
		decision.PositionSizeUSD = positionSizeUSD
	}
	if leverage, ok := decisionMap["leverage"].(float64); ok {
		decision.Leverage = int(leverage)
	} else if leverage, ok := decisionMap["leverage"].(int); ok {
		decision.Leverage = leverage
	}
	if stopLoss, ok := decisionMap["stop_loss"].(float64); ok {
		decision.StopLoss = stopLoss
	}
	if takeProfit, ok := decisionMap["take_profit"].(float64); ok {
		decision.TakeProfit = takeProfit
	}
	if closePercentage, ok := decisionMap["close_percentage"].(float64); ok {
		decision.ClosePercentage = closePercentage
	}
	if riskFactors, ok := decisionMap["risk_factors"].([]interface{}); ok {
		decision.RiskFactors = make([]string, 0, len(riskFactors))
		for _, rf := range riskFactors {
			if str, ok := rf.(string); ok {
				decision.RiskFactors = append(decision.RiskFactors, str)
			}
		}
	}
	if keyIndicators, ok := decisionMap["key_indicators"].([]interface{}); ok {
		decision.KeyIndicators = make([]string, 0, len(keyIndicators))
		for _, ki := range keyIndicators {
			if str, ok := ki.(string); ok {
				decision.KeyIndicators = append(decision.KeyIndicators, str)
			}
		}
	}
	if marketContext, ok := decisionMap["market_context"].(string); ok {
		decision.MarketContext = marketContext
	}

	return decision
}
