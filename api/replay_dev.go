//go:build dev

package api

import (
    "math"
    "net/http"
    "nofx/auth"
    "nofx/market"
    "os"
    "strconv"
    "time"

    "github.com/gin-gonic/gin"
)

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
