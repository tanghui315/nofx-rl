package news

import (
    "encoding/json"
    "log"
    "os"
    "strconv"
    "time"
)

// systemDB is the local interface the worker needs.
type systemDB interface {
    DB
    GetSystemConfig(key string) (string, error)
}

// StartWorker starts the periodic fetcher if the API key exists.
func StartWorker(database systemDB) {
    SetDatabase(database)
    if tv := os.Getenv("TAVILY_API_KEY"); tv == "" {
        log.Printf("💡 News worker disabled: TAVILY_API_KEY missing")
        return
    }
    intervalMin := 15
    if v, _ := database.GetSystemConfig("news_interval_minutes"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n > 0 { intervalMin = n }
    }
    go loop(database, time.Duration(intervalMin)*time.Minute)
}

func loop(db systemDB, every time.Duration) {
    t := time.NewTicker(every)
    defer t.Stop()
    runOnce(db)
    for range t.C {
        runOnce(db)
    }
}

func runOnce(db systemDB) {
    // load default_coins
    dc, _ := db.GetSystemConfig("default_coins")
    var coins []string
    if err := json.Unmarshal([]byte(dc), &coins); err != nil || len(coins) == 0 {
        coins = []string{"BTCUSDT","ETHUSDT","BNBUSDT"}
    }
    client := newTavily()
    keep := 5
    for _, sym := range coins {
        items, err := client.SearchNews(sym)
        if err != nil {
            log.Printf("⚠️  Tavily error for %s: %v", sym, err)
            continue
        }
        if err := SaveItems(sym, items, keep); err != nil {
            log.Printf("⚠️  Save news failed for %s: %v", sym, err)
        }
    }
}
