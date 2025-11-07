package market

import (
    "encoding/json"
    "fmt"
    "log"
    "nofx/mcp"
    "reflect"
    "strings"
    "sync"
    "time"
)

type WSMonitor struct {
	wsClient       *WSClient
	combinedClient *CombinedStreamsClient
	symbols        []string
	featuresMap    sync.Map
	alertsChan     chan Alert
	klineDataMap3m sync.Map // 存储每个交易对的K线历史数据
	klineDataMap4h sync.Map // 存储每个交易对的K线历史数据
	tickerDataMap  sync.Map // 存储每个交易对的ticker数据
	batchSize      int
	filterSymbols  sync.Map // 使用sync.Map来存储需要监控的币种和其状态
	symbolStats    sync.Map // 存储币种统计信息
	FilterSymbol   []string //经过筛选的币种

	// 异常监控
	anomalyDetectors map[string]*AnomalyDetector // symbol -> detector
	anomalyEventChan chan *AnomalyEvent          // 异常事件通道
	anomalyEnabled   bool                        // 是否启用异常监控
	detectorMutex    sync.RWMutex                // 保护 anomalyDetectors

	// 决策相关依赖（Phase 3）
	// 注意：不直接导入 config/manager/decision 包以避免循环依赖
	// 配置和依赖通过 Set 方法传递
	traderManager interface{} // *manager.TraderManager（避免导入）
	aiModel       interface{} // *config.AIModelConfig（避免导入）
	mcpClient     *mcp.Client
    anomalyConfig interface{} // *config.AnomalyConfig（避免导入）
    llmEvaluator  AnomalyLLMEvaluatorInterface // LLM评估器接口（避免导入 decision 包）

    // 执行动作冷却（按 symbol）
    lastActionTime       sync.Map // symbol -> time.Time
    actionCooldownMinute int      // 最小执行间隔（分钟）

    // 评估去抖动：限制同一 symbol 在短时间内重复 LLM 评估，避免短时多事件导致多条相似记录
    lastEvalTime   sync.Map // symbol -> time.Time
    evalCooldownMs int      // 毫秒

    // 延迟启用参数（symbols 未加载时的缓冲）
    pendingMu      sync.Mutex
    pendingEnabled bool
    pendingParams  struct {
        priceK        float64
        volM          float64
        consec        float64
        minQuoteUSDT  float64
        coolMin       int
        absFloorPct   float64
        actionCoolMin int
    }
}
type SymbolStats struct {
	LastActiveTime   time.Time
	AlertCount       int
	VolumeSpikeCount int
	LastAlertTime    time.Time
	Score            float64 // 综合评分
}

var WSMonitorCli *WSMonitor
var subKlineTime = []string{"3m", "4h"} // 管理订阅流的K线周期

func NewWSMonitor(batchSize int) *WSMonitor {
    WSMonitorCli = &WSMonitor{
		wsClient:         NewWSClient(),
		combinedClient:   NewCombinedStreamsClient(batchSize),
		alertsChan:       make(chan Alert, 1000),
		batchSize:        batchSize,
		anomalyDetectors: make(map[string]*AnomalyDetector),
        anomalyEventChan: make(chan *AnomalyEvent, 100), // 缓冲100个事件
        anomalyEnabled:   false,                         // 默认关闭，等待配置加载
        evalCooldownMs:   2000,                          // 默认 2s 去抖
    }
    return WSMonitorCli
}

func (m *WSMonitor) Initialize(coins []string) error {
	log.Println("初始化WebSocket监控器...")
	// 获取交易对信息
	apiClient := NewAPIClient()
	// 如果不指定交易对，则使用market市场的所有交易对币种
	if len(coins) == 0 {
		exchangeInfo, err := apiClient.GetExchangeInfo()
		if err != nil {
			return err
		}
		// 筛选永续合约交易对 --仅测试时使用
		//exchangeInfo.Symbols = exchangeInfo.Symbols[0:2]
		for _, symbol := range exchangeInfo.Symbols {
			if symbol.Status == "TRADING" && symbol.ContractType == "PERPETUAL" && strings.ToUpper(symbol.Symbol[len(symbol.Symbol)-4:]) == "USDT" {
				m.symbols = append(m.symbols, symbol.Symbol)
				m.filterSymbols.Store(symbol.Symbol, true)
			}
		}
	} else {
		m.symbols = coins
	}

	log.Printf("找到 %d 个交易对", len(m.symbols))
	// 初始化历史数据
	if err := m.initializeHistoricalData(); err != nil {
		log.Printf("初始化历史数据失败: %v", err)
	}

	return nil
}

func (m *WSMonitor) initializeHistoricalData() error {
	apiClient := NewAPIClient()

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 5) // 限制并发数

	for _, symbol := range m.symbols {
		wg.Add(1)
		semaphore <- struct{}{}

		go func(s string) {
			defer wg.Done()
			defer func() { <-semaphore }()

			// 获取历史K线数据
			klines, err := apiClient.GetKlines(s, "3m", 100)
			if err != nil {
				log.Printf("获取 %s 历史数据失败: %v", s, err)
				return
			}
			if len(klines) > 0 {
				m.klineDataMap3m.Store(s, klines)
				log.Printf("已加载 %s 的历史K线数据-3m: %d 条", s, len(klines))
			}
			// 获取历史K线数据
			klines4h, err := apiClient.GetKlines(s, "4h", 100)
			if err != nil {
				log.Printf("获取 %s 历史数据失败: %v", s, err)
				return
			}
			if len(klines4h) > 0 {
				m.klineDataMap4h.Store(s, klines4h)
				log.Printf("已加载 %s 的历史K线数据-4h: %d 条", s, len(klines4h))
			}
		}(symbol)
	}

	wg.Wait()
	return nil
}

func (m *WSMonitor) Start(coins []string) {
	log.Printf("启动WebSocket实时监控...")
	// 初始化交易对
	err := m.Initialize(coins)
	if err != nil {
		log.Printf("❌ 初始化币种失败: %v", err)
		return
	}

	err = m.combinedClient.Connect()
	if err != nil {
		log.Printf("❌ 批量订阅流失败: %v", err)
		return
	}
	// 订阅所有交易对
	err = m.subscribeAll()
	if err != nil {
		log.Printf("❌ 订阅币种交易对失败: %v", err)
		return
	}

	// 启动异常事件处理器（goroutine）
	go m.handleAnomalyEvents()

	log.Printf("✅ WebSocket实时监控启动完成")
}

