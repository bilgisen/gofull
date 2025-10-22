// internal/extractors/default.go
package extractors

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-shiori/go-readability"
)

// DefaultExtractor uses go-readability primarily and goquery as a fallback.
type DefaultExtractor struct {
	httpClient *http.Client
	userAgent  string
}

// NewDefaultExtractor constructs a DefaultExtractor.
// If client is nil, http.DefaultClient is used.
func NewDefaultExtractor(client *http.Client) *DefaultExtractor {
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Second,
		}
	}
	return &DefaultExtractor{
		httpClient: client,
		userAgent:  "Mozilla/5.0 (compatible; GoFullFeedBot/1.1; +https://gofull.app/bot)",
	}
}

// Extract tries to extract readable HTML and image URLs from a URL or raw HTML string.
func (d *DefaultExtractor) Extract(input any) (string, []string, error) {
	switch v := input.(type) {
	case string:
		// Assume it's a URL
		return d.extractFromURL(v)

	case map[string]string:
		// Expecting {"html": "<raw html>"}
		if htmlContent, ok := v["html"]; ok {
			return d.extractFromHTML(htmlContent)
		}
		return "", nil, errors.New("missing 'html' key in input map")

	default:
		return "", nil, fmt.Errorf("unsupported input type %T", input)
	}
}

func (d *DefaultExtractor) extractFromURL(articleURL string) (string, []string, error) {
	// First: try go-readability
	doc, err := readability.FromURL(articleURL, 15*time.Second)
	if err == nil && strings.TrimSpace(doc.Content) != "" {
		// Try to get meta tag images first, then fall back to content images
		imgUrls := extractImagesFromMetaTags(doc.Content)
		if len(imgUrls) == 0 {
			imgUrls = extractImagesFromHTMLWithBase(doc.Content, articleURL)
		}
		return sanitizeHTML(doc.Content), imgUrls, nil
	}

	// Second: manual fetch and goquery fallback
	req, err := http.NewRequestWithContext(context.Background(), "GET", articleURL, nil)
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("User-Agent", d.userAgent)

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}
	return d.extractFromHTMLWithBase(string(bodyBytes), articleURL)
}

// extractFromHTML attempts to find the main article container in a raw HTML string.
func (d *DefaultExtractor) extractFromHTML(body string) (string, []string, error) {
	return d.extractFromHTMLWithBase(body, "")
}

// extractFromHTMLWithBase attempts to find the main article container in a raw HTML string with base URL for relative links.
func (d *DefaultExtractor) extractFromHTMLWithBase(body, baseURL string) (string, []string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
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
	}

	for _, meta := range metaSelectors {
		if img, exists := doc.Find(meta.selector).First().Attr(meta.attr); exists && img != "" {
			img = strings.TrimSpace(img)
			if img != "" {
				images = []string{img} // We found our main image
				break
			}
		}
	}

	// If no meta tag image found, try to find it in the content
	if len(images) == 0 {
		// Look for figure with post-image class first, then other common patterns
		// Add cnbce.com specific selectors
		selectors := "figure.post-image img, .post-image img, figure img, img, .article-image img, .entry-thumbnail img, .wp-post-image, .post-thumbnail img, .article-header-image img, .featured-image img, .post-featured-image img, .td-post-featured-image img"
		
		doc.Find(selectors).Each(func(i int, s *goquery.Selection) {
			if src, exists := s.Attr("src"); exists && src != "" {
				src = strings.TrimSpace(src)
				if src == "" {
					return
				}

				// Handle different URL formats
				if strings.HasPrefix(src, "//") {
					src = "https:" + src
				} else if !strings.HasPrefix(src, "http") {
					if baseURL != "" {
						base, err := url.Parse(baseURL)
						if err == nil {
							src = base.Scheme + "://" + base.Host + "/" + strings.TrimLeft(src, "/")
						}
					} else {
						src = "https://" + strings.TrimLeft(src, "/")
					}
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

	// Get the main content
	contentDiv := doc.Find(`article, main, [role="main"], [itemprop="articleBody"], .post-content, .entry-content, .article-content, .content, body`).First()

	// Clean up the content
	content, err := contentDiv.Html()
	if err != nil {
		return "", images, fmt.Errorf("error getting HTML content: %v", err)
	}

	// Clean up the content
	content = strings.TrimSpace(html.UnescapeString(content))

	return content, images, nil
}

// sanitizeHTML ensures consistent wrapping and line breaks.
func sanitizeHTML(htmlContent string) string {
	htmlContent = strings.TrimSpace(htmlContent)
	if htmlContent == "" {
		return ""
	}
	if !strings.HasPrefix(htmlContent, "<div") {
		htmlContent = fmt.Sprintf(`<div class="gofull-article">%s</div>`, htmlContent)
	}
	return htmlContent
}

// extractImagesFromMetaTags extracts image URLs from Open Graph and Twitter Card meta tags.
func extractImagesFromMetaTags(htmlContent string) []string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	var urls []string

	// Check common meta tags for images
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
		{`meta[name="thumbnail"]`, "content"},
		{`meta[itemprop="image"]`, "content"},
	}

	for _, meta := range metaSelectors {
		if img, exists := doc.Find(meta.selector).First().Attr(meta.attr); exists && img != "" {
			img = strings.TrimSpace(img)
			if img != "" && !contains(urls, img) {
				urls = append(urls, img)
			}
		}
	}

	return urls
}

// extractImagesFromHTMLWithBase collects all non-empty <img src="..."> values.
func extractImagesFromHTMLWithBase(htmlContent, baseURL string) []string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	var images []string

	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists && src != "" {
			src = strings.TrimSpace(src)
			if src == "" {
				return
			}

			// Handle protocol-relative URLs
			if strings.HasPrefix(src, "//") {
				src = "https:" + src
			} else if baseURL != "" && !strings.HasPrefix(src, "http") {
				// Handle relative URLs if baseURL is provided
				base, err := url.Parse(baseURL)
				if err == nil {
					// Create absolute URL from relative path
					if strings.HasPrefix(src, "/") {
						// Absolute path
						src = base.Scheme + "://" + base.Host + src
					} else {
						// Relative path
						basePath := base.Path
						if !strings.HasSuffix(basePath, "/") {
							// Remove filename from base path
							lastSlash := strings.LastIndex(basePath, "/")
							if lastSlash >= 0 {
								basePath = basePath[:lastSlash+1]
							}
						}
						src = base.Scheme + "://" + base.Host + basePath + src
					}
				}
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

	return images
}