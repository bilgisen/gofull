package extractors

import (
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// IlketvExtractor handles content extraction for ilketv.com.tr domain.
type IlketvExtractor struct {
	httpClient *http.Client
}

// NewIlketvExtractor creates a new IlketvExtractor.
func NewIlketvExtractor(client *http.Client) *IlketvExtractor {
	if client == nil {
		client = http.DefaultClient
	}
	return &IlketvExtractor{
		httpClient: client,
	}
}

// Extract implements the Extractor interface for ilketv.com.tr URLs.
func (e *IlketvExtractor) Extract(input any) (string, []string, error) {
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
			return e.extractFromHTML(strings.NewReader(htmlContent))
		}

	case map[string]interface{}:
		// Handle map[string]interface{} with "content" or "html" key
		if content, ok := v["content"].(string); ok && content != "" {
			return e.cleanContent(content), nil, nil
		}
		if htmlContent, ok := v["html"].(string); ok && htmlContent != "" {
			return e.extractFromHTML(strings.NewReader(htmlContent))
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
func (e *IlketvExtractor) extractFromURL(url string) (string, []string, error) {
	// Skip processing for filtered URLs
	filteredPrefixes := []string{
		"https://www.ilketv.com.tr/video/",
		"https://www.ilketv.com.tr/galeri/",
		"https://www.ilketv.com.tr/foto-galeri/",
		"https://www.ilketv.com.tr/yazarlar/",
		"https://www.ilketv.com.tr/kose-yazilari/",
	}

	for _, prefix := range filteredPrefixes {
		if strings.HasPrefix(url, prefix) {
			return "", nil, fmt.Errorf("URL is in filtered list: %s", url)
		}
	}

	// Only process article URLs
	if !strings.Contains(url, "ilketv.com.tr/") || !strings.Contains(url, "-") {
		return "", nil, fmt.Errorf("not an Ilketv article URL: %s", url)
	}

	resp, err := e.httpClient.Get(url)
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	content, images, err := e.extractFromHTML(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to extract content: %w", err)
	}

	return content, images, nil
}

// extractFromHTML extracts content from HTML using ilketv.com.tr specific selectors.
func (e *IlketvExtractor) extractFromHTML(reader io.Reader) (string, []string, error) {
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Try to get content from feed content field first
	if content := doc.Find("content").Text(); content != "" {
		return e.cleanContent(content), nil, nil
	}

	// Extract main content - try multiple selectors for Ilketv
	var content string
	selectors := []string{
		"div.news-content",
		"div.article-content",
		"div.content-text",
		"div.detail-content",
		"article",
		".content",
		".article-body",
		".news-text",
	}

	for _, selector := range selectors {
		doc.Find(selector).Each(func(i int, s *goquery.Selection) {
			// Remove unwanted elements
			s.Find("script, style, iframe, noscript, .ad, .advertisement, .social-share, .related-news, .tags, .author-info, .date-info").Remove()
			
			// Get the clean HTML
			html, _ := s.Html()
			content = strings.TrimSpace(html)
			if content != "" {
				return // Stop after finding content
			}
		})
		if content != "" {
			break
		}
	}

	// Clean the content by removing HTML tags
	content = e.cleanContent(content)

	// Extract images from meta tags
	images := e.extractImagesFromMeta(doc)

	// If no images found in meta, try to find in content
	if len(images) == 0 {
		for _, selector := range selectors {
			doc.Find(selector + " img").Each(func(i int, s *goquery.Selection) {
				if src, exists := s.Attr("src"); exists && src != "" {
					src = strings.TrimSpace(src)
					if !strings.HasPrefix(src, "http") {
						src = "https://www.ilketv.com.tr" + src
					}
					if !contains(images, src) {
						images = append(images, src)
					}
				}
			})
			if len(images) > 0 {
				break
			}
		}
	}

	return content, images, nil
}

// cleanContent removes HTML tags and cleans up the content
func (e *IlketvExtractor) cleanContent(content string) string {
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
		"&mdash;", "—",
		"&ndash;", "–",
		"&rsquo;", "'",
		"&lsquo;", "'",
		"&rdquo;", "\"",
		"&ldquo;", "\"",
	)
	content = replacer.Replace(content)

	// Clean up whitespace
	content = strings.TrimSpace(content)
	content = strings.Join(strings.Fields(content), " ")

	return content
}

// extractImagesFromMeta extracts image URLs from Open Graph and Twitter Card meta tags.
func (e *IlketvExtractor) extractImagesFromMeta(doc *goquery.Document) []string {
	var images []string

	// Check common meta tags for images
	metaSelectors := []struct {
		selector string
		attr     string
	}{
		{`meta[property="og:image"]`, "content"},
		{`meta[name="twitter:image"]`, "content"},
		{`link[rel="image_src"]`, "href"},
		{`meta[property="og:image:url"]`, "content"},
		{`meta[name="msapplication-TileImage"]`, "content"},
	}

	for _, meta := range metaSelectors {
		doc.Find(meta.selector).Each(func(i int, s *goquery.Selection) {
			if url, exists := s.Attr(meta.attr); exists && url != "" {
				url = strings.TrimSpace(url)
				url = html.UnescapeString(url)
				
				if !strings.HasPrefix(url, "http") && !strings.HasPrefix(url, "//") {
					url = "https://www.ilketv.com.tr" + strings.TrimLeft(url, "/")
				} else if strings.HasPrefix(url, "//") {
					url = "https:" + url
				}
				
				// Skip common non-content images
				lowerUrl := strings.ToLower(url)
				if !strings.Contains(lowerUrl, "logo") && 
				   !strings.Contains(lowerUrl, "icon") && 
				   !strings.Contains(lowerUrl, "favicon") &&
				   !contains(images, url) {
					images = append(images, url)
				}
			}
		})
	}

	return images
}
