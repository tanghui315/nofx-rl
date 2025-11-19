package logger

import (
    "encoding/json"
    "fmt"
    "io/ioutil"
    "math"
    "os"
    "path/filepath"
    "strconv"
    "time"
)

// DecisionRecord 决策记录
type DecisionRecord struct {
    // Source 用于标记来源（如 "anomaly"），便于前端做区分
    Source         string             `json:"source,omitempty"`
    Timestamp      time.Time          `json:"timestamp"`       // 决策时间
    CycleNumber    int                `json:"cycle_number"`    // 周期编号
    SystemPrompt   string             `json:"system_prompt"`   // 系统提示词（发送给AI的系统prompt）
	InputPrompt    string             `json:"input_prompt"`    // 发送给AI的输入prompt
	CoTTrace       string             `json:"cot_trace"`       // AI思维链（输出）
	DecisionJSON   string             `json:"decision_json"`   // 决策JSON
	AccountState   AccountSnapshot    `json:"account_state"`   // 账户状态快照
	Positions      []PositionSnapshot `json:"positions"`       // 持仓快照
	CandidateCoins []string           `json:"candidate_coins"` // 候选币种列表
	Decisions      []DecisionAction   `json:"decisions"`       // 执行的决策
	ExecutionLog   []string           `json:"execution_log"`   // 执行日志
	Success        bool               `json:"success"`         // 是否成功
	ErrorMessage   string             `json:"error_message"`   // 错误信息（如果有）
}

// SetSource 兼容性辅助：允许外部通过接口方式设置来源
func (r *DecisionRecord) SetSource(s string) { r.Source = s }

// AccountSnapshot 账户状态快照
type AccountSnapshot struct {
	TotalBalance          float64 `json:"total_balance"`
	AvailableBalance      float64 `json:"available_balance"`
	TotalUnrealizedProfit float64 `json:"total_unrealized_profit"`
	PositionCount         int     `json:"position_count"`
	MarginUsedPct         float64 `json:"margin_used_pct"`
}

// PositionSnapshot 持仓快照
type PositionSnapshot struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"`
	PositionAmt      float64 `json:"position_amt"`
	EntryPrice       float64 `json:"entry_price"`
	MarkPrice        float64 `json:"mark_price"`
	UnrealizedProfit float64 `json:"unrealized_profit"`
	Leverage         float64 `json:"leverage"`
	LiquidationPrice float64 `json:"liquidation_price"`
}

// DecisionAction 决策动作
type DecisionAction struct {
	Action       string    `json:"action"`                  // open_long, open_short, close_long, close_short, update_stop_loss, update_take_profit, partial_close
	Symbol       string    `json:"symbol"`                  // 币种
	StrategyCode string    `json:"strategy_code,omitempty"` // 策略代码
	Quantity     float64   `json:"quantity"`                // 数量（部分平仓时使用）
	Leverage     int       `json:"leverage"`                // 杠杆（开仓时）
	Price        float64   `json:"price"`                   // 执行价格
	OrderID      int64     `json:"order_id"`                // 订单ID
	Timestamp    time.Time `json:"timestamp"`               // 执行时间
	Success      bool      `json:"success"`                 // 是否成功
	Error        string    `json:"error"`                   // 错误信息
}

// DecisionLogger 决策日志记录器
type DecisionLogger struct {
	logDir      string
	cycleNumber int
}

// NewDecisionLogger 创建决策日志记录器
func NewDecisionLogger(logDir string) *DecisionLogger {
	if logDir == "" {
		logDir = "decision_logs"
	}

	// 确保日志目录存在
	if err := os.MkdirAll(logDir, 0755); err != nil {
		fmt.Printf("⚠ 创建日志目录失败: %v\n", err)
	}

	return &DecisionLogger{
		logDir:      logDir,
		cycleNumber: 0,
	}
}

