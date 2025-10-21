// internal/app/feed_handler.go (Güncellenmiş Extract çağrıları ve Enclosure hatası düzeltildi)
package app

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	// Eksik importlar eklendi
	"os" // os.Getenv için
	"go.uber.org/zap" // logger.Log için

	"github.com/gorilla/feeds"
	"github.com/mmcdole/gofeed"

	"gofull/internal/extractors"
	"gofull/internal/logger"
)

// init fonksiyonu artık os importuna sahip
func init() {
	logger.InitLogger(os.Getenv("APP_ENV"))
}

// FeedHandler processes RSS/Atom feeds and rebuilds them with cleaned article content.
type FeedHandler struct {
	Client     *http.Client
	// HATA: Registry.Match metodu yok. ForURL kullanın.
	Registry   *extractors.Registry
	Cache      *Cache
	FeedParser *gofeed.Parser
}

// ProcessFeed fetches a feed URL, extracts and cleans each item, and returns a unified feed.
func (h *FeedHandler) ProcessFeed(feedURL string) (*feeds.Feed, error) {
	// HATA: logger.Log.Info için zap import gerekli
	logger.Log.Info("Processing feed", zap.String("url", feedURL))
	parsedFeed, err := h.FeedParser.ParseURL(feedURL)
	if err != nil {
		// HATA: logger.Log.Error için zap import gerekli
		logger.Log.Error("Failed to parse feed", zap.String("url", feedURL), zap.Error(err))
		return nil, fmt.Errorf("failed to parse feed: %w", err)
	}

	outputFeed := &feeds.Feed{
		Title:       parsedFeed.Title,
		Link:        &feeds.Link{Href: feedURL},
		Description: parsedFeed.Description,
		Author:      &feeds.Author{Name: parsedFeed.Author.Name},
		Created:     time.Now(),
	}

	for _, item := range parsedFeed.Items {
		processed, err := h.processItem(item)
		if err != nil {
			// HATA: logger.Log.Warn için zap import gerekli
			logger.Log.Warn("Skipping item", zap.String("title", item.Title), zap.Error(err))
			continue
		}
		outputFeed.Items = append(outputFeed.Items, processed)
	}

	// HATA: logger.Log.Info için zap import gerekli
	logger.Log.Info("Feed processed successfully", zap.Int("items", len(outputFeed.Items)))
	return outputFeed, nil
}

// processItem extracts article content, cleans it with the appropriate extractor, and builds a feeds.Item.
func (h *FeedHandler) processItem(item *gofeed.Item) (*feeds.Item, error) {
	if item.Link == "" {
		return nil, fmt.Errorf("item missing link")
	}

	// Extractor, item.Link'e göre seçilir (domain bazlı)
	domainExtractor := h.Registry.ForURL(item.Link)

	var contentHTML string
	var images []string // []string olarak değiştirildi
	var err error

	// Prefer feed content if available (HTML olarak)
	if item.Content != "" {
		// item.Content (HTML) ile Extract çağrısı
		contentHTML, images, err = domainExtractor.Extract(item.Content) // Değiştirildi
	} else {
		// item.Link (URL) ile Extract çağrısı
		contentHTML, images, err = domainExtractor.Extract(item.Link) // Değiştirildi
	}
	if err != nil {
		// HATA: logger.Log.Error için zap import gerekli
		logger.Log.Error("Failed to extract content", zap.String("url", item.Link), zap.Error(err))
		return nil, fmt.Errorf("extract failed: %w", err)
	}

	// Create a clean item with UUID
	feedItem := &feeds.Item{
		Id:          extractors.GenerateUniqueID(),
		Title:       item.Title,
		Link:        &feeds.Link{Href: item.Link},
		Description: summarizeHTML(contentHTML, 300),
		Created:     *item.PublishedParsed,
		Updated:     *item.UpdatedParsed,
		Author:      &feeds.Author{Name: item.Author.Name},
		Content:     contentHTML,
	}

	// Add image if found (ilk resmi al)
	if len(images) > 0 {
		// HATA: feeds.Item.Enclosures alanı yok. Doğru alan Enclosure.
		// feedItem.Enclosures = []*feeds.Enclosure{{Url: image, Type: "image/jpeg"}}
		feedItem.Enclosure = &feeds.Enclosure{ // Değiştirildi
			Url:  images[0], // İlk resmi kullan
			Type: "image/jpeg", // Gerekirse türü dinamik olarak belirleyin
			// Length belirtmek isterseniz, dosya boyutunu buraya ekleyebilirsiniz.
		}
	}

	// HATA: logger.Log.Info için zap import gerekli
	logger.Log.Info("Item processed successfully", zap.String("title", item.Title), zap.String("url", item.Link))
	return feedItem, nil
}

// fetchArticleHTML retrieves the full HTML of an article by URL.
// Bu fonksiyon artık processItem içinde doğrudan Extract(url) çağrısıyla yerine geçilmiştir.
// Ancak, hala Registry.ForURL(item.Link) kullanılıyorsa, bu extractor'ın URL ile çalışması beklenir.
// Yani item.Content yoksa, Extract(item.Link) çağrılır.
// Bu da DefaultExtractor'ı kullanır (NTVExtractor'ı değil, çünkü domain eşleşmiyorsa Default döner).
// Bu yapı, item.Content varsa onu HTML olarak işler, yoksa URL'yi işler.
// fetchArticleHTML fonksiyonu artık gerekli olmayabilir.
// Ancak, fetchArticleHTML başka bir yerde kullanılıyor olabilir veya
// Registry ForURL ile uygun extractor'ı döndürmüyor olabilir.
// Şimdilik, bu fonksiyonu koruyoruz ama processItem içinde doğrudan Extract(url) kullanıyoruz.
func (h *FeedHandler) fetchArticleHTML(articleURL string) (string, error) {
	resp, err := h.Client.Get(articleURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("non-200 response: %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return string(bodyBytes), nil
}

// summarizeHTML trims the HTML content to a short text summary.
func summarizeHTML(html string, limit int) string {
	plain := stripTags(html)
	if len(plain) > limit {
		return plain[:limit] + "..."
	}
	return plain
}

// stripTags removes basic HTML tags for generating text summary.
func stripTags(input string) string {
	replacer := strings.NewReplacer("<p>", " ", "</p>", " ", "<br>", " ", "</br>", " ")
	text := replacer.Replace(input)
	for _, tag := range []string{"<strong>", "</strong>", "<em>", "</em>", "<h2>", "</h2>"} {
		text = strings.ReplaceAll(text, tag, "")
	}
	return strings.TrimSpace(text)
}

// normalizeURL ensures URLs are absolute.
func normalizeURL(baseURL, href string) string {
	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		return href
	}
	parsedHref, err := url.Parse(href)
	if err != nil {
		return href
	}
	return parsedBase.ResolveReference(parsedHref).String()
}