// subscribeSymbol 注册监听
func (m *WSMonitor) subscribeSymbol(symbol, st string) []string {
	var streams []string
	stream := fmt.Sprintf("%s@kline_%s", strings.ToLower(symbol), st)
	ch := m.combinedClient.AddSubscriber(stream, 100)
	streams = append(streams, stream)
	go m.handleKlineData(symbol, ch, st)

	return streams
}
func (m *WSMonitor) subscribeAll() error {
	// 执行批量订阅
	log.Println("开始订阅所有交易对...")
	for _, symbol := range m.symbols {
		for _, st := range subKlineTime {
			m.subscribeSymbol(symbol, st)
		}
	}
	for _, st := range subKlineTime {
		err := m.combinedClient.BatchSubscribeKlines(m.symbols, st)
		if err != nil {
			log.Printf("❌ 订阅 %s K线失败: %v", st, err)
			return err
		}
	}
    log.Println("所有交易对订阅完成")

    // 若存在延迟启用的异常监控参数，此处立即创建检测器
    m.pendingMu.Lock()
    if m.pendingEnabled {
        p := m.pendingParams
        m.pendingEnabled = false
        m.pendingMu.Unlock()
        m.EnableAnomalyDetection(p.priceK, p.volM, p.consec, p.minQuoteUSDT, p.coolMin, p.absFloorPct, p.actionCoolMin)
    } else {
        m.pendingMu.Unlock()
    }
    return nil
}

func (m *WSMonitor) handleKlineData(symbol string, ch <-chan []byte, _time string) {
	for data := range ch {
		var klineData KlineWSData
		if err := json.Unmarshal(data, &klineData); err != nil {
			log.Printf("解析Kline数据失败: %v", err)
			continue
		}
		m.processKlineUpdate(symbol, klineData, _time)
	}
}

func (m *WSMonitor) getKlineDataMap(_time string) *sync.Map {
	var klineDataMap *sync.Map
	if _time == "3m" {
		klineDataMap = &m.klineDataMap3m
	} else if _time == "4h" {
		klineDataMap = &m.klineDataMap4h
	} else {
		klineDataMap = &sync.Map{}
	}
	return klineDataMap
}
func (m *WSMonitor) processKlineUpdate(symbol string, wsData KlineWSData, _time string) {
	// 转换WebSocket数据为Kline结构
	kline := Kline{
		OpenTime:  wsData.Kline.StartTime,
		CloseTime: wsData.Kline.CloseTime,
		Trades:    wsData.Kline.NumberOfTrades,
	}
	kline.Open, _ = parseFloat(wsData.Kline.OpenPrice)
	kline.High, _ = parseFloat(wsData.Kline.HighPrice)
	kline.Low, _ = parseFloat(wsData.Kline.LowPrice)
	kline.Close, _ = parseFloat(wsData.Kline.ClosePrice)
	kline.Volume, _ = parseFloat(wsData.Kline.Volume)
	kline.High, _ = parseFloat(wsData.Kline.HighPrice)
	kline.QuoteVolume, _ = parseFloat(wsData.Kline.QuoteVolume)
	kline.TakerBuyBaseVolume, _ = parseFloat(wsData.Kline.TakerBuyBaseVolume)
	kline.TakerBuyQuoteVolume, _ = parseFloat(wsData.Kline.TakerBuyQuoteVolume)
	// 更新K线数据
	var klineDataMap = m.getKlineDataMap(_time)
	value, exists := klineDataMap.Load(symbol)
	var klines []Kline
	isNewKline := false
	if exists {
		klines = value.([]Kline)

		// 检查是否是新的K线
		if len(klines) > 0 && klines[len(klines)-1].OpenTime == kline.OpenTime {
			// 更新当前K线
			klines[len(klines)-1] = kline
		} else {
			// 添加新K线
			klines = append(klines, kline)
			isNewKline = true

			// 保持数据长度
			if len(klines) > 100 {
				klines = klines[1:]
			}
		}
	} else {
		klines = []Kline{kline}
		isNewKline = true
	}

	klineDataMap.Store(symbol, klines)

	// 异常检测（仅在新K线时检测，且仅检测3m周期）
	if isNewKline && _time == "3m" && m.anomalyEnabled {
		m.detectAnomalies(symbol, &kline, klines)
	}
}

func (m *WSMonitor) GetCurrentKlines(symbol string, _time string) ([]Kline, error) {
	// 对每一个进来的symbol检测是否存在内类 是否的话就订阅它
	value, exists := m.getKlineDataMap(_time).Load(symbol)
	if !exists {
		// 如果Ws数据未初始化完成时,单独使用api获取 - 兼容性代码 (防止在未初始化完成是,已经有交易员运行)
		apiClient := NewAPIClient()
		klines, err := apiClient.GetKlines(symbol, _time, 100)
		if err != nil {
			return nil, fmt.Errorf("获取%v分钟K线失败: %v", _time, err)
		}

		// 动态缓存进缓存
		m.getKlineDataMap(_time).Store(strings.ToUpper(symbol), klines)

		// 订阅 WebSocket 流
		subStr := m.subscribeSymbol(symbol, _time)
		subErr := m.combinedClient.subscribeStreams(subStr)
		log.Printf("动态订阅流: %v", subStr)
		if subErr != nil {
			log.Printf("警告: 动态订阅%v分钟K线失败: %v (使用API数据)", _time, subErr)
		}

		// ✅ FIX: 返回深拷贝而非引用
		result := make([]Kline, len(klines))
		copy(result, klines)
		return result, nil
	}

	// ✅ FIX: 返回深拷贝而非引用，避免并发竞态条件
	klines := value.([]Kline)
	result := make([]Kline, len(klines))
	copy(result, klines)
	return result, nil
}

func (m *WSMonitor) Close() {
	m.wsClient.Close()
	close(m.alertsChan)

	// 关闭异常事件通道
	if m.anomalyEventChan != nil {
		close(m.anomalyEventChan)
	}
}

