package news

import (
	"strings"
	"time"
)

// ScoreResult 新闻评分结果
type ScoreResult struct {
	Score    float64            // [-2, +2]
	Hits     map[string]int     // 关键词命中计数
	Articles int                // 统计的文章数
	Window   time.Duration      // 窗口
}

var positiveKeywords = []string{
	"mainnet", "partnership", "listing", "etf", "upgrade", "airdrop", "burn", "buyback", "integration",
}
var negativeKeywords = []string{
	"hack", "exploit", "delist", "lawsuit", "sec", "fine", "outage", "halt", "downtime",
}

// Score 计算最近窗口的新闻得分（符号级）。简化：基于关键词与时效加权。
// 窗口内 <2h 权重1.0；<24h 权重0.5；>24h 权重0.2。
func Score(symbol string, window time.Duration) ScoreResult {
	items, _ := Latest(symbol, 20)
	now := time.Now()
	res := ScoreResult{Hits: map[string]int{}, Window: window}
	cutoff := now.Add(-window)
	total := 0.0
	for _, it := range items {
		ts := it.PublishedAt
		if ts.IsZero() {
			continue
		}
		if ts.Before(cutoff) {
			continue
		}
		age := now.Sub(ts)
		w := 0.2
		if age <= 2*time.Hour {
			w = 1.0
		} else if age <= 24*time.Hour {
			w = 0.5
		}
		title := strings.ToLower(it.Title + " " + it.Summary)
		score := 0.0
		for _, kw := range positiveKeywords {
			if strings.Contains(title, kw) {
				score += 1.0
				res.Hits[kw]++
			}
		}
		for _, kw := range negativeKeywords {
			if strings.Contains(title, kw) {
				score -= 1.0
				res.Hits[kw]++
			}
		}
		total += score * w
		res.Articles++
	}
	// 归一化并截断
	if total > 2 {
		total = 2
	} else if total < -2 {
		total = -2
	}
	res.Score = total
	return res
}

