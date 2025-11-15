//go:build dev

package api

import (
    "encoding/json"
    "math"
    "net/http"
    "nofx/auth"
    "nofx/logger"
    "nofx/market"
    traderpkg "nofx/trader"
    "os"
    "sort"
    "strconv"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
)

// devLossItem 用于 /api/dev/losses 的返回
type devLossItem struct {
    Symbol        string    `json:"symbol"`
    Side          string    `json:"side"`
    Quantity      float64   `json:"quantity"`
    OpenPrice     float64   `json:"open_price"`
    ClosePrice    float64   `json:"close_price"`
    PnL           float64   `json:"pnl"`
    Duration      string    `json:"duration"`
    OpenTime      time.Time `json:"open_time"`
    CloseTime     time.Time `json:"close_time"`
    OpenDecisionReason string `json:"open_decision_reason,omitempty"`
    OpenCoTExcerpt     string `json:"open_cot_excerpt,omitempty"`
}

// augmentRoutes 在 dev 构建中注册回放相关路由
func augmentRoutes(s *Server, api *gin.RouterGroup, protected *gin.RouterGroup) {
    // 仅在管理员模式并且设置了 REPLAY_TOKEN 时开放
    if !auth.IsAdminMode() {
        return
    }
    token := os.Getenv("REPLAY_TOKEN")
    if token == "" {
        return
    }

    // 使用受保护组（要求用户已登录），再叠加 X-Replay-Token 验证
    group := protected.Group("/anomaly/replay", func(c *gin.Context) {
        if c.GetHeader("X-Replay-Token") != token {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid replay token"})
            c.Abort()
            return
        }
        c.Next()
    })

    group.POST("/start", func(c *gin.Context) {
        var req struct {
            Symbol               string  `json:"symbol"`
            Pattern              string  `json:"pattern"` // price_spike | volume_spike | consecutive
            Interval             string  `json:"interval"` // 仅支持 3m
            Bars                 int     `json:"bars"`
            AbsChangePctPerBar   float64 `json:"abs_change_pct_per_bar"`
            AbsChangePctTotal    float64 `json:"abs_change_pct_total"`
            VolumeX              float64 `json:"volume_x"`
            Mode                 string  `json:"mode"` // stream | oneshot
            SpeedBarPerSecond    float64 `json:"speed"`
            Direction            string  `json:"direction"` // up | down（price_spike 用）
            TTLSeconds           int     `json:"ttl_seconds"`
        }
        if err := c.ShouldBindJSON(&req); err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
            return
        }
        if req.Interval == "" { req.Interval = "3m" }
        if req.Bars <= 0 { req.Bars = 3 }
        if req.SpeedBarPerSecond <= 0 { req.SpeedBarPerSecond = 1 }
        if req.Mode == "" { req.Mode = "stream" }

        err := market.StartReplay(market.ReplayRequest{
            Symbol:             req.Symbol,
            Pattern:            req.Pattern,
            Interval:           req.Interval,
            Bars:               req.Bars,
            AbsChangePctPerBar: req.AbsChangePctPerBar,
            AbsChangePctTotal:  req.AbsChangePctTotal,
            VolumeX:            req.VolumeX,
            Mode:               req.Mode,
            SpeedBarPerSecond:  req.SpeedBarPerSecond,
            Direction:          req.Direction,
            TTL:                time.Duration(req.TTLSeconds) * time.Second,
        })
        if err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
            return
        }
        c.JSON(http.StatusOK, gin.H{"status": "started"})
    })

    group.POST("/stop", func(c *gin.Context) {
        var req struct{ Symbol string `json:"symbol"` }
        _ = c.ShouldBindJSON(&req)
        market.StopReplay(req.Symbol)
        c.JSON(http.StatusOK, gin.H{"status": "stopped"})
    })

    group.GET("/status", func(c *gin.Context) {
        c.JSON(http.StatusOK, market.ReplayStatus())
    })

    // === Dev: 市场亏损调试（仅 dev + 管理员 + REPLAY_TOKEN）===
    dev := protected.Group("/dev", func(c *gin.Context) {
        if c.GetHeader("X-Replay-Token") != token {
            c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid replay token"})
            c.Abort()
            return
        }
        c.Next()
    })

    // GET /api/dev/losses?trader_id=xxx&lookback=500&limit=10&include_ctx=1
    dev.GET("/losses", func(c *gin.Context) {
        _, traderID, err := s.getTraderFromQuery(c)
        if err != nil {
            c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
            return
        }
        tr, err := s.traderManager.GetTrader(traderID)
        if err != nil {
            c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
            return
        }
        lookback := 500
        if v := c.Query("lookback"); v != "" {
            if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
                lookback = n
            }
        }
        limit := 10
        if v := c.Query("limit"); v != "" {
            if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
                limit = n
            }
        }
        includeCtx := c.Query("include_ctx") == "1"

        // 收集决策记录窗口
        records, _ := tr.GetDecisionLogger().GetLatestRecords(lookback)
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
            endTime = time.Now()
            startTime = endTime.Add(-time.Duration(lookback*3) * time.Minute)
        }
        // 允许通过 query symbols=BTCUSDT,ETHUSDT 覆盖；否则用决策符号；再否则用交易员配置的交易币种
        var symbols []string
        if qs := c.Query("symbols"); qs != "" {
            for _, p := range strings.Split(qs, ",") {
                p = strings.ToUpper(strings.TrimSpace(p))
                if p != "" { symbols = append(symbols, p) }
            }
        }
        if len(symbols) == 0 {
            for ssym := range symSet {
                symbols = append(symbols, ssym)
            }
        }
        if len(symbols) == 0 {
            for _, sc := range tr.GetTradingCoins() {
                sc = strings.ToUpper(strings.TrimSpace(sc))
                if strings.HasSuffix(sc, "USDT") {
                    symbols = append(symbols, sc)
                }
            }
        }

        // 交易所优先聚合
        useExchange := len(symbols) > 0
        type key struct{ sym, side string }
        grouped := map[key][]traderpkg.StdUMTrade{}
        for _, sym := range symbols {
            trs, err := tr.GetUserTrades(sym, startTime, endTime, 1000)
            if err != nil {
                useExchange = false
                break
            }
            for _, t := range trs {
                k := key{strings.ToUpper(t.Symbol), strings.ToUpper(t.PositionSide)}
                grouped[k] = append(grouped[k], t)
            }
        }

        var losses []devLossItem

        if useExchange {
            priceOf := func(asset string) (float64, error) {
                a := strings.ToUpper(strings.TrimSpace(asset))
                if a == "" || a == "USDT" {
                    return 1, nil
                }
                data, err := market.Get(a + "USDT")
                if err != nil {
                    return 0, err
                }
                return data.CurrentPrice, nil
            }
            for k, list := range grouped {
                // 按时间升序
                sort.Slice(list, func(i, j int) bool { return list[i].Time.Before(list[j].Time) })
                var qty, entryQty, realized, feeUSDT float64
                var openPrice float64
                var openTime time.Time
                isLong := strings.ToUpper(k.side) == "LONG"
                for _, trd := range list {
                    // 手续费
                    comm := trd.Commission
                    if trd.CommissionAsset != "" && strings.ToUpper(trd.CommissionAsset) != "USDT" {
                        if px, err := priceOf(trd.CommissionAsset); err == nil && px > 0 {
                            comm *= px
                        }
                    }
                    feeUSDT += comm
                    realized += trd.RealizedPnl
                    sideIsBuy := strings.ToUpper(trd.Side) == "BUY"
                    if isLong {
                        if sideIsBuy {
                            if qty <= 0 {
                                openPrice = trd.Price
                                openTime = trd.Time
                            }
                            qty += trd.Qty       // 开多加仓
                            entryQty += trd.Qty  // 计入开仓总量
                        } else {
                            qty -= trd.Qty       // 平多减仓
                        }
                    } else {
                        // SHORT：SELL 开仓，BUY 平仓
                        if !sideIsBuy {
                            if qty <= 0 {
                                openPrice = trd.Price
                                openTime = trd.Time
                            }
                            qty += trd.Qty       // 开空“增加头寸规模”用正数累计
                            entryQty += trd.Qty  // 计入开仓总量
                        } else {
                            qty -= trd.Qty       // 回补多头方向即平空
                        }
                    }
                    // 闭环
                    if qty <= 1e-12 && entryQty > 0 {
                        pnl := realized - feeUSDT
                        if pnl < 0 {
                            item := devLossItem{
                                Symbol:     k.sym,
                                Side:       strings.ToLower(k.side),
                                Quantity:   entryQty,
                                OpenPrice:  openPrice,
                                ClosePrice: trd.Price,
                                PnL:        pnl,
                                Duration:   trd.Time.Sub(openTime).String(),
                                OpenTime:   openTime,
                                CloseTime:  trd.Time,
                            }
                            if includeCtx {
                                enrichLossWithContext(&item, records)
                            }
                            losses = append(losses, item)
                        }
                        // reset
                        qty, entryQty, realized, feeUSDT = 0, 0, 0, 0
                        openPrice, openTime = 0, time.Time{}
                    }
                }
            }
        } else {
            // 日志回退
            perf, err := tr.GetDecisionLogger().AnalyzePerformance(lookback)
            if err == nil && perf != nil {
                for _, t := range perf.RecentTrades {
                    if t.PnL < 0 {
                        item := devLossItem{
                            Symbol:     t.Symbol,
                            Side:       t.Side,
                            Quantity:   t.Quantity,
                            OpenPrice:  t.OpenPrice,
                            ClosePrice: t.ClosePrice,
                            PnL:        t.PnL,
                            Duration:   t.Duration,
                            OpenTime:   t.OpenTime,
                            CloseTime:  t.CloseTime,
                        }
                        if includeCtx {
                            enrichLossWithContext(&item, records)
                        }
                        losses = append(losses, item)
                    }
                }
            }
        }

        sort.Slice(losses, func(i, j int) bool { return losses[i].CloseTime.After(losses[j].CloseTime) })
        if len(losses) > limit {
            losses = losses[:limit]
        }
        c.JSON(http.StatusOK, gin.H{
            "source": func() string { if useExchange { return "exchange" } ; return "log" }(),
            "lookback": lookback,
            "count": len(losses),
            "losses": losses,
        })
    })

    // 读取当前 3m ATR（dev 调试用）
    // GET /api/anomaly/replay/atr?symbol=ETHUSDT[&period=14]
    group.GET("/atr", func(c *gin.Context) {
        sym := c.Query("symbol")
        if sym == "" { c.JSON(http.StatusBadRequest, gin.H{"error":"symbol required"}); return }
        period := 14
        if v := c.Query("period"); v != "" { if n,err:=strconv.Atoi(v); err==nil && n>1 { period=n } }
        norm := market.Normalize(sym)

        // 首选：从 WSMonitor 获取（可能尚未就绪）
        kl, err := market.WSMonitorCli.GetCurrentKlines(norm, "3m")
        if err == nil && len(kl) >= period+1 {
            atr := devCalcATR(kl, period)
            lastClose := kl[len(kl)-1].Close
            atrPct := 0.0
            if lastClose > 0 { atrPct = atr/lastClose*100 }
            c.JSON(http.StatusOK, gin.H{
                "symbol": norm,
                "period": period,
                "close":  lastClose,
                "atr_abs": atr,
                "atr_pct": atrPct,
                "source": "ws_cache",
            })
            return
        }

        // 兜底：直接调用 REST 抓取足量K线，避免冷启动卡顿
        api := market.NewAPIClient()
        limit := period + 50
        if limit < 50 { limit = 50 }
        kl2, err2 := api.GetKlines(norm, "3m", limit)
        if err2 == nil && len(kl2) >= period+1 {
            atr := devCalcATR(kl2, period)
            lastClose := kl2[len(kl2)-1].Close
            atrPct := 0.0
            if lastClose > 0 { atrPct = atr/lastClose*100 }
            c.JSON(http.StatusOK, gin.H{
                "symbol": norm,
                "period": period,
                "close":  lastClose,
                "atr_abs": atr,
                "atr_pct": atrPct,
                "source": "api_fallback",
            })
            return
        }

        c.JSON(http.StatusBadRequest, gin.H{"error": "insufficient klines or fetch error", "detail": err2})
    })
}