// LogDecision 记录决策
func (l *DecisionLogger) LogDecision(record *DecisionRecord) error {
	l.cycleNumber++
	record.CycleNumber = l.cycleNumber
	record.Timestamp = time.Now()

	// 生成文件名：decision_YYYYMMDD_HHMMSS_cycleN.json
	filename := fmt.Sprintf("decision_%s_cycle%d.json",
		record.Timestamp.Format("20060102_150405"),
		record.CycleNumber)

	logPath := filepath.Join(l.logDir, filename)

	// 序列化为JSON（带缩进，方便阅读）
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化决策记录失败: %w", err)
	}

	// 写入文件
	if err := ioutil.WriteFile(logPath, data, 0644); err != nil {
		return fmt.Errorf("写入决策记录失败: %w", err)
	}

	// 若来源为异常监控，则额外写入汇总日志（便于运维排查）
	if record.Source == "anomaly" {
		// 推测 trader ID = 日志目录的最后一段（决策日志通常位于 decision_logs/<traderID>）
		traderID := filepath.Base(l.logDir)
		symbol := ""
		action := ""
		price := 0.0
		success := record.Success
		if len(record.Decisions) > 0 {
			dec := record.Decisions[0]
			symbol = dec.Symbol
			action = dec.Action
			price = dec.Price
			// 若单条动作失败，则认为本次异常处理失败
			if !dec.Success {
				success = false
			}
		}

		line := fmt.Sprintf(
			"%s | trader=%s | symbol=%s | action=%s | price=%.8f | success=%v | error=%s\n",
			record.Timestamp.Format("2006-01-02 15:04:05"),
			traderID,
			symbol,
			action,
			price,
			success,
			record.ErrorMessage,
		)

		// anomaly.log 放在决策日志目录的上一级（例如 decision_logs/anomaly.log）
		anomalyPath := filepath.Join(filepath.Dir(l.logDir), "anomaly.log")
		if f, err := os.OpenFile(anomalyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
			_, _ = f.WriteString(line)
			_ = f.Close()
		} else {
			fmt.Printf("⚠ 写入异常日志失败: %v\n", err)
		}
	}

	fmt.Printf("📝 决策记录已保存: %s\n", filename)
	return nil
}

// GetLatestRecords 获取最近N条记录（按时间正序：从旧到新）
func (l *DecisionLogger) GetLatestRecords(n int) ([]*DecisionRecord, error) {
	files, err := ioutil.ReadDir(l.logDir)
	if err != nil {
		return nil, fmt.Errorf("读取日志目录失败: %w", err)
	}

	// 先按修改时间倒序收集（最新的在前）
	var records []*DecisionRecord
	count := 0
	for i := len(files) - 1; i >= 0 && count < n; i-- {
		file := files[i]
		if file.IsDir() {
			continue
		}

		filepath := filepath.Join(l.logDir, file.Name())
		data, err := ioutil.ReadFile(filepath)
		if err != nil {
			continue
		}

		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}

		records = append(records, &record)
		count++
	}

	// 反转数组，让时间从旧到新排列（用于图表显示）
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}

	return records, nil
}

// GetRecordByDate 获取指定日期的所有记录
func (l *DecisionLogger) GetRecordByDate(date time.Time) ([]*DecisionRecord, error) {
	dateStr := date.Format("20060102")
	pattern := filepath.Join(l.logDir, fmt.Sprintf("decision_%s_*.json", dateStr))

	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("查找日志文件失败: %w", err)
	}

	var records []*DecisionRecord
	for _, filepath := range files {
		data, err := ioutil.ReadFile(filepath)
		if err != nil {
			continue
		}

		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}

		records = append(records, &record)
	}

	return records, nil
}

// CleanOldRecords 清理N天前的旧记录
func (l *DecisionLogger) CleanOldRecords(days int) error {
	cutoffTime := time.Now().AddDate(0, 0, -days)

	files, err := ioutil.ReadDir(l.logDir)
	if err != nil {
		return fmt.Errorf("读取日志目录失败: %w", err)
	}

	removedCount := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if file.ModTime().Before(cutoffTime) {
			filepath := filepath.Join(l.logDir, file.Name())
			if err := os.Remove(filepath); err != nil {
				fmt.Printf("⚠ 删除旧记录失败 %s: %v\n", file.Name(), err)
				continue
			}
			removedCount++
		}
	}

	if removedCount > 0 {
		fmt.Printf("🗑️ 已清理 %d 条旧记录（%d天前）\n", removedCount, days)
	}

	return nil
}

