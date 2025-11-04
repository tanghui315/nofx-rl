package market

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"strconv"
	"strings"
)

// Get 获取指定代币的市场数据
func Get(symbol string) (*Data, error) {
	var klines3m, klines4h []Kline
	var err error
	// 标准化symbol
	symbol = Normalize(symbol)
	// 获取3分钟K线数据 (最近10个)
	klines3m, err = WSMonitorCli.GetCurrentKlines(symbol, "3m") // 多获取一些用于计算
	if err != nil {
		return nil, fmt.Errorf("获取3分钟K线失败: %v", err)
	}

	// 获取4小时K线数据 (最近10个)
	klines4h, err = WSMonitorCli.GetCurrentKlines(symbol, "4h") // 多获取用于计算指标
	if err != nil {
		return nil, fmt.Errorf("获取4小时K线失败: %v", err)
	}

	// 计算当前指标 (基于3分钟最新数据)
	currentPrice := klines3m[len(klines3m)-1].Close
	currentEMA20 := calculateEMA(klines3m, 20)
	currentMACD := calculateMACD(klines3m)
	currentRSI7 := calculateRSI(klines3m, 7)

	// 计算价格变化百分比
	// 1小时价格变化 = 20个3分钟K线前的价格
	priceChange1h := 0.0
	if len(klines3m) >= 21 { // 至少需要21根K线 (当前 + 20根前)
		price1hAgo := klines3m[len(klines3m)-21].Close
		if price1hAgo > 0 {
			priceChange1h = ((currentPrice - price1hAgo) / price1hAgo) * 100
		}
	}

	// 4小时价格变化 = 1个4小时K线前的价格
	priceChange4h := 0.0
	if len(klines4h) >= 2 {
		price4hAgo := klines4h[len(klines4h)-2].Close
		if price4hAgo > 0 {
			priceChange4h = ((currentPrice - price4hAgo) / price4hAgo) * 100
		}
	}

	// 获取OI数据
	oiData, err := getOpenInterestData(symbol)
	if err != nil {
		// OI失败不影响整体,使用默认值
		oiData = &OIData{Latest: 0, Average: 0}
	}

	// 获取Funding Rate
	fundingRate, _ := getFundingRate(symbol)

	// 计算日内系列数据
	intradayData := calculateIntradaySeries(klines3m)

	// 计算长期数据
	longerTermData := calculateLongerTermData(klines4h)

	// 计算新增技术指标
	bollingerBands := calculateBollingerBands(klines3m, 20, 2.0)
	adxData := calculateADX(klines4h, 14)
	vwap := calculateVWAP(klines3m)
	multipleEMAs := calculateMultipleEMAs(klines4h)
	stochastic := calculateStochastic(klines3m, 14, 3)
	obv := calculateOBV(klines3m)
	
	// 计算高级功能
	ichimoku := CalculateIchimoku(klines4h, currentPrice)
	fvgAnalysis := DetectFVGs(klines3m, currentPrice)

	data := &Data{
		Symbol:            symbol,
		CurrentPrice:      currentPrice,
		PriceChange1h:     priceChange1h,
		PriceChange4h:     priceChange4h,
		CurrentEMA20:      currentEMA20,
		CurrentMACD:       currentMACD,
		CurrentRSI7:       currentRSI7,
		OpenInterest:      oiData,
		FundingRate:       fundingRate,
		IntradaySeries:    intradayData,
		LongerTermContext: longerTermData,
		
		// 新增指标
		BollingerBands:    bollingerBands,
		ADX:               adxData,
		VWAP:              vwap,
		MultipleEMAs:      multipleEMAs,
		Stochastic:        stochastic,
		OBV:               obv,
		
		// 高级功能
		Ichimoku:          ichimoku,
		FVG:               fvgAnalysis,
	}

	// 计算背离检测（需要在data对象创建后）
	divergence := AnalyzeDivergence(data, klines3m, klines4h)
	data.Divergence = divergence
	
	// 生成语义化分析
	data.Semantics = AnalyzeSemantics(data)

	return data, nil
}

