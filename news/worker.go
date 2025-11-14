package news

import (
    "encoding/json"
    "log"
    "strconv"
    "time"
)

// systemDB is the local interface the worker needs.
type systemDB interface {
    DB
    GetSystemConfig(key string) (string, error)
}

// StartWorker starts the periodic fetcher using the provided Telegram config.
// baseURL/ proxyURL / channels 由上层（main）从 config.json 解析后传入。
func StartWorker(database systemDB, baseURL, proxyURL string, channels []string) {
    SetDatabase(database)
    if len(channels) == 0 {
        log.Printf("💡 News worker disabled: no telegram channels configured")
        return
    }

    tgClient, err := newTelegramClient(baseURL, proxyURL, channels)
    if err != nil {
        log.Printf("💡 News worker disabled: failed to init telegram client: %v", err)
        return
    }

    intervalMin := 15
    if v, _ := database.GetSystemConfig("news_interval_minutes"); v != "" {
        if n, err := strconv.Atoi(v); err == nil && n > 0 { intervalMin = n }
    }
    go loop(database, tgClient, time.Duration(intervalMin)*time.Minute)
}

func loop(db systemDB, client *telegramClient, every time.Duration) {
    t := time.NewTicker(every)
    defer t.Stop()
    runOnce(db, client)
    for range t.C {
        runOnce(db, client)
    }
}

func runOnce(db systemDB, client *telegramClient) {
    // load default_coins
    dc, _ := db.GetSystemConfig("default_coins")
    var coins []string
    if err := json.Unmarshal([]byte(dc), &coins); err != nil || len(coins) == 0 {
        coins = []string{"BTCUSDT","ETHUSDT","BNBUSDT"}
    }

    // 从 Telegram 抓取一次最新消息
    items, err := client.FetchAll(10)
    if err != nil {
        log.Printf("⚠️  Telegram 抓取新闻失败: %v", err)
        return
    }

    keep := 5
    for _, sym := range coins {
        if err := SaveItems(sym, items, keep); err != nil {
            log.Printf("⚠️  Save news failed for %s: %v", sym, err)
        }
    }
}
