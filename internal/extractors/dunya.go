package extractors

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// DunyaExtractor handles content extraction for dunya.com domain.
// Only processes URLs that match specific patterns.
type DunyaExtractor struct {
	httpClient *http.Client
	allowedPatterns []string
}

// NewDunyaExtractor creates a new DunyaExtractor with URL pattern filtering.
func NewDunyaExtractor(client *http.Client) *DunyaExtractor {
	if client == nil {
		client = http.DefaultClient
	}
	return &DunyaExtractor{
		httpClient: client,
		allowedPatterns: []string{
			"is-dunyasi",  // İş dünyası kategorisi
			"finans",      // Finans kategorisi
			"sektorler",      // Sektorler kategorisi
		},
	}
}

// shouldProcessURL checks if a URL should be processed based on patterns.
func (d *DunyaExtractor) shouldProcessURL(url string) bool {
	// Check if URL matches any of the allowed patterns
	for _, pattern := range d.allowedPatterns {
		if strings.Contains(url, pattern) {
			return true
		}
	}

	// Also allow specific category patterns
	if strings.HasPrefix(url, "https://www.dunya.com/is-dunyasi/") ||
	   strings.HasPrefix(url, "https://www.dunya.com/finans/") ||
	   strings.HasPrefix(url, "https://www.dunya.com/sektorler/") {
		return true
	}

	return false
}

// Extract implements the Extractor interface for dunya.com URLs.
func (d *DunyaExtractor) Extract(input any) (string, []string, error) {
	switch v := input.(type) {
	case string:
		// Check if URL matches allowed patterns
		if !d.shouldProcessURL(v) {
			return "", nil, fmt.Errorf("URL does not match allowed patterns for dunya.com")
		}
		// Input is a URL, fetch and extract content
		return d.extractFromURL(v)
	case map[string]string:
		// Input is HTML content - we can't check URL pattern here, so allow it
		if html, ok := v["html"]; ok {
			return d.extractFromHTML(html)
		}
		return "", nil, fmt.Errorf("missing 'html' key in input map")
	default:
		return "", nil, fmt.Errorf("unsupported input type %T", input)
	}
}

// extractFromURL fetches the URL and extracts content.
func (d *DunyaExtractor) extractFromURL(articleURL string) (string, []string, error) {
	resp, err := d.httpClient.Get(articleURL)
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

	return d.extractFromHTML(string(bodyBytes))
}

// extractFromHTML extracts content from HTML using dunya.com specific selectors.
func (d *DunyaExtractor) extractFromHTML(html string) (string, []string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return "", nil, err
	}

	// Target the specific content div for dunya.com
	contentDiv := doc.Find(`div.content-text[property="articleBody"]`).First()
	if contentDiv.Length() == 0 {
		return "", nil, fmt.Errorf("content div not found")
	}

	// Remove unwanted elements
	contentDiv.Find("script, style, .ad, .advertisement, .social-share").Remove()

	// Extract images before getting HTML
	var images []string
	contentDiv.Find("img").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists && src != "" {
			if strings.HasPrefix(src, "http") {
				images = append(images, src)
			} else if strings.HasPrefix(src, "//") {
				images = append(images, "https:"+src)
			}
		}
	})

	// Get the HTML content
	htmlContent, err := contentDiv.Html()
	if err != nil {
		return "", nil, err
	}

	// Clean up the HTML
	htmlContent = strings.TrimSpace(htmlContent)
	if htmlContent == "" {
		return "", nil, fmt.Errorf("empty content")
	}

	return htmlContent, images, nil
}
