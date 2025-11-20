package trader

import (
    "encoding/json"
    "fmt"
    "log"
    "math"
    "strconv"
	"nofx/decision"
	"nofx/logger"
	"nofx/market"
	"nofx/mcp"
	"nofx/news"
	"os"
	"nofx/pool"
	"strings"
	"sync"
	"time"
)

// AutoTraderConfig 自动交易配置（简化版 - AI全权决策）
type AutoTraderConfig struct {
	// Trader标识
	ID      string // Trader唯一标识（用于日志目录等）
	Name    string // Trader显示名称
	AIModel string // AI模型: "qwen" 或 "deepseek"

	// 交易平台选择
	Exchange string // "binance", "hyperliquid" 或 "aster"

	// 币安API配置
	BinanceAPIKey    string
	BinanceSecretKey string

	// Hyperliquid配置
	HyperliquidPrivateKey string
	HyperliquidWalletAddr string
	HyperliquidTestnet    bool

	// Aster配置
	AsterUser       string // Aster主钱包地址
	AsterSigner     string // Aster API钱包地址
	AsterPrivateKey string // Aster API钱包私钥

	CoinPoolAPIURL string

	// AI配置
	UseQwen     bool
	DeepSeekKey string
	QwenKey     string

	// 自定义AI API配置
	CustomAPIURL    string
	CustomAPIKey    string
	CustomModelName string

	// 扫描配置
	ScanInterval time.Duration // 扫描间隔（建议3分钟）

	// 账户配置
	InitialBalance float64 // 初始金额（用于计算盈亏，需手动设置）

	// 杠杆配置
	BTCETHLeverage  int // BTC和ETH的杠杆倍数
	AltcoinLeverage int // 山寨币的杠杆倍数

	// 风险控制（仅作为提示，AI可自主决定）
	MaxDailyLoss    float64       // 最大日亏损百分比（提示）
	MaxDrawdown     float64       // 最大回撤百分比（提示）
	StopTradingTime time.Duration // 触发风控后暂停时长

	// 仓位模式
	IsCrossMargin bool // true=全仓模式, false=逐仓模式

	// 币种配置
	DefaultCoins []string // 默认币种列表（从数据库获取）
	TradingCoins []string // 实际交易币种列表

	// 系统提示词模板
	SystemPromptTemplate string // 系统提示词模板名称（如 "default", "aggressive"）

	// 是否在决策上下文中包含新闻
	IncludeNews bool
}

// AutoTrader 自动交易器
type AutoTrader struct {
	id                    string // Trader唯一标识
	name                  string // Trader显示名称
	aiModel               string // AI模型名称
	exchange              string // 交易平台名称
	config                AutoTraderConfig
	trader                Trader // 使用Trader接口（支持多平台）
	mcpClient             *mcp.Client
	decisionLogger        *logger.DecisionLogger // 决策日志记录器
	initialBalance        float64
	dailyPnL              float64
	customPrompt          string   // 自定义交易策略prompt
	overrideBasePrompt    bool     // 是否覆盖基础prompt
	systemPromptTemplate  string   // 系统提示词模板名称
	defaultCoins          []string // 默认币种列表（从数据库获取）
	tradingCoins          []string // 实际交易币种列表
	lastResetTime         time.Time
	stopUntil             time.Time
	isRunning             bool
	startTime             time.Time          // 系统启动时间
	callCount             int                // AI调用次数
	positionFirstSeenTime map[string]int64   // 持仓首次出现时间 (symbol_side -> timestamp毫秒)
	stopMonitorCh         chan struct{}      // 用于停止监控goroutine
	monitorWg             sync.WaitGroup     // 用于等待监控goroutine结束
	peakPnLCache          map[string]float64 // 最高收益缓存 (symbol_side -> 峰值盈亏百分比)
	peakPnLCacheMutex     sync.RWMutex       // 缓存读写锁
	lastBalanceSyncTime   time.Time          // 上次余额同步时间
	database              interface{}        // 数据库引用（用于自动更新余额）
	userID                string             // 用户ID
	router                *decision.Router   // 策略路由器 (v2)
}

// NewAutoTrader 创建自动交易器
func NewAutoTrader(config AutoTraderConfig, database interface{}, userID string) (*AutoTrader, error) {
	// 设置默认值
	if config.ID == "" {
		config.ID = "default_trader"
	}
	if config.Name == "" {
		config.Name = "Default Trader"
	}
	if config.AIModel == "" {
		if config.UseQwen {
			config.AIModel = "qwen"
		} else {
			config.AIModel = "deepseek"
		}
	}

	mcpClient := mcp.New()

	// 初始化AI
	if config.AIModel == "custom" {
		// 使用自定义API
		mcpClient.SetCustomAPI(config.CustomAPIURL, config.CustomAPIKey, config.CustomModelName)
		log.Printf("🤖 [%s] 使用自定义AI API: %s (模型: %s)", config.Name, config.CustomAPIURL, config.CustomModelName)
	} else if config.UseQwen || config.AIModel == "qwen" {
		// 使用Qwen (支持自定义URL和Model)
		mcpClient.SetQwenAPIKey(config.QwenKey, config.CustomAPIURL, config.CustomModelName)
		if config.CustomAPIURL != "" || config.CustomModelName != "" {
			log.Printf("🤖 [%s] 使用阿里云Qwen AI (自定义URL: %s, 模型: %s)", config.Name, config.CustomAPIURL, config.CustomModelName)
		} else {
			log.Printf("🤖 [%s] 使用阿里云Qwen AI", config.Name)
		}
	} else {
		// 默认使用DeepSeek (支持自定义URL和Model)
		mcpClient.SetDeepSeekAPIKey(config.DeepSeekKey, config.CustomAPIURL, config.CustomModelName)
		if config.CustomAPIURL != "" || config.CustomModelName != "" {
			log.Printf("🤖 [%s] 使用DeepSeek AI (自定义URL: %s, 模型: %s)", config.Name, config.CustomAPIURL, config.CustomModelName)
		} else {
			log.Printf("🤖 [%s] 使用DeepSeek AI", config.Name)
		}
	}

	// 初始化币种池API
	if config.CoinPoolAPIURL != "" {
		pool.SetCoinPoolAPI(config.CoinPoolAPIURL)
	}

	// 设置默认交易平台
	if config.Exchange == "" {
		config.Exchange = "binance"
	}

	// 根据配置创建对应的交易器
	var trader Trader
	var err error

	// 记录仓位模式（通用）
	marginModeStr := "全仓"
	if !config.IsCrossMargin {
		marginModeStr = "逐仓"
	}
	log.Printf("📊 [%s] 仓位模式: %s", config.Name, marginModeStr)

	switch config.Exchange {
	case "binance":
		log.Printf("🏦 [%s] 使用币安合约交易", config.Name)
		trader = NewFuturesTrader(config.BinanceAPIKey, config.BinanceSecretKey)
	case "hyperliquid":
		log.Printf("🏦 [%s] 使用Hyperliquid交易", config.Name)
		trader, err = NewHyperliquidTrader(config.HyperliquidPrivateKey, config.HyperliquidWalletAddr, config.HyperliquidTestnet)
		if err != nil {
			return nil, fmt.Errorf("初始化Hyperliquid交易器失败: %w", err)
		}
	case "aster":
		log.Printf("🏦 [%s] 使用Aster交易", config.Name)
		trader, err = NewAsterTrader(config.AsterUser, config.AsterSigner, config.AsterPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("初始化Aster交易器失败: %w", err)
		}
	default:
		return nil, fmt.Errorf("不支持的交易平台: %s", config.Exchange)
	}

	// 验证初始金额配置
	if config.InitialBalance <= 0 {
		return nil, fmt.Errorf("初始金额必须大于0，请在配置中设置InitialBalance")
	}

	// 初始化决策日志记录器（使用trader ID创建独立目录）
	logDir := fmt.Sprintf("decision_logs/%s", config.ID)
	decisionLogger := logger.NewDecisionLogger(logDir)

	// 设置默认系统提示词模板
	systemPromptTemplate := config.SystemPromptTemplate
	if systemPromptTemplate == "" {
		// feature/partial-close-dynamic-tpsl 分支默认使用 adaptive（支持动态止盈止损）
		systemPromptTemplate = "adaptive"
	}

	// 初始化路由器 (v2)
	// 默认策略与配置保持一致，或使用硬编码的新版策略
	defaultStrategy := "adaptive_moderate_v6_3"
	if config.SystemPromptTemplate != "" {
		defaultStrategy = config.SystemPromptTemplate
	}
	router := decision.NewRouter(defaultStrategy)

	return &AutoTrader{
		id:                    config.ID,
		name:                  config.Name,
		aiModel:               config.AIModel,
		exchange:              config.Exchange,
		config:                config,
		trader:                trader,
		mcpClient:             mcpClient,
		decisionLogger:        decisionLogger,
		initialBalance:        config.InitialBalance,
		systemPromptTemplate:  systemPromptTemplate,
		defaultCoins:          config.DefaultCoins,
		tradingCoins:          config.TradingCoins,
		lastResetTime:         time.Now(),
		startTime:             time.Now(),
		callCount:             0,
		isRunning:             false,
		positionFirstSeenTime: make(map[string]int64),
		stopMonitorCh:         make(chan struct{}),
		monitorWg:             sync.WaitGroup{},
		peakPnLCache:          make(map[string]float64),
		peakPnLCacheMutex:     sync.RWMutex{},
		lastBalanceSyncTime:   time.Now(), // 初始化为当前时间
		database:              database,
		userID:                userID,
		router:                router,
	}, nil
}

// Run 运行自动交易主循环
func (at *AutoTrader) Run() error {
	at.isRunning = true
	at.stopMonitorCh = make(chan struct{})
	at.startTime = time.Now()

	log.Println("🚀 AI驱动自动交易系统启动")
	log.Printf("💰 初始余额: %.2f USDT", at.initialBalance)
	log.Printf("⚙️  扫描间隔: %v", at.config.ScanInterval)
	log.Println("🤖 AI将全权决定杠杆、仓位大小、止损止盈等参数")
	at.monitorWg.Add(1)
	defer at.monitorWg.Done()

	// 启动回撤监控
	at.startDrawdownMonitor()

	ticker := time.NewTicker(at.config.ScanInterval)
	defer ticker.Stop()

	// 首次立即执行
	if err := at.runCycle(); err != nil {
		log.Printf("❌ 执行失败: %v", err)
	}

	for at.isRunning {
		select {
		case <-ticker.C:
			if err := at.runCycle(); err != nil {
				log.Printf("❌ 执行失败: %v", err)
			}
		case <-at.stopMonitorCh:
			log.Printf("[%s] ⏹ 收到停止信号，退出自动交易主循环", at.name)
			return nil
		}
	}

	return nil
}

// Stop 停止自动交易
func (at *AutoTrader) Stop() {
	if !at.isRunning {
		return
	}
	at.isRunning = false
	close(at.stopMonitorCh) // 通知监控goroutine停止
	at.monitorWg.Wait()     // 等待监控goroutine结束
	log.Println("⏹ 自动交易系统停止")
}

