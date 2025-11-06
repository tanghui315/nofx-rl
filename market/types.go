package market

import "time"

// Data 市场数据结构
type Data struct {
	Symbol            string
	CurrentPrice      float64
	PriceChange1h     float64 // 1小时价格变化百分比
	PriceChange4h     float64 // 4小时价格变化百分比
	CurrentEMA20      float64
	CurrentMACD       float64
	CurrentRSI7       float64
	OpenInterest      *OIData
	FundingRate       float64
	IntradaySeries    *IntradayData
	LongerTermContext *LongerTermData

	// 新增技术指标
	BollingerBands *BollingerBands // 布林带
	ADX            *ADXData        // 趋势强度
	VWAP           float64         // 成交量加权平均价
	MultipleEMAs   *MultiEMAData   // 多周期均线
	Stochastic     *StochasticData // 随机指标
	OBV            float64         // 能量潮

	// 高级功能
	Divergence *DivergenceAnalysis // 背离检测
	FVG        *FVGAnalysis        // Fair Value Gaps
	Ichimoku   *IchimokuCloud      // 一目均衡表

	// 语义化分析
	Semantics *SemanticAnalysis // 技术分析总结
}

// OIData Open Interest数据
type OIData struct {
	Latest  float64
	Average float64
}

// IntradayData 日内数据(3分钟间隔)
type IntradayData struct {
	MidPrices   []float64
	EMA20Values []float64
	MACDValues  []float64
	RSI7Values  []float64
	RSI14Values []float64
}

// LongerTermData 长期数据(4小时时间框架)
type LongerTermData struct {
	EMA20         float64
	EMA50         float64
	ATR3          float64
	ATR14         float64
	CurrentVolume float64
	AverageVolume float64
	MACDValues    []float64
	RSI14Values   []float64
}

// Binance API 响应结构
type ExchangeInfo struct {
	Symbols []SymbolInfo `json:"symbols"`
}

type SymbolInfo struct {
	Symbol            string `json:"symbol"`
	Status            string `json:"status"`
	BaseAsset         string `json:"baseAsset"`
	QuoteAsset        string `json:"quoteAsset"`
	ContractType      string `json:"contractType"`
	PricePrecision    int    `json:"pricePrecision"`
	QuantityPrecision int    `json:"quantityPrecision"`
}

// BollingerBands 布林带指标
type BollingerBands struct {
	Upper     float64 // 上轨
	Middle    float64 // 中轨（SMA）
	Lower     float64 // 下轨
	BandWidth float64 // 带宽 (Upper - Lower) / Middle
	PercentB  float64 // %B: (Price - Lower) / (Upper - Lower)
}

// ADXData ADX趋势强度指标
type ADXData struct {
	ADX     float64 // ADX值
	PlusDI  float64 // +DI (上升方向指标)
	MinusDI float64 // -DI (下降方向指标)
}

// MultiEMAData 多周期EMA
type MultiEMAData struct {
	EMA5   float64
	EMA10  float64
	EMA20  float64 // 与CurrentEMA20相同，保持一致性
	EMA30  float64
	EMA50  float64
	EMA100 float64
	EMA200 float64
}

// StochasticData 随机指标
type StochasticData struct {
	K float64 // %K值
	D float64 // %D值 (K的移动平均)
}

// SemanticAnalysis 语义化技术分析
type SemanticAnalysis struct {
	// 趋势分析
	TrendDirection string // "bullish", "bearish", "sideways"
	TrendStrength  string // "strong", "moderate", "weak"

	// 动量分析
	MomentumStatus string // "increasing", "decreasing", "neutral"

	// 波动性
	VolatilityLevel string // "high", "normal", "low"

	// 价格位置
	PricePosition string // "overbought", "oversold", "neutral"

	// 均线状态
	EMAAlignment string // "bullish_alignment", "bearish_alignment", "mixed"

	// 关键信号
	KeySignals []string // ["golden_cross", "rsi_oversold", "bb_squeeze", etc]

	// 支撑阻力
	NearestSupport       float64
	NearestResistance    float64
	DistanceToSupport    float64 // 百分比
	DistanceToResistance float64

	// 交易建议
	TradeSetup      string  // "long", "short", "wait"
	ConfidenceScore float64 // 0-100

	// 风险评估
	RiskLevel string // "low", "medium", "high"
}