// EnableAnomalyDetection 启用异常监控（从配置加载）
func (m *WSMonitor) EnableAnomalyDetection(
    priceThresholdK float64,
    volumeMultiplierM float64,
    consecutiveThreshold float64,
    minVolumeUSDT float64,
    coolingPeriod int,
    absMinPriceChangePct float64,
    actionCooldownMinutes int,
) {
    m.detectorMutex.Lock()
    defer m.detectorMutex.Unlock()

    if len(m.symbols) == 0 {
        m.pendingMu.Lock()
        m.pendingEnabled = true
        m.pendingParams = struct {
            priceK        float64
            volM          float64
            consec        float64
            minQuoteUSDT  float64
            coolMin       int
            absFloorPct   float64
            actionCoolMin int
        }{priceThresholdK, volumeMultiplierM, consecutiveThreshold, minVolumeUSDT, coolingPeriod, absMinPriceChangePct, actionCooldownMinutes}
        m.pendingMu.Unlock()

        m.anomalyEnabled = true
        if actionCooldownMinutes > 0 { m.actionCooldownMinute = actionCooldownMinutes } else { m.actionCooldownMinute = coolingPeriod }
        log.Printf("⏳ 异常监控已启用（延迟）：监控币种尚未加载，初始化完成后将自动创建检测器")
        return
    }

    // 顶级流动性白名单（按 3m 阈值下调）：BTC/ETH/BNB
    topSymbols := map[string]bool{"BTCUSDT": true, "ETHUSDT": true, "BNBUSDT": true}

    // 根据配置的灵敏度，计算顶级流动币的专用阈值
    topAbsFloor := absMinPriceChangePct
    topConsecutive := consecutiveThreshold
    if ac, ok := m.anomalyConfig.(AnomalyConfigInterface); ok && ac != nil {
        switch ac.GetSensitivity() {
        case "high":
            topAbsFloor = 1.5  // 单根绝对下限 1.5%
            topConsecutive = 0.04 // 连续三根累计 4%
        case "medium":
            topAbsFloor = 1.7
            topConsecutive = 0.05
        case "low":
            topAbsFloor = 2.0
            topConsecutive = 0.06
        }
    }

    // 为每个币种创建检测器（顶级流动币使用更低的绝对下限与连续阈值）
    for _, symbol := range m.symbols {
        absFloor := absMinPriceChangePct
        consec := consecutiveThreshold
        if topSymbols[strings.ToUpper(symbol)] {
            absFloor = topAbsFloor
            consec = topConsecutive
        }
        detector := NewAnomalyDetector(
            symbol,
            priceThresholdK,
            volumeMultiplierM,
            consec,
            minVolumeUSDT,
            coolingPeriod,
            absFloor,
        )
        m.anomalyDetectors[symbol] = detector
    }

    m.anomalyEnabled = true
    if actionCooldownMinutes <= 0 {
        actionCooldownMinutes = coolingPeriod
    }
    m.actionCooldownMinute = actionCooldownMinutes
    log.Printf("✅ 异常监控已启用：监控 %d 个币种", len(m.symbols))
}

// DisableAnomalyDetection 禁用异常监控
func (m *WSMonitor) DisableAnomalyDetection() {
	m.detectorMutex.Lock()
	defer m.detectorMutex.Unlock()

	m.anomalyEnabled = false
	m.anomalyDetectors = make(map[string]*AnomalyDetector)
	log.Printf("⏸️  异常监控已禁用")
}

// detectAnomalies 检测异常（在processKlineUpdate中调用）
func (m *WSMonitor) detectAnomalies(symbol string, currentKline *Kline, recentKlines []Kline) {
	m.detectorMutex.RLock()
	detector, exists := m.anomalyDetectors[symbol]
	m.detectorMutex.RUnlock()

	if !exists {
		return
	}

	// 执行检测（非阻塞）
	go func() {
		events := detector.Detect(currentKline, recentKlines)

		// 将检测到的事件发送到通道
		for _, event := range events {
			select {
			case m.anomalyEventChan <- event:
				// 事件已发送
			default:
				// 通道已满，丢弃事件（避免阻塞）
				log.Printf("⚠️  异常事件通道已满，丢弃事件：%s %s", event.Symbol, event.Type)
			}
		}
	}()
}

// AutoTraderInterface AutoTrader接口（避免导入循环）
type AutoTraderInterface interface {
    GetAccountInfo() (map[string]interface{}, error)
    GetPositions() ([]map[string]interface{}, error)
    ClosePosition(symbol, side string, quantity float64) (map[string]interface{}, error)
    // OpenPosition 开仓（用于异常监控，复用现有执行逻辑）
    OpenPosition(symbol, side string, positionSizeUSD float64, leverage int, stopLoss, takeProfit float64) (map[string]interface{}, error)
    // GetBTCETHLeverage 获取BTC/ETH杠杆配置
    GetBTCETHLeverage() int
    // GetAltcoinLeverage 获取山寨币杠杆配置
    GetAltcoinLeverage() int
    // SetLeverage 设置杠杆（可选，用于异常监控）
    SetLeverage(symbol string, leverage int) error
    // SetStopLoss 设置止损（可选，用于异常监控）
    SetStopLoss(symbol string, positionSide string, quantity, stopPrice float64) error
    // GetStatus 获取交易员状态（包含运行时长和调用次数）
    GetStatus() map[string]interface{}
    // LogAnomalyAction 记录异常处理动作到决策日志
    LogAnomalyAction(symbol, action string, price float64, extra map[string]interface{})
}

// TraderManagerInterface 交易员管理器接口（避免导入循环）
type TraderManagerInterface interface {
	GetTraderIDs() []string
	GetTrader(id string) (AutoTraderInterface, error)
}

// SetDependencies 设置决策相关依赖
func (m *WSMonitor) SetDependencies(
	traderManager interface{}, // *manager.TraderManager（避免导入）
	aiModel interface{}, // *config.AIModelConfig
	mcpClient *mcp.Client,
) {
	m.traderManager = traderManager
	m.aiModel = aiModel
	m.mcpClient = mcpClient
	log.Printf("✅ WSMonitor 依赖已设置")
}