// autoSyncBalanceIfNeeded 自动同步余额（每10分钟检查一次，变化>5%才更新）
func (at *AutoTrader) autoSyncBalanceIfNeeded() {
	// 距离上次同步不足10分钟，跳过
	if time.Since(at.lastBalanceSyncTime) < 10*time.Minute {
		return
	}

	log.Printf("🔄 [%s] 开始自动检查余额变化...", at.name)

	// 查询实际余额
	balanceInfo, err := at.trader.GetBalance()
	if err != nil {
		log.Printf("⚠️ [%s] 查询余额失败: %v", at.name, err)
		at.lastBalanceSyncTime = time.Now() // 即使失败也更新时间，避免频繁重试
		return
	}

	// 提取净值（优先）：总净值 = 钱包余额 + 未实现盈亏
	var actualBalance float64
	if wallet, ok := balanceInfo["totalWalletBalance"].(float64); ok {
		if unpnl, ok2 := balanceInfo["totalUnrealizedProfit"].(float64); ok2 {
			actualBalance = wallet + unpnl
		}
	}
	// 退化到 available/balance
	if actualBalance <= 0 {
		if availableBalance, ok := balanceInfo["available_balance"].(float64); ok && availableBalance > 0 {
			actualBalance = availableBalance
		} else if availableBalance, ok := balanceInfo["availableBalance"].(float64); ok && availableBalance > 0 {
			actualBalance = availableBalance
		} else if totalBalance, ok := balanceInfo["balance"].(float64); ok && totalBalance > 0 {
			actualBalance = totalBalance
		} else {
			log.Printf("⚠️ [%s] 无法提取账户净值/余额", at.name)
			at.lastBalanceSyncTime = time.Now()
			return
		}
	}

	oldBalance := at.initialBalance

	// 防止除以零：如果初始余额无效，直接更新为实际余额
	if oldBalance <= 0 {
		log.Printf("⚠️ [%s] 初始余额无效 (%.2f)，直接更新为实际余额 %.2f USDT", at.name, oldBalance, actualBalance)
		at.initialBalance = actualBalance
		if at.database != nil {
			type DatabaseUpdater interface {
				UpdateTraderInitialBalance(userID, id string, newBalance float64) error
			}
			if db, ok := at.database.(DatabaseUpdater); ok {
				if err := db.UpdateTraderInitialBalance(at.userID, at.id, actualBalance); err != nil {
					log.Printf("❌ [%s] 更新数据库失败: %v", at.name, err)
				} else {
					log.Printf("✅ [%s] 已自动同步余额到数据库", at.name)
				}
			} else {
				log.Printf("⚠️ [%s] 数据库类型不支持UpdateTraderInitialBalance接口", at.name)
			}
		} else {
			log.Printf("⚠️ [%s] 数据库引用为空，余额仅在内存中更新", at.name)
		}
		at.lastBalanceSyncTime = time.Now()
		return
	}

	changePercent := ((actualBalance - oldBalance) / oldBalance) * 100

	// 变化超过5%才更新
	if math.Abs(changePercent) > 5.0 {
		log.Printf("🔔 [%s] 检测到余额大幅变化: %.2f → %.2f USDT (%.2f%%)",
			at.name, oldBalance, actualBalance, changePercent)

		// 更新内存中的 initialBalance
		at.initialBalance = actualBalance

		// 更新数据库（需要类型断言）
		if at.database != nil {
			// 这里需要根据实际的数据库类型进行类型断言
			// 由于使用了 interface{}，我们需要在 TraderManager 层面处理更新
			// 或者在这里进行类型检查
			type DatabaseUpdater interface {
				UpdateTraderInitialBalance(userID, id string, newBalance float64) error
			}
			if db, ok := at.database.(DatabaseUpdater); ok {
				err := db.UpdateTraderInitialBalance(at.userID, at.id, actualBalance)
				if err != nil {
					log.Printf("❌ [%s] 更新数据库失败: %v", at.name, err)
				} else {
					log.Printf("✅ [%s] 已自动同步余额到数据库", at.name)
				}
			} else {
				log.Printf("⚠️ [%s] 数据库类型不支持UpdateTraderInitialBalance接口", at.name)
			}
		} else {
			log.Printf("⚠️ [%s] 数据库引用为空，余额仅在内存中更新", at.name)
		}
	} else {
		log.Printf("✓ [%s] 余额变化不大 (%.2f%%)，无需更新", at.name, changePercent)
	}

	at.lastBalanceSyncTime = time.Now()
}

// runCycle 运行一个交易周期（使用AI全权决策）
func (at *AutoTrader) runCycle() error {
	at.callCount++

	log.Print("\n" + strings.Repeat("=", 70) + "\n")
	log.Printf("⏰ %s - AI决策周期 #%d", time.Now().Format("2006-01-02 15:04:05"), at.callCount)
	log.Println(strings.Repeat("=", 70))

	// 创建决策记录
	record := &logger.DecisionRecord{
		ExecutionLog: []string{},
		Success:      true,
	}

	// 1. 检查是否需要停止交易
	if time.Now().Before(at.stopUntil) {
		remaining := at.stopUntil.Sub(time.Now())
		log.Printf("⏸ 风险控制：暂停交易中，剩余 %.0f 分钟", remaining.Minutes())
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("风险控制暂停中，剩余 %.0f 分钟", remaining.Minutes())
		at.decisionLogger.LogDecision(record)
		return nil
	}

	// 2. 重置日盈亏（每天重置）
	if time.Since(at.lastResetTime) > 24*time.Hour {
		at.dailyPnL = 0
		at.lastResetTime = time.Now()
		log.Println("📅 日盈亏已重置")
	}

	// 3. 自动同步余额（每10分钟检查一次，充值/提现后自动更新）
	at.autoSyncBalanceIfNeeded()

	// 4. 收集交易上下文
	ctx, err := at.buildTradingContext()
	if err != nil {
		record.Success = false
		record.ErrorMessage = fmt.Sprintf("构建交易上下文失败: %v", err)
		at.decisionLogger.LogDecision(record)
		return fmt.Errorf("构建交易上下文失败: %w", err)
	}

	// 保存账户状态快照
	record.AccountState = logger.AccountSnapshot{
		TotalBalance:          ctx.Account.TotalEquity,
		AvailableBalance:      ctx.Account.AvailableBalance,
		TotalUnrealizedProfit: ctx.Account.TotalPnL,
		PositionCount:         ctx.Account.PositionCount,
		MarginUsedPct:         ctx.Account.MarginUsedPct,
	}

	// 保存持仓快照
	for _, pos := range ctx.Positions {
		record.Positions = append(record.Positions, logger.PositionSnapshot{
			Symbol:           pos.Symbol,
			Side:             pos.Side,
			PositionAmt:      pos.Quantity,
			EntryPrice:       pos.EntryPrice,
			MarkPrice:        pos.MarkPrice,
			UnrealizedProfit: pos.UnrealizedPnL,
			Leverage:         float64(pos.Leverage),
			LiquidationPrice: pos.LiquidationPrice,
		})
	}

	log.Print(strings.Repeat("=", 70))
	for _, coin := range ctx.CandidateCoins {
		record.CandidateCoins = append(record.CandidateCoins, coin.Symbol)
	}

	log.Printf("📊 账户净值: %.2f USDT | 可用: %.2f USDT | 持仓: %d",
		ctx.Account.TotalEquity, ctx.Account.AvailableBalance, ctx.Account.PositionCount)

	// --- 策略路由与分组并行执行 (v2) ---

	var allDecisions []decision.Decision

	// 如果未配置路由器，或路由器不可用，则直接使用 system_prompt_template 进行单一策略决策
	if at.router == nil {
		log.Printf("⚠️ 未配置策略路由器，使用 system_prompt_template=%s 单一策略模式", at.systemPromptTemplate)
		fullDecision, err := decision.GetFullDecisionWithCustomPrompt(ctx, at.mcpClient, at.customPrompt, at.overrideBasePrompt, at.systemPromptTemplate)
		if err != nil {
			record.Success = false
			record.ErrorMessage = fmt.Sprintf("AI决策失败（单一策略模式）: %v", err)
			at.decisionLogger.LogDecision(record)
			return fmt.Errorf("AI决策失败: %w", err)
		}
		// 记录思维链、系统提示词与输入提示词
		record.CoTTrace = fullDecision.CoTTrace
		record.SystemPrompt = fullDecision.SystemPrompt
		record.InputPrompt = fullDecision.UserPrompt
		// 将 StrategyCode 标记为当前模板名称，便于日志分析
		for i := range fullDecision.Decisions {
			fullDecision.Decisions[i].StrategyCode = at.systemPromptTemplate
		}
		allDecisions = fullDecision.Decisions
	} else {
		// 1. 路由分析
		log.Printf("🛤️ 正在进行策略路由分析...")
		routing, routerTrace := at.router.AnalyzeRegime(ctx, at.mcpClient)
		record.StrategyRouting = routing
		if routerTrace != nil {
			record.RouterSystemPrompt = routerTrace.SystemPrompt
			record.RouterInputPrompt = routerTrace.UserPrompt
			record.RouterRawOutput = routerTrace.RawOutput
		}
		strategyGroups := decision.GroupByStrategy(routing)

		// 如果路由结果为空，同样回退到单一策略模式
		if len(strategyGroups) == 0 {
			log.Printf("⚠️ Router 未返回任何路由结果，使用 system_prompt_template=%s 单一策略模式", at.systemPromptTemplate)
			fullDecision, err := decision.GetFullDecisionWithCustomPrompt(ctx, at.mcpClient, at.customPrompt, at.overrideBasePrompt, at.systemPromptTemplate)
			if err != nil {
				record.Success = false
				record.ErrorMessage = fmt.Sprintf("AI决策失败（单一策略模式）: %v", err)
				at.decisionLogger.LogDecision(record)
				return fmt.Errorf("AI决策失败: %w", err)
			}
			// 记录思维链、系统提示词与输入提示词
			record.CoTTrace = fullDecision.CoTTrace
			record.SystemPrompt = fullDecision.SystemPrompt
			record.InputPrompt = fullDecision.UserPrompt
			for i := range fullDecision.Decisions {
				fullDecision.Decisions[i].StrategyCode = at.systemPromptTemplate
			}
			allDecisions = fullDecision.Decisions
		} else {
			var groupWg sync.WaitGroup
			var decisionMutex sync.Mutex
			var firstErr error
			var errMutex sync.Mutex

			// 记录路由结果到日志
			for strategy, symbols := range strategyGroups {
				log.Printf("  🔹 策略组 [%s]: %s", strategy, strings.Join(symbols, ", "))
			}

			// 2. 并行执行策略组
			for strategy, symbols := range strategyGroups {
				groupWg.Add(1)
				go func(strat string, syms []string) {
					defer groupWg.Done()

					// 为该组构建子上下文 (深拷贝基础信息，只保留相关币种)
					subCtx := *ctx // 浅拷贝
					// 过滤 Positions 和 CandidateCoins
					subCtx.Positions = filterPositionsBySymbols(ctx.Positions, syms)
					subCtx.CandidateCoins = filterCandidatesBySymbols(ctx.CandidateCoins, syms)
					
					// 如果该组没有币种（理论上不会发生），跳过
					if len(subCtx.Positions) == 0 && len(subCtx.CandidateCoins) == 0 {
						return
					}

					// 调用 AI (使用特定的策略模板)
					// 注意：GetFullDecisionWithCustomPrompt 内部会从 PromptManager 获取 strat 对应的模板
					// 如果 strat 是 "adaptive_moderate_v6_3" 这种标准名，它会自动拼装 Base + Strategy
					log.Printf("🤖 请求AI决策 [策略: %s] (币种: %d个)...", strat, len(syms))
					
					groupDecision, err := decision.GetFullDecisionWithCustomPrompt(&subCtx, at.mcpClient, at.customPrompt, at.overrideBasePrompt, strat)
					
					if err != nil {
						log.Printf("❌ 策略组 [%s] 执行失败: %v", strat, err)
						errMutex.Lock()
						if firstErr == nil {
							firstErr = err
						}
						errMutex.Unlock()
						return
					}
				
					// 聚合决策
					decisionMutex.Lock()
					// 将 StrategyCode 注入到每个决策中
					for i := range groupDecision.Decisions {
						groupDecision.Decisions[i].StrategyCode = strat
					}
					allDecisions = append(allDecisions, groupDecision.Decisions...)
					
					// 记录该组的思维链与输入提示词（按策略分段追加，便于前端查看）
					record.CoTTrace += fmt.Sprintf("\n\n=== 策略组: %s ===\n%s", strat, groupDecision.CoTTrace)
					record.InputPrompt += fmt.Sprintf("\n\n=== 策略组: %s ===\n%s", strat, groupDecision.UserPrompt)
					// 记录首个 system prompt 作为代表（通常各组共享同一基础模板）
					if record.SystemPrompt == "" {
						record.SystemPrompt = groupDecision.SystemPrompt
					}
					decisionMutex.Unlock()

				}(strategy, symbols)
			}

			// 等待所有组完成
			groupWg.Wait()
			
			if firstErr != nil && len(allDecisions) == 0 {
				record.Success = false
				record.ErrorMessage = fmt.Sprintf("所有策略组执行失败，首个错误: %v", firstErr)
				at.decisionLogger.LogDecision(record)
				return fmt.Errorf("AI决策失败: %w", firstErr)
			}
		}
	}

	// 即使部分失败，只要有决策就继续执行
	if len(allDecisions) > 0 {
		decisionJSON, _ := json.MarshalIndent(allDecisions, "", "  ")
		record.DecisionJSON = string(decisionJSON)
	}

	log.Println()
	log.Print(strings.Repeat("-", 70))
	
	// 8. 对决策排序
	sortedDecisions := sortDecisionsByPriority(allDecisions)

	log.Println("🔄 执行顺序（已优化）: 先平仓→后开仓")
	for i, d := range sortedDecisions {
		log.Printf("  [%d] %s %s [%s]", i+1, d.Symbol, d.Action, d.StrategyCode)
	}
	log.Println()

	// 执行决策并记录结果
	for _, d := range sortedDecisions {
		orderTypeLabel := "市价单"
		if strings.ToLower(d.OrderType) == "limit" {
			orderTypeLabel = "限价单"
		}

		actionRecord := logger.DecisionAction{
			Action:       d.Action,
			Symbol:       d.Symbol,
			StrategyCode: d.StrategyCode,
			Quantity:     0,
			Leverage:     d.Leverage,
			Price:        0,
			Timestamp:    time.Now(),
			Success:      false,
		}

		if err := at.executeDecisionWithRecord(&d, &actionRecord); err != nil {
			log.Printf("❌ 执行决策失败 (%s %s): %v", d.Symbol, d.Action, err)
			actionRecord.Error = err.Error()
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("❌ [%s] %s %s 失败: %v", orderTypeLabel, d.Symbol, d.Action, err))
		} else {
			actionRecord.Success = true
			record.ExecutionLog = append(record.ExecutionLog, fmt.Sprintf("✓ [%s] %s %s 成功", orderTypeLabel, d.Symbol, d.Action))
			// 成功执行后短暂延迟
			time.Sleep(1 * time.Second)
		}

		record.Decisions = append(record.Decisions, actionRecord)
	}

	// 9. 保存决策记录
	if err := at.decisionLogger.LogDecision(record); err != nil {
		log.Printf("⚠ 保存决策记录失败: %v", err)
	}

	return nil
}