// calculateEMA 计算EMA
func calculateEMA(klines []Kline, period int) float64 {
	if len(klines) < period {
		return 0
	}

	// 计算SMA作为初始EMA
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += klines[i].Close
	}
	ema := sum / float64(period)

	// 计算EMA
	multiplier := 2.0 / float64(period+1)
	for i := period; i < len(klines); i++ {
		ema = (klines[i].Close-ema)*multiplier + ema
	}

	return ema
}

// calculateMACD 计算MACD
func calculateMACD(klines []Kline) float64 {
	if len(klines) < 26 {
		return 0
	}

	// 计算12期和26期EMA
	ema12 := calculateEMA(klines, 12)
	ema26 := calculateEMA(klines, 26)

	// MACD = EMA12 - EMA26
	return ema12 - ema26
}

// calculateRSI 计算RSI
func calculateRSI(klines []Kline, period int) float64 {
	if len(klines) <= period {
		return 0
	}

	gains := 0.0
	losses := 0.0

	// 计算初始平均涨跌幅
	for i := 1; i <= period; i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			gains += change
		} else {
			losses += -change
		}
	}

	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)

	// 使用Wilder平滑方法计算后续RSI
	for i := period + 1; i < len(klines); i++ {
		change := klines[i].Close - klines[i-1].Close
		if change > 0 {
			avgGain = (avgGain*float64(period-1) + change) / float64(period)
			avgLoss = (avgLoss * float64(period-1)) / float64(period)
		} else {
			avgGain = (avgGain * float64(period-1)) / float64(period)
			avgLoss = (avgLoss*float64(period-1) + (-change)) / float64(period)
		}
	}

	if avgLoss == 0 {
		return 100
	}

	rs := avgGain / avgLoss
	rsi := 100 - (100 / (1 + rs))

	return rsi
}

// calculateATR 计算ATR
func calculateATR(klines []Kline, period int) float64 {
	if len(klines) <= period {
		return 0
	}

	trs := make([]float64, len(klines))
	for i := 1; i < len(klines); i++ {
		high := klines[i].High
		low := klines[i].Low
		prevClose := klines[i-1].Close

		tr1 := high - low
		tr2 := math.Abs(high - prevClose)
		tr3 := math.Abs(low - prevClose)

		trs[i] = math.Max(tr1, math.Max(tr2, tr3))
	}

	// 计算初始ATR
	sum := 0.0
	for i := 1; i <= period; i++ {
		sum += trs[i]
	}
	atr := sum / float64(period)

	// Wilder平滑
	for i := period + 1; i < len(klines); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}

	return atr
}

// calculateIntradaySeries 计算日内系列数据
func calculateIntradaySeries(klines []Kline) *IntradayData {
	data := &IntradayData{
		MidPrices:   make([]float64, 0, 10),
		EMA20Values: make([]float64, 0, 10),
		MACDValues:  make([]float64, 0, 10),
		RSI7Values:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
	}

	// 获取最近10个数据点
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		data.MidPrices = append(data.MidPrices, klines[i].Close)

		// 计算每个点的EMA20
		if i >= 19 {
			ema20 := calculateEMA(klines[:i+1], 20)
			data.EMA20Values = append(data.EMA20Values, ema20)
		}

		// 计算每个点的MACD
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}

		// 计算每个点的RSI
		if i >= 7 {
			rsi7 := calculateRSI(klines[:i+1], 7)
			data.RSI7Values = append(data.RSI7Values, rsi7)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	return data
}

