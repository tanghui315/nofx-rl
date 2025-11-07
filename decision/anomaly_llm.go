package decision

import (
	"encoding/json"
	"fmt"
	"log"
	"nofx/config"
	"nofx/market"
	"nofx/mcp"
	"regexp"
	"strings"
	"time"
)

// AnomalyDecision 异常事件LLM决策响应
type AnomalyDecision struct {
	// 评估结果
	ShouldAct     bool    `json:"should_act"`      // 是否应该采取行动
	Confidence    float64 `json:"confidence"`      // 置信度 0.0-1.0
	Action        string  `json:"action"`          // 具体行动："chase_long", "chase_short", "reduce_long", "reduce_short", "close_all", "wait"
	Urgency       string  `json:"urgency"`         // 紧急程度："low", "medium", "high", "critical"
	
	// 仓位建议（开仓或追涨）
	PositionSizeUSD float64 `json:"position_size_usd,omitempty"` // 建议仓位大小（USD）
	Leverage        int     `json:"leverage,omitempty"`          // 建议杠杆倍数
	StopLoss        float64 `json:"stop_loss,omitempty"`         // 止损价格
	TakeProfit      float64 `json:"take_profit,omitempty"`       // 止盈价格
	
	// 减仓建议（杀跌）
	ClosePercentage float64 `json:"close_percentage,omitempty"` // 平仓百分比（0-100）
	
	// 推理过程
	Reasoning     string   `json:"reasoning"`              // 推理过程
	RiskFactors   []string `json:"risk_factors"`           // 识别的风险因素
	KeyIndicators []string `json:"key_indicators"`         // 关键指标
	MarketContext string   `json:"market_context"`         // 市场背景分析
}

// EvaluateAnomalyEvent 评估异常事件并给出LLM决策
func EvaluateAnomalyEvent(
    event *market.AnomalyEvent,
    ctx *Context,
    anomalyConfig *config.AnomalyConfig,
    aiModelConfig *config.AIModelConfig,
    mcpClient *mcp.Client,
) (*AnomalyDecision, string, string, error) {
    log.Printf("🤖 开始LLM评估异常事件：%s %s (严重程度: %s)", event.Symbol, event.Type, event.Severity)
    
    // 构建 LLM Prompt
    prompt := buildAnomalyPrompt(event, ctx, anomalyConfig)
    
    // 调用 LLM
    startTime := time.Now()
    response, err := callLLM(aiModelConfig, mcpClient, prompt)
    if err != nil {
        log.Printf("❌ LLM调用失败: %v", err)
        return nil, prompt, "", fmt.Errorf("LLM调用失败: %v", err)
    }
    elapsed := time.Since(startTime)
    
    log.Printf("✅ LLM响应完成 (耗时: %.2fs)", elapsed.Seconds())
    
    // 解析 LLM 响应
    decision, err := parseAnomalyDecision(response)
    if err != nil {
        log.Printf("❌ 解析LLM响应失败: %v\n响应内容:\n%s", err, response)
        return nil, prompt, response, fmt.Errorf("解析LLM响应失败: %v", err)
    }
    
    log.Printf("🎯 LLM决策：行动=%v, 操作=%s, 置信度=%.0f%%, 紧急度=%s", 
        decision.ShouldAct, decision.Action, decision.Confidence*100, decision.Urgency)
    
    return decision, prompt, response, nil
}

