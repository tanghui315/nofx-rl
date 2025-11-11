package telemetry

import (
    "encoding/json"
    "fmt"
    "nofx/market"
    "os"
    "path/filepath"
    "sort"
    "sync"
    "time"
)

// LabelEntry is the output schema for trade-labels.jsonl
type LabelEntry struct {
    SourceTS              string  `json:"source_ts"`
    Symbol                string  `json:"symbol"`
    HorizonMin            int     `json:"horizon_min"`
    DirectionalReturn     float64 `json:"directional_return"`
    MaxFavorableExcursion float64 `json:"max_favorable_excursion"`
    MaxAdverseExcursion   float64 `json:"max_adverse_excursion"`
    Hit                   string  `json:"hit"`     // "tp"|"sl"|"none" (unknown here)
    TickID                string  `json:"tick_id"` // optional
}

type pending struct {
    Symbol   string
    SourceTS time.Time
    Done     map[int]bool // horizon minutes written
}

type Labeler struct {
    mu      sync.Mutex
    items   []*pending
    started bool
}

var labeler Labeler

// StartLabeler launches the background goroutine. Safe to call multiple times.
func StartLabeler() {
    labeler.mu.Lock()
    if labeler.started {
        labeler.mu.Unlock()
        return
    }
    labeler.started = true
    labeler.mu.Unlock()

    go func() {
        ticker := time.NewTicker(60 * time.Second)
        defer ticker.Stop()
        for range ticker.C {
            processDue()
        }
    }()
}

// EnqueueLabel schedules label computation at T+5/15/30/60 minutes for the given symbol and source time.
func EnqueueLabel(symbol string, ts time.Time) {
    labeler.mu.Lock()
    defer labeler.mu.Unlock()
    labeler.items = append(labeler.items, &pending{Symbol: symbol, SourceTS: ts.UTC(), Done: map[int]bool{}})
}

func processDue() {
    labeler.mu.Lock()
    items := make([]*pending, len(labeler.items))
    copy(items, labeler.items)
    labeler.mu.Unlock()

    now := time.Now().UTC()
    horizons := []int{5, 15, 30, 60}
    for _, it := range items {
        for _, h := range horizons {
            if it.Done[h] {
                continue
            }
            if now.Before(it.SourceTS.Add(time.Duration(h) * time.Minute)) {
                continue
            }
            if le, ok := computeLabel(it.Symbol, it.SourceTS, h); ok {
                _ = appendLabelJSONL(le)
            }
            labeler.mu.Lock()
            it.Done[h] = true
            labeler.mu.Unlock()
        }
    }
    // GC items that finished all horizons + 10 minutes grace
    labeler.mu.Lock()
    var keep []*pending
    for _, it := range labeler.items {
        allDone := it.Done[5] && it.Done[15] && it.Done[30] && it.Done[60]
        if allDone && now.After(it.SourceTS.Add(70*time.Minute)) {
            continue
        }
        keep = append(keep, it)
    }
    labeler.items = keep
    labeler.mu.Unlock()
}

func appendLabelJSONL(le LabelEntry) error {
    day := le.SourceTS[:10] // YYYY-MM-DD
    dir := filepath.Join("logs", "trading")
    if err := os.MkdirAll(dir, 0755); err != nil {
        return err
    }
    path := filepath.Join(dir, fmt.Sprintf("%s-trade-labels.jsonl", day))
    f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
    if err != nil { return err }
    defer f.Close()
    b, _ := json.Marshal(le)
    _, err = f.Write(append(b, '\n'))
    return err
}

// computeLabel fetches recent 3m klines and computes directional return & excursions.
// It uses close prices of the containing candles at source and at horizon end.
func computeLabel(symbol string, source time.Time, horizon int) (LabelEntry, bool) {
    cli := market.NewAPIClient()
    klines, err := cli.GetKlines(symbol, "3m", 1000)
    if err != nil || len(klines) == 0 {
        return LabelEntry{}, false
    }
    // Bin search by time
    // Convert source and end to ms
    srcMs := source.UnixMilli()
    endMs := source.Add(time.Duration(horizon) * time.Minute).UnixMilli()

    // ensure klines are sorted by OpenTime
    sort.Slice(klines, func(i, j int) bool { return klines[i].OpenTime < klines[j].OpenTime })

    // find candle index for ts in [OpenTime, CloseTime]
    idxSrc := findCandle(klines, srcMs)
    idxEnd := findCandle(klines, endMs)
    if idxSrc < 0 || idxEnd < 0 { return LabelEntry{}, false }

    p0 := klines[idxSrc].Close
    p1 := klines[idxEnd].Close
    if p0 <= 0 || p1 <= 0 { return LabelEntry{}, false }
    // MFE/MAE within (idxSrc..idxEnd)
    maxUp := -1e9
    maxDn := 1e9
    for i := idxSrc; i <= idxEnd && i < len(klines); i++ {
        up := (klines[i].High - p0) / p0
        dn := (klines[i].Low - p0) / p0
        if up > maxUp { maxUp = up }
        if dn < maxDn { maxDn = dn }
    }
    le := LabelEntry{
        SourceTS:              source.UTC().Format(time.RFC3339),
        Symbol:                symbol,
        HorizonMin:            horizon,
        DirectionalReturn:     (p1 - p0) / p0,
        MaxFavorableExcursion: maxUp,
        MaxAdverseExcursion:   maxDn,
        Hit:                   "none",
        TickID:                "",
    }
    return le, true
}

func findCandle(kl []market.Kline, tsMs int64) int {
    // linear scan is fine for 1000, but implement binary for neatness
    lo, hi := 0, len(kl)-1
    for lo <= hi {
        mid := (lo + hi) / 2
        if tsMs < kl[mid].OpenTime {
            hi = mid - 1
        } else if tsMs > kl[mid].CloseTime {
            lo = mid + 1
        } else {
            return mid
        }
    }
    return -1
}

