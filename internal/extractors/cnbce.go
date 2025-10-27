package extractors

import (
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// CNBCEExtractor handles content extraction for cnbce.com domain.
// It processes any URL on the domain with specific handling for article pages.
type CNBCEExtractor struct {
	httpClient *http.Client
}

// NewCNBCEExtractor creates a new CNBCEExtractor.
func NewCNBCEExtractor(client *http.Client) *CNBCEExtractor {
	if client == nil {
		client = http.DefaultClient
	}
	return &CNBCEExtractor{
		httpClient: client,
	}
}

// isFilteredURL checks if the URL matches any of the filtered patterns
func (c *CNBCEExtractor) isFilteredURL(url string) bool {
	// Normalize the URL for consistent comparison
	normalizedURL := strings.ToLower(strings.TrimRight(url, "/"))
	
	filteredPatterns := []string{
		"cnbce.com/haberler",
		"cnbce.com/tv",
		"cnbce.com/art-e",
		"cnbce.com/gundem",
		"cnbce.com/son-dakika",
		"//cnbce.com/haberler",
		"www.cnbce.com/haberler",
	}

	// Check for exact matches first
	for _, pattern := range filteredPatterns {
		if strings.Contains(normalizedURL, pattern) {
			return true
		}
	}

	// Check for URL paths that start with /haberler/
	if u, err := http.ParseRequestURI(normalizedURL); err == nil {
		path := strings.ToLower(u.Path)
		if strings.HasPrefix(path, "/haberler/") {
			return true
		}
	}

	return false
}

// Extract implements the Extractor interface for cnbce.com URLs.
func (c *CNBCEExtractor) Extract(input any) (string, []string, error) {
	switch v := input.(type) {
	case string:
		// Input is a URL, check if it's filtered
		if c.isFilteredURL(v) {
			return "", nil, fmt.Errorf("URL is in filtered list: %s", v)
		}
		return c.extractFromURL(v)

	case map[string]string:
		// Handle map[string]string with "html" key
		if htmlContent, ok := v["html"]; ok {
			// Check if URL is provided in the map and if it's filtered
			if url, exists := v["url"]; exists && c.isFilteredURL(url) {
				return "", nil, fmt.Errorf("URL is in filtered list: %s", url)
			}
			if link, exists := v["link"]; exists && c.isFilteredURL(link) {
				return "", nil, fmt.Errorf("URL is in filtered list: %s", link)
			}
			return c.extractFromHTML(htmlContent)
		}

	case map[string]interface{}:
		// Handle map[string]interface{} with "html" key
		if htmlContent, ok := v["html"].(string); ok {
			// Check if URL is provided in the map and if it's filtered
			if url, exists := v["url"].(string); exists && c.isFilteredURL(url) {
				return "", nil, fmt.Errorf("URL is in filtered list: %s", url)
			}
			if link, exists := v["link"].(string); exists && c.isFilteredURL(link) {
				return "", nil, fmt.Errorf("URL is in filtered list: %s", link)
			}
			return c.extractFromHTML(htmlContent)
		}
		// If no html key, try to get URL from common fields
		if url, ok := v["url"].(string); ok && url != "" {
			return c.extractFromURL(url)
		}
		if link, ok := v["link"].(string); ok && link != "" {
			return c.extractFromURL(link)
		}

	default:
		return "", nil, fmt.Errorf("unsupported input type: %T", input)
	}

	return "", nil, errors.New("invalid input format - expected URL or map with 'html' content")
}

// extractFromURL fetches the URL and extracts content.
func (c *CNBCEExtractor) extractFromURL(articleURL string) (string, []string, error) {

	req, err := http.NewRequest("GET", articleURL, nil)
	if err != nil {
		return "", nil, err
	}
	// Set a user agent to avoid being blocked
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}

	return c.extractFromHTML(string(bodyBytes))
}