// calculateLongerTermData 计算长期数据
func calculateLongerTermData(klines []Kline) *LongerTermData {
	data := &LongerTermData{
		MACDValues:  make([]float64, 0, 10),
		RSI14Values: make([]float64, 0, 10),
	}

	// 计算EMA
	data.EMA20 = calculateEMA(klines, 20)
	data.EMA50 = calculateEMA(klines, 50)

	// 计算ATR
	data.ATR3 = calculateATR(klines, 3)
	data.ATR14 = calculateATR(klines, 14)

	// 计算成交量
	if len(klines) > 0 {
		data.CurrentVolume = klines[len(klines)-1].Volume
		// 计算平均成交量
		sum := 0.0
		for _, k := range klines {
			sum += k.Volume
		}
		data.AverageVolume = sum / float64(len(klines))
	}

	// 计算MACD和RSI序列
	start := len(klines) - 10
	if start < 0 {
		start = 0
	}

	for i := start; i < len(klines); i++ {
		if i >= 25 {
			macd := calculateMACD(klines[:i+1])
			data.MACDValues = append(data.MACDValues, macd)
		}
		if i >= 14 {
			rsi14 := calculateRSI(klines[:i+1], 14)
			data.RSI14Values = append(data.RSI14Values, rsi14)
		}
	}

	return data
}

// getOpenInterestData 获取OI数据
func getOpenInterestData(symbol string) (*OIData, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/openInterest?symbol=%s", symbol)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		OpenInterest string `json:"openInterest"`
		Symbol       string `json:"symbol"`
		Time         int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	oi, _ := strconv.ParseFloat(result.OpenInterest, 64)

	return &OIData{
		Latest:  oi,
		Average: oi * 0.999, // 近似平均值
	}, nil
}