// SetAnomalyConfig 设置异常监控配置（避免导入循环）
func (m *WSMonitor) SetAnomalyConfig(anomalyConfig interface{}) { // *config.AnomalyConfig
	m.anomalyConfig = anomalyConfig
}

// SetLLMEvaluator 设置LLM评估器（避免导入循环）
func (m *WSMonitor) SetLLMEvaluator(evaluator AnomalyLLMEvaluatorInterface) {
	m.llmEvaluator = evaluator
	log.Printf("✅ LLM评估器已设置")
}

// handleAnomalyEvents 处理异常事件（goroutine）
func (m *WSMonitor) handleAnomalyEvents() {
	log.Printf("🎯 异常事件处理器启动")

	for event := range m.anomalyEventChan {
		// 异步处理，避免阻塞
		go m.processAnomalyEvent(event)
	}

	log.Printf("🛑 异常事件处理器停止")
}

// processAnomalyEvent 处理单个异常事件（完整流程）
func (m *WSMonitor) processAnomalyEvent(event *AnomalyEvent) {
	log.Printf("🚨 异常事件：[%s] %s %s (严重程度: %s, 价格变化: %.2f%%, 成交量倍数: %.1fx)",
		event.Symbol, event.Type, event.Direction, event.Severity,
		event.PriceChange3m, event.VolumeRatio)

	// Step 1: 检查配置是否已设置
	if m.anomalyConfig == nil {
		log.Printf("💡 异常监控配置未设置，跳过事件")
		return
	}

	// 使用类型断言获取配置（避免导入循环）
	// 这里需要调用配置的方法，但由于不能导入，我们使用反射或接口
	// 为了简化，我们假设配置已经通过 SetAnomalyConfig 设置
	// 实际使用时需要通过接口或反射调用方法

	// Step 2: 获取所有活跃的trader并处理
	if m.traderManager == nil {
		log.Printf("⚠️  交易员管理器未设置，跳过异常事件处理")
		return
	}

    traderMgr, ok := m.traderManager.(TraderManagerInterface)
    var traderIDs []string
    if ok {
        traderIDs = traderMgr.GetTraderIDs()
    } else {
        log.Printf("⚠️  交易员管理器类型错误，尝试反射方式获取 trader 列表")
        traderIDs = m.listTraderIDsReflect()
    }
    if len(traderIDs) == 0 {
        log.Printf("⚠️  没有活跃的trader，跳过异常事件处理")
        return
    }

	// 为每个trader处理异常事件
    for _, traderID := range traderIDs {
        go m.processAnomalyEventForTrader(event, traderID)
    }
}

// AnomalyConfigInterface 异常配置接口（避免导入循环）
type AnomalyConfigInterface interface {
	IsEnabled() bool
	ShouldTakeAction() bool
	GetMode() string
	GetSensitivity() string
	GetUseLLM() bool
	GetGambitEnabled() bool
	CanGambit() bool
	GetGambitConfig() map[string]interface{}
	GetGambitVolumeThreshold() float64
}

// AnomalyLLMEvaluatorInterface LLM评估器接口（避免导入 decision 包）
// 通过接口调用，解决导入循环问题
type AnomalyLLMEvaluatorInterface interface {
	// Evaluate 评估异常事件并返回决策
	// ctxMap: 决策上下文（map格式，避免导入 decision.Context）
	// 返回: 决策结果（map格式，包含 action, confidence, position_size_usd 等字段）
	Evaluate(event *AnomalyEvent, ctxMap map[string]interface{}, anomalyConfig interface{}, aiModel interface{}) (map[string]interface{}, error)
	// Validate 验证决策的合理性和安全性（4层验证）
	// 返回: 验证错误，nil表示通过
	Validate(decisionMap map[string]interface{}, event *AnomalyEvent, ctxMap map[string]interface{}, anomalyConfig interface{}) error
}

// processAnomalyEventForTrader 为指定trader处理异常事件
func (m *WSMonitor) processAnomalyEventForTrader(
    event *AnomalyEvent,
    traderID string,
) {
    log.Printf("🎯 为trader %s 处理异常事件：%s", traderID, event.Symbol)

    // 检查配置
    if m.anomalyConfig == nil {
        log.Printf("💡 异常监控配置未设置，跳过")
        return
    }

	anomalyConfig, ok := m.anomalyConfig.(AnomalyConfigInterface)
	if !ok {
		log.Printf("⚠️  异常监控配置类型错误")
		return
	}

	if !anomalyConfig.IsEnabled() {
		log.Printf("💡 异常监控未启用，跳过事件")
		return
	}

    if !anomalyConfig.ShouldTakeAction() {
        log.Printf("💡 观察模式：仅记录事件，不执行操作")
        return
    }

    // 获取 trader（接口或通过反射回退），并检查运行状态
    at, err := m.getAutoTraderByID(traderID)
    if err != nil {
        log.Printf("⚠️  未找到 trader %s，跳过异常处理", traderID)
        return
    }
    if st := at.GetStatus(); st != nil {
        if running, ok := st["is_running"].(bool); ok && !running {
            log.Printf("⏭️  跳过异常处理：trader %s 未运行（is_running=false）", traderID)
            return
        }
    }

    // 执行动作冷却：同一 symbol 在最小间隔内只执行一次
    if !m.canExecuteAction(event.Symbol) {
        log.Printf("⏸️  动作冷却中：%s，跳过执行", event.Symbol)
        return
    }

    // 检查是否使用LLM
    if !anomalyConfig.GetUseLLM() {
        // 不使用LLM：使用预设规则
        m.applyRuleBasedDecision(event, anomalyConfig, at, traderID)
        return
    }

    // 评估去抖：若同一 symbol 在 evalCooldownMs 内刚评估过，则跳过评估，避免重复日志
    if m.evalCooldownMs > 0 {
        if v, ok := m.lastEvalTime.Load(event.Symbol); ok {
            if t, ok2 := v.(time.Time); ok2 {
                if time.Since(t) < time.Duration(m.evalCooldownMs)*time.Millisecond {
                    log.Printf("⏸️  评估去抖：%s 距上次评估过短，跳过本次 LLM 评估", event.Symbol)
                    return
                }
            }
        }
        m.lastEvalTime.Store(event.Symbol, time.Now())
    }

    // Step 4: LLM评估（通过接口调用，避免导入循环）
    if m.llmEvaluator == nil {
        log.Printf("⚠️  LLM评估器未设置，使用规则决策")
        m.applyRuleBasedDecision(event, anomalyConfig, at, traderID)
        return
    }

	// 准备决策上下文
    ctxMap, err := m.prepareDecisionContext(event.Symbol, at)
	if err != nil {
		log.Printf("❌ 准备决策上下文失败: %v", err)
        m.applyRuleBasedDecision(event, anomalyConfig, at, traderID)
        return
    }

	// 调用LLM评估（通过接口）
	decisionMap, err := m.llmEvaluator.Evaluate(event, ctxMap, m.anomalyConfig, m.aiModel)
	if err != nil {
		log.Printf("❌ LLM评估失败: %v，使用规则决策", err)
        m.applyRuleBasedDecision(event, anomalyConfig, at, traderID)
        return
    }

	// Step 5: 决策验证（4层验证）
	if err := m.llmEvaluator.Validate(decisionMap, event, ctxMap, m.anomalyConfig); err != nil {
		log.Printf("⚠️  决策验证失败: %v，使用规则决策", err)
        m.applyRuleBasedDecision(event, anomalyConfig, at, traderID)
        return
    }

	// Step 6: 执行LLM决策
    m.executeLLMDecision(decisionMap, event, ctxMap, at, traderID)
}