type Kline struct {
	OpenTime            int64   `json:"openTime"`
	Open                float64 `json:"open"`
	High                float64 `json:"high"`
	Low                 float64 `json:"low"`
	Close               float64 `json:"close"`
	Volume              float64 `json:"volume"`
	CloseTime           int64   `json:"closeTime"`
	QuoteVolume         float64 `json:"quoteVolume"`
	Trades              int     `json:"trades"`
	TakerBuyBaseVolume  float64 `json:"takerBuyBaseVolume"`
	TakerBuyQuoteVolume float64 `json:"takerBuyQuoteVolume"`
}

type KlineResponse []interface{}

type PriceTicker struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

type Ticker24hr struct {
	Symbol             string `json:"symbol"`
	PriceChange        string `json:"priceChange"`
	PriceChangePercent string `json:"priceChangePercent"`
	Volume             string `json:"volume"`
	QuoteVolume        string `json:"quoteVolume"`
}

// 特征数据结构
type SymbolFeatures struct {
	Symbol           string    `json:"symbol"`
	Timestamp        time.Time `json:"timestamp"`
	Price            float64   `json:"price"`
	PriceChange15Min float64   `json:"price_change_15min"`
	PriceChange1H    float64   `json:"price_change_1h"`
	PriceChange4H    float64   `json:"price_change_4h"`
	Volume           float64   `json:"volume"`
	VolumeRatio5     float64   `json:"volume_ratio_5"`
	VolumeRatio20    float64   `json:"volume_ratio_20"`
	VolumeTrend      float64   `json:"volume_trend"`
	RSI14            float64   `json:"rsi_14"`
	SMA5             float64   `json:"sma_5"`
	SMA10            float64   `json:"sma_10"`
	SMA20            float64   `json:"sma_20"`
	HighLowRatio     float64   `json:"high_low_ratio"`
	Volatility20     float64   `json:"volatility_20"`
	PositionInRange  float64   `json:"position_in_range"`
}

