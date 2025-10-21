package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/go-shiori/go-readability"
	"github.com/hashicorp/go-retryablehttp"
)

// FeedHandler handles fetching and returning RSS feed content.
// Uses in-memory cache to reduce redundant fetches.
type FeedHandler struct {
	Cache *Cache
}

// NewFeedHandler creates a new FeedHandler.
func NewFeedHandler(cache *Cache) *FeedHandler {
	return &FeedHandler{Cache: cache}
}

// ServeHTTP implements http.Handler for FeedHandler.
func (h *FeedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	url := strings.TrimSpace(r.URL.Query().Get("url"))
	if url == "" {
		http.Error(w, "missing 'url' parameter", http.StatusBadRequest)
		return
	}

	// Parse limit param (default: 10)
	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
	}

	cacheKey := fmt.Sprintf("%s|%d", url, limit)

	// Check cache
	if cached, ok := h.Cache.Get(cacheKey); ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(cached))
		return
	}

	// Use retryable HTTP client
	client := retryablehttp.NewClient()
	client.RetryMax = 3
	client.Logger = nil

	// Fetch RSS feed
	resp, err := client.StandardClient().Get(url)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to fetch RSS: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	parser := gofeed.NewParser()
	feed, err := parser.Parse(resp.Body)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to parse feed: %v", err), http.StatusInternalServerError)
		return
	}

	// Limit items
	if len(feed.Items) > limit {
		feed.Items = feed.Items[:limit]
	}

	// Process each entry content
	type Item struct {
		Title       string `json:"title"`
		Link        string `json:"link"`
		Published   string `json:"published"`
		Description string `json:"description,omitempty"`
		Content     string `json:"content,omitempty"`
	}

	var items []Item
	for _, i := range feed.Items {
		content := i.Content
		if content == "" && i.Link != "" {
			// Try extracting readable content
			article, err := readability.FromURL(i.Link, 15*time.Second)
			if err == nil {
				content = article.TextContent
			}
		}

		items = append(items, Item{
			Title:       i.Title,
			Link:        i.Link,
			Published:   formatTime(i.PublishedParsed),
			Description: i.Description,
			Content:     strings.TrimSpace(content),
		})
	}

	data := map[string]any{
		"feed_title": feed.Title,
		"feed_link":  feed.Link,
		"item_count": len(items),
		"items":      items,
	}

	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		http.Error(w, "failed to serialize response", http.StatusInternalServerError)
		return
	}

	// Cache the JSON response
	h.Cache.Set(cacheKey, string(jsonBytes))

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(jsonBytes)
}

func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}