// buildTradingContext 构建交易上下文
func (at *AutoTrader) buildTradingContext() (*decision.Context, error) {
	// 1. 获取账户信息
	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("获取账户余额失败: %w", err)
	}

	// 获取账户字段
	totalWalletBalance := 0.0
	totalUnrealizedProfit := 0.0
	availableBalance := 0.0

	if wallet, ok := balance["totalWalletBalance"].(float64); ok {
		totalWalletBalance = wallet
	}
	if unrealized, ok := balance["totalUnrealizedProfit"].(float64); ok {
		totalUnrealizedProfit = unrealized
	}
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Total Equity = 钱包余额 + 未实现盈亏
	totalEquity := totalWalletBalance + totalUnrealizedProfit

	// 2. 获取持仓信息
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("获取持仓失败: %w", err)
	}

	var positionInfos []decision.PositionInfo
	totalMarginUsed := 0.0

	// 当前持仓的key集合（用于清理已平仓的记录）
	currentPositionKeys := make(map[string]bool)

	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity // 空仓数量为负，转为正数
		}

		// 跳过已平仓的持仓（quantity = 0），防止"幽灵持仓"传递给AI
		if quantity == 0 {
			continue
		}

		unrealizedPnl := pos["unRealizedProfit"].(float64)
		liquidationPrice := pos["liquidationPrice"].(float64)

		// 计算占用保证金（估算）
		leverage := 10 // 默认值，实际应该从持仓信息获取
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}
		marginUsed := (quantity * markPrice) / float64(leverage)
		totalMarginUsed += marginUsed

		// 计算盈亏百分比（基于保证金，考虑杠杆）
		pnlPct := calculatePnLPercentage(unrealizedPnl, marginUsed)

		// 跟踪持仓首次出现时间
		posKey := symbol + "_" + side
		currentPositionKeys[posKey] = true
		if _, exists := at.positionFirstSeenTime[posKey]; !exists {
			// 新持仓，记录当前时间
			at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()
		}
		updateTime := at.positionFirstSeenTime[posKey]

		// 获取当前止损止盈设置
		var stopLoss, takeProfit float64
		if sl, tp, err := at.trader.GetStopTakePrices(symbol, strings.ToUpper(side)); err == nil {
			stopLoss = sl
			takeProfit = tp
		}

		positionInfos = append(positionInfos, decision.PositionInfo{
			Symbol:           symbol,
			Side:             side,
			EntryPrice:       entryPrice,
			MarkPrice:        markPrice,
			Quantity:         quantity,
			Leverage:         leverage,
			UnrealizedPnL:    unrealizedPnl,
			UnrealizedPnLPct: pnlPct,
			LiquidationPrice: liquidationPrice,
			MarginUsed:       marginUsed,
			UpdateTime:       updateTime,
			StopLoss:         stopLoss,
			TakeProfit:       takeProfit,
		})
	}

	// 清理已平仓的持仓记录
	for key := range at.positionFirstSeenTime {
		if !currentPositionKeys[key] {
			delete(at.positionFirstSeenTime, key)
		}
	}

	// 3. 获取交易员的候选币种池
	candidateCoins, err := at.getCandidateCoins()
	if err != nil {
		return nil, fmt.Errorf("获取候选币种失败: %w", err)
	}

	// 4. 计算总盈亏
	totalPnL := totalEquity - at.initialBalance
	totalPnLPct := 0.0
	if at.initialBalance > 0 {
		totalPnLPct = (totalPnL / at.initialBalance) * 100
	}

	marginUsedPct := 0.0
	if totalEquity > 0 {
		marginUsedPct = (totalMarginUsed / totalEquity) * 100
	}

	// 5. 分析历史表现（最近100个周期，避免长期持仓的交易记录丢失）
	// 优先使用“交易所成交对账”，失败则回退日志口径，窗口默认500以稳定统计
	performance := at.analyzePerformanceForContext(500)

	// 6. 构建上下文
	ctx := &decision.Context{
		CurrentTime:     time.Now().Format("2006-01-02 15:04:05"),
		RuntimeMinutes:  int(time.Since(at.startTime).Minutes()),
		CallCount:       at.callCount,
		BTCETHLeverage:  at.config.BTCETHLeverage,  // 使用配置的杠杆倍数
		AltcoinLeverage: at.config.AltcoinLeverage, // 使用配置的杠杆倍数
		Account: decision.AccountInfo{
			TotalEquity:      totalEquity,
			AvailableBalance: availableBalance,
			TotalPnL:         totalPnL,
			TotalPnLPct:      totalPnLPct,
			MarginUsed:       totalMarginUsed,
			MarginUsedPct:    marginUsedPct,
			PositionCount:    len(positionInfos),
		},
		Positions:      positionInfos,
		CandidateCoins: candidateCoins,
		Performance:    performance, // 添加历史表现分析
	}

	// 可选：注入新闻（控制 token 体积）
	if at.GetIncludeNews() {
		// 选取待注入的符号：优先当前持仓前2个，其次候选前1个
		symSet := make(map[string]bool)
		var pick []string
		for _, p := range positionInfos {
			if len(pick) >= 2 {
				break
			}
			if !symSet[p.Symbol] {
				pick = append(pick, p.Symbol)
				symSet[p.Symbol] = true
			}
		}
		if len(pick) == 0 && len(candidateCoins) > 0 {
			pick = append(pick, candidateCoins[0].Symbol)
		}
		var briefs []decision.NewsBrief
		for _, s := range pick {
			items, _ := news.Latest(s, 3)
			for _, it := range items {
				briefs = append(briefs, decision.NewsBrief{
					Symbol:      s,
					Title:       it.Title,
					Source:      it.Source,
					URL:         it.URL,
					PublishedAt: it.PublishedAt.Format("2006-01-02 15:04:05"),
					Summary:     it.Summary,
				})
			}
		}
		ctx.News = briefs
	}

	return ctx, nil
}

// analyzePerformanceForContext 统一“AI学习与反思”的口径：优先交易所成交，其次日志回退
func (at *AutoTrader) analyzePerformanceForContext(lookback int) interface{} {
	// 限制窗口，避免性能问题
	if lookback <= 0 || lookback > 1000 {
		lookback = 500
	}

	// 采集决策日志窗口，确定时间范围与涉及的symbols
	records, _ := at.decisionLogger.GetLatestRecords(lookback)
	var startTime, endTime time.Time
	symSet := map[string]struct{}{}
	if len(records) > 0 {
		startTime = records[0].Timestamp
		endTime = records[len(records)-1].Timestamp
		for _, r := range records {
			for _, d := range r.Decisions {
				if d.Symbol != "" {
					symSet[strings.ToUpper(d.Symbol)] = struct{}{}
				}
			}
		}
	} else {
		// 没有记录：估一个时间窗（lookback*3分钟）
		endTime = time.Now()
		startTime = endTime.Add(-time.Duration(lookback*3) * time.Minute)
	}
	var symbols []string
	for s := range symSet {
		symbols = append(symbols, s)
	}

	// 费率解析：优先底层，次之环境变量，最后默认
	exchange := at.exchange
	resolver := func(symbol string) float64 {
		if r := at.GetTakerFeeRate(symbol); r > 0 {
			return r
		}
		key := "NOFX_TAKER_FEE_RATE_" + strings.ToUpper(exchange)
		if v := os.Getenv(key); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f < 0.01 {
				return f
			}
		}
		if v := os.Getenv("NOFX_TAKER_FEE_RATE"); v != "" {
			if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 && f < 0.01 {
				return f
			}
		}
		switch strings.ToLower(exchange) {
		case "binance":
			return 0.0004
		case "hyperliquid", "aster":
			return 0.0005
		default:
			return 0.0004
		}
	}

	// 交易所优先：聚合 userTrades → PerformanceAnalysis
	exchangeAnalysis := func() (*logger.PerformanceAnalysis, error) {
		if len(symbols) == 0 {
			return nil, fmt.Errorf("no symbols from decisions")
		}
		priceCache := map[string]float64{}
		priceOf := func(asset string) (float64, error) {
			a := strings.ToUpper(strings.TrimSpace(asset))
			if a == "" || a == "USDT" {
				return 1, nil
			}
			if v, ok := priceCache[a]; ok {
				return v, nil
			}
			data, err := market.Get(a + "USDT")
			if err != nil {
				return 0, err
			}
			priceCache[a] = data.CurrentPrice
			return data.CurrentPrice, nil
		}

		type key struct{ sym, side string }
		grouped := map[key][]StdUMTrade{}
		for _, sym := range symbols {
			trs, err := at.GetUserTrades(sym, startTime, endTime, 1000)
			if err != nil {
				return nil, err
			}
			for _, t := range trs {
				k := key{strings.ToUpper(t.Symbol), strings.ToUpper(t.PositionSide)}
				grouped[k] = append(grouped[k], t)
			}
		}

		analysis := &logger.PerformanceAnalysis{
			RecentTrades: []logger.TradeOutcome{},
			SymbolStats:  map[string]*logger.SymbolPerformance{},
		}

		for k, list := range grouped {
			// 简单 FIFO 聚合：按方向聚合成开/平周期
			var qty, entryQty, realized, feeUSDT float64
			var openPrice float64
			var openTime time.Time

			for _, tr := range list {
				// 累计费用（转换为USDT）
				comm := tr.Commission
				if tr.CommissionAsset != "" && strings.ToUpper(tr.CommissionAsset) != "USDT" {
					if px, err := priceOf(tr.CommissionAsset); err == nil && px > 0 {
						comm *= px
					}
				}
				feeUSDT += comm
				realized += tr.RealizedPnl

				// BUY 增加仓位，SELL 减少仓位（方向使用 PositionSide 区分，简化为数量聚合）
				if strings.ToUpper(tr.Side) == "BUY" {
					if qty <= 0 {
						openPrice = tr.Price
						openTime = tr.Time
					}
					qty += tr.Qty
					entryQty += tr.Qty
				} else {
					qty -= tr.Qty
				}

				// 成为闭环（数量归零或反向穿越）
				if qty <= 1e-12 && entryQty > 0 {
					positionValue := openPrice * entryQty
					outcome := logger.TradeOutcome{
						Symbol:        k.sym,
						Side:          strings.ToLower(k.side),
						Quantity:      entryQty,
						Leverage:      0,
						OpenPrice:     openPrice,
						ClosePrice:    tr.Price,
						PositionValue: positionValue,
						MarginUsed:    0,
						PnL:           realized - feeUSDT,
						PnLPct:        0,
						Duration:      tr.Time.Sub(openTime).String(),
						OpenTime:      openTime,
						CloseTime:     tr.Time,
						WasStopLoss:   false,
					}
					analysis.RecentTrades = append(analysis.RecentTrades, outcome)
					analysis.TotalTrades++
					if outcome.PnL > 0 {
						analysis.WinningTrades++
						analysis.AvgWin += outcome.PnL
					} else if outcome.PnL < 0 {
						analysis.LosingTrades++
						analysis.AvgLoss += outcome.PnL
					}
					if _, ok := analysis.SymbolStats[k.sym]; !ok {
						analysis.SymbolStats[k.sym] = &logger.SymbolPerformance{Symbol: k.sym}
					}
					st := analysis.SymbolStats[k.sym]
					st.TotalTrades++
					st.TotalPnL += outcome.PnL
					if outcome.PnL > 0 {
						st.WinningTrades++
					} else if outcome.PnL < 0 {
						st.LosingTrades++
					}
					// 重置
					qty, entryQty, realized, feeUSDT = 0, 0, 0, 0
					openPrice, openTime = 0, time.Time{}
				}
			}
		}
		// 汇总指标
		if analysis.TotalTrades > 0 {
			analysis.WinRate = (float64(analysis.WinningTrades) / float64(analysis.TotalTrades)) * 100
			tw := analysis.AvgWin
			tl := analysis.AvgLoss
			if analysis.WinningTrades > 0 {
				analysis.AvgWin /= float64(analysis.WinningTrades)
			}
			if analysis.LosingTrades > 0 {
				analysis.AvgLoss /= float64(analysis.LosingTrades)
			}
			if tl != 0 {
				analysis.ProfitFactor = tw / (-tl)
			} else if tw > 0 {
				analysis.ProfitFactor = 999.0
			}
		}
		best, worst := -1e12, 1e12
		for sym, st := range analysis.SymbolStats {
			if st.TotalTrades > 0 {
				st.WinRate = (float64(st.WinningTrades) / float64(st.TotalTrades)) * 100
				st.AvgPnL = st.TotalPnL / float64(st.TotalTrades)
				if st.TotalPnL > best {
					best = st.TotalPnL
					analysis.BestSymbol = sym
				}
				if st.TotalPnL < worst {
					worst = st.TotalPnL
					analysis.WorstSymbol = sym
				}
			}
		}
		// 倒序保留最近10条
		if n := len(analysis.RecentTrades); n > 1 {
			for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
				analysis.RecentTrades[i], analysis.RecentTrades[j] = analysis.RecentTrades[j], analysis.RecentTrades[i]
			}
		}
		if len(analysis.RecentTrades) > 10 {
			analysis.RecentTrades = analysis.RecentTrades[:10]
		}
		// 夏普比率基于净值记录（与前端一致）
		analysis.SharpeRatio = at.decisionLogger.CalculateSharpeForWindow(records)
		return analysis, nil
	}

	if ex, err := exchangeAnalysis(); err == nil && ex != nil && ex.TotalTrades > 0 {
		return ex
	}

	// 回退：日志口径 + 费率解析器
	perf, err := at.decisionLogger.AnalyzePerformanceWithFee(lookback, resolver)
	if err != nil {
		log.Printf("⚠️  日志口径表现分析失败: %v", err)
		return nil
	}
	return perf
}