// 复制一份 ATR 计算（dev 调试用）
func devCalcATR(klines []market.Kline, period int) float64 {
    if len(klines) < period+1 { return 0 }
    trs := make([]float64, len(klines))
    for i := 1; i < len(klines); i++ {
        cur := klines[i]; prev := klines[i-1]
        tr1 := cur.High - cur.Low
        if tr1 < 0 { tr1 = -tr1 }
        tr2 := math.Abs(cur.High - prev.Close)
        tr3 := math.Abs(cur.Low - prev.Close)
        trs[i] = math.Max(tr1, math.Max(tr2, tr3))
    }
    sum := 0.0
    for i := 1; i <= period; i++ { sum += trs[i] }
    atr := sum/float64(period)
    for i := period+1; i < len(klines); i++ { atr = (atr*float64(period-1) + trs[i]) / float64(period) }
    return atr
}

// 为亏损记录补充开仓时的“reasoning/思维链片段”（从最近的开仓决策日志匹配）
func enrichLossWithContext(item *devLossItem, records []*logger.DecisionRecord) {
    sym := strings.ToUpper(item.Symbol)
    want := "open_long"
    if strings.ToLower(item.Side) == "short" {
        want = "open_short"
    }
    var (
        best *logger.DecisionRecord
        bestDelta = time.Duration(1<<62)
        reason string
    )
    for _, r := range records {
        for _, d := range r.Decisions {
            if !d.Success || strings.ToUpper(d.Symbol) != sym || d.Action != want {
                continue
            }
            delta := item.OpenTime.Sub(d.Timestamp)
            if delta < 0 { delta = -delta }
            if delta < bestDelta {
                bestDelta = delta
                best = r
                // 尝试从决策JSON中解析该动作的reasoning
                if r.DecisionJSON != "" {
                    var arr []map[string]interface{}
                    if json.Unmarshal([]byte(r.DecisionJSON), &arr) == nil {
                        for _, obj := range arr {
                            symVal, _ := obj["symbol"].(string)
                            actVal, _ := obj["action"].(string)
                            if strings.EqualFold(symVal, sym) && actVal == want {
                                if rv, ok := obj["reasoning"].(string); ok {
                                    reason = rv
                                }
                                break
                            }
                        }
                    }
                }
            }
        }
    }
    if best != nil {
        item.OpenDecisionReason = truncateStr(reason, 480)
        item.OpenCoTExcerpt = truncateStr(best.CoTTrace, 480)
    }
}

func truncateStr(s string, n int) string {
    if n <= 0 || len(s) <= n { return s }
    // 简单截断，避免多字节问题
    out := s[:n]
    if len(out) > 3 { out = out[:n-3] + "..." }
    // 确保是有效JSON字符串片段
    _, _ = json.Marshal(out)
    return out
}
