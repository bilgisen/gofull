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

// KisadalgaExtractor handles content extraction for kisadalga.net domain.
// It processes any URL on the domain with the allowed path pattern.
type KisadalgaExtractor struct {
	httpClient *http.Client
}

// NewKisadalgaExtractor creates a new KisadalgaExtractor.
func NewKisadalgaExtractor(client *http.Client) *KisadalgaExtractor {
	if client == nil {
		client = http.DefaultClient
	}
	return &KisadalgaExtractor{
		httpClient: client,
	}
}

// Extract implements the Extractor interface for kisadalga.net URLs.
func (k *KisadalgaExtractor) Extract(input any) (string, []string, error) {
	switch v := input.(type) {
	case string:
		// Input is a URL, fetch and extract content
		return k.extractFromURL(v)

	case map[string]string:
		// Handle map[string]string with "html" key
		if htmlContent, ok := v["html"]; ok {
			return k.extractFromHTML(htmlContent)
		}

	case map[string]interface{}:
		// Handle map[string]interface{} with "html" key
		if htmlContent, ok := v["html"].(string); ok {
			return k.extractFromHTML(htmlContent)
		}
		// If no html key, try to get URL from common fields
		if url, ok := v["url"].(string); ok && url != "" {
			return k.extractFromURL(url)
		}
		if link, ok := v["link"].(string); ok && link != "" {
			return k.extractFromURL(link)
		}

	default:
		return "", nil, fmt.Errorf("unsupported input type: %T", input)
	}

	return "", nil, errors.New("invalid input format - expected URL or map with 'html' content")
}

// extractFromURL fetches the URL and extracts content.
func (k *KisadalgaExtractor) extractFromURL(articleURL string) (string, []string, error) {
	resp, err := k.httpClient.Get(articleURL)
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

	return k.extractFromHTML(string(bodyBytes))
}

// extractFromHTML extracts content from HTML using kisadalga.net specific selectors.
func (k *KisadalgaExtractor) extractFromHTML(htmlContent string) (string, []string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", nil, err
	}

	// Get the main content - target div.text-content with property="articleBody"
	contentDiv := doc.Find(`div.text-content[property="articleBody"]`).First()
	if contentDiv.Length() == 0 {
		// Fallback to other possible content containers if the main one is not found
		contentDiv = doc.Find(`div.text-content, div.article-content, .content`).First()
	}

	// If we still don't have content, return an error
	if contentDiv.Length() == 0 {
		return "", nil, fmt.Errorf("could not find main content in the page")
	}

	// Remove unwanted elements that might be inside the content
	contentDiv.Find(`script, style, iframe, noscript, 
		.ad, .advertisement, .banner, .banner-wide, .rel-link,
		.social-share, .related-news, .tags, .author, .date`).Remove()
	
	// Remove any divs that look like ads or scripts
	contentDiv.Find(`div`).Each(func(i int, s *goquery.Selection) {
		// Remove divs with specific IDs that indicate ads
		if id, exists := s.Attr("id"); exists && (strings.Contains(id, "kisadalganet") || strings.Contains(id, "ad-")) {
			s.Remove()
			return
		}
		
		// Remove divs with inline scripts or ad-related classes
		html, _ := s.Html()
		if strings.Contains(html, "googletag.") || strings.Contains(html, "adsbygoogle") {
			s.Remove()
		}
	})

	// Clean up the content
	content, err := contentDiv.Html()
	if err != nil {
		return "", nil, fmt.Errorf("error getting HTML content: %v", err)
	}

	content = strings.TrimSpace(html.UnescapeString(content))

	// Clean up the content by removing "Kısa Dalga -" prefix if it exists at the beginning
	content = strings.TrimPrefix(content, "Kısa Dalga -")
	content = strings.TrimSpace(content)

	// Extract images from the content
	var images []string
	contentDiv.Find("img").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists && src != "" {
			src = strings.TrimSpace(src)
			if src != "" {
				if !strings.HasPrefix(src, "http") && !strings.HasPrefix(src, "//") {
					src = "https://kisadalga.net" + strings.TrimLeft(src, "/")
				} else if strings.HasPrefix(src, "//") {
					src = "https:" + src
				}
				images = append(images, src)
			}
		}
	})

	// If no images found in content, try to get from meta tags
	if len(images) == 0 {
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
						img = "https://kisadalga.net" + strings.TrimLeft(img, "/")
					} else if strings.HasPrefix(img, "//") {
						img = "https:" + img
					}
					images = []string{img}
					break
				}
			}
		}
	}

	// Belirli bir anahtar kelimeyi içeren görsel URL'leri sabit bir URL ile değiştir.
	// Örnek: .../son-dakika-kirmizi-co9r-cover-blpy_cover.jpg -> https://newstr.netlify.app/public/images/breaking-news.jpg
	processedImages := []string{}
	for _, imgURL := range images {
		// URL'nin son parçasını al (dosya adı)
		parts := strings.Split(imgURL, "/")
		if len(parts) > 0 {
			fileName := parts[len(parts)-1]
			// Dosya adının belirli bir anahtar kelimeyi içerip içermediğini kontrol et
			// Örnek: "son-dakika" kelimesini içeriyorsa
			if strings.Contains(fileName, "son-dakika") { // Buradaki "son-dakika" anahtarını kendi ihtiyacınıza göre değiştirin
				// Belirli bir anahtar kelimeyi içerenler için sabit URL'yi ekle
				processedImages = append(processedImages, "https://newstr.netlify.app/public/images/breaking-news.jpg")
			} else {
				// Anahtar kelimeyi içermeyen orijinal URL'yi koru
				processedImages = append(processedImages, imgURL)
			}
		} else {
			// URL parse edilemediyse, olduğu gibi koru (dikkatli olunmalı)
			processedImages = append(processedImages, imgURL)
		}
	}
	// İşlenmiş listeyi orijinal images değişkenine atayın
	images = processedImages

	return content, images, nil
}