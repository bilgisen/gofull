package extractors

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
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
		imgs := extractImagesFromHTML(doc.Content)
		return sanitizeHTML(doc.Content), imgs, nil
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
	return d.extractFromHTML(string(bodyBytes))
}

// extractFromHTML attempts to find the main article container in a raw HTML string.
func (d *DefaultExtractor) extractFromHTML(body string) (string, []string, error) {
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
				imgs := extractImagesFromHTML(htmlStr)
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

// extractImagesFromHTML collects all non-empty <img src="..."> values.
func extractImagesFromHTML(html string) []string {
	var images []string
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}

	doc.Find("img").Each(func(_ int, s *goquery.Selection) {
		if src, ok := s.Attr("src"); ok && src != "" {
			if !strings.HasPrefix(src, "data:") && len(src) > 6 {
				images = append(images, src)
			}
		}
	})
	return images
}