// getAutoTraderByID 获取实现 AutoTraderInterface 的 trader 实例（优先接口，失败则反射回退）
func (m *WSMonitor) getAutoTraderByID(traderID string) (AutoTraderInterface, error) {
    if tm, ok := m.traderManager.(TraderManagerInterface); ok && tm != nil {
        return tm.GetTrader(traderID)
    }
    // 反射调用 GetTrader(string) (T, error)
    v := reflect.ValueOf(m.traderManager)
    if !v.IsValid() {
        return nil, fmt.Errorf("traderManager invalid")
    }
    mth := v.MethodByName("GetTrader")
    if !mth.IsValid() {
        return nil, fmt.Errorf("GetTrader method not found")
    }
    out := mth.Call([]reflect.Value{reflect.ValueOf(traderID)})
    if len(out) != 2 {
        return nil, fmt.Errorf("GetTrader signature mismatch")
    }
    if !out[1].IsNil() {
        if err, ok := out[1].Interface().(error); ok {
            return nil, err
        }
        return nil, fmt.Errorf("GetTrader returned non-error second value")
    }
    traderVal := out[0]
    if !traderVal.IsValid() || traderVal.IsNil() {
        return nil, fmt.Errorf("nil trader returned")
    }
    if at, ok := traderVal.Interface().(AutoTraderInterface); ok {
        return at, nil
    }
    return nil, fmt.Errorf("trader does not implement AutoTraderInterface")
}

// prepareDecisionContext 准备决策上下文
// 注意：返回 map[string]interface{} 以避免导入 decision 包（解决导入循环）
func (m *WSMonitor) prepareDecisionContext(symbol string, autoTrader AutoTraderInterface) (map[string]interface{}, error) {
	// 获取账户信息
	accountInfoMap, err := autoTrader.GetAccountInfo()
	if err != nil {
		return nil, fmt.Errorf("获取账户信息失败: %w", err)
	}

	// 获取持仓信息
	positions, err := autoTrader.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("获取持仓失败: %w", err)
	}

	// 构建账户信息
	totalEquity := 0.0
	availableBalance := 0.0
	marginUsed := 0.0

	if eq, ok := accountInfoMap["total_equity"].(float64); ok {
		totalEquity = eq
	}
	if avail, ok := accountInfoMap["available_balance"].(float64); ok {
		availableBalance = avail
	}
	if used, ok := accountInfoMap["margin_used"].(float64); ok {
		marginUsed = used
	}

	marginUsedPct := 0.0
	if totalEquity > 0 {
		marginUsedPct = (marginUsed / totalEquity) * 100
	}

	// 构建账户信息（使用 map 结构避免导入 decision 包）
	accountInfo := map[string]interface{}{
		"total_equity":      totalEquity,
		"available_balance": availableBalance,
		"margin_used":       marginUsed,
		"margin_used_pct":   marginUsedPct,
		"position_count":    len(positions),
	}

	// 构建持仓信息（使用 map 结构避免导入 decision 包）
	positionInfos := make([]map[string]interface{}, 0)
	for _, pos := range positions {
		if posSymbol, ok := pos["symbol"].(string); ok && posSymbol == symbol {
			entryPrice := 0.0
			markPrice := 0.0
			quantity := 0.0
			leverage := 0
			unrealizedPnL := 0.0
			unrealizedPnLPct := 0.0

			if ep, ok := pos["entryPrice"].(float64); ok {
				entryPrice = ep
			}
			if mp, ok := pos["markPrice"].(float64); ok {
				markPrice = mp
			}
			if qty, ok := pos["quantity"].(float64); ok {
				quantity = qty
			}
			if lev, ok := pos["leverage"].(int); ok {
				leverage = lev
			}
			if pnl, ok := pos["unrealizedPnL"].(float64); ok {
				unrealizedPnL = pnl
			}
			if pnlPct, ok := pos["unrealizedPnLPct"].(float64); ok {
				unrealizedPnLPct = pnlPct
			}

			side := "long"
			if s, ok := pos["side"].(string); ok {
				side = s
			}

			positionInfos = append(positionInfos, map[string]interface{}{
				"symbol":             posSymbol,
				"side":               side,
				"entry_price":        entryPrice,
				"mark_price":         markPrice,
				"quantity":           quantity,
				"leverage":           leverage,
				"unrealized_pnl":     unrealizedPnL,
				"unrealized_pnl_pct": unrealizedPnLPct,
			})
		}
	}

	// 获取市场数据
	marketData, err := Get(symbol)
	if err != nil {
		return nil, fmt.Errorf("获取市场数据失败: %w", err)
	}

	marketDataMap := make(map[string]*Data)
	marketDataMap[symbol] = marketData

	// 获取交易员的杠杆配置
	btcEthLeverage := autoTrader.GetBTCETHLeverage()
	altcoinLeverage := autoTrader.GetAltcoinLeverage()

	// 获取交易员状态（包含运行时长和调用次数）
	status := autoTrader.GetStatus()
	runtimeMinutes := 0
	callCount := 0
	if rt, ok := status["runtime_minutes"].(int); ok {
		runtimeMinutes = rt
	}
	if cc, ok := status["call_count"].(int); ok {
		callCount = cc
	}

	// 构建Context（使用 map 结构避免导入 decision 包）
	ctx := map[string]interface{}{
		"current_time":     time.Now().Format("2006-01-02 15:04:05"),
		"runtime_minutes":  runtimeMinutes, // 从trader状态获取
		"call_count":       callCount,      // 从trader状态获取
		"account":          accountInfo,
		"positions":        positionInfos,
		"candidate_coins":  []interface{}{},
		"market_data_map":  marketDataMap,
		"oi_top_data_map":  make(map[string]interface{}),
		"btc_eth_leverage": btcEthLeverage,  // 从trader配置获取
		"altcoin_leverage": altcoinLeverage, // 从trader配置获取
	}

	return ctx, nil
}

