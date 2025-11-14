package news

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// telegramClient 通过抓取 Telegram 频道网页获取新闻内容。
// 为了简单起见，这里不做币种分类，只负责把频道里的消息转成通用的 Item 列表。
type telegramClient struct {
	http     *http.Client
	baseURL  string   // 例如 https://t.me/s
	channels []string // 频道 ID 列表，例如 ["ChannelPANews"]
}

// newTelegramClient 从配置构建一个 telegramClient。
// baseURL 为空时默认使用 https://t.me/s。
// proxyRaw 为空则不使用代理。
func newTelegramClient(baseURL, proxyRaw string, channels []string) (*telegramClient, error) {
	if len(channels) == 0 {
		return nil, fmt.Errorf("no telegram channels configured")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = "https://t.me/s"
	}

	client := &http.Client{
		Timeout: 20 * time.Second,
	}

	if strings.TrimSpace(proxyRaw) != "" {
		u, err := url.Parse(proxyRaw)
		if err != nil {
			log.Printf("⚠️  TELEGRAM_PROXY_URL 无效: %v", err)
		} else {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(u),
			}
		}
	}

	return &telegramClient{
		http:     client,
		baseURL:  strings.TrimRight(baseURL, "/"),
		channels: channels,
	}, nil
}

// FetchAll 拉取所有配置频道的最新消息，合并为一组 Item。
// 为避免过多请求，每个频道只取最近若干条（perChannelLimit）。
func (c *telegramClient) FetchAll(perChannelLimit int) ([]Item, error) {
	if perChannelLimit <= 0 {
		perChannelLimit = 10
	}

	var all []Item
	for _, ch := range c.channels {
		items, err := c.fetchChannel(ch, perChannelLimit)
		if err != nil {
			log.Printf("⚠️  Telegram 频道 %s 抓取失败: %v", ch, err)
			continue
		}
		all = append(all, items...)
	}
	return all, nil
}

// fetchChannel 抓取单个频道的最新消息。
func (c *telegramClient) fetchChannel(channelID string, limit int) ([]Item, error) {
	u := fmt.Sprintf("%s/%s", c.baseURL, channelID)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}

	// 模拟浏览器 User-Agent，减小被屏蔽几率
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; NoFxBot/1.0)")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("telegram status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return parseTelegramHTML(resp.Body, channelID, limit)
}

// parseTelegramHTML 解析 Telegram 频道 HTML，提取消息列表。
func parseTelegramHTML(r io.Reader, channelID string, limit int) ([]Item, error) {
	doc, err := goquery.NewDocumentFromReader(r)
	if err != nil {
		return nil, fmt.Errorf("parse telegram html: %w", err)
	}

	var items []Item

	doc.Find(".tgme_widget_message_wrap").EachWithBreak(func(i int, sel *goquery.Selection) bool {
		if limit > 0 && len(items) >= limit {
			return false
		}

		title, content, pubTime := extractTelegramMessage(sel)
		if strings.TrimSpace(title) == "" && strings.TrimSpace(content) == "" {
			return true
		}

		source := channelID
		link := "" // 可选：后续可结合 data-post 拼接 t.me 链接

		items = append(items, Item{
			Title:       title,
			URL:         link,
			Source:      source,
			PublishedAt: pubTime,
			Summary:     content,
		})
		return true
	})

	return items, nil
}

// extractTelegramMessage 从单条消息节点提取标题、内容与时间。
func extractTelegramMessage(sel *goquery.Selection) (title, content string, pubTime time.Time) {
	// 文本内容在 .js-message_text
	messageText := sel.Find(".js-message_text")
	if messageText.Length() > 0 {
		html, _ := messageText.Html()
		if html != "" {
			lines := strings.Split(html, "<br/>")
			if len(lines) > 0 {
				title = stripHTML(lines[0])
			}
			if len(lines) > 1 {
				content = stripHTML(strings.Join(lines[1:], "\n"))
			}
		}
	}

	// 发布时间 <time datetime="...">
	if dt, ok := sel.Find("time").Attr("datetime"); ok && dt != "" {
		// Telegram datetime 通常是 ISO8601/RFC3339
		if t, err := time.Parse(time.RFC3339, dt); err == nil {
			pubTime = t
		}
	}
	if pubTime.IsZero() {
		pubTime = time.Now()
	}
	return
}

// stripHTML 移除字符串中的 HTML 标签，保留纯文本。
func stripHTML(s string) string {
	re := regexp.MustCompile(`\<[\S\s]+?\>`)
	s = re.ReplaceAllString(s, "\n")
	// 合并多余空白
	reSpace := regexp.MustCompile(`\s{2,}`)
	s = reSpace.ReplaceAllString(s, "\n")
	return strings.TrimSpace(s)
}
