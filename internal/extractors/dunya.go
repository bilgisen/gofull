package extractors

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// DunyaExtractor handles content extraction for dunya.com domain.
// It processes any URL on the domain without specific pattern filtering.
type DunyaExtractor struct {
	httpClient *http.Client
	// allowedPrefixes kaldırıldı
}

// NewDunyaExtractor creates a new DunyaExtractor.
func NewDunyaExtractor(client *http.Client) *DunyaExtractor {
	if client == nil {
		client = http.DefaultClient
	}
	return &DunyaExtractor{
		httpClient: client,
		// allowedPrefixes yok
	}
}

// Extract implements the Extractor interface for dunya.com URLs.
// It now processes any URL string passed to it.
func (d *DunyaExtractor) Extract(input any) (string, []string, error) {
	switch v := input.(type) {
	case string:
		// Input is a URL, fetch and extract content (no prefix check)
		return d.extractFromURL(v)
	case map[string]string:
		// Input is HTML content
		if htmlContent, ok := v["html"]; ok {
			return d.extractFromHTML(htmlContent)
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
func (d *DunyaExtractor) extractFromHTML(htmlContent string) (string, []string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", nil, err
	}

	// First, try to get meta tag images (more reliable for main article image)
	var images []string
	if ogImage, exists := doc.Find(`meta[property="og:image"]`).Attr("content"); exists && ogImage != "" {
		images = append(images, ogImage)
	}
	if twitterImage, exists := doc.Find(`meta[name="twitter:image"]`).Attr("content"); exists && twitterImage != "" {
		images = append(images, twitterImage)
	}

	// Target the specific content div for dunya.com
	contentDiv := doc.Find(`div.content-text[property="articleBody"]`).First()
	if contentDiv.Length() == 0 {
		return "", nil, fmt.Errorf("content div not found")
	}

	// Remove unwanted elements
	contentDiv.Find("script, style, .ad, .advertisement, .social-share").Remove()

	// Extract images from content as fallback
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
	htmlContent, err = contentDiv.Html()
	if err != nil {
		return "", nil, err
	}

	// Decode HTML entities (e.g., &ouml; -> ö) in the raw HTML string
	decodedHTML := html.UnescapeString(htmlContent)

	// Clean up the HTML
	decodedHTML = strings.TrimSpace(decodedHTML)
	if decodedHTML == "" {
		return "", nil, fmt.Errorf("empty content")
	}

	return decodedHTML, images, nil
}