// 警报数据结构
type Alert struct {
	Type      string    `json:"type"`
	Symbol    string    `json:"symbol"`
	Value     float64   `json:"value"`
	Threshold float64   `json:"threshold"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

type Config struct {
	AlertThresholds AlertThresholds `json:"alert_thresholds"`
	UpdateInterval  int             `json:"update_interval"` // seconds
	CleanupConfig   CleanupConfig   `json:"cleanup_config"`
}

type AlertThresholds struct {
	VolumeSpike      float64 `json:"volume_spike"`
	PriceChange15Min float64 `json:"price_change_15min"`
	VolumeTrend      float64 `json:"volume_trend"`
	RSIOverbought    float64 `json:"rsi_overbought"`
	RSIOversold      float64 `json:"rsi_oversold"`
}
type CleanupConfig struct {
	InactiveTimeout   time.Duration `json:"inactive_timeout"`    // 不活跃超时时间
	MinScoreThreshold float64       `json:"min_score_threshold"` // 最低评分阈值
	NoAlertTimeout    time.Duration `json:"no_alert_timeout"`    // 无警报超时时间
	CheckInterval     time.Duration `json:"check_interval"`      // 检查间隔
}

var config = Config{
	AlertThresholds: AlertThresholds{
		VolumeSpike:      3.0,
		PriceChange15Min: 0.05,
		VolumeTrend:      2.0,
		RSIOverbought:    70,
		RSIOversold:      30,
	},
	CleanupConfig: CleanupConfig{
		InactiveTimeout:   30 * time.Minute,
		MinScoreThreshold: 15.0,
		NoAlertTimeout:    20 * time.Minute,
		CheckInterval:     5 * time.Minute,
	},
	UpdateInterval: 60, // 1 minute
}

// ========================================
// 高级功能数据结构
// ========================================

// Peak 峰值/谷值点
type Peak struct {
	Index int       // K线索引
	Value float64   // 值
	Type  string    // "high" / "low"
	Time  time.Time // 时间
}

// Divergence 背离检测结果
type Divergence struct {
	Type           string    // "bullish" / "bearish" / "hidden_bullish" / "hidden_bearish"
	Indicator      string    // "RSI" / "MACD" / "OBV"
	Strength       float64   // 背离强度 0-100
	PricePeaks     []Peak    // 价格的峰/谷
	IndicatorPeaks []Peak    // 指标的峰/谷
	DetectedAt     time.Time // 检测时间
	Description    string    // 描述
}

// DivergenceAnalysis 完整背离分析
type DivergenceAnalysis struct {
	HasDivergence  bool        // 是否存在背离
	RSIDivergence  *Divergence // RSI背离
	MACDDivergence *Divergence // MACD背离
	OBVDivergence  *Divergence // OBV背离（价量背离）

	// 综合评估
	Signal     string  // "strong_reversal" / "reversal_warning" / "continuation" / "none"
	Confidence float64 // 信心度 0-100
	Summary    string  // 文字总结
}

// FVG Fair Value Gap 数据
type FVG struct {
	Type         string    // "bullish" / "bearish"
	UpperBound   float64   // 上边界
	LowerBound   float64   // 下边界
	MidPoint     float64   // 中点（50%回填位置）
	Size         float64   // 大小（绝对值）
	SizePercent  float64   // 大小（百分比）
	CreatedAt    time.Time // 形成时间
	CreatedIndex int       // K线索引

	// 回填状态
	FilledPercent float64 // 已回填百分比 0-100
	Status        string  // "unfilled" / "partial" / "filled"

	// 距离当前价格
	DistancePercent float64 // 距离当前价格的百分比
	IsNearby        bool    // 是否靠近当前价格（2%范围内）
}

// FVGAnalysis FVG 分析
type FVGAnalysis struct {
	BullishFVGs []FVG // 看涨FVG列表
	BearishFVGs []FVG // 看跌FVG列表

	// 最近的未回填FVG
	NearestBullishFVG *FVG
	NearestBearishFVG *FVG

	// 交易信号
	Signal      string  // "buy_at_fvg" / "sell_at_fvg" / "none"
	TargetPrice float64 // 目标价格（FVG 50%位置）
	Confidence  float64 // 信心度 0-100
	Summary     string  // 文字总结
}

// IchimokuCloud 一目均衡表
type IchimokuCloud struct {
	// 五条核心线
	Tenkan      float64 // 转换线（快线）9期
	Kijun       float64 // 基准线（慢线）26期
	SenkouSpanA float64 // 先行带A（当前）
	SenkouSpanB float64 // 先行带B（当前）
	ChikouSpan  float64 // 延迟线

	// 未来云边界（预测支撑/阻力）
	FutureSenkouA float64 // 26期后的Span A
	FutureSenkouB float64 // 26期后的Span B

	// 云的属性
	CloudColor     string  // "green" / "red" / "neutral"
	CloudTop       float64 // 云的上边界
	CloudBottom    float64 // 云的下边界
	CloudThickness float64 // 云的厚度（绝对值）
	CloudPercent   float64 // 云的厚度（百分比）

	// 价格与云的关系
	PricePosition string  // "above_cloud" / "in_cloud" / "below_cloud"
	PriceToCloud  float64 // 价格距离云的百分比

	// TK交叉
	TKCross    string  // "bullish" / "bearish" / "neutral"
	TKDistance float64 // TK之间的距离（绝对值）
	TKPercent  float64 // TK之间的距离（百分比）

	// Chikou确认
	ChikouPosition string // "above_price" / "below_price" / "neutral"

	// 综合信号
	Signal     string  // "strong_buy" / "buy" / "neutral" / "sell" / "strong_sell"
	Strength   float64 // 信号强度 0-100
	Confidence float64 // 信心度 0-100
}
