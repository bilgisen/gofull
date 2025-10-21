// FILE: internal/extractors/default.go
package extractors

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-shiori/go-readability"
	"github.com/PuerkitoBio/goquery"
)

// DefaultExtractor uses go-readability and goquery fallback to extract content.
type DefaultExtractor struct {
	httpClient *http.Client
	userAgent  string
}

// NewDefaultExtractor constructs a DefaultExtractor. The httpClient can be nil to use http.DefaultClient.
func NewDefaultExtractor(client *http.Client) *DefaultExtractor {
	return &DefaultExtractor{
		httpClient: client,
		userAgent:  "Mozilla/5.0 (compatible; RSSFullTextBot/1.0)",
	}
}

// Extract implements the extractors.Extractor interface.
// It expects the input to be a string representing a URL.
func (d *DefaultExtractor) Extract(input any) (string, []string, error) {
	// Check if input is a string
	urlStr, ok := input.(string)
	if !ok {
		return "", nil, fmt.Errorf("DefaultExtractor: input must be a string (URL), got %T", input)
	}

	// Use the string as article URL
	articleURL := urlStr

	if d.httpClient == nil {
		d.httpClient = http.DefaultClient
	}

	// First try go-readability (it fetches itself internally)
	if doc, err := readability.FromURL(articleURL, 15*time.Second); err == nil {
		content := strings.TrimSpace(doc.TextContent)
		if content != "" {
			return wrapAsHTML(content), extractImagesFromHTML(doc.Byline), nil
		}
	}

	// Fallback: fetch raw HTML and use goquery selectors to try to find main article
	resp, err := d.httpClient.Get(articleURL)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}
	bodyStr := string(bodyBytes)
	// try to parse and find common article containers
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(bodyStr))
	if err != nil {
		return "", nil, err
	}

	// heuristics: try common article selectors
	candidates := []string{"article", ".article-body", ".content", ".news-detail__content", ".story-body"}
	var foundHTML string
	for _, sel := range candidates {
		if sel == "article" {
			if s := doc.Find("article").First(); s.Length() > 0 {
				// remove scripts/iframes/ads
				s.Find("script, iframe, .ad, .advertisement, .social-share, .related-news").Remove()
				htmlStr, _ := s.Html()
				foundHTML = strings.TrimSpace(htmlStr)
				break
			}
		} else {
			if s := doc.Find(sel).First(); s.Length() > 0 {
				s.Find("script, iframe, .ad, .advertisement, .social-share, .related-news").Remove()
				htmlStr, _ := s.Html()
				foundHTML = strings.TrimSpace(htmlStr)
				break
			}
		}
	}

	if foundHTML != "" {
		imgs := []string{}
		doc.Find("img").Each(func(i int, s *goquery.Selection) {
			if src, ok := s.Attr("src"); ok {
				imgs = append(imgs, src)
			}
		})
		return foundHTML, imgs, nil
	}

	return "", nil, fmt.Errorf("no extractor matched")
}

func wrapAsHTML(text string) string {
	// wrap plain text into a simple HTML container
	escaped := strings.ReplaceAll(text, "\n", "<br>\n")
	return fmt.Sprintf(`<div class="gofull-article">%s</div>`, escaped)
}

func extractImagesFromHTML(html string) []string {
	// Basic image extraction from HTML - look for img tags
	var images []string
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil
	}

	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists && src != "" {
			// Basic validation - skip data URLs and very short strings
			if !strings.HasPrefix(src, "data:") && len(src) > 5 {
				images = append(images, src)
			}
		}
	})

	return images
}