// executeDecisionWithRecord 执行AI决策并记录详细信息
func (at *AutoTrader) executeDecisionWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	switch decision.Action {
	case "open_long":
		return at.executeOpenLongWithRecord(decision, actionRecord)
	case "open_short":
		return at.executeOpenShortWithRecord(decision, actionRecord)
	case "close_long":
		return at.executeCloseLongWithRecord(decision, actionRecord)
	case "close_short":
		return at.executeCloseShortWithRecord(decision, actionRecord)
	case "update_stop_loss":
		return at.executeUpdateStopLossWithRecord(decision, actionRecord)
	case "update_take_profit":
		return at.executeUpdateTakeProfitWithRecord(decision, actionRecord)
	case "partial_close":
		return at.executePartialCloseWithRecord(decision, actionRecord)
	case "hold", "wait":
		// 无需执行，仅记录
		return nil
	default:
		return fmt.Errorf("未知的action: %s", decision.Action)
	}
}

// executeOpenLongWithRecord 执行开多仓并记录详细信息
func (at *AutoTrader) executeOpenLongWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("  📈 开多仓: %s", decision.Symbol)

	// 预检：最小止损距离与入场结构（可配置）
	marketData, err := market.Get(decision.Symbol)
	if err != nil {
		return err
	}
	entryRefPrice := marketData.CurrentPrice
	if entryRefPrice <= 0 {
		return fmt.Errorf("无法获取当前价格")
	}
	// 3m ATR 百分比
	atrPct := 0.0
	if md := marketData.IntradaySeries; md != nil && md.ATR14 > 0 && entryRefPrice > 0 {
		atrPct = (md.ATR14 / entryRefPrice) * 100.0
	}
	// 最小止损距离：max(基线%, ATR系数×ATR%)
	enforceMinSL := os.Getenv("NOFX_ENFORCE_MIN_SL")
	if enforceMinSL == "" || enforceMinSL == "1" || strings.ToLower(enforceMinSL) == "true" {
		basePct := 1.0
		if v := os.Getenv("NOFX_MIN_SL_PCT_BASE"); v != "" {
			if f, e := strconv.ParseFloat(v, 64); e == nil && f > 0 {
				basePct = f
			}
		}
		atrMult := 0.8
		if v := os.Getenv("NOFX_MIN_SL_ATR_MULT"); v != "" {
			if f, e := strconv.ParseFloat(v, 64); e == nil && f >= 0 {
				atrMult = f
			}
		}
		minSlPct := math.Max(basePct, atrPct*atrMult)
		if decision.StopLoss > 0 {
			riskPct := (entryRefPrice - decision.StopLoss) / entryRefPrice * 100.0
			if riskPct < minSlPct {
				return fmt.Errorf("止损过近: %.2f%% < 最小要求 %.2f%%（基于3m ATR与基线）", riskPct, minSlPct)
			}
		}
	}
	// 可选：入场需贴近3m EMA20（软约束，默认关闭）
	if v := os.Getenv("NOFX_NEAR_EMA20_ENABLED"); strings.ToLower(v) == "true" || v == "1" {
		thr := 0.4
		if t := os.Getenv("NOFX_NEAR_EMA20_PCT"); t != "" {
			if f, e := strconv.ParseFloat(t, 64); e == nil && f > 0 {
				thr = f
			}
		}
		ema := marketData.CurrentEMA20
		if ema > 0 {
			dist := math.Abs((entryRefPrice-ema) / ema * 100.0)
			if dist > thr {
				log.Printf("  ⚠ 入场距离3m EMA20较远: %.2f%% > 阈值 %.2f%%（软约束提醒）", dist, thr)
			}
		}
	}

	// ⚠️ 关键：检查是否已有同币种同方向持仓，如果有则拒绝开仓（防止仓位叠加超限）
	positions, err := at.trader.GetPositions()
	if err == nil {
		for _, pos := range positions {
			if pos["symbol"] == decision.Symbol && pos["side"] == "long" {
				return fmt.Errorf("❌ %s 已有多仓，拒绝开仓以防止仓位叠加超限。如需换仓，请先给出 close_long 决策", decision.Symbol)
			}
		}
	}

	// 计算数量
	calcPrice := marketData.CurrentPrice
	if strings.ToLower(decision.OrderType) == "limit" && decision.LimitPrice > 0 {
		calcPrice = decision.LimitPrice
	}
	quantity := decision.PositionSizeUSD / calcPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = calcPrice

	// ⚠️ 保证金验证：防止保证金不足错误（code=-2019）
	requiredMargin := decision.PositionSizeUSD / float64(decision.Leverage)

	balance, err := at.trader.GetBalance()
	if err != nil {
		return fmt.Errorf("获取账户余额失败: %w", err)
	}
	availableBalance := 0.0
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// 手续费估算（Taker费率 0.04%）
	estimatedFee := decision.PositionSizeUSD * 0.0004
	totalRequired := requiredMargin + estimatedFee

	if totalRequired > availableBalance {
		return fmt.Errorf("❌ 保证金不足: 需要 %.2f USDT（保证金 %.2f + 手续费 %.2f），可用 %.2f USDT",
			totalRequired, requiredMargin, estimatedFee, availableBalance)
	}

	// 设置仓位模式
	if err := at.trader.SetMarginMode(decision.Symbol, at.config.IsCrossMargin); err != nil {
		log.Printf("  ⚠️ 设置仓位模式失败: %v", err)
		// 继续执行，不影响交易
	}

	// 设置杠杆 (通用)
	if err := at.trader.SetLeverage(decision.Symbol, decision.Leverage); err != nil {
		return fmt.Errorf("设置杠杆失败: %w", err)
	}

	// 先取消旧挂单 (Agent行为：新的决策覆盖旧的)
	if err := at.trader.CancelAllOrders(decision.Symbol); err != nil {
		log.Printf("  ⚠ 取消旧挂单失败: %v", err)
	}

	var orderID int64

	// 分支：限价单 vs 市价单
	if strings.ToLower(decision.OrderType) == "limit" {
		log.Printf("  ⏳ 提交限价买单: %s 价格: %.4f 数量: %.4f", decision.Symbol, decision.LimitPrice, quantity)
		
		req := &OrderRequest{
			Symbol:       decision.Symbol,
			Side:         "BUY",
			PositionSide: "LONG",
			Type:         "LIMIT",
			Quantity:     quantity,
			Price:        decision.LimitPrice,
			TimeInForce:  decision.TimeInForce,
			PostOnly:     decision.PostOnly,
		}
		res, err := at.trader.CreateOrder(req)
		if err != nil {
			return fmt.Errorf("限价下单失败: %w", err)
		}
		
		if id, ok := res["orderId"].(int64); ok {
			orderID = id
		}
		log.Printf("  ✓ 限价单已提交，订单ID: %v", orderID)
		actionRecord.OrderID = orderID
		
		// 限价单不立即设置止损止盈，等待成交后由后续周期或监控服务处理
		// TODO: 记录待设置的 SL/TP 到数据库或内存？
		return nil

	} else {
		// 市价单 (Market)
		// 使用 OpenLong (封装了 CreateOrder Market + FormatQuantity)
		// 但由于 OpenLong 内部也会 CancelAllOrders 和 SetLeverage，稍微有点重复，不过无害
		
		// 这里直接调用 OpenLong 以保持兼容性，或者重构为 CreateOrder(MARKET)
		// 为了稳健，继续使用 OpenLong，因为它经过测试
		order, err := at.trader.OpenLong(decision.Symbol, quantity, decision.Leverage)
		if err != nil {
			return err
		}

		// 记录订单ID
		if id, ok := order["orderId"].(int64); ok {
			orderID = id
			actionRecord.OrderID = orderID
		}

		log.Printf("  ✓ 市价开仓成功，订单ID: %v, 数量: %.4f", orderID, quantity)
		
		// 记录开仓时间
		posKey := decision.Symbol + "_long"
		at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()

		// 设置止损止盈 (市价单立即设置)
		// 下保护单前的方向/距离校验（防误触）
		enforceProtect := os.Getenv("NOFX_ENFORCE_PROTECT_ORDER_DIR")
		if enforceProtect == "" || enforceProtect == "1" || strings.ToLower(enforceProtect) == "true" {
			if decision.StopLoss >= marketData.CurrentPrice {
				log.Printf("  ⚠ 调整止损方向：做多止损需 < 当前价，调整略低于参考价")
			}
			if decision.TakeProfit <= marketData.CurrentPrice {
				log.Printf("  ⚠ 调整止盈方向：做多止盈需 > 当前价，调整略高于参考价")
			}
		}
		if err := at.trader.SetStopLoss(decision.Symbol, "LONG", quantity, decision.StopLoss); err != nil {
			log.Printf("  ⚠ 设置止损失败: %v", err)
		}
		if err := at.trader.SetTakeProfit(decision.Symbol, "LONG", quantity, decision.TakeProfit); err != nil {
			log.Printf("  ⚠ 设置止盈失败: %v", err)
		}
	}

	return nil
}

