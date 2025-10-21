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
	logger     interface{ Printf(...interface{}) }
}

// NewDefaultExtractor constructs a DefaultExtractor. The httpClient can be nil to use http.DefaultClient.
// The logger must implement the interface{ Printf(...interface{}) } method, e.g., *zap.SugaredLogger.
func NewDefaultExtractor(client *http.Client, logger interface{ Printf(...interface{}) }) *DefaultExtractor {
	return &DefaultExtractor{
		httpClient: client,
		userAgent:  "Mozilla/5.0 (compatible; RSSFullTextBot/1.0)",
		logger:     logger,
	}
}

// Extract implements the extractors.Extractor interface.
// It expects the input to be a string representing a URL.
func (d *DefaultExtractor) Extract(input any) (string, []string, error) {
	// Gelen input'un bir string olup olmadığını kontrol et
	urlStr, ok := input.(string)
	if !ok {
		return "", nil, fmt.Errorf("DefaultExtractor: input must be a string (URL), got %T", input)
	}

	// String geldiğinde, bunu URL olarak değerlendir
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
		d.logger.Printf("http get failed: %v", err)
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

func extractImagesFromHTML(_ string) []string {
	// placeholder: readability's doc.Byline isn't image list; real extraction would parse doc's HTML
	return nil
}