// applyRuleBasedDecision 基于规则的决策（不使用LLM）
func (m *WSMonitor) applyRuleBasedDecision(
	event *AnomalyEvent,
	anomalyConfig AnomalyConfigInterface,
	autoTrader AutoTraderInterface,
	traderID string,
) {
	log.Printf("📋 使用规则决策（LLM未开启）")

	// 简单规则：
	// - Guard模式 + 持仓反向 → 减仓50%
	// - Aggressive模式 + 严重程度HIGH → 小仓位追单

	mode := anomalyConfig.GetMode()
    switch mode {
    case "guard":
        // 防守模式：只减仓 + 收紧止损（执行实际操作）
        positions, err := autoTrader.GetPositions()
        if err != nil {
            log.Printf("❌ 获取持仓失败: %v", err)
            return
        }
        // 目标减仓比例（固定50%，后续可读取配置）
        reducePct := 50.0
        executed := false
        if event.Direction == "DOWN" { // 多仓受压
            for _, pos := range positions {
                if sym, ok := pos["symbol"].(string); ok && sym == event.Symbol {
                    if side, _ := pos["side"].(string); side == "long" {
                        m.executeReduceLong(event.Symbol, reducePct, autoTrader, traderID)
                        m.tightenStopLoss(event.Symbol, "long", autoTrader)
                        executed = true
                        break
                    }
                }
            }
        } else if event.Direction == "UP" { // 空仓受压
            for _, pos := range positions {
                if sym, ok := pos["symbol"].(string); ok && sym == event.Symbol {
                    if side, _ := pos["side"].(string); side == "short" {
                        m.executeReduceShort(event.Symbol, reducePct, autoTrader, traderID)
                        m.tightenStopLoss(event.Symbol, "short", autoTrader)
                        executed = true
                        break
                    }
                }
            }
        }
        if executed {
            m.markAction(event.Symbol)
        }

	case "balanced", "aggressive":
		// 平衡/激进模式：可以追涨杀跌
		if event.Severity == SeverityHigh && event.PriceChange3m >= 5.0 {
			log.Printf("💡 %s模式：检测到极端行情，建议小仓位追单（规则决策）", mode)
		}
	}
}

// executeLLMDecision 执行LLM决策（通过map格式，避免导入 decision 包）
func (m *WSMonitor) executeLLMDecision(
	decisionMap map[string]interface{},
	event *AnomalyEvent,
	ctxMap map[string]interface{},
	autoTrader AutoTraderInterface,
	traderID string,
) {
	// 提取决策字段
	action, _ := decisionMap["action"].(string)
	shouldAct, _ := decisionMap["should_act"].(bool)
	confidence, _ := decisionMap["confidence"].(float64)
	
	log.Printf("🎯 执行LLM决策：%s %s, 操作=%s, 置信度=%.0f%%, 是否行动=%v",
		event.Symbol, event.Type, action, confidence*100, shouldAct)
	
    if !shouldAct {
        reasoning, _ := decisionMap["reasoning"].(string)
        log.Printf("💡 LLM建议观望：%s", reasoning)
        // 记录到“最近决策”，把 prompt 和 LLM 原文一并写入，便于前端展示
        extra := map[string]interface{}{"note": reasoning}
        if ipt, ok := decisionMap["input_prompt"].(string); ok { extra["input_prompt"] = ipt }
        if cot, ok := decisionMap["cot_trace"].(string); ok { extra["cot_trace"] = cot }
        m.logAnomalyDecision(autoTrader, event.Symbol, "wait", nil, extra)
        return
    }
	
	// 根据action执行相应操作
    switch action {
    case "chase_long":
        m.executeOpenLong(event.Symbol, decisionMap, autoTrader, traderID)
        m.markAction(event.Symbol)
    case "chase_short":
        m.executeOpenShort(event.Symbol, decisionMap, autoTrader, traderID)
        m.markAction(event.Symbol)
    case "reduce_long":
        closePct, _ := decisionMap["close_percentage"].(float64)
        if closePct > 0 {
            m.executeReduceLong(event.Symbol, closePct, autoTrader, traderID)
            m.markAction(event.Symbol)
        }
    case "reduce_short":
        closePct, _ := decisionMap["close_percentage"].(float64)
        if closePct > 0 {
            m.executeReduceShort(event.Symbol, closePct, autoTrader, traderID)
            m.markAction(event.Symbol)
        }
    case "close_all":
        m.executeCloseAll(event.Symbol, autoTrader, traderID)
        m.markAction(event.Symbol)
    case "wait":
        reasoning, _ := decisionMap["reasoning"].(string)
        log.Printf("💡 LLM建议等待：%s", reasoning)
    default:
        log.Printf("⚠️  未知操作：%s", action)
    }

    // 将 prompt/cot 透传给日志（如果 adapter 提供）
    {
        extra := map[string]interface{}{}
        if ipt, ok := decisionMap["input_prompt"].(string); ok { extra["input_prompt"] = ipt }
        if cot, ok := decisionMap["cot_trace"].(string); ok { extra["cot_trace"] = cot }
        // action 已执行或 wait 情况上面已处理；此处仅当 extra 有内容时再补记一条“无动作”记录避免重复
        if len(extra) > 0 && action != "wait" {
            m.logAnomalyDecision(autoTrader, event.Symbol, "context", nil, extra)
        }
    }
}

