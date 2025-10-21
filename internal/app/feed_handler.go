// internal/app/feed_handler.go
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
	"github.com/PuerkitoBio/goquery"

	"gofull/internal/extractors" // Registry ve Extractor için import
)

// FeedHandler handles fetching and returning RSS feed content.
// Uses in-memory cache to reduce redundant fetches.
type FeedHandler struct {
	Cache    *Cache
	Client   *http.Client // Extractor'lar için HTTP client gerekebilir
	Registry *extractors.Registry // Extractor seçimi için Registry
}

// NewFeedHandler creates a new FeedHandler.
// Registry ve Client artık FeedHandler'ın bir parçası.
func NewFeedHandler(cache *Cache, client *http.Client, registry *extractors.Registry) *FeedHandler {
	return &FeedHandler{
		Cache:    cache,
		Client:   client, // retryablehttp.NewClient().StandardClient() gibi bir şey olabilir
		Registry: registry,
	}
}

// Item represents a single feed item with content and image.
type Item struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	Published   string `json:"published"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content,omitempty"`
	Image       string `json:"image,omitempty"` // Yeni alan
}

// ServeHTTP implements http.Handler for FeedHandler.
func (h *FeedHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	urlParam := strings.TrimSpace(r.URL.Query().Get("url"))
	if urlParam == "" {
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

	cacheKey := fmt.Sprintf("%s|%d", urlParam, limit)

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
	resp, err := client.StandardClient().Get(urlParam)
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
	var items []Item // Item tipi burada kullanılıyor
	for _, i := range feed.Items {
		// processItem fonksiyonunu çağır
		item := h.processItem(i)
		items = append(items, item)
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

// processItem extracts content and image using registered extractors.
func (h *FeedHandler) processItem(i *gofeed.Item) Item { // Item tipi burada döndürülüyor
	content := i.Content
	imageURL := "" // Varsayılan olarak boş

	if i.Link != "" {
		// Registry'den uygun extractor'ı al
		extractor := h.Registry.ForURL(i.Link)

		// Extractor'dan içerik ve görsel al
		// DunyaExtractor any input destekliyor.
		// DefaultExtractor da any input destekliyor.
		extractedContent, extractedImages, err := extractor.Extract(i.Link)
		if err == nil {
			// Extractor içerik veya görsel sağladıysa kullan
			if extractedContent != "" {
				content = cleanHTMLContent(extractedContent) // Feed'deki content'i extractor'dan gelenle değiştir
			}
			if len(extractedImages) > 0 {
				imageURL = extractedImages[0] // İlk görsel URL'sini al
			}
		} else {
			// Extractor başarısız olursa, readability ile dene (eski yöntem)
			if content == "" {
				article, err := readability.FromURL(i.Link, 15*time.Second)
				if err == nil {
					content = cleanHTMLContent(article.Content) // Readability içeriğini de temizle
				}
			}
			// Görsel alımı için readability yeterli olmayabilir.
			// DunyaExtractor gibi özel extractor'lar görsel alımında daha başarılıdır.
		}
	}

	return Item{ // Item tipi burada oluşturuluyor
		Title:       i.Title,
		Link:        i.Link,
		Published:   formatTime(i.PublishedParsed),
		Description: i.Description,
		Content:     strings.TrimSpace(content),
		Image:       imageURL, // Yeni alan
	}
}

func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// cleanHTMLContent removes unwanted HTML tags and cleans up the content
func cleanHTMLContent(html string) string {
	// Basic cleaning - remove script and style tags
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return html // Return original if parsing fails
	}

	// Remove script and style elements
	doc.Find("script, style").Remove()

	// Get the cleaned HTML
	cleaned, err := doc.Html()
	if err != nil {
		return html // Return original if getting HTML fails
	}

	// Remove extra whitespace and newlines
	cleaned = strings.TrimSpace(cleaned)
	cleaned = strings.Join(strings.Fields(cleaned), " ")

	return cleaned
}