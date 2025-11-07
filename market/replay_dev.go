//go:build dev

package market

import (
    "errors"
    "fmt"
    "math"
    "strings"
    "sync"
    "time"
)

type ReplayRequest struct {
    Symbol             string
    Pattern            string  // price_spike | volume_spike | consecutive
    Interval           string  // 仅支持 3m
    Bars               int
    AbsChangePctPerBar float64 // price_spike 用
    AbsChangePctTotal  float64 // consecutive 用
    VolumeX            float64 // volume_spike 用
    Mode               string  // stream | oneshot
    SpeedBarPerSecond  float64 // 播放速度，bar/s
    Direction          string  // up | down（price_spike）
    TTL                time.Duration
}

type replaySession struct {
    stopCh chan struct{}
}

var (
    replayMu      sync.Mutex
    activeReplays = map[string]*replaySession{} // symbol -> session
)

// StartReplay 启动回放（dev 构建专用）。不会影响生产订阅流。
func StartReplay(req ReplayRequest) error {
    if WSMonitorCli == nil {
        return errors.New("monitor not initialized")
    }
    symbol := Normalize(req.Symbol)
    if symbol == "USDT" || len(symbol) < 6 {
        return fmt.Errorf("invalid symbol: %s", req.Symbol)
    }
    if req.Interval == "" { req.Interval = "3m" }
    if strings.ToLower(req.Interval) != "3m" {
        return errors.New("only 3m interval is supported in dev replay")
    }
    if req.Bars <= 0 { req.Bars = 3 }
    if req.SpeedBarPerSecond <= 0 { req.SpeedBarPerSecond = 1 }
    if req.Mode == "" { req.Mode = "stream" }

    replayMu.Lock()
    // 停止同 symbol 旧的回放
    if s, ok := activeReplays[symbol]; ok {
        close(s.stopCh)
        delete(activeReplays, symbol)
    }
    sess := &replaySession{stopCh: make(chan struct{})}
    activeReplays[symbol] = sess
    replayMu.Unlock()

    go runReplay(symbol, req, sess)
    return nil
}

func StopReplay(symbol string) {
    replayMu.Lock()
    defer replayMu.Unlock()
    if symbol == "" {
        for k, s := range activeReplays {
            close(s.stopCh)
            delete(activeReplays, k)
        }
        return
    }
    symbol = Normalize(symbol)
    if s, ok := activeReplays[symbol]; ok {
        close(s.stopCh)
        delete(activeReplays, symbol)
    }
}

func ReplayStatus() map[string]interface{} {
    replayMu.Lock()
    defer replayMu.Unlock()
    list := []string{}
    for k := range activeReplays {
        list = append(list, k)
    }
    return map[string]interface{}{
        "active": list,
        "count":  len(list),
    }
}

func runReplay(symbol string, req ReplayRequest, sess *replaySession) {
    bars := req.Bars
    tick := time.NewTicker(time.Duration(1.0/req.SpeedBarPerSecond*1000) * time.Millisecond)
    defer tick.Stop()

    // 基准价：优先用最近K线收盘，否则用 Market.Get
    basePrice := 1000.0
    if kl, err := WSMonitorCli.GetCurrentKlines(symbol, "3m"); err == nil && len(kl) > 0 {
        basePrice = kl[len(kl)-1].Close
    } else if md, err := Get(symbol); err == nil && md != nil && md.CurrentPrice > 0 {
        basePrice = md.CurrentPrice
    }
    if basePrice <= 0 {
        basePrice = 1000
    }

    // 基准量：最近20根平均成交（如果没有，给个默认）
    baseVol := 100.0
    if kl, err := WSMonitorCli.GetCurrentKlines(symbol, "3m"); err == nil && len(kl) >= 1 {
        sum := 0.0
        n := 0
        for i := len(kl)-1; i >= 0 && n < 20; i-- { sum += kl[i].Volume; n++ }
        if n > 0 { baseVol = sum/float64(n) }
        if baseVol <= 0 { baseVol = 100 }
    }

    now := time.Now()
    openTime := now.Add(-time.Duration(bars) * 3 * time.Minute).UnixMilli()

    for i := 0; i < bars; i++ {
        select {
        case <-sess.stopCh:
            return
        case <-tick.C:
        }

        // 生成一根K线
        ws := KlineWSData{}
        ws.EventType = "kline"
        ws.EventTime = time.Now().UnixMilli()
        ws.Symbol = symbol
        ws.Kline.Symbol = symbol
        ws.Kline.Interval = "3m"
        ws.Kline.StartTime = openTime
        ws.Kline.CloseTime = openTime + int64(3*time.Minute/time.Millisecond)
        ws.Kline.NumberOfTrades = 100
        ws.Kline.IsFinal = true

        // 价格路径
        var changePct float64
        switch strings.ToLower(req.Pattern) {
        case "price_spike":
            step := req.AbsChangePctPerBar
            if step == 0 { step = 1.5 }
            dir := 1.0
            if strings.ToLower(req.Direction) == "down" { dir = -1 }
            changePct = dir * step / 100.0
        case "consecutive":
            total := req.AbsChangePctTotal
            if total == 0 { total = 5.0 }
            sign := 1.0
            if strings.ToLower(req.Direction) == "down" { sign = -1 }
            changePct = sign * (total/float64(bars)) / 100.0
        default:
            changePct = 0
        }

        open := basePrice
        close := basePrice * (1 + changePct)
        high := math.Max(open, close) * 1.001
        low := math.Min(open, close) * 0.999

        // 成交量
        vol := baseVol
        if strings.ToLower(req.Pattern) == "volume_spike" {
            x := req.VolumeX
            if x <= 0 { x = 20 }
            vol = baseVol * x
        }

        // 写入ws数据字符串字段
        ws.Kline.OpenPrice = fmt.Sprintf("%.6f", open)
        ws.Kline.ClosePrice = fmt.Sprintf("%.6f", close)
        ws.Kline.HighPrice = fmt.Sprintf("%.6f", high)
        ws.Kline.LowPrice = fmt.Sprintf("%.6f", low)
        ws.Kline.Volume = fmt.Sprintf("%.6f", vol)
        ws.Kline.QuoteVolume = fmt.Sprintf("%.6f", vol*close)
        ws.Kline.TakerBuyBaseVolume = fmt.Sprintf("%.6f", vol*0.5)
        ws.Kline.TakerBuyQuoteVolume = fmt.Sprintf("%.6f", vol*close*0.5)

        // 注入到监控器（等效新K线）
        WSMonitorCli.processKlineUpdate(symbol, ws, "3m")

        // 更新基准价/时间
        basePrice = close
        openTime += int64(3 * time.Minute / time.Millisecond)
    }

    // oneshot 模式退出；stream 模式保持存活直到 TTL 或 stop
    if strings.ToLower(req.Mode) == "oneshot" {
        replayMu.Lock()
        delete(activeReplays, symbol)
        replayMu.Unlock()
        return
    }

    // TTL 到期自动停止
    if req.TTL > 0 {
        select {
        case <-sess.stopCh:
            return
        case <-time.After(req.TTL):
        }
        StopReplay(symbol)
    }
}