// GetStatistics 获取统计信息
func (l *DecisionLogger) GetStatistics() (*Statistics, error) {
	files, err := ioutil.ReadDir(l.logDir)
	if err != nil {
		return nil, fmt.Errorf("读取日志目录失败: %w", err)
	}

	stats := &Statistics{}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filepath := filepath.Join(l.logDir, file.Name())
		data, err := ioutil.ReadFile(filepath)
		if err != nil {
			continue
		}

		var record DecisionRecord
		if err := json.Unmarshal(data, &record); err != nil {
			continue
		}

		stats.TotalCycles++

		for _, action := range record.Decisions {
			if action.Success {
				switch action.Action {
				case "open_long", "open_short":
					stats.TotalOpenPositions++
				case "close_long", "close_short", "auto_close_long", "auto_close_short":
					stats.TotalClosePositions++
					// 🔧 BUG FIX：partial_close 不計入 TotalClosePositions，避免重複計數
					// case "partial_close": // 不計數，因為只有完全平倉才算一次
					// update_stop_loss 和 update_take_profit 不計入統計
				}
			}
		}

		if record.Success {
			stats.SuccessfulCycles++
		} else {
			stats.FailedCycles++
		}
	}

	return stats, nil
}

// Statistics 统计信息
type Statistics struct {
	TotalCycles         int `json:"total_cycles"`
	SuccessfulCycles    int `json:"successful_cycles"`
	FailedCycles        int `json:"failed_cycles"`
	TotalOpenPositions  int `json:"total_open_positions"`
	TotalClosePositions int `json:"total_close_positions"`
}

// TradeOutcome 单笔交易结果
type TradeOutcome struct {
	Symbol        string    `json:"symbol"`         // 币种
	Side          string    `json:"side"`           // long/short
	Quantity      float64   `json:"quantity"`       // 仓位数量
	Leverage      int       `json:"leverage"`       // 杠杆倍数
	OpenPrice     float64   `json:"open_price"`     // 开仓价
	ClosePrice    float64   `json:"close_price"`    // 平仓价
	PositionValue float64   `json:"position_value"` // 仓位价值（quantity × openPrice）
	MarginUsed    float64   `json:"margin_used"`    // 保证金使用（positionValue / leverage）
	PnL           float64   `json:"pn_l"`           // 盈亏（USDT）
	PnLPct        float64   `json:"pn_l_pct"`       // 盈亏百分比（相对保证金）
	Duration      string    `json:"duration"`       // 持仓时长
	OpenTime      time.Time `json:"open_time"`      // 开仓时间
	CloseTime     time.Time `json:"close_time"`     // 平仓时间
	WasStopLoss   bool      `json:"was_stop_loss"`  // 是否止损
}

// PerformanceAnalysis 交易表现分析
type PerformanceAnalysis struct {
	TotalTrades   int                           `json:"total_trades"`   // 总交易数
	WinningTrades int                           `json:"winning_trades"` // 盈利交易数
	LosingTrades  int                           `json:"losing_trades"`  // 亏损交易数
	WinRate       float64                       `json:"win_rate"`       // 胜率
	AvgWin        float64                       `json:"avg_win"`        // 平均盈利
	AvgLoss       float64                       `json:"avg_loss"`       // 平均亏损
	ProfitFactor  float64                       `json:"profit_factor"`  // 盈亏比
	SharpeRatio   float64                       `json:"sharpe_ratio"`   // 夏普比率（风险调整后收益）
	RecentTrades  []TradeOutcome                `json:"recent_trades"`  // 最近N笔交易
	SymbolStats   map[string]*SymbolPerformance `json:"symbol_stats"`   // 各币种表现
	BestSymbol    string                        `json:"best_symbol"`    // 表现最好的币种
	WorstSymbol   string                        `json:"worst_symbol"`   // 表现最差的币种
}

// SymbolPerformance 币种表现统计
type SymbolPerformance struct {
	Symbol        string  `json:"symbol"`         // 币种
	TotalTrades   int     `json:"total_trades"`   // 交易次数
	WinningTrades int     `json:"winning_trades"` // 盈利次数
	LosingTrades  int     `json:"losing_trades"`  // 亏损次数
	WinRate       float64 `json:"win_rate"`       // 胜率
	TotalPnL      float64 `json:"total_pn_l"`     // 总盈亏
	AvgPnL        float64 `json:"avg_pn_l"`       // 平均盈亏
}