// buildAnomalyPrompt 构建异常事件评估的 Prompt
func buildAnomalyPrompt(event *market.AnomalyEvent, ctx *Context, anomalyConfig *config.AnomalyConfig) string {
    var sb strings.Builder
	
	// System Prompt
	sb.WriteString("# 角色定位\n")
	sb.WriteString("你是一个专业的加密货币交易风控分析师，专门负责评估异常市场事件并提供快速决策建议。\n\n")
	
	sb.WriteString("# 核心任务\n")
	sb.WriteString("分析当前异常事件，判断是否应该采取行动（追涨杀跌），并给出具体的操作建议。\n\n")
	
	// 异常事件详情
	sb.WriteString("# 异常事件详情\n")
	sb.WriteString(fmt.Sprintf("- **币种**: %s\n", event.Symbol))
	sb.WriteString(fmt.Sprintf("- **类型**: %s\n", translateAnomalyType(string(event.Type))))
	sb.WriteString(fmt.Sprintf("- **严重程度**: %s\n", translateSeverity(string(event.Severity))))
	sb.WriteString(fmt.Sprintf("- **方向**: %s\n", event.Direction))
	sb.WriteString(fmt.Sprintf("- **触发时间**: %s\n", event.Timestamp.Format("2006-01-02 15:04:05")))
	sb.WriteString("\n")
	
    // 市场数据
    sb.WriteString("# 市场数据\n")
    sb.WriteString(fmt.Sprintf("- **当前价格**: $%.2f\n", event.CurrentPrice))
    sb.WriteString(fmt.Sprintf("- **3分钟价格变化**: %.2f%%\n", event.PriceChange3m))
    sb.WriteString(fmt.Sprintf("- **15分钟价格变化**: %.2f%%\n", event.PriceChange15m))
    sb.WriteString(fmt.Sprintf("- **成交量倍数**: %.1fx（相对均值）\n", event.VolumeRatio))
    sb.WriteString(fmt.Sprintf("- **ATR倍数**: %.1fx（波动率）\n", event.ATRRatio))
    sb.WriteString("\n")

    // 触发币技术指标摘要（从 market_data_map 中获取更丰富的上下文）
    if ctx.MarketDataMap != nil {
        if md, ok := ctx.MarketDataMap[event.Symbol]; ok && md != nil {
            sb.WriteString("# 技术指标与上下文（摘要）\n")
            // 短中期价格与动量
            sb.WriteString(fmt.Sprintf("- 1h/4h 涨跌幅: %.2f%% / %.2f%%\n", md.PriceChange1h, md.PriceChange4h))
            sb.WriteString(fmt.Sprintf("- RSI(7): %.1f | MACD: %.3f | EMA20: %.3f\n", md.CurrentRSI7, md.CurrentMACD, md.CurrentEMA20))

            // 多周期均线
            if md.MultipleEMAs != nil {
                sb.WriteString(fmt.Sprintf("- EMA(10/20/50/200): %.2f / %.2f / %.2f / %.2f\n",
                    md.MultipleEMAs.EMA10, md.MultipleEMAs.EMA20, md.MultipleEMAs.EMA50, md.MultipleEMAs.EMA200))
            }

            // 布林带
            if md.BollingerBands != nil {
                sb.WriteString(fmt.Sprintf("- 布林带: 带宽 %.2f%% | %%B %.2f\n", md.BollingerBands.BandWidth, md.BollingerBands.PercentB))
            }

            // 趋势强度
            if md.ADX != nil {
                sb.WriteString(fmt.Sprintf("- ADX: %.1f | +DI: %.1f | -DI: %.1f\n", md.ADX.ADX, md.ADX.PlusDI, md.ADX.MinusDI))
            }

            // VWAP 偏离
            if md.VWAP > 0 {
                vwapDiff := 0.0
                if md.VWAP != 0 {
                    vwapDiff = (md.CurrentPrice - md.VWAP) / md.VWAP * 100
                }
                sb.WriteString(fmt.Sprintf("- VWAP: %.2f (%+.2f%% 相对价格)\n", md.VWAP, vwapDiff))
            }

            // OI / Funding
            if md.OpenInterest != nil {
                sb.WriteString(fmt.Sprintf("- OI: 最新 %.2f, 均值 %.2f\n", md.OpenInterest.Latest, md.OpenInterest.Average))
            }
            sb.WriteString(fmt.Sprintf("- Funding: %.2e\n", md.FundingRate))

            // 4h 上下文
            if md.LongerTermContext != nil {
                sb.WriteString(fmt.Sprintf("- 4h ATR14: %.3f | 平均量: %.0f\n", md.LongerTermContext.ATR14, md.LongerTermContext.AverageVolume))
            }

            // 语义化总结
            if md.Semantics != nil {
                signals := strings.Join(md.Semantics.KeySignals, ", ")
                sb.WriteString(fmt.Sprintf("- 语义: 趋势 %s/%s, 动量 %s, 波动 %s\n",
                    md.Semantics.TrendDirection, md.Semantics.TrendStrength, md.Semantics.MomentumStatus, md.Semantics.VolatilityLevel))
                if len(signals) > 0 {
                    sb.WriteString(fmt.Sprintf("- 关键信号: %s\n", signals))
                }
            }
            sb.WriteString("\n")
        }
    }
	
	// 当前持仓情况
	sb.WriteString("# 当前持仓情况\n")
	hasPosition := false
	var relevantPosition *PositionInfo
	for i := range ctx.Positions {
		pos := &ctx.Positions[i]
		if pos.Symbol == event.Symbol {
			hasPosition = true
			relevantPosition = pos
			sb.WriteString(fmt.Sprintf("- **持仓方向**: %s\n", pos.Side))
			sb.WriteString(fmt.Sprintf("- **持仓量**: %.4f\n", pos.Quantity))
			sb.WriteString(fmt.Sprintf("- **杠杆**: %dx\n", pos.Leverage))
			sb.WriteString(fmt.Sprintf("- **入场价格**: $%.2f\n", pos.EntryPrice))
			sb.WriteString(fmt.Sprintf("- **当前价格**: $%.2f\n", pos.MarkPrice))
			sb.WriteString(fmt.Sprintf("- **未实现盈亏**: $%.2f (%.2f%%)\n", pos.UnrealizedPnL, pos.UnrealizedPnLPct))
			sb.WriteString(fmt.Sprintf("- **强平价格**: $%.2f\n", pos.LiquidationPrice))
			break
		}
	}
	if !hasPosition {
		sb.WriteString("- **当前无持仓**\n")
	}
	sb.WriteString("\n")
	
	// 账户状态
	sb.WriteString("# 账户状态\n")
	sb.WriteString(fmt.Sprintf("- **总权益**: $%.2f\n", ctx.Account.TotalEquity))
	sb.WriteString(fmt.Sprintf("- **可用余额**: $%.2f\n", ctx.Account.AvailableBalance))
	sb.WriteString(fmt.Sprintf("- **保证金使用率**: %.1f%%\n", ctx.Account.MarginUsedPct))
	sb.WriteString(fmt.Sprintf("- **持仓数量**: %d\n", ctx.Account.PositionCount))
	sb.WriteString("\n")
	
	// 风控配置
	sb.WriteString("# 风控配置\n")
	sb.WriteString(fmt.Sprintf("- **防护模式**: %s\n", translateMode(string(anomalyConfig.Mode))))
	sb.WriteString(fmt.Sprintf("- **灵敏度**: %s\n", translateSensitivity(string(anomalyConfig.Sensitivity))))
	sb.WriteString(fmt.Sprintf("- **搏一搏模式**: %v\n", anomalyConfig.GambitEnabled))
	sb.WriteString("\n")
	
	// 决策指引
	sb.WriteString("# 决策指引\n\n")
	
	// 根据模式给出不同的指引
	switch anomalyConfig.Mode {
	case config.AnomalyModeWatch:
		sb.WriteString("**当前模式：观察模式**\n")
		sb.WriteString("- 只需评估事件严重性，给出分析\n")
		sb.WriteString("- should_act 应该设为 false\n")
		sb.WriteString("- 专注于风险提示和市场分析\n")
		
	case config.AnomalyModeGuard:
		sb.WriteString("**当前模式：防守模式**\n")
		sb.WriteString("- 只处理下跌/暴跌事件（减仓/止损）\n")
		sb.WriteString("- 不追涨，不开新仓\n")
		sb.WriteString("- 如果有持仓且方向不利，建议减仓或平仓\n")
		if hasPosition && relevantPosition != nil {
			if (event.Direction == "DOWN" && relevantPosition.Side == "long") ||
				(event.Direction == "UP" && relevantPosition.Side == "short") {
				sb.WriteString("- **警告**：当前持仓方向与异常方向相反，建议考虑减仓\n")
			}
		}
		
	case config.AnomalyModeBalanced:
		sb.WriteString("**当前模式：平衡模式**\n")
		sb.WriteString("- 可以追涨或杀跌，但需要谨慎\n")
		sb.WriteString("- 仓位控制：建议使用可用余额的5-10%\n")
		sb.WriteString("- 杠杆控制：BTC/ETH建议5-10x，山寨币建议3-5x\n")
		sb.WriteString("- 必须设置严格的止损（3-5%）\n")
		
	case config.AnomalyModeAggressive:
		sb.WriteString("**当前模式：激进模式**\n")
		sb.WriteString("- 可以大胆追涨杀跌\n")
		sb.WriteString("- 仓位控制：可使用可用余额的10-20%\n")
		sb.WriteString("- 杠杆控制：BTC/ETH可10-20x，山寨币可5-10x\n")
		sb.WriteString("- 止损设置：5-10%\n")
	}
	sb.WriteString("\n")
	
	// 搏一搏模式指引
	if anomalyConfig.GambitEnabled && (anomalyConfig.Mode == config.AnomalyModeBalanced || anomalyConfig.Mode == config.AnomalyModeAggressive) {
		sb.WriteString("## 搏一搏模式（高风险）\n")
		sb.WriteString("**触发条件**：\n")
		sb.WriteString("- 严重程度必须是 HIGH\n")
		sb.WriteString("- 价格变化 ≥ 5%（极端行情）\n")
		sb.WriteString("- 成交量倍数 ≥ 3x（确认流动性）\n")
		sb.WriteString("- LLM置信度 ≥ 80%（高度确信）\n\n")
		sb.WriteString("**硬性约束**：\n")
		gambitConfig := anomalyConfig.GetGambitConfig()
		maxAmount := gambitConfig["max_amount"].(float64)
		maxPositionPct := gambitConfig["max_position_pct"].(float64)
		maxStopLoss := gambitConfig["max_stop_loss"].(float64)
		
		sb.WriteString(fmt.Sprintf("- 仓位上限：min(账户净值×%.1f%%, $%.0f) = $%.2f\n", 
			maxPositionPct*100, maxAmount, 
			min64(ctx.Account.TotalEquity*maxPositionPct, maxAmount)))
		sb.WriteString(fmt.Sprintf("- 杠杆上限：%dx（系统绝对上限50x）\n", calculateGambitMaxLeverage(ctx, event.Symbol)))
		sb.WriteString(fmt.Sprintf("- 止损要求：≤%.0f%%（强制）\n", maxStopLoss*100))
		sb.WriteString("\n**风险警告**：搏一搏模式高风险，判断错误会快速亏损！\n\n")
	}
	
	// 输出格式要求
	sb.WriteString("# 输出格式\n")
	sb.WriteString("请以JSON格式输出，包含以下字段：\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n")
	sb.WriteString("  \"should_act\": true/false,\n")
	sb.WriteString("  \"confidence\": 0.85,  // 0.0-1.0\n")
	sb.WriteString("  \"action\": \"chase_long\",  // chase_long, chase_short, reduce_long, reduce_short, close_all, wait\n")
	sb.WriteString("  \"urgency\": \"high\",  // low, medium, high, critical\n")
	sb.WriteString("  \"position_size_usd\": 500.0,  // 仅chase_long/chase_short时需要\n")
	sb.WriteString("  \"leverage\": 10,  // 仅chase_long/chase_short时需要\n")
	sb.WriteString("  \"stop_loss\": 42000.0,  // 仅chase_long/chase_short时需要\n")
	sb.WriteString("  \"take_profit\": 48000.0,  // 仅chase_long/chase_short时需要\n")
	sb.WriteString("  \"close_percentage\": 50.0,  // 仅reduce_*/close_all时需要，0-100\n")
	sb.WriteString("  \"reasoning\": \"市场出现极端暴涨...\",\n")
	sb.WriteString("  \"risk_factors\": [\"成交量激增\", \"波动率过高\"],\n")
	sb.WriteString("  \"key_indicators\": [\"3分钟涨幅7%\", \"成交量5倍\"],\n")
	sb.WriteString("  \"market_context\": \"当前市场处于高度FOMO状态...\"\n")
	sb.WriteString("}\n")
	sb.WriteString("```\n\n")
	
	sb.WriteString("# 重要提示\n")
	sb.WriteString("1. 确保JSON格式严格正确\n")
	sb.WriteString("2. confidence必须在0.0-1.0之间\n")
	sb.WriteString("3. 如果should_act=true，必须提供完整的仓位参数\n")
	sb.WriteString("4. reasoning必须包含清晰的逻辑推理\n")
	sb.WriteString("5. 止损必须严格设置，不能过于宽松\n")
	sb.WriteString("6. 如果不确定，宁可不操作（should_act=false）\n")
	
	return sb.String()
}