// extractFromHTML extracts content from HTML using cnbce.com specific selectors.
func (c *CNBCEExtractor) extractFromHTML(htmlContent string) (string, []string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", nil, err
	}

	// First, try to get the main image from meta tags (most reliable)
	var images []string

	// Try to get the main image from meta tags in order of preference
	metaSelectors := []struct {
		selector string
		attr     string
	}{
		{`meta[property="og:image"]`, "content"},
		{`meta[name="twitter:image"]`, "content"},
		{`link[rel="image_src"]`, "href"},
		{`meta[property="og:image:url"]`, "content"},
		{`meta[name="twitter:image:src"]`, "content"},
		{`meta[property="og:image:secure_url"]`, "content"},
		{`meta[itemprop="image"]`, "content"},
	}

	for _, meta := range metaSelectors {
		if img, exists := doc.Find(meta.selector).First().Attr(meta.attr); exists && img != "" {
			img = strings.TrimSpace(img)
			if img != "" {
				if !strings.HasPrefix(img, "http") && !strings.HasPrefix(img, "//") {
					img = "https://www.cnbce.com" + strings.TrimLeft(img, "/")
				} else if strings.HasPrefix(img, "//") {
					img = "https:" + img
				}
				images = []string{img}
				break
			}
		}
	}

	// Try to find the main content with specific class
	contentDiv := doc.Find(".content-text").First()
	if contentDiv.Length() == 0 {
		// If no content-text class found, try other selectors as fallback
		contentSelectors := []string{
			"div.article-body",    // Alternative content class
			"div.entry-content",   // Common WordPress content class
			"article",             // Standard HTML5 article tag
			"div.post-content",    // Common class for post content
			"div.article-content", // Common class for article content
		}

		for _, selector := range contentSelectors {
			contentDiv = doc.Find(selector).First()
			if contentDiv.Length() > 0 {
				break
			}
		}

		// If we still don't have content, use the body as a last resort
		if contentDiv.Length() == 0 {
			contentDiv = doc.Find("body")
		}
	}

	// Remove unwanted elements that might contain related articles, ads, or other unwanted content
	unwantedSelectors := []string{
		".related-news",
		".mceNonEditable",
		".block.lg\\:hidden",
		".relative.hidden.space-y-7.5.lg\\:block",
		".adpro.big-box",
		".related-articles",
		".popular-news",
		".populer-haberler",
		".diger-haberler",
		".benzer-haberler",
		".more-news",
		".daha-fazla",
		".tags",
		".etiketler",
		".social-share",
		".yazar-bilgisi",
		".author-info",
		".yorumlar",
		".comments",
		".reklam",
		".advertisement",
	}

	for _, selector := range unwantedSelectors {
		contentDiv.Find(selector).Remove()
	}

	// If no meta tag image found, try to find it in the content
	if len(images) == 0 {
		// Look for images in the content area
		contentDiv.Find("img").Each(func(i int, s *goquery.Selection) {
			if src, exists := s.Attr("src"); exists && src != "" {
				src = strings.TrimSpace(src)
				if src == "" {
					return
				}

				// Handle different URL formats
				if strings.HasPrefix(src, "//") {
					src = "https:" + src
				} else if !strings.HasPrefix(src, "http") {
					src = "https://www.cnbce.com" + strings.TrimLeft(src, "/")
				}

				// Skip small images and icons
				lowerSrc := strings.ToLower(src)
				if !strings.HasSuffix(lowerSrc, ".svg") &&
					!strings.Contains(lowerSrc, "icon") &&
					!strings.Contains(lowerSrc, "logo") &&
					!contains(images, src) {
					images = append(images, src)
				}
			}
		})
	}

	// Clean up the content
	contentDiv.Find(`script, style, iframe, noscript, .ad, .advertisement, .social-share, .related-news, .tags, .author, .date`).Remove()

	// Get the HTML content
	content, err := contentDiv.Html()
	if err != nil {
		return "", images, fmt.Errorf("error getting HTML content: %v", err)
	}

	content = strings.TrimSpace(html.UnescapeString(content))

	// Ensure we return at least an empty slice, not nil
	if images == nil {
		images = []string{}
	}

	return content, images, nil
}