// executeOpenShortWithRecord 执行开空仓并记录详细信息
func (at *AutoTrader) executeOpenShortWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {

	log.Printf("  📉 开空仓: %s", decision.Symbol)

	// 预检：最小止损距离与入场结构（可配置）
	marketData, err := market.Get(decision.Symbol)
	if err != nil {
		return err
	}
	entryRefPrice := marketData.CurrentPrice
	if entryRefPrice <= 0 {
		return fmt.Errorf("无法获取当前价格")
	}
	atrPct := 0.0
	if md := marketData.IntradaySeries; md != nil && md.ATR14 > 0 && entryRefPrice > 0 {
		atrPct = (md.ATR14 / entryRefPrice) * 100.0
	}
	enforceMinSL := os.Getenv("NOFX_ENFORCE_MIN_SL")
	if enforceMinSL == "" || enforceMinSL == "1" || strings.ToLower(enforceMinSL) == "true" {
		basePct := 1.0
		if v := os.Getenv("NOFX_MIN_SL_PCT_BASE"); v != "" {
			if f, e := strconv.ParseFloat(v, 64); e == nil && f > 0 {
				basePct = f
			}
		}
		atrMult := 0.8
		if v := os.Getenv("NOFX_MIN_SL_ATR_MULT"); v != "" {
			if f, e := strconv.ParseFloat(v, 64); e == nil && f >= 0 {
				atrMult = f
			}
		}
		minSlPct := math.Max(basePct, atrPct*atrMult)
		if decision.StopLoss > 0 {
			riskPct := (decision.StopLoss - entryRefPrice) / entryRefPrice * 100.0
			if riskPct < minSlPct {
				return fmt.Errorf("止损过近: %.2f%% < 最小要求 %.2f%%（基于3m ATR与基线）", riskPct, minSlPct)
			}
		}
	}
	if v := os.Getenv("NOFX_NEAR_EMA20_ENABLED"); strings.ToLower(v) == "true" || v == "1" {
		thr := 0.4
		if t := os.Getenv("NOFX_NEAR_EMA20_PCT"); t != "" {
			if f, e := strconv.ParseFloat(t, 64); e == nil && f > 0 {
				thr = f
			}
		}
		ema := marketData.CurrentEMA20
		if ema > 0 {
			dist := math.Abs((entryRefPrice-ema) / ema * 100.0)
			if dist > thr {
				log.Printf("  ⚠ 入场距离3m EMA20较远: %.2f%% > 阈值 %.2f%%（软约束提醒）", dist, thr)
			}
		}
	}

	// ⚠️ 关键：检查是否已有同币种同方向持仓，如果有则拒绝开仓（防止仓位叠加超限）
	positions, err := at.trader.GetPositions()
	if err == nil {
		for _, pos := range positions {
			if pos["symbol"] == decision.Symbol && pos["side"] == "short" {
				return fmt.Errorf("❌ %s 已有空仓，拒绝开仓以防止仓位叠加超限。如需换仓，请先给出 close_short 决策", decision.Symbol)
			}
		}
	}

	// 计算数量
	calcPrice := marketData.CurrentPrice
	if strings.ToLower(decision.OrderType) == "limit" && decision.LimitPrice > 0 {
		calcPrice = decision.LimitPrice
	}
	quantity := decision.PositionSizeUSD / calcPrice
	actionRecord.Quantity = quantity
	actionRecord.Price = calcPrice

	// ⚠️ 保证金验证：防止保证金不足错误（code=-2019）
	requiredMargin := decision.PositionSizeUSD / float64(decision.Leverage)

	balance, err := at.trader.GetBalance()
	if err != nil {
		return fmt.Errorf("获取账户余额失败: %w", err)
	}
	availableBalance := 0.0
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// 手续费估算（Taker费率 0.04%）
	estimatedFee := decision.PositionSizeUSD * 0.0004
	totalRequired := requiredMargin + estimatedFee

	if totalRequired > availableBalance {
		return fmt.Errorf("❌ 保证金不足: 需要 %.2f USDT（保证金 %.2f + 手续费 %.2f），可用 %.2f USDT",
			totalRequired, requiredMargin, estimatedFee, availableBalance)
	}

	// 设置仓位模式
	if err := at.trader.SetMarginMode(decision.Symbol, at.config.IsCrossMargin); err != nil {
		log.Printf("  ⚠️ 设置仓位模式失败: %v", err)
		// 继续执行，不影响交易
	}

	// 设置杠杆 (通用)
	if err := at.trader.SetLeverage(decision.Symbol, decision.Leverage); err != nil {
		return fmt.Errorf("设置杠杆失败: %w", err)
	}

	// 先取消旧挂单 (Agent行为：新的决策覆盖旧的)
	if err := at.trader.CancelAllOrders(decision.Symbol); err != nil {
		log.Printf("  ⚠ 取消旧挂单失败: %v", err)
	}

	var orderID int64

	// 分支：限价单 vs 市价单
	if strings.ToLower(decision.OrderType) == "limit" {
		log.Printf("  ⏳ 提交限价卖单: %s 价格: %.4f 数量: %.4f", decision.Symbol, decision.LimitPrice, quantity)
		
		req := &OrderRequest{
			Symbol:       decision.Symbol,
			Side:         "SELL",
			PositionSide: "SHORT",
			Type:         "LIMIT",
			Quantity:     quantity,
			Price:        decision.LimitPrice,
			TimeInForce:  decision.TimeInForce,
			PostOnly:     decision.PostOnly,
		}
		res, err := at.trader.CreateOrder(req)
		if err != nil {
			return fmt.Errorf("限价下单失败: %w", err)
		}
		
		if id, ok := res["orderId"].(int64); ok {
			orderID = id
		}
		log.Printf("  ✓ 限价单已提交，订单ID: %v", orderID)
		actionRecord.OrderID = orderID
		
		// 限价单不立即设置止损止盈
		return nil

	} else {
		// 市价单 (Market)
		order, err := at.trader.OpenShort(decision.Symbol, quantity, decision.Leverage)
		if err != nil {
			return err
		}

		// 记录订单ID
		if id, ok := order["orderId"].(int64); ok {
			orderID = id
			actionRecord.OrderID = orderID
		}

		log.Printf("  ✓ 市价开仓成功，订单ID: %v, 数量: %.4f", orderID, quantity)

		// 记录开仓时间
		posKey := decision.Symbol + "_short"
		at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()

		// 设置止损止盈
		enforceProtect := os.Getenv("NOFX_ENFORCE_PROTECT_ORDER_DIR")
		if enforceProtect == "" || enforceProtect == "1" || strings.ToLower(enforceProtect) == "true" {
			if decision.StopLoss <= marketData.CurrentPrice {
				log.Printf("  ⚠ 调整止损方向：做空止损需 > 当前价，调整略高于参考价")
			}
			if decision.TakeProfit >= marketData.CurrentPrice {
				log.Printf("  ⚠ 调整止盈方向：做空止盈需 < 当前价，调整略低于参考价")
			}
		}
		if err := at.trader.SetStopLoss(decision.Symbol, "SHORT", quantity, decision.StopLoss); err != nil {
			log.Printf("  ⚠ 设置止损失败: %v", err)
		}
		if err := at.trader.SetTakeProfit(decision.Symbol, "SHORT", quantity, decision.TakeProfit); err != nil {
			log.Printf("  ⚠ 设置止盈失败: %v", err)
		}
	}

	return nil
}

// executeCloseLongWithRecord 执行平多仓并记录详细信息
func (at *AutoTrader) executeCloseLongWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("  🔄 平多仓: %s", decision.Symbol)

	// 获取当前价格
	marketData, err := market.Get(decision.Symbol)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// 平仓
	order, err := at.trader.CloseLong(decision.Symbol, 0) // 0 = 全部平仓
	if err != nil {
		return err
	}

	// 记录订单ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	log.Printf("  ✓ 平仓成功")
	return nil
}

// executeCloseShortWithRecord 执行平空仓并记录详细信息
func (at *AutoTrader) executeCloseShortWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("  🔄 平空仓: %s", decision.Symbol)

	// 获取当前价格
	marketData, err := market.Get(decision.Symbol)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// 平仓
	order, err := at.trader.CloseShort(decision.Symbol, 0) // 0 = 全部平仓
	if err != nil {
		return err
	}

	// 记录订单ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	log.Printf("  ✓ 平仓成功")
	return nil
}

// executeUpdateStopLossWithRecord 执行调整止损并记录详细信息
func (at *AutoTrader) executeUpdateStopLossWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("  🎯 调整止损: %s → %.2f", decision.Symbol, decision.NewStopLoss)

	// 获取当前价格
	marketData, err := market.Get(decision.Symbol)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// 获取当前持仓
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("获取持仓失败: %w", err)
	}

	// 查找目标持仓
	var targetPosition map[string]interface{}
	for _, pos := range positions {
		symbol, _ := pos["symbol"].(string)
		posAmt, _ := pos["positionAmt"].(float64)
		if symbol == decision.Symbol && posAmt != 0 {
			targetPosition = pos
			break
		}
	}

	if targetPosition == nil {
		return fmt.Errorf("持仓不存在: %s", decision.Symbol)
	}

	// 获取持仓方向和数量
	side, _ := targetPosition["side"].(string)
	positionSide := strings.ToUpper(side)
	positionAmt, _ := targetPosition["positionAmt"].(float64)

	// 验证新止损价格合理性
	if positionSide == "LONG" && decision.NewStopLoss >= marketData.CurrentPrice {
		return fmt.Errorf("多单止损必须低于当前价格 (当前: %.2f, 新止损: %.2f)", marketData.CurrentPrice, decision.NewStopLoss)
	}
	if positionSide == "SHORT" && decision.NewStopLoss <= marketData.CurrentPrice {
		return fmt.Errorf("空单止损必须高于当前价格 (当前: %.2f, 新止损: %.2f)", marketData.CurrentPrice, decision.NewStopLoss)
	}

	// ⚠️ 防御性检查：检测是否存在双向持仓（不应该出现，但提供保护）
	var hasOppositePosition bool
	oppositeSide := ""
	for _, pos := range positions {
		symbol, _ := pos["symbol"].(string)
		posSide, _ := pos["side"].(string)
		posAmt, _ := pos["positionAmt"].(float64)
		if symbol == decision.Symbol && posAmt != 0 && strings.ToUpper(posSide) != positionSide {
			hasOppositePosition = true
			oppositeSide = strings.ToUpper(posSide)
			break
		}
	}

	if hasOppositePosition {
		log.Printf("  🚨 警告：检测到 %s 存在双向持仓（%s + %s），这违反了策略规则",
			decision.Symbol, positionSide, oppositeSide)
		log.Printf("  🚨 取消止损单将影响两个方向的订单，请检查是否为用户手动操作导致")
		log.Printf("  🚨 建议：手动平掉其中一个方向的持仓，或检查系统是否有BUG")
	}

	// 取消旧的止损单（只删除止损单，不影响止盈单）
	// 注意：如果存在双向持仓，这会删除两个方向的止损单
	if err := at.trader.CancelStopLossOrders(decision.Symbol); err != nil {
		log.Printf("  ⚠ 取消旧止损单失败: %v", err)
		// 不中断执行，继续设置新止损
	}

	// 调用交易所 API 修改止损
	quantity := math.Abs(positionAmt)
	err = at.trader.SetStopLoss(decision.Symbol, positionSide, quantity, decision.NewStopLoss)
	if err != nil {
		return fmt.Errorf("修改止损失败: %w", err)
	}

	log.Printf("  ✓ 止损已调整: %.2f (当前价格: %.2f)", decision.NewStopLoss, marketData.CurrentPrice)
	return nil
}