// tightenStopLoss 收紧剩余持仓的止损（以当前价±2%为参考）
func (m *WSMonitor) tightenStopLoss(symbol, side string, autoTrader AutoTraderInterface) {
    marketData, err := Get(symbol)
    if err != nil {
        log.Printf("⚠️  获取市场数据失败，无法收紧止损: %v", err)
        return
    }
    positions, err := autoTrader.GetPositions()
    if err != nil {
        log.Printf("⚠️  获取持仓失败，无法收紧止损: %v", err)
        return
    }
    var remainQty float64
    for _, p := range positions {
        if sym, ok := p["symbol"].(string); ok && sym == symbol {
            if s, _ := p["side"].(string); s == side {
                if q, ok2 := p["quantity"].(float64); ok2 {
                    remainQty = q
                }
            }
        }
    }
    if remainQty <= 0 {
        return
    }
    var stop float64
    if side == "long" {
        stop = marketData.CurrentPrice * 0.98
    } else {
        stop = marketData.CurrentPrice * 1.02
    }
    if err := autoTrader.SetStopLoss(symbol, strings.ToUpper(side), remainQty, stop); err != nil {
        log.Printf("⚠️  设置止损失败: %v", err)
    } else {
        log.Printf("🔒 已收紧止损：%s %s 剩余%.4f @ %.4f", symbol, side, remainQty, stop)
    }
}

// 动作冷却工具
func (m *WSMonitor) canExecuteAction(symbol string) bool {
    if m.actionCooldownMinute <= 0 {
        return true
    }
    if v, ok := m.lastActionTime.Load(symbol); ok {
        if t, ok2 := v.(time.Time); ok2 {
            if time.Since(t) < time.Duration(m.actionCooldownMinute)*time.Minute {
                return false
            }
        }
    }
    return true
}

func (m *WSMonitor) markAction(symbol string) {
    m.lastActionTime.Store(symbol, time.Now())
}

// executeOpenLong 执行开多仓（通过接口调用，复用现有执行逻辑）
func (m *WSMonitor) executeOpenLong(
    symbol string,
    decisionMap map[string]interface{}, // 决策结果（map格式）
    autoTrader AutoTraderInterface,
    traderID string,
) {
	// 直接从map中提取决策字段
	positionSizeUSD, _ := decisionMap["position_size_usd"].(float64)
	leverage := 0
	if lev, ok := decisionMap["leverage"].(int); ok {
		leverage = lev
	} else if lev, ok := decisionMap["leverage"].(float64); ok {
		leverage = int(lev)
	}
	stopLoss, _ := decisionMap["stop_loss"].(float64)
	takeProfit, _ := decisionMap["take_profit"].(float64)

	log.Printf("📈 执行开多仓：%s, 仓位: $%.2f, 杠杆: %dx, 止损: %.2f, 止盈: %.2f",
		symbol, positionSizeUSD, leverage, stopLoss, takeProfit)

	// 通过接口调用 OpenPosition，复用现有执行逻辑
    order, err := autoTrader.OpenPosition(symbol, "long", positionSizeUSD, leverage, stopLoss, takeProfit)
    if err != nil {
        log.Printf("❌ 开多仓失败: %v", err)
        return
    }

    log.Printf("✅ 开多仓成功：%s, 订单ID: %v", symbol, order["orderId"])
    m.logAnomalyDecision(autoTrader, symbol, "open_long", order, nil)
}

// executeOpenShort 执行开空仓（通过接口调用，复用现有执行逻辑）
func (m *WSMonitor) executeOpenShort(
	symbol string,
	decisionMap map[string]interface{}, // 决策结果（map格式）
	autoTrader AutoTraderInterface,
	traderID string,
) {
	// 直接从map中提取决策字段
	positionSizeUSD, _ := decisionMap["position_size_usd"].(float64)
	leverage := 0
	if lev, ok := decisionMap["leverage"].(int); ok {
		leverage = lev
	} else if lev, ok := decisionMap["leverage"].(float64); ok {
		leverage = int(lev)
	}
	stopLoss, _ := decisionMap["stop_loss"].(float64)
	takeProfit, _ := decisionMap["take_profit"].(float64)

	log.Printf("📉 执行开空仓：%s, 仓位: $%.2f, 杠杆: %dx, 止损: %.2f, 止盈: %.2f",
		symbol, positionSizeUSD, leverage, stopLoss, takeProfit)

	// 通过接口调用 OpenPosition，复用现有执行逻辑
    order, err := autoTrader.OpenPosition(symbol, "short", positionSizeUSD, leverage, stopLoss, takeProfit)
    if err != nil {
        log.Printf("❌ 开空仓失败: %v", err)
        return
    }

    log.Printf("✅ 开空仓成功：%s, 订单ID: %v", symbol, order["orderId"])
    m.logAnomalyDecision(autoTrader, symbol, "open_short", order, nil)
}

// extractDecisionFields 从决策对象中提取字段（使用反射，避免导入 decision 包）
func (m *WSMonitor) extractDecisionFields(decision interface{}) (map[string]interface{}, bool) {
	// 使用反射访问结构体字段
	v := reflect.ValueOf(decision)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil, false
	}

	result := make(map[string]interface{})

	// 提取 PositionSizeUSD
	if field := v.FieldByName("PositionSizeUSD"); field.IsValid() && field.Kind() == reflect.Float64 {
		result["position_size_usd"] = field.Float()
	}

	// 提取 Leverage
	if field := v.FieldByName("Leverage"); field.IsValid() && field.Kind() == reflect.Int {
		result["leverage"] = int(field.Int())
	}

	// 提取 StopLoss
	if field := v.FieldByName("StopLoss"); field.IsValid() && field.Kind() == reflect.Float64 {
		result["stop_loss"] = field.Float()
	}

	// 提取 TakeProfit
	if field := v.FieldByName("TakeProfit"); field.IsValid() && field.Kind() == reflect.Float64 {
		result["take_profit"] = field.Float()
	}

	return result, true
}

