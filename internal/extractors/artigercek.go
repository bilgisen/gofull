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

// ArtigercekExtractor handles content extraction for artigercek.com domain.
type ArtigercekExtractor struct {
	httpClient *http.Client
}

// NewArtigercekExtractor creates a new ArtigercekExtractor.
func NewArtigercekExtractor(client *http.Client) *ArtigercekExtractor {
	if client == nil {
		client = http.DefaultClient
	}
	return &ArtigercekExtractor{
		httpClient: client,
	}
}

// Extract implements the Extractor interface for artigercek.com URLs.
func (e *ArtigercekExtractor) Extract(input any) (string, []string, error) {
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
func (e *ArtigercekExtractor) extractFromURL(url string) (string, []string, error) {
	// Skip processing for filtered URLs
	filteredPrefixes := []string{
		"https://www.artigercek.com/video/",
		"https://www.artigercek.com/galeri/",
		"https://www.artigercek.com/foto-galeri/",
		"https://www.artigercek.com/yazarlar/",
		"https://www.artigercek.com/kose-yazilari/",
		"https://www.artigercek.com/roportaj/",
		"https://artigercek.com/video/",
		"https://artigercek.com/galeri/",
		"https://artigercek.com/foto-galeri/",
		"https://artigercek.com/yazarlar/",
		"https://artigercek.com/kose-yazilari/",
		"https://artigercek.com/roportaj/",
	}

	for _, prefix := range filteredPrefixes {
		if strings.HasPrefix(url, prefix) {
			return "", nil, fmt.Errorf("URL is in filtered list: %s", url)
		}
	}

	// Only process article URLs - be more flexible with URL patterns
	if !strings.Contains(url, "artigercek.com/") {
		return "", nil, fmt.Errorf("not an Artigercek article URL: %s", url)
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

// extractFromHTML extracts content from HTML using artigercek.com specific selectors.
func (e *ArtigercekExtractor) extractFromHTML(reader io.Reader) (string, []string, error) {
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Extract main content from the specific div
	var content string
	sel := doc.Find("div.content-text[property=\"articleBody\"]")
	if sel.Length() > 0 {
		// Remove ad scripts and unwanted elements
		sel.Find("script, style, iframe, noscript, .adpro, .advertisement, .social-share, .related-news, .tags, .author-info, .date-info, .category-info").Remove()
		
		// Get the clean HTML
		html, _ := sel.Html()
		content = strings.TrimSpace(html)
	} else {
		// Fallback to other selectors if the specific one is not found
		selectors := []string{
			"div.article-content",
			"div.content-text",
			"div.news-content",
			"div.detail-content",
			"div.post-content",
			"article",
			".content",
			".article-body",
			".news-text",
			".entry-content",
			"div[itemprop=\"articleBody\"]",
		}

		for _, selector := range selectors {
			doc.Find(selector).Each(func(i int, s *goquery.Selection) {
				// Remove unwanted elements
				s.Find("script, style, iframe, noscript, .ad, .advertisement, .social-share, .related-news, .tags, .author-info, .date-info, .category-info").Remove()
				
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
	}

	// Clean the content by removing HTML tags
	content = e.cleanContent(content)

	// Extract images from meta tags
	images := e.extractImagesFromMeta(doc)

	// If no images found in meta, try to find in content
	if len(images) == 0 {
		selectors := []string{
			"div.article-content",
			"div.content-text",
			"div.news-content",
			"div.detail-content",
			"div.post-content",
			"article",
			".content",
			".article-body",
			".news-text",
			".entry-content",
			"div[itemprop=\"articleBody\"]",
		}
		for _, selector := range selectors {
			doc.Find(selector + " img").Each(func(i int, s *goquery.Selection) {
				if src, exists := s.Attr("src"); exists && src != "" {
					src = strings.TrimSpace(src)
					src = html.UnescapeString(src)
					if !strings.HasPrefix(src, "http") {
						if strings.HasPrefix(src, "/") {
							src = "https://www.artigercek.com" + src
						} else {
							src = "https://www.artigercek.com/" + src
						}
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

// cleanContent removes HTML tags, ads, links and cleans up the content
func (e *ArtigercekExtractor) cleanContent(content string) string {
	// Remove CDATA if present
	content = strings.ReplaceAll(content, "<![CDATA[", "")
	content = strings.ReplaceAll(content, "]]>", "")

	// Remove ad scripts specifically
	adScriptRegex := regexp.MustCompile(`(?s)<script[^>]*>.*?</script>`)
	content = adScriptRegex.ReplaceAllString(content, "")

	// Remove ad containers
	adContainerRegex := regexp.MustCompile(`(?s)<div[^>]*class="[^"]*adpro[^"]*"[^>]*>.*?</div>`)
	content = adContainerRegex.ReplaceAllString(content, "")

	// Remove all HTML tags
	re := regexp.MustCompile(`<[^>]*>`)
	content = re.ReplaceAllString(content, "")

	// Remove "Artı Gerçek-" prefix from the beginning
	content = strings.TrimPrefix(content, "Artı Gerçek-")
	content = strings.TrimPrefix(content, "Artı Gerçek- ")

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
		"&eacute;", "é",
		"&uuml;", "ü",
		"&ouml;", "ö",
		"&ccedil;", "ç",
		"&uuml;", "ü",
		"&ccedil;", "ç",
		"&gbreve;", "ğ",
		"&inodot;", "ı",
		"&scedil;", "ş",
		"&Ouml;", "Ö",
		"&Uuml;", "Ü",
		"&Ccedil;", "Ç",
		"&Gbreve;", "Ğ",
		"&Inodot;", "İ",
		"&Scedil;", "Ş",
		"&ouml;", "ö",
		"&auml;", "ä",
		"&ouml;", "ö",
		"&uuml;", "ü",
		"&szlig;", "ß",
		"&agrave;", "à",
		"&aacute;", "á",
		"&egrave;", "è",
		"&eacute;", "é",
		"&igrave;", "ì",
		"&iacute;", "í",
		"&ograve;", "ò",
		"&oacute;", "ó",
		"&ugrave;", "ù",
		"&uacute;", "ú",
		"&acirc;", "â",
		"&ecirc;", "ê",
		"&icirc;", "î",
		"&ocirc;", "ô",
		"&ucirc;", "û",
		"&#039;", "'",
		"&quot;", "\"",
		"&#34;", "\"",
	)
	content = replacer.Replace(content)

	// Replace "(Haber Merkezi)" with "Brief.tr" before HTML entity decoding
	content = strings.Replace(content, "&#34; (Haber Merkezi)", "Brief.tr", -1)
	content = strings.Replace(content, "\" (Haber Merkezi)", "Brief.tr", -1)
	content = strings.Replace(content, " (Haber Merkezi)", "Brief.tr", -1)
	content = strings.Replace(content, "(Haber Merkezi)", "Brief.tr", -1)
	content = strings.Replace(content, " Haber Merkezi", "Brief.tr", -1)
	content = strings.Replace(content, "Haber Merkezi", "Brief.tr", -1)

	// Use html.UnescapeString to decode any remaining HTML entities
	content = html.UnescapeString(content)

	// Clean up whitespace and newlines
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.ReplaceAll(content, "\t", " ")
	content = strings.ReplaceAll(content, "  ", " ")
	content = strings.TrimSpace(content)
	content = strings.Join(strings.Fields(content), " ")

	return content
}

// extractImagesFromMeta extracts image URLs from Open Graph, Twitter Card meta tags, and specific figure elements.
func (e *ArtigercekExtractor) extractImagesFromMeta(doc *goquery.Document) []string {
	var images []string

	// First, try to extract from the specific figure.post-image element
	doc.Find("figure.post-image picture img").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists && src != "" {
			src = strings.TrimSpace(src)
			src = html.UnescapeString(src)
			if !strings.HasPrefix(src, "http") {
				if strings.HasPrefix(src, "/") {
					src = "https://www.artigercek.com" + src
				} else {
					src = "https://www.artigercek.com/" + src
				}
			}
			if !contains(images, src) {
				images = append(images, src)
			}
		}
	})

	// If no images found in figure, try to extract from link rel=preload elements
	if len(images) == 0 {
		doc.Find("link[rel=\"preload\"][as=\"image\"]").Each(func(i int, s *goquery.Selection) {
			if href, exists := s.Attr("href"); exists && href != "" {
				href = strings.TrimSpace(href)
				href = html.UnescapeString(href)
				if !strings.HasPrefix(href, "http") {
					if strings.HasPrefix(href, "/") {
						href = "https://www.artigercek.com" + href
					} else {
						href = "https://www.artigercek.com/" + href
					}
				}
				if !contains(images, href) {
					images = append(images, href)
				}
			}
		})
	}

	// If still no images found, check common meta tags for images
	if len(images) == 0 {
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
						url = "https://www.artigercek.com" + strings.TrimLeft(url, "/")
					} else if strings.HasPrefix(url, "//") {
						url = "https:" + url
					}
					
					// Skip common non-content images
					lowerUrl := strings.ToLower(url)
					if !strings.Contains(lowerUrl, "logo") && 
					   !strings.Contains(lowerUrl, "icon") && 
					   !strings.Contains(lowerUrl, "favicon") &&
					   !strings.Contains(lowerUrl, "default") &&
					   !contains(images, url) {
						images = append(images, url)
					}
				}
			})
		}
	}

	return images
}