// executeUpdateTakeProfitWithRecord 执行调整止盈并记录详细信息
func (at *AutoTrader) executeUpdateTakeProfitWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("  🎯 调整止盈: %s → %.2f", decision.Symbol, decision.NewTakeProfit)

	// 获取当前价格
	marketData, err := market.Get(decision.Symbol)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// 获取当前持仓
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("获取持仓失败: %w", err)
	}

	// 查找目标持仓
	var targetPosition map[string]interface{}
	for _, pos := range positions {
		symbol, _ := pos["symbol"].(string)
		posAmt, _ := pos["positionAmt"].(float64)
		if symbol == decision.Symbol && posAmt != 0 {
			targetPosition = pos
			break
		}
	}

	if targetPosition == nil {
		return fmt.Errorf("持仓不存在: %s", decision.Symbol)
	}

	// 获取持仓方向和数量
	side, _ := targetPosition["side"].(string)
	positionSide := strings.ToUpper(side)
	positionAmt, _ := targetPosition["positionAmt"].(float64)

	// 验证新止盈价格合理性
	if positionSide == "LONG" && decision.NewTakeProfit <= marketData.CurrentPrice {
		return fmt.Errorf("多单止盈必须高于当前价格 (当前: %.2f, 新止盈: %.2f)", marketData.CurrentPrice, decision.NewTakeProfit)
	}
	if positionSide == "SHORT" && decision.NewTakeProfit >= marketData.CurrentPrice {
		return fmt.Errorf("空单止盈必须低于当前价格 (当前: %.2f, 新止盈: %.2f)", marketData.CurrentPrice, decision.NewTakeProfit)
	}

	// ⚠️ 防御性检查：检测是否存在双向持仓（不应该出现，但提供保护）
	var hasOppositePosition bool
	oppositeSide := ""
	for _, pos := range positions {
		symbol, _ := pos["symbol"].(string)
		posSide, _ := pos["side"].(string)
		posAmt, _ := pos["positionAmt"].(float64)
		if symbol == decision.Symbol && posAmt != 0 && strings.ToUpper(posSide) != positionSide {
			hasOppositePosition = true
			oppositeSide = strings.ToUpper(posSide)
			break
		}
	}

	if hasOppositePosition {
		log.Printf("  🚨 警告：检测到 %s 存在双向持仓（%s + %s），这违反了策略规则",
			decision.Symbol, positionSide, oppositeSide)
		log.Printf("  🚨 取消止盈单将影响两个方向的订单，请检查是否为用户手动操作导致")
		log.Printf("  🚨 建议：手动平掉其中一个方向的持仓，或检查系统是否有BUG")
	}

	// 取消旧的止盈单（只删除止盈单，不影响止损单）
	// 注意：如果存在双向持仓，这会删除两个方向的止盈单
	if err := at.trader.CancelTakeProfitOrders(decision.Symbol); err != nil {
		log.Printf("  ⚠ 取消旧止盈单失败: %v", err)
		// 不中断执行，继续设置新止盈
	}

	// 调用交易所 API 修改止盈
	quantity := math.Abs(positionAmt)
	err = at.trader.SetTakeProfit(decision.Symbol, positionSide, quantity, decision.NewTakeProfit)
	if err != nil {
		return fmt.Errorf("修改止盈失败: %w", err)
	}

	log.Printf("  ✓ 止盈已调整: %.2f (当前价格: %.2f)", decision.NewTakeProfit, marketData.CurrentPrice)
	return nil
}

// executePartialCloseWithRecord 执行部分平仓并记录详细信息
func (at *AutoTrader) executePartialCloseWithRecord(decision *decision.Decision, actionRecord *logger.DecisionAction) error {
	log.Printf("  📊 部分平仓: %s %.1f%%", decision.Symbol, decision.ClosePercentage)

	// 验证百分比范围
	if decision.ClosePercentage <= 0 || decision.ClosePercentage > 100 {
		return fmt.Errorf("平仓百分比必须在 0-100 之间，当前: %.1f", decision.ClosePercentage)
	}

	// 获取当前价格
	marketData, err := market.Get(decision.Symbol)
	if err != nil {
		return err
	}
	actionRecord.Price = marketData.CurrentPrice

	// 获取当前持仓
	positions, err := at.trader.GetPositions()
	if err != nil {
		return fmt.Errorf("获取持仓失败: %w", err)
	}

	// 查找目标持仓
	var targetPosition map[string]interface{}
	for _, pos := range positions {
		symbol, _ := pos["symbol"].(string)
		posAmt, _ := pos["positionAmt"].(float64)
		if symbol == decision.Symbol && posAmt != 0 {
			targetPosition = pos
			break
		}
	}

	if targetPosition == nil {
		return fmt.Errorf("持仓不存在: %s", decision.Symbol)
	}

	// 获取持仓方向和数量
	side, _ := targetPosition["side"].(string)
	positionSide := strings.ToUpper(side)
	positionAmt, _ := targetPosition["positionAmt"].(float64)

	// 计算平仓数量
	totalQuantity := math.Abs(positionAmt)
	closeQuantity := totalQuantity * (decision.ClosePercentage / 100.0)
	actionRecord.Quantity = closeQuantity

	// ✅ Layer 2: 最小仓位检查（避免产生无法成交的小额剩余）
	markPrice, ok := targetPosition["markPrice"].(float64)
	if !ok || markPrice <= 0 {
		return fmt.Errorf("无法解析当前价格，无法执行最小仓位检查")
	}

	currentPositionValue := totalQuantity * markPrice
	remainingQuantity := totalQuantity - closeQuantity
	remainingValue := remainingQuantity * markPrice

	const MIN_POSITION_VALUE = 10.0 // 交易所最小名义价值底线
	if remainingValue > 0 && remainingValue <= MIN_POSITION_VALUE {
		log.Printf("⚠️ 检测到 partial_close 后剩余仓位 %.2f USDT ≤ %.0f USDT，自动改为全平",
			remainingValue, MIN_POSITION_VALUE)
		log.Printf("  → 当前仓位价值: %.2f USDT, 平仓 %.1f%%, 剩余: %.2f USDT",
			currentPositionValue, decision.ClosePercentage, remainingValue)

		if positionSide == "LONG" {
			decision.Action = "close_long"
			return at.executeCloseLongWithRecord(decision, actionRecord)
		}
		decision.Action = "close_short"
		return at.executeCloseShortWithRecord(decision, actionRecord)
	}

	// 执行平仓
	var order map[string]interface{}
	// 先进行数量步长校验：若格式化后为0，则回退为“全平”，避免交易所 -4003 错误
	formattedQtyStr, fmtErr := at.trader.FormatQuantity(decision.Symbol, closeQuantity)
	if fmtErr != nil {
		return fmt.Errorf("部分平仓失败（数量格式化失败）: %w", fmtErr)
	}
	formattedQty, _ := strconv.ParseFloat(formattedQtyStr, 64)
	if formattedQty <= 0 {
		log.Printf("⚠️ partial_close 数量 %.8f 低于 LOT_SIZE 步长，自动回退为全平以避免下单失败", closeQuantity)
		if positionSide == "LONG" {
			decision.Action = "close_long"
			return at.executeCloseLongWithRecord(decision, actionRecord)
		}
		decision.Action = "close_short"
		return at.executeCloseShortWithRecord(decision, actionRecord)
	}

	if positionSide == "LONG" {
		order, err = at.trader.CloseLong(decision.Symbol, formattedQty)
	} else {
		order, err = at.trader.CloseShort(decision.Symbol, formattedQty)
	}

	if err != nil {
		return fmt.Errorf("部分平仓失败: %w", err)
	}

	// 记录订单ID
	if orderID, ok := order["orderId"].(int64); ok {
		actionRecord.OrderID = orderID
	}

	log.Printf("  ✓ 部分平仓成功: 平仓 %.4f (%.1f%%), 剩余 %.4f",
		closeQuantity, decision.ClosePercentage, remainingQuantity)

	// ✅ 恢复剩余仓位的止损/止盈（部分平仓后交易所可能取消原保护单）
	if decision.NewStopLoss > 0 {
		log.Printf("  → 为剩余仓位 %.4f 恢复止损单: %.2f", remainingQuantity, decision.NewStopLoss)
		if err := at.trader.SetStopLoss(decision.Symbol, positionSide, remainingQuantity, decision.NewStopLoss); err != nil {
			log.Printf("  ⚠️ 恢复止损失败: %v（不影响平仓结果）", err)
		}
	}
	if decision.NewTakeProfit > 0 {
		log.Printf("  → 为剩余仓位 %.4f 恢复止盈单: %.2f", remainingQuantity, decision.NewTakeProfit)
		if err := at.trader.SetTakeProfit(decision.Symbol, positionSide, remainingQuantity, decision.NewTakeProfit); err != nil {
			log.Printf("  ⚠️ 恢复止盈失败: %v（不影响平仓结果）", err)
		}
	}
	if decision.NewStopLoss <= 0 && decision.NewTakeProfit <= 0 {
		log.Printf("  ⚠️⚠️⚠️ 警告: 部分平仓后AI未提供新的止盈止损价格")
		log.Printf("  → 剩余仓位 %.4f (价值 %.2f USDT) 目前没有止盈止损保护", remainingQuantity, remainingValue)
		log.Printf("  → 建议: 在 partial_close 决策中包含 new_stop_loss 和 new_take_profit 字段")
	}

	return nil
}

// GetID 获取trader ID
func (at *AutoTrader) GetID() string {
	return at.id
}

// GetName 获取trader名称
func (at *AutoTrader) GetName() string {
	return at.name
}

// GetAIModel 获取AI模型
func (at *AutoTrader) GetAIModel() string {
	return at.aiModel
}

// GetExchange 获取交易所
func (at *AutoTrader) GetExchange() string {
	return at.exchange
}

// SetCustomPrompt 设置自定义交易策略prompt
func (at *AutoTrader) SetCustomPrompt(prompt string) {
	at.customPrompt = prompt
}

// SetOverrideBasePrompt 设置是否覆盖基础prompt
func (at *AutoTrader) SetOverrideBasePrompt(override bool) {
	at.overrideBasePrompt = override
}

// SetSystemPromptTemplate 设置系统提示词模板
func (at *AutoTrader) SetSystemPromptTemplate(templateName string) {
	at.systemPromptTemplate = templateName
}

// GetSystemPromptTemplate 获取当前系统提示词模板名称
func (at *AutoTrader) GetSystemPromptTemplate() string {
	return at.systemPromptTemplate
}

// GetDecisionLogger 获取决策日志记录器
func (at *AutoTrader) GetDecisionLogger() *logger.DecisionLogger {
	return at.decisionLogger
}

// GetIncludeNews 是否在决策上下文中包含新闻
func (at *AutoTrader) GetIncludeNews() bool { return at.config.IncludeNews }

// GetTradingCoins 获取该交易员配置的交易币种列表
func (at *AutoTrader) GetTradingCoins() []string {
	return at.tradingCoins
}
// GetTakerFeeRate 获取交易员账户在当前交易所下的 symbol taker 费率（若底层实现支持）；否则返回0
func (at *AutoTrader) GetTakerFeeRate(symbol string) float64 {
	type feeProvider interface {
		GetTakerFeeRate(symbol string) (float64, error)
	}
	if fp, ok := at.trader.(feeProvider); ok && fp != nil {
		if r, err := fp.GetTakerFeeRate(symbol); err == nil && r > 0 {
			return r
		}
	}
	return 0
}

// GetUserTrades 从底层交易器（若支持）拉取指定 symbol 的用户成交
func (at *AutoTrader) GetUserTrades(symbol string, start, end time.Time, limit int) ([]StdUMTrade, error) {
	type tradeProvider interface {
		GetUserTrades(symbol string, start, end time.Time, limit int) ([]StdUMTrade, error)
	}
	if tp, ok := at.trader.(tradeProvider); ok && tp != nil {
		return tp.GetUserTrades(symbol, start, end, limit)
	}
	return nil, fmt.Errorf("underlying trader does not support user trades")
}

