package extractors

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// T24Extractor handles content extraction for t24.com.tr domain.
// It extracts content from the articleBody property.
type T24Extractor struct {
	httpClient *http.Client
}

// NewT24Extractor creates a new T24Extractor.
func NewT24Extractor(client *http.Client) *T24Extractor {
	if client == nil {
		client = http.DefaultClient
	}
	return &T24Extractor{
		httpClient: client,
	}
}

// Extract implements the Extractor interface for t24.com.tr URLs.
func (t *T24Extractor) Extract(input any) (string, []string, error) {
	var images []string
	
	// Check for enclosure URL in the input (from RSS feed)
	switch v := input.(type) {
	case map[string]string:
		// Check for enclosure URL in the map
		if enclosureURL, ok := v["enclosure"]; ok && enclosureURL != "" {
			images = append(images, enclosureURL)
		}
		// Handle HTML content if present
		if htmlContent, ok := v["html"]; ok {
			content, htmlImages, err := t.extractFromHTML(htmlContent)
			if err != nil {
				return "", images, err
			}
			// Prepend the enclosure image to the beginning of the images slice
			images = append(images, htmlImages...)
			return content, images, nil
		}

	case map[string]interface{}:
		// Check for enclosure URL in the map
		if enclosureVal, ok := v["enclosure"]; ok {
			if enclosureURL, ok := enclosureVal.(string); ok && enclosureURL != "" {
				images = append(images, enclosureURL)
			}
		}
		// Handle HTML content if present
		if htmlContent, ok := v["html"].(string); ok {
			content, htmlImages, err := t.extractFromHTML(htmlContent)
			if err != nil {
				return "", images, err
			}
			// Prepend the enclosure image to the beginning of the images slice
			images = append(images, htmlImages...)
			return content, images, nil
		}

	case string:
		// Input is a URL, fetch and extract content
		content, htmlImages, err := t.extractFromURL(v)
		if err != nil {
			return "", images, err
		}
		images = append(images, htmlImages...)
		return content, images, nil

	default:
		return "", nil, errors.New("unsupported input type for T24Extractor")
	}

	// If we get here but have an enclosure image, return it even without content
	if len(images) > 0 {
		return "", images, nil
	}

	return "", nil, errors.New("no valid content found in input")
}

// extractFromURL fetches the URL and extracts content.
func (t *T24Extractor) extractFromURL(articleURL string) (string, []string, error) {
	resp, err := t.httpClient.Get(articleURL)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, errors.New("failed to fetch URL: " + resp.Status)
	}

	htmlContent, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}

	return t.extractFromHTML(string(htmlContent))
}

// extractFromHTML extracts content from HTML using t24.com.tr specific selectors.
func (t *T24Extractor) extractFromHTML(htmlContent string) (string, []string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", nil, err
	}

	// Find the article body using the property="articleBody" attribute
	articleBody := doc.Find(`[property="articleBody"]`).First()
	if articleBody.Length() == 0 {
		return "", nil, errors.New("article body not found")
	}

	// Clean up the content
	cleanContent(articleBody)

	// Get the text content (without HTML tags)
	textContent := articleBody.Text()
	// Normalize whitespace
	textContent = strings.Join(strings.Fields(textContent), " ")

	// Extract images
	var images []string
	doc.Find(`[property="articleBody"] img`).Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists && src != "" {
			images = append(images, src)
		}
	})

	return textContent, images, nil
}

// cleanContent removes unwanted elements from the article content.
func cleanContent(s *goquery.Selection) {
	// Remove script and style elements
	s.Find("script, style, iframe, noscript, .ad-container, .ad, .advertisement, .social-share, .related-news, .tags, .comments, .author-info, .article-footer, .article-meta, .article-share, .article-tags, .article-related, .article-comments, .article-author, .article-date, .article-category, .article-source, .article-url, .article-title, .article-image, .article-video, .article-audio, .article-gallery, .article-pagination, .article-navigation, .article-pagination, .article-navigation, .article-pagination, .article-navigation, .article-pagination, .article-navigation, .article-pagination, .article-navigation, .article-pagination, .article-navigation, .article-pagination, .article-navigation").Remove()

	// Remove empty paragraphs
	s.Find("p:empty").Remove()

	// Clean up attributes
	s.Find("*[style], *[class], *[id], *[data-*]").Each(func(i int, el *goquery.Selection) {
		el.RemoveAttr("style")
		el.RemoveAttr("class")
		el.RemoveAttr("id")
		el.RemoveAttr("data-*")
	})

	// Convert relative URLs to absolute
	s.Find("a[href^='/']").Each(func(i int, el *goquery.Selection) {
		if href, exists := el.Attr("href"); exists {
			el.SetAttr("href", "https://t24.com.tr"+href)
		}
	})

	s.Find("img[src^='/']").Each(func(i int, el *goquery.Selection) {
		if src, exists := el.Attr("src"); exists {
			el.SetAttr("src", "https://t24.com.tr"+src)
		}
	})
}

// Ensure T24Extractor implements Extractor interface
var _ Extractor = (*T24Extractor)(nil)
