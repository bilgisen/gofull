package extractors

import (
	"context"
	"errors"
	"fmt"
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
		if html, ok := v["html"]; ok {
			return d.extractFromHTML(html)
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

	// Try common containers
	selectors := []string{
		"article",
		"main",
		".article-body",
		".post-content",
		".entry-content",
		".content",
		".news-detail__content",
		".story-body",
	}

	for _, sel := range selectors {
		if s := doc.Find(sel).First(); s.Length() > 0 {
			s.Find("script, iframe, style, .ad, .advertisement, .promo, .related, .share").Remove()
			htmlStr, _ := s.Html()
			htmlStr = sanitizeHTML(htmlStr)
			if htmlStr != "" {
				// Try to get meta tag images first, then fall back to content images
				imgs := extractImagesFromMetaTags(body)
				if len(imgs) == 0 {
					imgs = extractImagesFromHTMLWithBase(htmlStr, baseURL)
				}
				return htmlStr, imgs, nil
			}
		}
	}

	return "", nil, errors.New("no main article content found")
}

// sanitizeHTML ensures consistent wrapping and line breaks.
func sanitizeHTML(html string) string {
	html = strings.TrimSpace(html)
	if html == "" {
		return ""
	}
	if !strings.HasPrefix(html, "<div") {
		html = fmt.Sprintf(`<div class="gofull-article">%s</div>`, html)
	}
	return html
}

// extractImagesFromMetaTags extracts image URLs from Open Graph and Twitter Card meta tags.
func extractImagesFromMetaTags(html string) []string {
	var images []string
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}

	// Open Graph image
	if ogImage, exists := doc.Find(`meta[property="og:image"]`).Attr("content"); exists && ogImage != "" {
		images = append(images, ogImage)
	}

	// Twitter Card image
	if twitterImage, exists := doc.Find(`meta[name="twitter:image"]`).Attr("content"); exists && twitterImage != "" {
		images = append(images, twitterImage)
	}

	// Article image (schema.org)
	if articleImage, exists := doc.Find(`meta[property="article:image"]`).Attr("content"); exists && articleImage != "" {
		images = append(images, articleImage)
	}

	return images
}

// extractImagesFromHTMLWithBase collects all non-empty <img src="..."> values.
func extractImagesFromHTMLWithBase(html, baseURL string) []string {
	var images []string
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}

	doc.Find("img").Each(func(_ int, s *goquery.Selection) {
		if src, ok := s.Attr("src"); ok && src != "" {
			// Skip data URLs and very short URLs
			if strings.HasPrefix(src, "data:") || len(src) < 6 {
				return
			}

			// Convert relative URLs to absolute URLs
			if !strings.HasPrefix(src, "http") {
				// Handle protocol-relative URLs (//example.com/image.jpg)
				if strings.HasPrefix(src, "//") {
					images = append(images, "https:"+src)
					return
				}

				// Handle relative URLs
				if strings.HasPrefix(src, "/") {
					// Absolute path from domain root
					if parsedURL, err := url.Parse(baseURL); err == nil {
						images = append(images, parsedURL.Scheme+"://"+parsedURL.Host+src)
					}
				} else {
					// Relative path - combine with base URL path
					if parsedURL, err := url.Parse(baseURL); err == nil {
						baseDir := strings.TrimSuffix(parsedURL.Path, "/")
						fullURL := baseDir + "/" + strings.TrimPrefix(src, "./")
						images = append(images, parsedURL.Scheme+"://"+parsedURL.Host+fullURL)
					}
				}
			} else {
				images = append(images, src)
			}
		}
	})
	return images
}