// CancelOrder 通过底层交易器取消指定订单
func (at *AutoTrader) CancelOrder(symbol string, orderID int64) error {
	if at.trader == nil {
		return fmt.Errorf("underlying trader is nil")
	}
	return at.trader.CancelOrder(symbol, orderID)
}

// LogAnomalyAction 记录异常监控触发的紧急操作到决策日志（用于前端“最近决策”展示）
func (at *AutoTrader) LogAnomalyAction(symbol, action string, price float64, extra map[string]interface{}) {
	if at.decisionLogger == nil {
		return
	}
	act := logger.DecisionAction{
		Action:    action,
		Symbol:    symbol,
		Quantity:  0,
		Leverage:  0,
		Price:     price,
		OrderID:   0,
		Timestamp: time.Now(),
		Success:   true,
		Error:     "",
	}
	if extra != nil {
		if q, ok := extra["quantity"].(float64); ok {
			act.Quantity = q
		}
		if lev, ok := extra["leverage"].(int); ok {
			act.Leverage = lev
		}
		switch v := extra["order_id"].(type) {
		case int64:
			act.OrderID = v
		case float64:
			act.OrderID = int64(v)
		case int:
			act.OrderID = int64(v)
		}
	}
	rec := &logger.DecisionRecord{
		Source:       "anomaly",
		InputPrompt:  fmt.Sprintf("[ANOMALY] %s %s", symbol, action),
		DecisionJSON: "{}",
		Decisions:    []logger.DecisionAction{act},
		Success:      true,
	}
	if extra != nil {
		if note, ok := extra["note"].(string); ok && note != "" {
			rec.ExecutionLog = []string{"LLM: " + note}
		}
		if ip, ok := extra["input_prompt"].(string); ok && ip != "" {
			rec.InputPrompt = ip
		}
		if cot, ok := extra["cot_trace"].(string); ok && cot != "" {
			rec.CoTTrace = cot
		}
	}
	_ = at.decisionLogger.LogDecision(rec)
}

// GetStatus 获取系统状态（用于API）
func (at *AutoTrader) GetStatus() map[string]interface{} {
	aiProvider := "DeepSeek"
	if at.config.UseQwen {
		aiProvider = "Qwen"
	}

	status := map[string]interface{}{
		"trader_id":       at.id,
		"trader_name":     at.name,
		"ai_model":        at.aiModel,
		"exchange":        at.exchange,
		"is_running":      at.isRunning,
		"start_time":      at.startTime.Format(time.RFC3339),
		"runtime_minutes": int(time.Since(at.startTime).Minutes()),
		"call_count":      at.callCount,
		"initial_balance": at.initialBalance,
		"scan_interval":   at.config.ScanInterval.String(),
		"stop_until":      at.stopUntil.Format(time.RFC3339),
		"last_reset_time": at.lastResetTime.Format(time.RFC3339),
		"ai_provider":     aiProvider,
	}

	// 注入策略路由信息
	if at.router != nil {
		status["strategy_routing"] = at.router.GetRoutesSnapshot()
	}

	return status
}

// GetOpenOrders 获取当前所有挂单
// 注意：如果未指定symbol，部分交易所可能不支持一次性获取所有挂单，这里简化处理
func (at *AutoTrader) GetOpenOrders(symbol string) ([]*OpenOrder, error) {
	if symbol == "" {
		// 如果为空，理论上应该遍历 at.tradingCoins
		// 为了避免请求过多，我们暂时只支持单个查询，或者如果底层支持 GetAllOpenOrders 则更好
		// 目前接口只有 GetOpenOrders(symbol)
		// 如果一定要全部，前端最好分开请求，或者在这里做聚合（但会很慢）

		// 策略优化：仅查询 TradingCoins
		var allOrders []*OpenOrder
		// 并发限制？目前按顺序查询，避免实现复杂的并发限流
		for _, sym := range at.tradingCoins {
			orders, err := at.trader.GetOpenOrders(sym)
			if err == nil && len(orders) > 0 {
				allOrders = append(allOrders, orders...)
			}
		}
		return allOrders, nil
	}
	return at.trader.GetOpenOrders(symbol)
}

// GetAccountInfo 获取账户信息（用于API）
func (at *AutoTrader) GetAccountInfo() (map[string]interface{}, error) {
	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("获取余额失败: %w", err)
	}

	// 获取账户字段
	totalWalletBalance := 0.0
	totalUnrealizedProfit := 0.0
	availableBalance := 0.0

	if wallet, ok := balance["totalWalletBalance"].(float64); ok {
		totalWalletBalance = wallet
	}
	if unrealized, ok := balance["totalUnrealizedProfit"].(float64); ok {
		totalUnrealizedProfit = unrealized
	}
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// Total Equity = 钱包余额 + 未实现盈亏
	totalEquity := totalWalletBalance + totalUnrealizedProfit

	// 获取持仓计算总保证金
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("获取持仓失败: %w", err)
	}

	totalMarginUsed := 0.0
	totalUnrealizedPnL := 0.0
	for _, pos := range positions {
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}
		unrealizedPnl := pos["unRealizedProfit"].(float64)
		totalUnrealizedPnL += unrealizedPnl

		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}
		marginUsed := (quantity * markPrice) / float64(leverage)
		totalMarginUsed += marginUsed
	}

	totalPnL := totalEquity - at.initialBalance
	totalPnLPct := 0.0
	if at.initialBalance > 0 {
		totalPnLPct = (totalPnL / at.initialBalance) * 100
	}

	marginUsedPct := 0.0
	if totalEquity > 0 {
		marginUsedPct = (totalMarginUsed / totalEquity) * 100
	}

	return map[string]interface{}{
		// 核心字段
		"total_equity":      totalEquity,           // 账户净值 = wallet + unrealized
		"wallet_balance":    totalWalletBalance,    // 钱包余额（不含未实现盈亏）
		"unrealized_profit": totalUnrealizedProfit, // 未实现盈亏（从API）
		"available_balance": availableBalance,      // 可用余额

		// 盈亏统计
		"total_pnl":            totalPnL,           // 总盈亏 = equity - initial
		"total_pnl_pct":        totalPnLPct,        // 总盈亏百分比
		"total_unrealized_pnl": totalUnrealizedPnL, // 未实现盈亏（从持仓计算）
		"initial_balance":      at.initialBalance,  // 初始余额
		"daily_pnl":            at.dailyPnL,        // 日盈亏

		// 持仓信息
		"position_count":  len(positions),  // 持仓数量
		"margin_used":     totalMarginUsed, // 保证金占用
		"margin_used_pct": marginUsedPct,   // 保证金使用率
	}, nil
}

// ClosePosition 手动平仓（供API调用）
func (at *AutoTrader) ClosePosition(symbol, side string, quantity float64) (map[string]interface{}, error) {
	log.Printf("🔧 手动平仓请求: %s %s 数量=%.4f", symbol, side, quantity)

	var result map[string]interface{}
	var err error

	if side == "long" {
		result, err = at.trader.CloseLong(symbol, quantity)
	} else if side == "short" {
		result, err = at.trader.CloseShort(symbol, quantity)
	} else {
		return nil, fmt.Errorf("无效的持仓方向: %s，必须是 'long' 或 'short'", side)
	}

	if err != nil {
		log.Printf("❌ 手动平仓失败: %v", err)
		return nil, err
	}

	log.Printf("✓ 手动平仓成功: %s %s", symbol, side)
	return result, nil
}

// OpenPosition 开仓（用于异常监控，复用现有执行逻辑）
func (at *AutoTrader) OpenPosition(symbol, side string, positionSizeUSD float64, leverage int, stopLoss, takeProfit float64) (map[string]interface{}, error) {
	log.Printf("🔧 异常监控开仓请求: %s %s, 仓位: $%.2f, 杠杆: %dx", symbol, side, positionSizeUSD, leverage)

	// ⚠️ 关键：检查是否已有同币种同方向持仓，如果有则拒绝开仓（防止仓位叠加超限）
	positions, err := at.trader.GetPositions()
	if err == nil {
		for _, pos := range positions {
			if pos["symbol"] == symbol && pos["side"] == side {
				return nil, fmt.Errorf("❌ %s 已有%s仓，拒绝开仓以防止仓位叠加超限", symbol, side)
			}
		}
	}

	// 获取当前价格
	marketData, err := market.Get(symbol)
	if err != nil {
		return nil, fmt.Errorf("获取市场价格失败: %w", err)
	}

	// 计算数量
	quantity := positionSizeUSD / marketData.CurrentPrice

	// ⚠️ 保证金验证：防止保证金不足错误（code=-2019）
	requiredMargin := positionSizeUSD / float64(leverage)

	balance, err := at.trader.GetBalance()
	if err != nil {
		return nil, fmt.Errorf("获取账户余额失败: %w", err)
	}
	availableBalance := 0.0
	if avail, ok := balance["availableBalance"].(float64); ok {
		availableBalance = avail
	}

	// 手续费估算（Taker费率 0.04%）
	estimatedFee := positionSizeUSD * 0.0004
	totalRequired := requiredMargin + estimatedFee

	if totalRequired > availableBalance {
		return nil, fmt.Errorf("❌ 保证金不足: 需要 %.2f USDT（保证金 %.2f + 手续费 %.2f），可用 %.2f USDT",
			totalRequired, requiredMargin, estimatedFee, availableBalance)
	}

	// 设置仓位模式
	if err := at.trader.SetMarginMode(symbol, at.config.IsCrossMargin); err != nil {
		log.Printf("  ⚠️ 设置仓位模式失败: %v", err)
		// 继续执行，不影响交易
	}

	// 开仓
	var order map[string]interface{}
	if side == "long" {
		order, err = at.trader.OpenLong(symbol, quantity, leverage)
	} else if side == "short" {
		order, err = at.trader.OpenShort(symbol, quantity, leverage)
	} else {
		return nil, fmt.Errorf("无效的持仓方向: %s，必须是 'long' 或 'short'", side)
	}

	if err != nil {
		return nil, fmt.Errorf("开仓失败: %w", err)
	}

	log.Printf("✓ 异常监控开仓成功，订单ID: %v, 数量: %.4f", order["orderId"], quantity)

	// 记录开仓时间
	posKey := symbol + "_" + side
	at.positionFirstSeenTime[posKey] = time.Now().UnixMilli()

	// 设置止损止盈
	if stopLoss > 0 {
		sideUpper := strings.ToUpper(side)
		if err := at.trader.SetStopLoss(symbol, sideUpper, quantity, stopLoss); err != nil {
			log.Printf("  ⚠ 设置止损失败: %v", err)
		}
	}
	if takeProfit > 0 {
		sideUpper := strings.ToUpper(side)
		if err := at.trader.SetTakeProfit(symbol, sideUpper, quantity, takeProfit); err != nil {
			log.Printf("  ⚠ 设置止盈失败: %v", err)
		}
	}

	return order, nil
}

// GetBTCETHLeverage 获取BTC/ETH杠杆配置（用于异常监控）
func (at *AutoTrader) GetBTCETHLeverage() int {
	return at.config.BTCETHLeverage
}

// GetAltcoinLeverage 获取山寨币杠杆配置（用于异常监控）
func (at *AutoTrader) GetAltcoinLeverage() int {
	return at.config.AltcoinLeverage
}

// SetLeverage 设置杠杆（用于异常监控）
func (at *AutoTrader) SetLeverage(symbol string, leverage int) error {
	return at.trader.SetLeverage(symbol, leverage)
}

// SetStopLoss 设置止损（用于异常监控）
func (at *AutoTrader) SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error {
	return at.trader.SetStopLoss(symbol, positionSide, quantity, stopPrice)
}