// getFundingRate 获取资金费率
func getFundingRate(symbol string) (float64, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/premiumIndex?symbol=%s", symbol)

	resp, err := http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		Symbol          string `json:"symbol"`
		MarkPrice       string `json:"markPrice"`
		IndexPrice      string `json:"indexPrice"`
		LastFundingRate string `json:"lastFundingRate"`
		NextFundingTime int64  `json:"nextFundingTime"`
		InterestRate    string `json:"interestRate"`
		Time            int64  `json:"time"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	rate, _ := strconv.ParseFloat(result.LastFundingRate, 64)
	return rate, nil
}

// Format 格式化输出市场数据
func Format(data *Data) string {
	var sb strings.Builder

	// 首先输出语义化的技术分析总结
	if data.Semantics != nil {
		sb.WriteString(FormatSemanticAnalysis(data))
		sb.WriteString("\n---\n\n")
	}

	// 然后是详细的技术指标数据
	sb.WriteString("## 📈 Detailed Technical Indicators\n\n")
	
	sb.WriteString(fmt.Sprintf("**Current Price:** %.2f\n\n", data.CurrentPrice))

	// 布林带
	if data.BollingerBands != nil {
		bb := data.BollingerBands
		sb.WriteString("**Bollinger Bands (20, 2):**\n")
		sb.WriteString(fmt.Sprintf("- Upper Band: %.2f\n", bb.Upper))
		sb.WriteString(fmt.Sprintf("- Middle Band: %.2f\n", bb.Middle))
		sb.WriteString(fmt.Sprintf("- Lower Band: %.2f\n", bb.Lower))
		sb.WriteString(fmt.Sprintf("- Band Width: %.2f%%\n", bb.BandWidth))
		sb.WriteString(fmt.Sprintf("- %%B Position: %.2f\n\n", bb.PercentB))
	}

	// 多周期均线
	if data.MultipleEMAs != nil {
		emas := data.MultipleEMAs
		sb.WriteString("**Moving Averages:**\n")
		sb.WriteString(fmt.Sprintf("- EMA5:  %.2f\n", emas.EMA5))
		sb.WriteString(fmt.Sprintf("- EMA10: %.2f\n", emas.EMA10))
		sb.WriteString(fmt.Sprintf("- EMA20: %.2f\n", emas.EMA20))
		sb.WriteString(fmt.Sprintf("- EMA30: %.2f\n", emas.EMA30))
		sb.WriteString(fmt.Sprintf("- EMA50: %.2f\n", emas.EMA50))
		sb.WriteString(fmt.Sprintf("- EMA100: %.2f\n\n", emas.EMA100))
	}

	// ADX趋势强度
	if data.ADX != nil {
		adx := data.ADX
		sb.WriteString("**ADX Trend Strength:**\n")
		sb.WriteString(fmt.Sprintf("- ADX: %.1f ", adx.ADX))
		if adx.ADX > 40 {
			sb.WriteString("(Very Strong Trend)\n")
		} else if adx.ADX > 25 {
			sb.WriteString("(Strong Trend)\n")
		} else if adx.ADX > 20 {
			sb.WriteString("(Trending)\n")
		} else {
			sb.WriteString("(Weak/Sideways)\n")
		}
		sb.WriteString(fmt.Sprintf("- +DI: %.1f\n", adx.PlusDI))
		sb.WriteString(fmt.Sprintf("- -DI: %.1f\n\n", adx.MinusDI))
	}

	// MACD和RSI
	sb.WriteString("**Momentum Indicators:**\n")
	sb.WriteString(fmt.Sprintf("- MACD: %.3f\n", data.CurrentMACD))
	sb.WriteString(fmt.Sprintf("- RSI(7): %.1f\n", data.CurrentRSI7))
	
	// Stochastic
	if data.Stochastic != nil {
		sb.WriteString(fmt.Sprintf("- Stochastic %%K: %.1f\n", data.Stochastic.K))
		sb.WriteString(fmt.Sprintf("- Stochastic %%D: %.1f\n", data.Stochastic.D))
	}
	sb.WriteString("\n")

	// VWAP
	if data.VWAP > 0 {
		vwapDiff := ((data.CurrentPrice - data.VWAP) / data.VWAP) * 100
		sb.WriteString(fmt.Sprintf("**VWAP:** %.2f (Price is %.2f%% %s VWAP)\n\n", 
			data.VWAP, math.Abs(vwapDiff), 
			map[bool]string{true: "above", false: "below"}[vwapDiff > 0]))
	}

	// OBV
	if data.OBV != 0 {
		sb.WriteString(fmt.Sprintf("**On-Balance Volume (OBV):** %.0f\n\n", data.OBV))
	}

	sb.WriteString(fmt.Sprintf("**Open Interest & Funding Rate (%s):**\n", data.Symbol))

	if data.OpenInterest != nil {
		sb.WriteString(fmt.Sprintf("Open Interest: Latest: %.2f Average: %.2f\n\n",
			data.OpenInterest.Latest, data.OpenInterest.Average))
	}

	sb.WriteString(fmt.Sprintf("Funding Rate: %.2e\n\n", data.FundingRate))

	if data.IntradaySeries != nil {
		sb.WriteString("Intraday series (3‑minute intervals, oldest → latest):\n\n")

		if len(data.IntradaySeries.MidPrices) > 0 {
			sb.WriteString(fmt.Sprintf("Mid prices: %s\n\n", formatFloatSlice(data.IntradaySeries.MidPrices)))
		}

		if len(data.IntradaySeries.EMA20Values) > 0 {
			sb.WriteString(fmt.Sprintf("EMA indicators (20‑period): %s\n\n", formatFloatSlice(data.IntradaySeries.EMA20Values)))
		}

		if len(data.IntradaySeries.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.IntradaySeries.MACDValues)))
		}

		if len(data.IntradaySeries.RSI7Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (7‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI7Values)))
		}

		if len(data.IntradaySeries.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14‑Period): %s\n\n", formatFloatSlice(data.IntradaySeries.RSI14Values)))
		}
	}

	if data.LongerTermContext != nil {
		sb.WriteString("Longer‑term context (4‑hour timeframe):\n\n")

		sb.WriteString(fmt.Sprintf("20‑Period EMA: %.3f vs. 50‑Period EMA: %.3f\n\n",
			data.LongerTermContext.EMA20, data.LongerTermContext.EMA50))

		sb.WriteString(fmt.Sprintf("3‑Period ATR: %.3f vs. 14‑Period ATR: %.3f\n\n",
			data.LongerTermContext.ATR3, data.LongerTermContext.ATR14))

		sb.WriteString(fmt.Sprintf("Current Volume: %.3f vs. Average Volume: %.3f\n\n",
			data.LongerTermContext.CurrentVolume, data.LongerTermContext.AverageVolume))

		if len(data.LongerTermContext.MACDValues) > 0 {
			sb.WriteString(fmt.Sprintf("MACD indicators: %s\n\n", formatFloatSlice(data.LongerTermContext.MACDValues)))
		}

		if len(data.LongerTermContext.RSI14Values) > 0 {
			sb.WriteString(fmt.Sprintf("RSI indicators (14‑Period): %s\n\n", formatFloatSlice(data.LongerTermContext.RSI14Values)))
		}
	}

	// 高级功能输出
	sb.WriteString("\n## 🚀 Advanced Technical Analysis\n\n")
	
	// 1. Ichimoku Cloud
	if data.Ichimoku != nil {
		sb.WriteString(formatIchimoku(data.Ichimoku))
	}
	
	// 2. Fair Value Gaps
	if data.FVG != nil {
		sb.WriteString(formatFVG(data.FVG, data.CurrentPrice))
	}
	
	// 3. Divergence Analysis
	if data.Divergence != nil && data.Divergence.HasDivergence {
		sb.WriteString(formatDivergence(data.Divergence))
	}

	return sb.String()
}

// formatFloatSlice 格式化float64切片为字符串
func formatFloatSlice(values []float64) string {
	strValues := make([]string, len(values))
	for i, v := range values {
		strValues[i] = fmt.Sprintf("%.3f", v)
	}
	return "[" + strings.Join(strValues, ", ") + "]"
}

// Normalize 标准化symbol,确保是USDT交易对
func Normalize(symbol string) string {
	symbol = strings.ToUpper(symbol)
	if strings.HasSuffix(symbol, "USDT") {
		return symbol
	}
	return symbol + "USDT"
}

// parseFloat 解析float值
func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case string:
		return strconv.ParseFloat(val, 64)
	case float64:
		return val, nil
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}

// ========================================
// 高级功能格式化输出
// ========================================

// formatIchimoku 格式化Ichimoku Cloud输出
func formatIchimoku(ichimoku *IchimokuCloud) string {
	var sb strings.Builder
	
	sb.WriteString("### ☁️ Ichimoku Cloud Analysis\n\n")
	
	// 五条线
	sb.WriteString("**Core Lines:**\n")
	sb.WriteString(fmt.Sprintf("- Tenkan-sen (Conversion): %.2f\n", ichimoku.Tenkan))
	sb.WriteString(fmt.Sprintf("- Kijun-sen (Base): %.2f\n", ichimoku.Kijun))
	sb.WriteString(fmt.Sprintf("- Senkou Span A: %.2f\n", ichimoku.SenkouSpanA))
	sb.WriteString(fmt.Sprintf("- Senkou Span B: %.2f\n", ichimoku.SenkouSpanB))
	sb.WriteString(fmt.Sprintf("- Chikou Span: %.2f\n\n", ichimoku.ChikouSpan))
	
	// 云的状态
	sb.WriteString("**Cloud Status:**\n")
	cloudEmoji := map[string]string{"green": "🟢", "red": "🔴", "neutral": "⚪"}[ichimoku.CloudColor]
	sb.WriteString(fmt.Sprintf("- Color: %s %s\n", cloudEmoji, strings.ToUpper(ichimoku.CloudColor)))
	sb.WriteString(fmt.Sprintf("- Thickness: %.2f (%.2f%%)\n", ichimoku.CloudThickness, ichimoku.CloudPercent))
	sb.WriteString(fmt.Sprintf("- Support: %.2f | Resistance: %.2f\n\n", ichimoku.CloudBottom, ichimoku.CloudTop))
	
	// 价格位置
	positionEmoji := map[string]string{
		"above_cloud": "⬆️",
		"in_cloud":    "↔️",
		"below_cloud": "⬇️",
	}[ichimoku.PricePosition]
	sb.WriteString("**Price Position:**\n")
	sb.WriteString(fmt.Sprintf("- %s %s (%.2f%% from cloud)\n\n", 
		positionEmoji, 
		strings.ReplaceAll(ichimoku.PricePosition, "_", " "), 
		math.Abs(ichimoku.PriceToCloud)))
	
	// TK交叉
	tkEmoji := map[string]string{"bullish": "🟢", "bearish": "🔴", "neutral": "⚪"}[ichimoku.TKCross]
	sb.WriteString("**TK Cross:**\n")
	sb.WriteString(fmt.Sprintf("- Status: %s %s\n", tkEmoji, strings.ToUpper(ichimoku.TKCross)))
	sb.WriteString(fmt.Sprintf("- Distance: %.2f (%.2f%%)\n\n", math.Abs(ichimoku.TKDistance), ichimoku.TKPercent))
	
	// 综合信号
	signalEmoji := map[string]string{
		"strong_buy":  "🚀",
		"buy":         "🟢",
		"neutral":     "⚪",
		"sell":        "🔴",
		"strong_sell": "💥",
	}[ichimoku.Signal]
	sb.WriteString("**Signal:**\n")
	sb.WriteString(fmt.Sprintf("- %s %s\n", signalEmoji, strings.ToUpper(strings.ReplaceAll(ichimoku.Signal, "_", " "))))
	sb.WriteString(fmt.Sprintf("- Strength: %.0f/100\n", ichimoku.Strength))
	sb.WriteString(fmt.Sprintf("- Confidence: %.0f/100\n\n", ichimoku.Confidence))
	
	return sb.String()
}

// formatFVG 格式化FVG输出
func formatFVG(fvg *FVGAnalysis, currentPrice float64) string {
	var sb strings.Builder
	
	sb.WriteString("### 📦 Fair Value Gaps (FVG) Analysis\n\n")
	
	sb.WriteString(fmt.Sprintf("**Summary:** %d Bullish FVGs, %d Bearish FVGs detected\n\n", 
		len(fvg.BullishFVGs), len(fvg.BearishFVGs)))
	
	// 最近的看涨FVG
	if fvg.NearestBullishFVG != nil {
		sb.WriteString("**Nearest Bullish FVG:**\n")
		formatSingleFVG(&sb, fvg.NearestBullishFVG, currentPrice)
	}
	
	// 最近的看跌FVG
	if fvg.NearestBearishFVG != nil {
		sb.WriteString("**Nearest Bearish FVG:**\n")
		formatSingleFVG(&sb, fvg.NearestBearishFVG, currentPrice)
	}
	
	// 交易信号
	if fvg.Signal != "none" {
		signalEmoji := map[string]string{
			"buy_at_fvg":  "🟢",
			"sell_at_fvg": "🔴",
		}[fvg.Signal]
		sb.WriteString("\n**Trading Signal:**\n")
		sb.WriteString(fmt.Sprintf("- %s %s\n", signalEmoji, strings.ToUpper(strings.ReplaceAll(fvg.Signal, "_", " "))))
		sb.WriteString(fmt.Sprintf("- Target Price: $%.2f\n", fvg.TargetPrice))
		sb.WriteString(fmt.Sprintf("- Confidence: %.0f/100\n", fvg.Confidence))
		sb.WriteString(fmt.Sprintf("- Summary: %s\n", fvg.Summary))
	}
	
	sb.WriteString("\n")
	return sb.String()
}

// formatSingleFVG 格式化单个FVG
func formatSingleFVG(sb *strings.Builder, fvg *FVG, currentPrice float64) {
	sb.WriteString(fmt.Sprintf("- Range: $%.2f - $%.2f (%.2f%% wide)\n", 
		fvg.LowerBound, fvg.UpperBound, fvg.SizePercent))
	sb.WriteString(fmt.Sprintf("- Mid Point: $%.2f\n", fvg.MidPoint))
	sb.WriteString(fmt.Sprintf("- Distance: %.2f%% %s current price\n", 
		math.Abs(fvg.DistancePercent),
		map[bool]string{true: "above", false: "below"}[fvg.MidPoint > currentPrice]))
	
	statusEmoji := map[string]string{
		"unfilled": "⚪",
		"partial":  "🟡",
		"filled":   "🟢",
	}[fvg.Status]
	sb.WriteString(fmt.Sprintf("- Status: %s %s (%.0f%% filled)\n", 
		statusEmoji, strings.ToUpper(fvg.Status), fvg.FilledPercent))
	
	if fvg.IsNearby {
		sb.WriteString("- ⚠️ NEARBY - Price approaching FVG zone\n")
	}
	sb.WriteString("\n")
}

// formatDivergence 格式化背离分析输出
func formatDivergence(divergence *DivergenceAnalysis) string {
	var sb strings.Builder
	
	sb.WriteString("### 🔄 Divergence Analysis\n\n")
	
	sb.WriteString(fmt.Sprintf("**Status:** %s\n\n", divergence.Summary))
	
	// RSI背离
	if divergence.RSIDivergence != nil {
		sb.WriteString("**RSI Divergence:**\n")
		formatSingleDivergence(&sb, divergence.RSIDivergence)
	}
	
	// MACD背离
	if divergence.MACDDivergence != nil {
		sb.WriteString("**MACD Divergence:**\n")
		formatSingleDivergence(&sb, divergence.MACDDivergence)
	}
	
	// OBV背离
	if divergence.OBVDivergence != nil {
		sb.WriteString("**OBV Divergence (Volume):**\n")
		formatSingleDivergence(&sb, divergence.OBVDivergence)
	}
	
	// 综合信号
	if divergence.Signal != "none" {
		signalEmoji := map[string]string{
			"strong_reversal":  "💥",
			"reversal_warning": "⚠️",
			"continuation":     "➡️",
		}[divergence.Signal]
		sb.WriteString("\n**Signal:**\n")
		sb.WriteString(fmt.Sprintf("- %s %s\n", signalEmoji, strings.ToUpper(strings.ReplaceAll(divergence.Signal, "_", " "))))
		sb.WriteString(fmt.Sprintf("- Confidence: %.0f/100\n", divergence.Confidence))
	}
	
	sb.WriteString("\n")
	return sb.String()
}

// formatSingleDivergence 格式化单个背离
func formatSingleDivergence(sb *strings.Builder, div *Divergence) {
	typeEmoji := map[string]string{
		"bullish":        "🟢",
		"bearish":        "🔴",
		"hidden_bullish": "🔵",
		"hidden_bearish": "🟠",
	}[div.Type]
	
	sb.WriteString(fmt.Sprintf("- Type: %s %s\n", typeEmoji, strings.ToUpper(strings.ReplaceAll(div.Type, "_", " "))))
	sb.WriteString(fmt.Sprintf("- Strength: %.0f/100\n", div.Strength))
	sb.WriteString(fmt.Sprintf("- Description: %s\n", div.Description))
	sb.WriteString(fmt.Sprintf("- Price Peaks: %d | Indicator Peaks: %d\n\n", 
		len(div.PricePeaks), len(div.IndicatorPeaks)))
}