// parseAnomalyDecision 解析 LLM 返回的异常决策
func parseAnomalyDecision(response string) (*AnomalyDecision, error) {
	// 提取 JSON 代码块
	jsonStr := extractJSONFromResponse(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("无法从响应中提取JSON")
	}
	
	var decision AnomalyDecision
	if err := json.Unmarshal([]byte(jsonStr), &decision); err != nil {
		return nil, fmt.Errorf("JSON解析失败: %v", err)
	}
	
	// 验证必填字段
	if decision.Confidence < 0 || decision.Confidence > 1 {
		return nil, fmt.Errorf("confidence必须在0-1之间，当前值: %.2f", decision.Confidence)
	}
	
	if decision.ShouldAct {
		if decision.Action == "" {
			return nil, fmt.Errorf("should_act=true时，action不能为空")
		}
		
		// 验证追涨杀跌的参数
		if decision.Action == "chase_long" || decision.Action == "chase_short" {
			if decision.PositionSizeUSD <= 0 {
				return nil, fmt.Errorf("追涨杀跌时，position_size_usd必须>0")
			}
			if decision.Leverage <= 0 {
				return nil, fmt.Errorf("追涨杀跌时，leverage必须>0")
			}
			if decision.StopLoss <= 0 {
				return nil, fmt.Errorf("追涨杀跌时，stop_loss必须>0")
			}
		}
		
		// 验证减仓的参数
		if strings.HasPrefix(decision.Action, "reduce_") || decision.Action == "close_all" {
			if decision.ClosePercentage <= 0 || decision.ClosePercentage > 100 {
				return nil, fmt.Errorf("减仓时，close_percentage必须在0-100之间")
			}
		}
	}
	
	return &decision, nil
}

