// internal/app/feed_handler.go
package app

import (
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-shiori/go-readability"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/mmcdole/gofeed"

	"gofull/internal/extractors"
	"gofull/internal/extractors/filters"
)

// FeedHandler handles fetching and returning RSS feed content.
type FeedHandler struct {
	Cache     *Cache
	Client    *http.Client
	Registry  *extractors.Registry
	FilterReg *filters.FilterRegistry
}

// NewFeedHandler creates a new FeedHandler with filter support
func NewFeedHandler(cache *Cache, client *http.Client, registry *extractors.Registry, filterReg *filters.FilterRegistry) *FeedHandler {
	return &FeedHandler{
		Cache:     cache,
		Client:    client,
		Registry:  registry,
		FilterReg: filterReg,
	}
}

// Item represents a single feed item with content and image.
type Item struct {
	Title       string `json:"title"`
	Link        string `json:"link"`
	GUID        string `json:"guid"`
	Published   string `json:"published"`
	Description string `json:"description,omitempty"`
	Content     string `json:"content,omitempty"`
	Image       string `json:"image,omitempty"`
	Category    string `json:"category,omitempty"`
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

	// Process items with filtering
	var items []Item
	processedCount := 0
	skippedCount := 0

	for _, feedItem := range feed.Items {
		// Stop if we reached the limit
		if processedCount >= limit {
			break
		}

		// Apply URL filter
		if feedItem.Link != "" && !h.FilterReg.ShouldProcess(feedItem.Link) {
			log.Printf("⏭️  Skipping filtered URL: %s", feedItem.Link)
			skippedCount++
			continue
		}

		// Process the item
		item := h.processItem(feedItem)
		items = append(items, item)
		processedCount++

		log.Printf("✅ [%d/%d] Processed: %s (skipped: %d)", processedCount, limit, feedItem.Title, skippedCount)
	}

	data := map[string]any{
		"feed_title":     feed.Title,
		"feed_link":      feed.Link,
		"items_returned": len(items),
		"items_skipped":  skippedCount,
		"items":          items,
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

// getCategoryFromURL determines the category of a news article based on its URL
func getCategoryFromURL(url string) string {
	url = strings.ToLower(url)
	
	// Turkish news
	if strings.Contains(url, "/gundem/") || 
	   strings.Contains(url, "/turkiye/") || 
	   strings.Contains(url, "/politika/") || 
	   strings.Contains(url, "/siyaset/") || 
	   strings.Contains(url, "/haber/") {
		return "turkiye"
	}
	
	// World news
	if strings.Contains(url, "/dunya/") || 
	   strings.Contains(url, "/abd/") || 
	   strings.Contains(url, "/israil/") || 
	   strings.Contains(url, "/avrupa/") {
		return "world"
	}
	
	// Business
	if strings.Contains(url, "/ekonomi/") || 
	   strings.Contains(url, "/sektorler/") || 
	   strings.Contains(url, "/sirket/") || 
	   strings.Contains(url, "/borsa/") || 
	   strings.Contains(url, "/kobi/") || 
	   strings.Contains(url, "/ntvpara/") || 
	   strings.Contains(url, "/piyasalar/") || 
	   strings.Contains(url, "/veriler/") || 
	   strings.Contains(url, "/enerji/") || 
	   strings.Contains(url, "/gayrimenkul/") || 
	   strings.Contains(url, "/is-dunyasi/") || 
	   strings.Contains(url, "/sirket-haberleri/") || 
	   strings.Contains(url, "/finans/") {
		return "business"
	}
	
	// Technology
	if strings.Contains(url, "/teknoloji/") || 
	   strings.Contains(url, "/bilisim/") || 
	   strings.Contains(url, "/bilim/") {
		return "technology"
	}
	
	// Health
	if strings.Contains(url, "/saglik/") || 
	   strings.Contains(url, "/health/") || 
	   strings.Contains(url, "/saglikli-yasam/") {
		return "health"
	}
	
	// Entertainment
	if strings.Contains(url, "/magazin/") || 
	   strings.Contains(url, "/kultur/") || 
	   strings.Contains(url, "/sanat/") {
		return "entertainment"
	}
	
	return "turkiye" // Default category
}

// processItem extracts content and image using registered extractors.
func (h *FeedHandler) processItem(i *gofeed.Item) Item {
	content := i.Content
	imageURL := ""

	// Create a map to pass feed item data to extractor
	itemData := map[string]interface{}{
		"link": i.Link,
	}

	// If the feed item has an image, add it to the data
	if i.Image != nil && i.Image.URL != "" {
		itemData["image"] = i.Image.URL
		imageURL = i.Image.URL
	}

	if i.Link != "" {
		// Get appropriate extractor from registry
		extractor := h.Registry.ForURL(i.Link)

		// Log which extractor is being used
		extractorType := fmt.Sprintf("%T", extractor)
		log.Printf("🔍 Using extractor: %s for URL: %s", extractorType, i.Link)

		// Extract content and images using the extractor with item data
		extractedContent, extractedImages, err := extractor.Extract(itemData)
		if err == nil {
			if extractedContent != "" {
				content = cleanHTMLContent(extractedContent)
			}
			if len(extractedImages) > 0 {
				imageURL = extractedImages[0]
				log.Printf("🖼️  Found image for %s: %s", i.Link, imageURL)
			} else {
				log.Printf("⚠️  No images found for URL: %s", i.Link)
			}
		} else {
			// Fallback to readability
			log.Printf("⚠️  Extractor failed for %s, using readability: %v", i.Link, err)
			if content == "" {
				article, err := readability.FromURL(i.Link, 15*time.Second)
				if err == nil {
					content = cleanHTMLContent(article.Content)
					// Try to extract images from the readability content
					doc, err := goquery.NewDocumentFromReader(strings.NewReader(article.Content))
					if err == nil {
						doc.Find("img").Each(func(i int, s *goquery.Selection) {
							if src, exists := s.Attr("src"); exists && src != "" && imageURL == "" {
								imageURL = src
							}
						})
						if imageURL != "" {
							log.Printf("🖼️  Found fallback image from readability: %s", imageURL)
						}
					}
				}
			}
		}
	}

	// If we still don't have an image, try to get it from the feed item's enclosures
	if imageURL == "" && len(i.Enclosures) > 0 {
		for _, enc := range i.Enclosures {
			if strings.HasPrefix(enc.Type, "image/") && enc.URL != "" {
				imageURL = enc.URL
				log.Printf("🖼️  Found image from feed enclosure: %s", imageURL)
				break
			}
		}
	}

	// If we still don't have an image, try to get it from the feed item's image
	if imageURL == "" && i.Image != nil && i.Image.URL != "" {
		imageURL = i.Image.URL
		log.Printf("🖼️  Found image from feed item: %s", imageURL)
	}

	// Determine category from URL
	category := getCategoryFromURL(i.Link)

	return Item{
		Title:       i.Title,
		Link:        i.Link,
		GUID:        extractors.GenerateGUIDFromURL(i.Link),
		Published:   formatTime(i.PublishedParsed),
		Description: i.Description,
		Content:     strings.TrimSpace(content),
		Image:       imageURL,
		Category:    category,
	}
}

func formatTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// cleanHTMLContent cleans and normalizes HTML content by removing unwanted elements
// and ensuring proper UTF-8 encoding
func cleanHTMLContent(htmlContent string) string {
	// Return early if content is empty or whitespace only
	if strings.TrimSpace(htmlContent) == "" {
		return ""
	}

	// Remove <html><body> and </body></html> tags
	htmlContent = strings.ReplaceAll(htmlContent, "<html><body>", "")
	htmlContent = strings.ReplaceAll(htmlContent, "</body></html>", "")

	// Create a new document from the HTML content
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return htmlContent
	}

	// Remove unwanted elements (scripts, styles, ads, etc.)
	unwantedSelectors := []string{
		// Basic elements
		"script", "style", "iframe", "noscript", "object", "embed", "video", "audio",
		"form", "input", "button", "select", "textarea", "label", "fieldset",
		"header", "footer", "nav", "aside", "menu", "dialog", "figure", "figcaption",
		
		// Ads and tracking
		".ad", ".advertisement", ".ad-container", ".ad-wrapper", ".ad-banner", 
		".ad-header", ".ad-sidebar", ".ad-slot", ".ad-unit", ".advert",
		".social-share", ".social-likes", ".sharing", ".share-buttons",
		".related-news", ".related-posts", ".related-articles", ".recommended",
		".popular-posts", ".trending", ".newsletter", ".subscribe",
		".tags", ".tag-cloud", ".post-tags", ".post-meta", ".post-footer",
		".author", ".byline", ".post-date", ".timestamp", ".comments",
	}

	// Add dynamic selectors for common ad patterns
	for _, sel := range unwantedSelectors {
		doc.Find(sel).Remove()
	}

	// Clean up divs and other elements
	doc.Find("div, section, article, main").Each(func(i int, s *goquery.Selection) {
		// Get element attributes
		class, _ := s.Attr("class")
		id, _ := s.Attr("id")
		role, _ := s.Attr("role")
		
		// Check for unwanted elements
		isUnwanted := false
		
		// Check class and ID patterns
		lowerClass := strings.ToLower(class)
		lowerId := strings.ToLower(id)
		
		unwantedPatterns := []string{
			"ad", "banner", "sponsor", "recommend", "related", "popular",
			"widget", "sidebar", "sticky", "modal", "popup", "newsletter",
			"subscribe", "social", "share", "comment", "cookie", "consent",
			"notification", "alert", "promo", "teaser", "recommendation",
			"trending", "most-viewed", "most-read", "signup",
		}
		
		for _, pattern := range unwantedPatterns {
			if strings.Contains(lowerClass, pattern) || strings.Contains(lowerId, pattern) {
				isUnwanted = true
				break
			}
		}
		
		// Check for empty or nearly empty elements
		text := strings.TrimSpace(s.Text())
		if !isUnwanted && text == "" && s.Children().Length() == 0 {
			isUnwanted = true
		}
		
		// Check for common ad roles
		if role == "banner" || role == "complementary" || role == "contentinfo" {
			isUnwanted = true
		}
		
		// Remove if unwanted
		if isUnwanted {
			s.Remove()
		}
	})

	// Clean up empty elements
	doc.Find("p, span, div, section, article, header, footer, aside").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if text == "" && s.Children().Length() == 0 {
			s.Remove()
		}
	})

	// Normalize whitespace in text nodes
	doc.Find("body").Each(func(i int, s *goquery.Selection) {
		s.Contents().Each(func(i int, node *goquery.Selection) {
			if node.Nodes[0].Type == 1 { // ElementNode
				// Skip non-text nodes
				return
			}
			if node.Nodes[0].Type == 3 { // TextNode
				node.ReplaceWithHtml(strings.Join(strings.Fields(node.Text()), " "))
			}
		})
	})

	// Get the cleaned HTML
	htmlStr, err := doc.Html()
	if err != nil {
		return htmlContent
	}

	// Decode HTML entities and normalize
	htmlStr = html.UnescapeString(htmlStr)
	
	// Clean up whitespace
	htmlStr = regexp.MustCompile(`\s+`).ReplaceAllString(htmlStr, " ")
	htmlStr = strings.TrimSpace(htmlStr)
	
	// Remove any remaining empty HTML tags
	htmlStr = regexp.MustCompile(`<\w+\s*(?:[^>]*)>\s*<\/\w+>`).ReplaceAllString(htmlStr, "")
	htmlStr = regexp.MustCompile(`<\w+\s*(?:[^>]*)>\s*<\/\w+\s*>`).ReplaceAllString(htmlStr, "")

	return htmlStr
}