// normalizeAction 规范化动作名称并推断方向
// 返回: 标准动作类型(open/close/partial_close/auto_close/other) 与 方向(long/short/"")
func normalizeAction(act string) (string, string) {
    switch act {
    case "open_long":
        return "open", "long"
    case "open_short":
        return "open", "short"
    case "close_long":
        return "close", "long"
    case "close_short":
        return "close", "short"
    case "auto_close_long":
        return "auto_close", "long"
    case "auto_close_short":
        return "auto_close", "short"
    case "partial_close":
        return "partial_close", ""
    case "partial_close_long", "reduce_long":
        return "partial_close", "long"
    case "partial_close_short", "reduce_short":
        return "partial_close", "short"
    default:
        return act, ""
    }
}

// AnalyzePerformance 向后兼容的分析函数，使用默认费率解析器（环境变量 NOFX_TAKER_FEE_RATE 或 0.0004）
func (l *DecisionLogger) AnalyzePerformance(lookbackCycles int) (*PerformanceAnalysis, error) {
    return l.AnalyzePerformanceWithFee(lookbackCycles, nil)
}

// AnalyzePerformance 分析最近N个周期的交易表现
// AnalyzePerformanceWithFee 支持自定义费用解析器（按 symbol 返回 taker 费率，单位比例，如0.0004）
func (l *DecisionLogger) AnalyzePerformanceWithFee(lookbackCycles int, feeResolver func(symbol string) float64) (*PerformanceAnalysis, error) {
	records, err := l.GetLatestRecords(lookbackCycles)
	if err != nil {
		return nil, fmt.Errorf("读取历史记录失败: %w", err)
	}

	if len(records) == 0 {
		return &PerformanceAnalysis{
			RecentTrades: []TradeOutcome{},
			SymbolStats:  make(map[string]*SymbolPerformance),
		}, nil
	}

    analysis := &PerformanceAnalysis{
        RecentTrades: []TradeOutcome{},
        SymbolStats:  make(map[string]*SymbolPerformance),
    }

    // 交易费用（默认 Taker 0.04%/腿）；若提供 feeResolver 则优先使用（可按 symbol 可变）
    defaultFee := 0.0004
    if v := os.Getenv("NOFX_TAKER_FEE_RATE"); v != "" {
        if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f < 0.01 {
            defaultFee = f
        }
    }
    if feeResolver == nil {
        feeResolver = func(symbol string) float64 { return defaultFee }
    }

	// 追踪持仓状态：symbol_side -> {side, openPrice, openTime, quantity, leverage}
	openPositions := make(map[string]map[string]interface{})

	// 为了避免开仓记录在窗口外导致匹配失败，需要先从所有历史记录中找出未平仓的持仓
	// 获取更多历史记录来构建完整的持仓状态（使用更大的窗口）
	allRecords, err := l.GetLatestRecords(lookbackCycles * 3) // 扩大3倍窗口
	if err == nil && len(allRecords) > len(records) {
		// 先从扩大的窗口中收集所有开仓记录
        for _, record := range allRecords {
            for _, action := range record.Decisions {
                if !action.Success {
                    continue
                }

                symbol := action.Symbol
                normType, side := normalizeAction(action.Action)
                // 若 partial_close 未显式带方向，尝试从已知持仓中推断
                if normType == "partial_close" && side == "" {
                    for key, pos := range openPositions {
                        if posSymbol, _ := pos["side"].(string); key == symbol+"_"+posSymbol {
                            side = posSymbol
                            break
                        }
                    }
                }

                posKey := symbol + "_" + side

                switch normType {
                case "open":
                    // 记录开仓
                    openPositions[posKey] = map[string]interface{}{
                        "side":      side,
                        "openPrice": action.Price,
                        "openTime":  action.Timestamp,
                        "quantity":  action.Quantity,
                        "leverage":  action.Leverage,
                    }
                case "close", "auto_close":
                    // 移除已平仓记录
                    delete(openPositions, posKey)
                    // partial_close 不處理，保留持倉記錄
                }
            }
        }
	}

	// 遍历分析窗口内的记录，生成交易结果
	for _, record := range records {
        for _, action := range record.Decisions {
            if !action.Success {
                continue
            }

            symbol := action.Symbol
            normType, side := normalizeAction(action.Action)
            // partial_close 推断方向
            if normType == "partial_close" && side == "" {
                for key, pos := range openPositions {
                    if posSymbol, _ := pos["side"].(string); key == symbol+"_"+posSymbol {
                        side = posSymbol
                        break
                    }
                }
            }

            posKey := symbol + "_" + side // 使用symbol_side作为key，区分多空持仓

            switch normType {
            case "open":
                // 更新开仓记录（可能已经在预填充时记录过了）
                openPositions[posKey] = map[string]interface{}{
                    "side":               side,
                    "openPrice":          action.Price,
                    "openTime":           action.Timestamp,
                    "quantity":           action.Quantity,
                    "leverage":           action.Leverage,
                    "remainingQuantity":  action.Quantity, // 🔧 BUG FIX：追蹤剩餘數量
                    "accumulatedPnL":     0.0,             // 🔧 BUG FIX：累積部分平倉盈虧
                    "partialCloseCount":  0,               // 🔧 BUG FIX：部分平倉次數
                    "partialCloseVolume": 0.0,             // 🔧 BUG FIX：部分平倉總量
                }

            case "close", "partial_close", "auto_close":
                // 查找对应的开仓记录（可能来自预填充或当前窗口）
                if openPos, exists := openPositions[posKey]; exists {
                    openPrice := openPos["openPrice"].(float64)
                    openTime := openPos["openTime"].(time.Time)
                    side := openPos["side"].(string)
					quantity := openPos["quantity"].(float64)
					leverage := openPos["leverage"].(int)

					// 🔧 BUG FIX：取得追蹤字段（若不存在則初始化）
					remainingQty, _ := openPos["remainingQuantity"].(float64)
					if remainingQty == 0 {
						remainingQty = quantity // 兼容舊數據（沒有 remainingQuantity 字段）
					}
					accumulatedPnL, _ := openPos["accumulatedPnL"].(float64)
					partialCloseCount, _ := openPos["partialCloseCount"].(int)
					partialCloseVolume, _ := openPos["partialCloseVolume"].(float64)

					// 对于 partial_close，使用实际平仓数量；否则使用剩余仓位数量
					actualQuantity := remainingQty
					if action.Action == "partial_close" {
						actualQuantity = action.Quantity
					}

                // 计算本次平仓的盈亏（USDT），并扣除手续费（按开/平各一次）
                var pnl float64
                if side == "long" {
                    pnl = actualQuantity * (action.Price - openPrice)
                } else {
                    pnl = actualQuantity * (openPrice - action.Price)
                }
                // 手续费 = 名义价值 × 费率（开 + 平）；费率可按 symbol 解析
                feeRate := feeResolver(symbol)
                openFee := actualQuantity * openPrice * feeRate
                closeFee := actualQuantity * action.Price * feeRate
                pnl -= (openFee + closeFee)

					// 🔧 BUG FIX：處理 partial_close 聚合邏輯
					if action.Action == "partial_close" {
						// 累積盈虧和數量
						accumulatedPnL += pnl
						remainingQty -= actualQuantity
						partialCloseCount++
						partialCloseVolume += actualQuantity

						// 更新 openPositions（保留持倉記錄，但更新追蹤數據）
						openPos["remainingQuantity"] = remainingQty
						openPos["accumulatedPnL"] = accumulatedPnL
						openPos["partialCloseCount"] = partialCloseCount
						openPos["partialCloseVolume"] = partialCloseVolume

						// 判斷是否已完全平倉
						if remainingQty <= 0.0001 { // 使用小閾值避免浮點誤差
							// ✅ 完全平倉：記錄為一筆完整交易
							positionValue := quantity * openPrice
							marginUsed := positionValue / float64(leverage)
							pnlPct := 0.0
							if marginUsed > 0 {
								pnlPct = (accumulatedPnL / marginUsed) * 100
							}

							outcome := TradeOutcome{
								Symbol:        symbol,
								Side:          side,
								Quantity:      quantity, // 使用原始總量
								Leverage:      leverage,
								OpenPrice:     openPrice,
								ClosePrice:    action.Price, // 最後一次平倉價格
								PositionValue: positionValue,
								MarginUsed:    marginUsed,
								PnL:           accumulatedPnL, // 🔧 使用累積盈虧
								PnLPct:        pnlPct,
								Duration:      action.Timestamp.Sub(openTime).String(),
								OpenTime:      openTime,
								CloseTime:     action.Timestamp,
							}

							analysis.RecentTrades = append(analysis.RecentTrades, outcome)
							analysis.TotalTrades++ // 🔧 只在完全平倉時計數

							// 分类交易
							if accumulatedPnL > 0 {
								analysis.WinningTrades++
								analysis.AvgWin += accumulatedPnL
							} else if accumulatedPnL < 0 {
								analysis.LosingTrades++
								analysis.AvgLoss += accumulatedPnL
							}

							// 更新币种统计
							if _, exists := analysis.SymbolStats[symbol]; !exists {
								analysis.SymbolStats[symbol] = &SymbolPerformance{
									Symbol: symbol,
								}
							}
							stats := analysis.SymbolStats[symbol]
							stats.TotalTrades++
							stats.TotalPnL += accumulatedPnL
							if accumulatedPnL > 0 {
								stats.WinningTrades++
							} else if accumulatedPnL < 0 {
								stats.LosingTrades++
							}

							// 刪除持倉記錄
							delete(openPositions, posKey)
						}
						// ⚠️ 否則不做任何操作（等待後續 partial_close 或 full close）

					} else {
						// 🔧 完全平倉（close_long/close_short/auto_close）
						// 如果之前有部分平倉，需要加上累積的 PnL
                    totalPnL := accumulatedPnL + pnl

						positionValue := quantity * openPrice
						marginUsed := positionValue / float64(leverage)
						pnlPct := 0.0
						if marginUsed > 0 {
							pnlPct = (totalPnL / marginUsed) * 100
						}

                    outcome := TradeOutcome{
                        Symbol:        symbol,
                        Side:          side,
                        Quantity:      quantity, // 使用原始總量
                        Leverage:      leverage,
                        OpenPrice:     openPrice,
							ClosePrice:    action.Price,
							PositionValue: positionValue,
							MarginUsed:    marginUsed,
							PnL:           totalPnL, // 🔧 包含之前部分平倉的 PnL
							PnLPct:        pnlPct,
							Duration:      action.Timestamp.Sub(openTime).String(),
							OpenTime:      openTime,
							CloseTime:     action.Timestamp,
						}

						analysis.RecentTrades = append(analysis.RecentTrades, outcome)
						analysis.TotalTrades++

						// 分类交易
						if totalPnL > 0 {
							analysis.WinningTrades++
							analysis.AvgWin += totalPnL
						} else if totalPnL < 0 {
							analysis.LosingTrades++
							analysis.AvgLoss += totalPnL
						}

						// 更新币种统计
						if _, exists := analysis.SymbolStats[symbol]; !exists {
							analysis.SymbolStats[symbol] = &SymbolPerformance{
								Symbol: symbol,
							}
						}
						stats := analysis.SymbolStats[symbol]
						stats.TotalTrades++
						stats.TotalPnL += totalPnL
						if totalPnL > 0 {
							stats.WinningTrades++
						} else if totalPnL < 0 {
							stats.LosingTrades++
						}

						// 刪除持倉記錄
						delete(openPositions, posKey)
					}
				}
			}
		}
	}

	// 计算统计指标
	if analysis.TotalTrades > 0 {
		analysis.WinRate = (float64(analysis.WinningTrades) / float64(analysis.TotalTrades)) * 100

		// 计算总盈利和总亏损
		totalWinAmount := analysis.AvgWin   // 当前是累加的总和
		totalLossAmount := analysis.AvgLoss // 当前是累加的总和（负数）

		if analysis.WinningTrades > 0 {
			analysis.AvgWin /= float64(analysis.WinningTrades)
		}
		if analysis.LosingTrades > 0 {
			analysis.AvgLoss /= float64(analysis.LosingTrades)
		}

		// Profit Factor = 总盈利 / 总亏损（绝对值）
		// 注意：totalLossAmount 是负数，所以取负号得到绝对值
		if totalLossAmount != 0 {
			analysis.ProfitFactor = totalWinAmount / (-totalLossAmount)
		} else if totalWinAmount > 0 {
			// 只有盈利没有亏损的情况，设置为一个很大的值表示完美策略
			analysis.ProfitFactor = 999.0
		}
	}

	// 计算各币种胜率和平均盈亏
	bestPnL := -999999.0
	worstPnL := 999999.0
	for symbol, stats := range analysis.SymbolStats {
		if stats.TotalTrades > 0 {
			stats.WinRate = (float64(stats.WinningTrades) / float64(stats.TotalTrades)) * 100
			stats.AvgPnL = stats.TotalPnL / float64(stats.TotalTrades)

			if stats.TotalPnL > bestPnL {
				bestPnL = stats.TotalPnL
				analysis.BestSymbol = symbol
			}
			if stats.TotalPnL < worstPnL {
				worstPnL = stats.TotalPnL
				analysis.WorstSymbol = symbol
			}
		}
	}

	// 只保留最近的交易（倒序：最新的在前）
	if len(analysis.RecentTrades) > 10 {
		// 反转数组，让最新的在前
		for i, j := 0, len(analysis.RecentTrades)-1; i < j; i, j = i+1, j-1 {
			analysis.RecentTrades[i], analysis.RecentTrades[j] = analysis.RecentTrades[j], analysis.RecentTrades[i]
		}
		analysis.RecentTrades = analysis.RecentTrades[:10]
	} else if len(analysis.RecentTrades) > 0 {
		// 反转数组
		for i, j := 0, len(analysis.RecentTrades)-1; i < j; i, j = i+1, j-1 {
			analysis.RecentTrades[i], analysis.RecentTrades[j] = analysis.RecentTrades[j], analysis.RecentTrades[i]
		}
	}

	// 计算夏普比率（需要至少2个数据点）
	analysis.SharpeRatio = l.calculateSharpeRatio(records)

	return analysis, nil
}

