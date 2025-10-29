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
		// Handle map[string]string with "html" key
		if htmlContent, ok := v["html"]; ok {
			return d.extractFromHTML(htmlContent)
		}

	case map[string]interface{}:
		// Handle map[string]interface{} with "html" key
		if htmlContent, ok := v["html"].(string); ok {
			return d.extractFromHTML(htmlContent)
		}
		// If no html key, try to get URL from common fields
		if url, ok := v["url"].(string); ok && url != "" {
			return d.extractFromURL(url)
		}
		if link, ok := v["link"].(string); ok && link != "" {
			return d.extractFromURL(link)
		}

	default:
		return "", nil, fmt.Errorf("unsupported input type: %T", input)
	}

	return "", nil, errors.New("invalid input format - expected URL or map with 'html' content")
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
	}

	for _, meta := range metaSelectors {
		if img, exists := doc.Find(meta.selector).First().Attr(meta.attr); exists && img != "" {
			img = strings.TrimSpace(img)
			if img != "" {
				if !strings.HasPrefix(img, "http") && !strings.HasPrefix(img, "//") {
					img = "https://www.dunya.com" + strings.TrimLeft(img, "/")
				} else if strings.HasPrefix(img, "//") {
					img = "https:" + img
				}
				images = []string{img}
				break
			}
		}
	}

	// Get the main content - specifically target div.content-text
	contentDiv := doc.Find("div.content-text").First()
	if contentDiv.Length() == 0 {
		// Fallback to other possible content containers if the main one is not found
		contentDiv = doc.Find(`div[property="articleBody"], .article-content`).First()
	}

	// If we still don't have content, return an error
	if contentDiv.Length() == 0 {
		return "", images, fmt.Errorf("could not find main content in the page")
	}

	// Remove unwanted elements that might be inside the content
	contentDiv.Find(`script, style, iframe, noscript, .ad, .advertisement, .social-share, .related-news, .tags, .author, .date`).Remove()

	// Clean up the content
	content, err := contentDiv.Html()
	if err != nil {
		return "", images, fmt.Errorf("error getting HTML content: %v", err)
	}

	content = strings.TrimSpace(html.UnescapeString(content))

	// If no meta tag image found, try to find it in the content
	if len(images) == 0 {
		// Look for images only within the content area
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
					src = "https://www.dunya.com" + strings.TrimLeft(src, "/")
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

	// Ensure we return at least an empty slice, not nil
	if images == nil {
		images = []string{}
	}

	return content, images, nil
}
