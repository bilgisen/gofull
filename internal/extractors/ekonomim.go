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

// EkonomimExtractor handles content extraction for ekonomim.com domain.
// It processes any URL on the domain without specific pattern filtering.
type EkonomimExtractor struct {
	httpClient *http.Client
}

// NewEkonomimExtractor creates a new EkonomimExtractor.
func NewEkonomimExtractor(client *http.Client) *EkonomimExtractor {
	if client == nil {
		client = http.DefaultClient
	}
	return &EkonomimExtractor{
		httpClient: client,
	}
}

// Extract implements the Extractor interface for ekonomim.com URLs.
func (e *EkonomimExtractor) Extract(input any) (string, []string, error) {
	switch v := input.(type) {
	case string:
		// Input is a URL, fetch and extract content
		return e.extractFromURL(v)

	case map[string]string:
		// Handle map[string]string with "html" key
		if htmlContent, ok := v["html"]; ok {
			return e.extractFromHTML(htmlContent)
		}

	case map[string]interface{}:
		// Handle map[string]interface{} with "html" key
		if htmlContent, ok := v["html"].(string); ok {
			return e.extractFromHTML(htmlContent)
		}
		// If no html key, try to get URL from common fields
		if url, ok := v["url"].(string); ok && url != "" {
			return e.extractFromURL(url)
		}
		if link, ok := v["link"].(string); ok && link != "" {
			return e.extractFromURL(link)
		}

	default:
		return "", nil, fmt.Errorf("unsupported input type: %T", input)
	}

	return "", nil, errors.New("invalid input format - expected URL or map with 'html' content")
}

// extractFromURL fetches the URL and extracts content.
func (e *EkonomimExtractor) extractFromURL(articleURL string) (string, []string, error) {
	req, err := http.NewRequest("GET", articleURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers to mimic a browser
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("HTTP status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read response body: %w", err)
	}

	return e.extractFromHTML(string(bodyBytes))
}

// extractFromHTML extracts content from HTML using ekonomim.com specific selectors.
func (e *EkonomimExtractor) extractFromHTML(htmlContent string) (string, []string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Try to get the main image from meta tags (most reliable)
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
					img = "https://www.ekonomim.com" + strings.TrimLeft(img, "/")
				} else if strings.HasPrefix(img, "//") {
					img = "https:" + img
				}
				images = []string{img}
				break
			}
		}
	}

	// Get the main content - try multiple selectors to find the main article content
	var contentDiv *goquery.Selection
	
	// Try different selectors in order of preference
	selectors := []string{
		`div.content-text[property="articleBody"]`,
		`div.content-text`,
		`div[class*="content-text"]`,
		`div.article-content`,
		`article`,
		`.content`,
	}

	for _, selector := range selectors {
		contentDiv = doc.Find(selector).First()
		if contentDiv.Length() > 0 {
			break
		}
	}

	// If we still don't have content, return an error
	if contentDiv.Length() == 0 {
		return "", images, fmt.Errorf("could not find main content in the page")
	}

	// Remove unwanted elements that might be inside the content
	contentDiv.Find(`script, style, iframe, noscript, .ad, .advertisement, .social-share, 
		.related-news, .tags, .author, .date, .comments, .adpro, the-ads, 
		.picture-bottom_wrapper, .google-news_wrapper, .mceNonEditable`).Remove()

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
					src = "https://www.ekonomim.com" + strings.TrimLeft(src, "/")
				}

				// Skip small images and icons
				lowerSrc := strings.ToLower(src)
				if !strings.HasSuffix(lowerSrc, ".svg") && 
				   !strings.Contains(lowerSrc, "icon") && 
				   !strings.Contains(lowerSrc, "logo") {
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