// calculateSharpeRatio 计算夏普比率
// 基于账户净值的变化计算风险调整后收益
func (l *DecisionLogger) calculateSharpeRatio(records []*DecisionRecord) float64 {
	if len(records) < 2 {
		return 0.0
	}

	// 提取每个周期的账户净值
	// 注意：TotalBalance字段实际存储的是TotalEquity（账户总净值）
	// TotalUnrealizedProfit字段实际存储的是TotalPnL（相对初始余额的盈亏）
	var equities []float64
	for _, record := range records {
		// 直接使用TotalBalance，因为它已经是完整的账户净值
		equity := record.AccountState.TotalBalance
		if equity > 0 {
			equities = append(equities, equity)
		}
	}

	if len(equities) < 2 {
		return 0.0
	}

	// 计算周期收益率（period returns）
	var returns []float64
	for i := 1; i < len(equities); i++ {
		if equities[i-1] > 0 {
			periodReturn := (equities[i] - equities[i-1]) / equities[i-1]
			returns = append(returns, periodReturn)
		}
	}

	if len(returns) == 0 {
		return 0.0
	}

	// 计算平均收益率
	sumReturns := 0.0
	for _, r := range returns {
		sumReturns += r
	}
	meanReturn := sumReturns / float64(len(returns))

	// 计算收益率标准差
	sumSquaredDiff := 0.0
	for _, r := range returns {
		diff := r - meanReturn
		sumSquaredDiff += diff * diff
	}
	variance := sumSquaredDiff / float64(len(returns))
	stdDev := math.Sqrt(variance)

	// 避免除以零
	if stdDev == 0 {
		if meanReturn > 0 {
			return 999.0 // 无波动的正收益
		} else if meanReturn < 0 {
			return -999.0 // 无波动的负收益
		}
		return 0.0
	}

	// 计算夏普比率（假设无风险利率为0）
	// 注：直接返回周期级别的夏普比率（非年化），正常范围 -2 到 +2
	sharpeRatio := meanReturn / stdDev
	return sharpeRatio
}

// CalculateSharpeForWindow 对外暴露：基于给定窗口记录计算夏普比率
func (l *DecisionLogger) CalculateSharpeForWindow(records []*DecisionRecord) float64 {
    return l.calculateSharpeRatio(records)
}
