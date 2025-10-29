package extractors

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// EkonomimExtractor handles content extraction for ekonomim.com domain.
// It processes article URLs on the domain.
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
		// Handle map[string]string with "content" or "html" key
		if content, ok := v["content"]; ok {
			return e.cleanContent(content), nil, nil
		}
		if htmlContent, ok := v["html"]; ok {
			return e.extractFromHTML(htmlContent)
		}

	case map[string]interface{}:
		// Handle map[string]interface{} with "content" or "html" key
		if content, ok := v["content"].(string); ok && content != "" {
			return e.cleanContent(content), nil, nil
		}
		if htmlContent, ok := v["html"].(string); ok && htmlContent != "" {
			return e.extractFromHTML(htmlContent)
		}
		// If no content/html key, try to get URL from common fields
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
	// Skip processing for filtered URLs
	filteredPrefixes := []string{
		"https://www.ekonomim.com/spor/",
		"https://www.ekonomim.com/dunya/",
		"https://www.ekonomim.com/foto-galeri/",
		"https://www.ekonomim.com/finans/",
		"https://www.ekonomim.com/sektorler/",
		"https://www.ekonomim.com/yasam/",
		"https://www.ekonomim.com/ekonomi/",
		"https://www.ekonomim.com/sirketler/",
		"https://www.ekonomim.com/yazar/",
		"https://www.ekonomim.com/yazarlar/",
		"https://www.ekonomim.com/son-dakika/",
	}

	for _, prefix := range filteredPrefixes {
		if strings.HasPrefix(articleURL, prefix) {
			return "", nil, fmt.Errorf("URL is in filtered list: %s", articleURL)
		}
	}

	// Basic URL validation
	if !strings.Contains(articleURL, "ekonomim.com/") || 
	   !strings.Contains(articleURL, "-") {
		return "", nil, fmt.Errorf("not a valid Ekonomim article URL: %s", articleURL)
	}

	resp, err := e.httpClient.Get(articleURL)
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
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
					img = "https://www.ekonomim.com" + strings.TrimLeft(img, "/")
				} else if strings.HasPrefix(img, "//") {
					img = "https:" + img
				}
				images = []string{img}
				break
			}
		}
	}

	// Get the main content - specifically target div.content-text with property="articleBody"
	contentDiv := doc.Find(`div.content-text[property="articleBody"]`).First()
	if contentDiv.Length() == 0 {
		// Fallback to other possible content containers if the main one is not found
		contentDiv = doc.Find("div.content-text, div.article-content, article, .content").First()
	}

	// If we still don't have content, return an error
	if contentDiv.Length() == 0 {
		return "", images, fmt.Errorf("could not find main content in the page")
	}

	// Remove unwanted elements that might be inside the content
	contentDiv.Find(`script, style, iframe, noscript, .ad, .advertisement, .social-share, 
		.related-news, .tags, .author, .date, .comments, .adpro, the-ads, 
		.picture-bottom_wrapper, .google-news_wrapper, .mceNonEditable`).Remove()

	// Get the HTML content and clean it up
	content, err := contentDiv.Html()
	if err != nil {
		return "", images, fmt.Errorf("error getting HTML content: %v", err)
	}

	// Clean the content by removing any remaining HTML tags
	content = e.cleanContent(content)

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

	return content, images, nil
}

// cleanContent removes HTML tags and cleans up the content
func (e *EkonomimExtractor) cleanContent(content string) string {
	// Remove CDATA if present
	content = strings.ReplaceAll(content, "<![CDATA[", "")
	content = strings.ReplaceAll(content, "]]>", "")

	// Remove HTML tags using a simple regex
	re := regexp.MustCompile(`<[^>]*>`)
	content = re.ReplaceAllString(content, "")

	// Replace HTML entities
	replacer := strings.NewReplacer(
		"&quot;", "\"",
		"&amp;", "&",
		"&lt;", "<",
		"&gt;", ">",
		"&nbsp;", " ",
		"&apos;", "'",
	)
	content = replacer.Replace(content)

	// Clean up whitespace
	content = strings.TrimSpace(content)
	content = strings.Join(strings.Fields(content), " ")

	return content
}

// extractImagesFromMeta extracts image URLs from Open Graph and Twitter Card meta tags.
func (e *EkonomimExtractor) extractImagesFromMeta(doc *goquery.Document) []string {
	var images []string

	// Check common meta tags for images
	metaSelectors := []struct {
		selector string
		attr     string
	}{
		{`meta[property="og:image"]`, "content"},
		{`meta[name="twitter:image"]`, "content"},
		{`meta[property="og:image:url"]`, "content"},
		{`meta[itemprop="image"]`, "content"},
	}

	for _, meta := range metaSelectors {
		doc.Find(meta.selector).Each(func(i int, s *goquery.Selection) {
			if src, exists := s.Attr(meta.attr); exists && src != "" {
				src = strings.TrimSpace(src)
				if !strings.HasPrefix(src, "http") {
					src = "https://www.ekonomim.com" + src
				}
				images = append(images, src)
			}
		})
	}

	return images
}
