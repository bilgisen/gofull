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
func (e *EkonomimExtractor) extractFromURL(url string) (string, []string, error) {
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
		if strings.HasPrefix(url, prefix) {
			return "", nil, fmt.Errorf("URL is in filtered list: %s", url)
		}
	}

	// Only process article URLs under allowed paths
	if !strings.Contains(url, "ekonomim.com/") || 
	   !strings.Contains(url, "-") || 
	   !strings.HasPrefix(url, "https://www.ekonomim.com/gundem/") {
		return "", nil, fmt.Errorf("not an allowed Ekonomim article URL: %s", url)
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

// extractFromHTML extracts content from HTML using ekonomim.com specific selectors.
func (e *EkonomimExtractor) extractFromHTML(reader io.Reader) (string, []string, error) {
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Extract main content from content-text div
	var content string
	doc.Find(`div.content-text[property="articleBody"]`).Each(func(i int, s *goquery.Selection) {
		// Remove unwanted elements
		s.Find(`script, style, iframe, noscript, .ad, .advertisement, .social-share, 
			.related-news, .tags, .author, .date, .comments, .adpro, the-ads`).Remove()
		
		// Get the clean HTML
		html, _ := s.Html()
		content = strings.TrimSpace(html)
	})

	// If content not found with specific selector, try fallback selectors
	if content == "" {
		doc.Find("div.article-content, article, .content, .article-body").Each(func(i int, s *goquery.Selection) {
			s.Find(`script, style, iframe, noscript, .ad, .advertisement, .social-share, 
				.related-news, .tags, .author, .date, .comments, .adpro, the-ads`).Remove()
			
			html, _ := s.Html()
			content = strings.TrimSpace(html)
		})
	}

	// Clean the content by removing HTML tags
	content = e.cleanContent(content)

	// Extract images from meta tags
	images := e.extractImagesFromMeta(doc)

	// If no images found in meta, try to find in content
	if len(images) == 0 {
		doc.Find("div.article-content img, article img, .content img").Each(func(i int, s *goquery.Selection) {
			if src, exists := s.Attr("src"); exists && src != "" {
				src = strings.TrimSpace(src)
				if !strings.HasPrefix(src, "http") {
					src = "https://www.ekonomim.com" + src
				}
				images = append(images, src)
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