// executeReduceLong 执行减多仓
func (m *WSMonitor) executeReduceLong(
	symbol string,
	closePercentage float64,
	autoTrader AutoTraderInterface,
	traderID string,
) {
	log.Printf("📉 执行减多仓：%s, 平仓比例: %.1f%%", symbol, closePercentage)

	// 获取持仓
	positions, err := autoTrader.GetPositions()
	if err != nil {
		log.Printf("❌ 获取持仓失败: %v", err)
		return
	}

	// 找到多仓
	for _, pos := range positions {
		if posSymbol, ok := pos["symbol"].(string); ok && posSymbol == symbol {
			if side, ok := pos["side"].(string); ok && side == "long" {
				quantity := 0.0
				if qty, ok := pos["quantity"].(float64); ok {
					quantity = qty
				}

				closeQuantity := quantity * (closePercentage / 100.0)

				// 平仓（使用ClosePosition方法）
                result, err := autoTrader.ClosePosition(symbol, "long", closeQuantity)
                if err != nil {
                    log.Printf("❌ 减多仓失败: %v", err)
                    return
                }

                log.Printf("✅ 减多仓成功：%s, 平仓数量: %.4f, 结果: %v", symbol, closeQuantity, result)
                m.logAnomalyDecision(autoTrader, symbol, "partial_close_long", result, map[string]interface{}{"quantity": closeQuantity})
                return
            }
        }
    }

	log.Printf("⚠️  未找到 %s 的多仓", symbol)
}

// executeReduceShort 执行减空仓
func (m *WSMonitor) executeReduceShort(
	symbol string,
	closePercentage float64,
	autoTrader AutoTraderInterface,
	traderID string,
) {
	log.Printf("📈 执行减空仓：%s, 平仓比例: %.1f%%", symbol, closePercentage)

	// 获取持仓
	positions, err := autoTrader.GetPositions()
	if err != nil {
		log.Printf("❌ 获取持仓失败: %v", err)
		return
	}

	// 找到空仓
	for _, pos := range positions {
		if posSymbol, ok := pos["symbol"].(string); ok && posSymbol == symbol {
			if side, ok := pos["side"].(string); ok && side == "short" {
				quantity := 0.0
				if qty, ok := pos["quantity"].(float64); ok {
					quantity = qty
				}

				closeQuantity := quantity * (closePercentage / 100.0)

				// 平仓（使用ClosePosition方法）
                result, err := autoTrader.ClosePosition(symbol, "short", closeQuantity)
                if err != nil {
                    log.Printf("❌ 减空仓失败: %v", err)
                    return
                }

                log.Printf("✅ 减空仓成功：%s, 平仓数量: %.4f, 结果: %v", symbol, closeQuantity, result)
                m.logAnomalyDecision(autoTrader, symbol, "partial_close_short", result, map[string]interface{}{"quantity": closeQuantity})
                return
            }
        }
    }

	log.Printf("⚠️  未找到 %s 的空仓", symbol)
}

// executeCloseAll 执行全平仓
func (m *WSMonitor) executeCloseAll(
	symbol string,
	autoTrader AutoTraderInterface,
	traderID string,
) {
	log.Printf("🛑 执行全平仓：%s", symbol)

	// 获取持仓
	positions, err := autoTrader.GetPositions()
	if err != nil {
		log.Printf("❌ 获取持仓失败: %v", err)
		return
	}

	// 平掉所有该币种的持仓
	for _, pos := range positions {
		if posSymbol, ok := pos["symbol"].(string); ok && posSymbol == symbol {
			side, _ := pos["side"].(string)

			if side == "long" {
                if result, err := autoTrader.ClosePosition(symbol, "long", 0); err != nil {
                    log.Printf("❌ 平多仓失败: %v", err)
                } else {
                    log.Printf("✅ 多仓已全部平仓")
                    m.logAnomalyDecision(autoTrader, symbol, "close_long", result, nil)
                }
            } else if side == "short" {
                if result, err := autoTrader.ClosePosition(symbol, "short", 0); err != nil {
                    log.Printf("❌ 平空仓失败: %v", err)
                } else {
                    log.Printf("✅ 空仓已全部平仓")
                    m.logAnomalyDecision(autoTrader, symbol, "close_short", result, nil)
                }
            }
        }
    }
}

// logAnomalyDecision 将异常触发的执行记录写入决策日志（标记 source=anomaly）
func (m *WSMonitor) logAnomalyDecision(autoTrader AutoTraderInterface, symbol, action string, order map[string]interface{}, extra map[string]interface{}) {
    // 当前价格（可选）
    price := 0.0
    if md, err := Get(symbol); err == nil {
        price = md.CurrentPrice
    }
    // 额外信息：合并 order 信息
    extraMap := map[string]interface{}{}
    for k, v := range extra {
        extraMap[k] = v
    }
    if order != nil {
        if oid, ok := order["orderId"]; ok {
            extraMap["order_id"] = oid
        }
    }
    autoTrader.LogAnomalyAction(symbol, action, price, extraMap)
}

// GetMonitoredCount 返回当前监控的币种数量
func (m *WSMonitor) GetMonitoredCount() int {
    return len(m.symbols)
}

// listTraderIDsReflect 通过反射获取 TraderManager 的 trader 列表
func (m *WSMonitor) listTraderIDsReflect() []string {
    ids := []string{}
    v := reflect.ValueOf(m.traderManager)
    if !v.IsValid() {
        return ids
    }
    // 优先调用 GetTraderIDs() []string
    if mth := v.MethodByName("GetTraderIDs"); mth.IsValid() {
        out := mth.Call(nil)
        if len(out) == 1 {
            if s, ok := out[0].Interface().([]string); ok {
                return s
            }
        }
    }
    // 次选 GetAllTraders() map[string]T
    if mth := v.MethodByName("GetAllTraders"); mth.IsValid() {
        out := mth.Call(nil)
        if len(out) == 1 {
            mv := out[0]
            if mv.IsValid() && mv.Kind() == reflect.Map {
                for _, k := range mv.MapKeys() {
                    if k.Kind() == reflect.String {
                        ids = append(ids, k.Interface().(string))
                    }
                }
            }
        }
    }
    return ids
}

// GetAnomalyEventChan 获取异常事件通道（供其他模块订阅）
func (m *WSMonitor) GetAnomalyEventChan() <-chan *AnomalyEvent {
	return m.anomalyEventChan
}
