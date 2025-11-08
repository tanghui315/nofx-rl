package news

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"
    "strings"
    "time"
)

type Item struct {
    Title       string    `json:"title"`
    URL         string    `json:"url"`
    Source      string    `json:"source"`
    PublishedAt time.Time `json:"published_at"`
    Summary     string    `json:"summary"`
}

type tavilyClient struct {
    apiKey string
    http   *http.Client
}

func newTavily() *tavilyClient {
    return &tavilyClient{
        apiKey: os.Getenv("TAVILY_API_KEY"),
        http:   &http.Client{Timeout: 8 * time.Second},
    }
}

// SearchNews queries Tavily for recent news related to the symbol.
func (c *tavilyClient) SearchNews(symbol string) ([]Item, error) {
    if c.apiKey == "" {
        return nil, fmt.Errorf("TAVILY_API_KEY missing")
    }
    // Minimal, conservative payload to avoid 400 from unsupported fields.
    // We’ll expand once we confirm account permissions and endpoint behavior.
    q := fmt.Sprintf("%s crypto news", symbol)
    payload := map[string]interface{}{
        "api_key":       c.apiKey,
        "query":         q,
        "search_depth":  "basic",
        "include_answer": false,
        "max_results":   10,
    }
    buf, _ := json.Marshal(payload)
    req, _ := http.NewRequest("POST", "https://api.tavily.com/search", bytes.NewBuffer(buf))
    req.Header.Set("Content-Type", "application/json")
    // Also pass key via header for compatibility across account tiers
    req.Header.Set("X-API-Key", c.apiKey)

    resp, err := c.http.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode != http.StatusOK {
        b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
        return nil, fmt.Errorf("tavily status: %d body: %s", resp.StatusCode, strings.TrimSpace(string(b)))
    }
    var out struct{
        Results []struct{
            Title string `json:"title"`
            URL   string `json:"url"`
            Source string `json:"source"`
            PublishedDate string `json:"published_date"`
            Content string `json:"content"`
        } `json:"results"`
    }
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { return nil, err }

    items := make([]Item, 0, len(out.Results))
    for _, r := range out.Results {
        var ts time.Time
        if r.PublishedDate != "" {
            if t, err := time.Parse(time.RFC3339, r.PublishedDate); err == nil { ts = t }
        }
        source := r.Source
        if source == "" {
            if u, err := url.Parse(r.URL); err == nil { source = u.Hostname() }
        }
        items = append(items, Item{
            Title: r.Title, URL: r.URL, Source: source, PublishedAt: ts, Summary: r.Content,
        })
    }
    return items, nil
}