// GetPositions 获取持仓列表（用于API）
func (at *AutoTrader) GetPositions() ([]map[string]interface{}, error) {
	positions, err := at.trader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("获取持仓失败: %w", err)
	}

	var result []map[string]interface{}
	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity
		}
		unrealizedPnl := pos["unRealizedProfit"].(float64)
		liquidationPrice := pos["liquidationPrice"].(float64)

		leverage := 10
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}

		// 计算占用保证金
		marginUsed := (quantity * markPrice) / float64(leverage)

		// 计算盈亏百分比（基于保证金）
		pnlPct := calculatePnLPercentage(unrealizedPnl, marginUsed)

		// 查询当前止盈/止损（如果交易所支持）
		stopLoss := 0.0
		takeProfit := 0.0
		if at.trader != nil {
			if sl, tp, err := at.trader.GetStopTakePrices(symbol, strings.ToUpper(side)); err == nil {
				stopLoss = sl
				takeProfit = tp
			}
		}

		result = append(result, map[string]interface{}{
			"symbol":             symbol,
			"side":               side,
			"entry_price":        entryPrice,
			"mark_price":         markPrice,
			"quantity":           quantity,
			"leverage":           leverage,
			"unrealized_pnl":     unrealizedPnl,
			"unrealized_pnl_pct": pnlPct,
			"liquidation_price":  liquidationPrice,
			"margin_used":        marginUsed,
			"stop_loss":          stopLoss,
			"take_profit":        takeProfit,
		})
	}

	return result, nil
}

// calculatePnLPercentage 计算盈亏百分比（基于保证金，自动考虑杠杆）
// 收益率 = 未实现盈亏 / 保证金 × 100%
func calculatePnLPercentage(unrealizedPnl, marginUsed float64) float64 {
	if marginUsed > 0 {
		return (unrealizedPnl / marginUsed) * 100
	}
	return 0.0
}

// filterPositionsBySymbols 根据符号列表筛选持仓
func filterPositionsBySymbols(positions []decision.PositionInfo, symbols []string) []decision.PositionInfo {
	symMap := make(map[string]bool)
	for _, s := range symbols {
		symMap[s] = true
	}
	var filtered []decision.PositionInfo
	for _, p := range positions {
		if symMap[p.Symbol] {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

// filterCandidatesBySymbols 根据符号列表筛选候选币种
func filterCandidatesBySymbols(candidates []decision.CandidateCoin, symbols []string) []decision.CandidateCoin {
	symMap := make(map[string]bool)
	for _, s := range symbols {
		symMap[s] = true
	}
	var filtered []decision.CandidateCoin
	for _, c := range candidates {
		if symMap[c.Symbol] {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// sortDecisionsByPriority 对决策进行排序：先平仓后开仓，同类操作按字母顺序
func sortDecisionsByPriority(decisions []decision.Decision) []decision.Decision {
	if len(decisions) <= 1 {
		return decisions
	}

	// 定义优先级
	getActionPriority := func(action string) int {
		switch action {
		case "close_long", "close_short", "partial_close":
			return 1 // 最高优先级：先平仓（包括部分平仓）
		case "update_stop_loss", "update_take_profit":
			return 2 // 调整持仓止盈止损
		case "open_long", "open_short":
			return 3 // 次优先级：后开仓
		case "hold", "wait":
			return 4 // 最低优先级：观望
		default:
			return 999 // 未知动作放最后
		}
	}

	// 复制决策列表
	sorted := make([]decision.Decision, len(decisions))
	copy(sorted, decisions)

	// 按优先级排序
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if getActionPriority(sorted[i].Action) > getActionPriority(sorted[j].Action) {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	return sorted
}

// getCandidateCoins 获取交易员的候选币种列表
func (at *AutoTrader) getCandidateCoins() ([]decision.CandidateCoin, error) {
	if len(at.tradingCoins) == 0 {
		// 使用数据库配置的默认币种列表
		var candidateCoins []decision.CandidateCoin

		if len(at.defaultCoins) > 0 {
			// 使用数据库中配置的默认币种
			for _, coin := range at.defaultCoins {
				symbol := normalizeSymbol(coin)
				candidateCoins = append(candidateCoins, decision.CandidateCoin{
					Symbol:  symbol,
					Sources: []string{"default"}, // 标记为数据库默认币种
				})
			}
			log.Printf("📋 [%s] 使用数据库默认币种: %d个币种 %v",
				at.name, len(candidateCoins), at.defaultCoins)
			return candidateCoins, nil
		} else {
			// 如果数据库中没有配置默认币种，则使用AI500+OI Top作为fallback
			const ai500Limit = 20 // AI500取前20个评分最高的币种

			mergedPool, err := pool.GetMergedCoinPool(ai500Limit)
			if err != nil {
				return nil, fmt.Errorf("获取合并币种池失败: %w", err)
			}

			// 构建候选币种列表（包含来源信息）
			for _, symbol := range mergedPool.AllSymbols {
				sources := mergedPool.SymbolSources[symbol]
				candidateCoins = append(candidateCoins, decision.CandidateCoin{
					Symbol:  symbol,
					Sources: sources, // "ai500" 和/或 "oi_top"
				})
			}

			log.Printf("📋 [%s] 数据库无默认币种配置，使用AI500+OI Top: AI500前%d + OI_Top20 = 总计%d个候选币种",
				at.name, ai500Limit, len(candidateCoins))
			return candidateCoins, nil
		}
	} else {
		// 使用自定义币种列表
		var candidateCoins []decision.CandidateCoin
		for _, coin := range at.tradingCoins {
			// 确保币种格式正确（转为大写USDT交易对）
			symbol := normalizeSymbol(coin)
			candidateCoins = append(candidateCoins, decision.CandidateCoin{
				Symbol:  symbol,
				Sources: []string{"custom"}, // 标记为自定义来源
			})
		}

		log.Printf("📋 [%s] 使用自定义币种: %d个币种 %v",
			at.name, len(candidateCoins), at.tradingCoins)
		return candidateCoins, nil
	}
}

// normalizeSymbol 标准化币种符号（确保以USDT结尾）
func normalizeSymbol(symbol string) string {
	// 转为大写
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	// 确保以USDT结尾
	if !strings.HasSuffix(symbol, "USDT") {
		symbol = symbol + "USDT"
	}

	return symbol
}

// 启动回撤监控
func (at *AutoTrader) startDrawdownMonitor() {
	at.monitorWg.Add(1)
	go func() {
		defer at.monitorWg.Done()

		ticker := time.NewTicker(1 * time.Minute) // 每分钟检查一次
		defer ticker.Stop()

		log.Println("📊 启动持仓回撤监控（每分钟检查一次）")

		for {
			select {
			case <-ticker.C:
				at.checkPositionDrawdown()
			case <-at.stopMonitorCh:
				log.Println("⏹ 停止持仓回撤监控")
				return
			}
		}
	}()
}

// 检查持仓回撤情况
func (at *AutoTrader) checkPositionDrawdown() {
	// 获取当前持仓
	positions, err := at.trader.GetPositions()
	if err != nil {
		log.Printf("❌ 回撤监控：获取持仓失败: %v", err)
		return
	}

	for _, pos := range positions {
		symbol := pos["symbol"].(string)
		side := pos["side"].(string)
		entryPrice := pos["entryPrice"].(float64)
		markPrice := pos["markPrice"].(float64)
		quantity := pos["positionAmt"].(float64)
		if quantity < 0 {
			quantity = -quantity // 空仓数量为负，转为正数
		}

		// 计算当前盈亏百分比
		leverage := 10 // 默认值
		if lev, ok := pos["leverage"].(float64); ok {
			leverage = int(lev)
		}

		var currentPnLPct float64
		if side == "long" {
			currentPnLPct = ((markPrice - entryPrice) / entryPrice) * float64(leverage) * 100
		} else {
			currentPnLPct = ((entryPrice - markPrice) / entryPrice) * float64(leverage) * 100
		}

		// 使用 symbol+side 作为 key，避免多空持仓相互干扰
		key := fmt.Sprintf("%s_%s", symbol, strings.ToLower(side))

		// 若切换方向，清理对侧缓存（避免旧峰值影响回撤判断）
		opposite := "long"
		if strings.ToLower(side) == "long" {
			opposite = "short"
		}
		oppositeKey := fmt.Sprintf("%s_%s", symbol, opposite)
		at.ClearPeakPnLCacheByKey(oppositeKey)

		// 获取该持仓的历史最高收益
		at.peakPnLCacheMutex.RLock()
		peakPnLPct, exists := at.peakPnLCache[key]
		at.peakPnLCacheMutex.RUnlock()

		if !exists {
			// 如果没有历史最高记录，使用当前盈亏作为初始值
			peakPnLPct = currentPnLPct
			at.UpdatePeakPnL(symbol, side, currentPnLPct)
		} else {
			// 更新峰值缓存
			at.UpdatePeakPnL(symbol, side, currentPnLPct)
		}

		// 计算回撤（从最高点下跌的幅度）
		var drawdownPct float64
		if peakPnLPct > 0 && currentPnLPct < peakPnLPct {
			drawdownPct = ((peakPnLPct - currentPnLPct) / peakPnLPct) * 100
		}

		// 检查平仓条件：收益大于5%且回撤超过40%
		if currentPnLPct > 5.0 && drawdownPct >= 40.0 {
			log.Printf("🚨 触发回撤平仓条件: %s %s | 当前收益: %.2f%% | 最高收益: %.2f%% | 回撤: %.2f%%",
				symbol, side, currentPnLPct, peakPnLPct, drawdownPct)

			// 执行平仓
			if err := at.emergencyClosePosition(symbol, side); err != nil {
				log.Printf("❌ 回撤平仓失败 (%s %s): %v", symbol, side, err)
			} else {
				log.Printf("✅ 回撤平仓成功: %s %s", symbol, side)
				// 平仓后清理该 symbol 的两侧缓存
				at.ClearPeakPnLCache(symbol, "long")
				at.ClearPeakPnLCache(symbol, "short")
			}
		} else if currentPnLPct > 5.0 {
			// 记录接近平仓条件的情况（用于调试）
			log.Printf("📊 回撤监控: %s %s | 收益: %.2f%% | 最高: %.2f%% | 回撤: %.2f%%",
				symbol, side, currentPnLPct, peakPnLPct, drawdownPct)
		}
	}
}

// 紧急平仓函数
func (at *AutoTrader) emergencyClosePosition(symbol, side string) error {
	switch side {
	case "long":
		order, err := at.trader.CloseLong(symbol, 0) // 0 = 全部平仓
		if err != nil {
			return err
		}
		log.Printf("✅ 紧急平多仓成功，订单ID: %v", order["orderId"])
	case "short":
		order, err := at.trader.CloseShort(symbol, 0) // 0 = 全部平仓
		if err != nil {
			return err
		}
		log.Printf("✅ 紧急平空仓成功，订单ID: %v", order["orderId"])
	default:
		return fmt.Errorf("未知的持仓方向: %s", side)
	}

	return nil
}

// GetPeakPnLCache 获取最高收益缓存
func (at *AutoTrader) GetPeakPnLCache() map[string]float64 {
	at.peakPnLCacheMutex.RLock()
	defer at.peakPnLCacheMutex.RUnlock()

	// 返回缓存的副本
	cache := make(map[string]float64)
	for k, v := range at.peakPnLCache {
		cache[k] = v
	}
	return cache
}

// UpdatePeakPnL 更新最高收益缓存（按 symbol+side 维度）
func (at *AutoTrader) UpdatePeakPnL(symbol, side string, currentPnLPct float64) {
	at.peakPnLCacheMutex.Lock()
	defer at.peakPnLCacheMutex.Unlock()

	key := fmt.Sprintf("%s_%s", symbol, strings.ToLower(side))
	if peak, exists := at.peakPnLCache[key]; exists {
		// 更新峰值（如果是多头，取较大值；如果是空头，currentPnLPct为负，也要比较）
		if currentPnLPct > peak {
			at.peakPnLCache[key] = currentPnLPct
		}
	} else {
		// 首次记录
		at.peakPnLCache[key] = currentPnLPct
	}
}

// ClearPeakPnLCache 清除指定 symbol+side 的峰值缓存
func (at *AutoTrader) ClearPeakPnLCache(symbol, side string) {
	at.peakPnLCacheMutex.Lock()
	defer at.peakPnLCacheMutex.Unlock()

	key := fmt.Sprintf("%s_%s", symbol, strings.ToLower(side))
	delete(at.peakPnLCache, key)
}

// ClearPeakPnLCacheByKey 通过完整 key 清理（内部使用）
func (at *AutoTrader) ClearPeakPnLCacheByKey(key string) {
	at.peakPnLCacheMutex.Lock()
	delete(at.peakPnLCache, key)
	at.peakPnLCacheMutex.Unlock()
}
