package news

import (
    "database/sql"
    "log"
    "sort"
    "time"
)

// DB is the minimal database contract used by the news package.
type DB interface {
    InsertNews(symbol, title, url, source string, publishedAt time.Time, summary string, fetchedAt time.Time) error
    TrimNews(symbol string, keep int) error
    Query(query string, args ...any) (*sql.Rows, error)
}

var db DB

func SetDatabase(database DB) { db = database }

// SaveItems upserts items for a symbol and trims to keep the latest N (default 5)
func SaveItems(symbol string, items []Item, keep int) error {
    if db == nil { return nil }
    if keep <= 0 { keep = 5 }
    // Sort by published_at desc, fallback fetched order
    sort.SliceStable(items, func(i,j int) bool { return items[i].PublishedAt.After(items[j].PublishedAt) })
    for _, it := range items {
        if err := dbInsertNews(symbol, it); err != nil {
            log.Printf("⚠️  news upsert failed: %v", err)
        }
    }
    return dbTrimNews(symbol, keep)
}

// Latest returns up to limit items for symbol
func Latest(symbol string, limit int) ([]Item, error) {
    if db == nil { return nil, nil }
    return dbListNews(symbol, limit)
}

// ---- thin wrappers over config.Database (local helpers implemented below) ----

func dbInsertNews(symbol string, it Item) error {
    return db.InsertNews(symbol, it.Title, it.URL, it.Source, it.PublishedAt, it.Summary, time.Now())
}

func dbTrimNews(symbol string, keep int) error { return db.TrimNews(symbol, keep) }
func dbListNews(symbol string, limit int) ([]Item, error) {
    if limit <= 0 { limit = 5 }
    rows, err := db.Query(`
        SELECT title, url, source, published_at, summary, fetched_at
        FROM news_items WHERE symbol = ?
        ORDER BY COALESCE(published_at, fetched_at) DESC, id DESC LIMIT ?
    `, symbol, limit)
    if err != nil { return nil, err }
    defer rows.Close()
    var out []Item
    for rows.Next() {
        var title, url, source, summary string
        var pub sql.NullTime
        var fetched time.Time
        if err := rows.Scan(&title, &url, &source, &pub, &summary, &fetched); err != nil { return nil, err }
        it := Item{Title: title, URL: url, Source: source, Summary: summary}
        if pub.Valid { it.PublishedAt = pub.Time }
        out = append(out, it)
    }
    return out, nil
}