// extractJSONFromResponse 从 LLM 响应中提取 JSON
func extractJSONFromResponse(response string) string {
	// 尝试提取 ```json ... ``` 代码块
	reJSON := regexp.MustCompile(`(?is)` + "```json\\s*([\\s\\S]*?)\\s*```")
	matches := reJSON.FindStringSubmatch(response)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	
	// 尝试提取 { ... } JSON对象
	reObject := regexp.MustCompile(`(?is)\{[\s\S]*\}`)
	matches = reObject.FindStringSubmatch(response)
	if len(matches) > 0 {
		return strings.TrimSpace(matches[0])
	}
	
	return ""
}

// callLLM 调用 LLM API
func callLLM(aiModelConfig *config.AIModelConfig, mcpClient *mcp.Client, prompt string) (string, error) {
    if mcpClient == nil {
        return "", fmt.Errorf("MCP 客户端未初始化")
    }
    // 最小 system 提示，主要信息放在 user 提示中。
    system := "You are a professional crypto risk-control analyst. Return STRICT JSON as instructed."
    return mcpClient.CallWithMessages(system, prompt)
}

// Helper functions

func translateAnomalyType(t string) string {
	translations := map[string]string{
		"PRICE_SPIKE":      "价格暴涨",
		"PRICE_CRASH":      "价格暴跌",
		"VOLUME_SPIKE":     "成交量突增",
		"VOLATILITY_SPIKE": "波动率突增",
		"CONSECUTIVE_UP":   "连续上涨",
		"CONSECUTIVE_DOWN": "连续下跌",
	}
	if trans, ok := translations[t]; ok {
		return trans
	}
	return t
}

func translateSeverity(s string) string {
	translations := map[string]string{
		"LOW":    "低",
		"MEDIUM": "中",
		"HIGH":   "高",
	}
	if trans, ok := translations[s]; ok {
		return trans
	}
	return s
}

func translateMode(m string) string {
	translations := map[string]string{
		"off":        "关闭",
		"watch":      "观察模式（只提醒）",
		"guard":      "防守模式（只止损）",
		"balanced":   "平衡模式（可追涨杀跌）",
		"aggressive": "激进模式（大胆操作）",
	}
	if trans, ok := translations[m]; ok {
		return trans
	}
	return m
}

func translateSensitivity(s string) string {
	translations := map[string]string{
		"low":    "低（5%+）",
		"medium": "中（3%+）",
		"high":   "高（1.5%+）",
	}
	if trans, ok := translations[s]; ok {
		return trans
	}
	return s
}

func min64(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func calculateGambitMaxLeverage(ctx *Context, symbol string) int {
	// 判断币种类型
	if symbol == "BTCUSDT" || symbol == "ETHUSDT" {
		return minInt(ctx.BTCETHLeverage*2, 50)
	}
	return minInt(ctx.AltcoinLeverage*2, 50)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
