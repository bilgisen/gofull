package extractors

import (
	"context"
	"fmt"
	htmlpkg "html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
)

// AAExtractor handles content extraction for aa.com.tr domain.
// It processes article URLs on the domain.
type AAExtractor struct {
	httpClient *http.Client
}

// NewAAExtractor creates a new AAExtractor.
func NewAAExtractor(client *http.Client) *AAExtractor {
	if client == nil {
		client = http.DefaultClient
	}
	return &AAExtractor{
		httpClient: client,
	}
}

// Extract implements the Extractor interface for aa.com.tr URLs.
func (e *AAExtractor) Extract(input any) (string, []string, error) {
	switch v := input.(type) {
	case string:
		// If input is a string, treat it as a URL
		return e.extractFromURL(v)
	case map[string]any:
		// Handle feed item with potential image field
		var url string
		var images []string
		
		// Try to get URL from different possible fields
		if link, ok := v["link"].(string); ok && link != "" {
			url = link
		} else if urlVal, ok := v["url"].(string); ok && urlVal != "" {
			url = urlVal
		} else {
			return "", nil, fmt.Errorf("no URL found in input map")
		}
		
		// Extract content from URL first
		content, extractedImages, err := e.extractFromURL(url)
		if err != nil {
			return "", nil, err
		}
		
		// If we have an image from the feed, use that as the primary image
		// and append any additional images from the content
		if img, ok := v["image"].(string); ok && img != "" {
			// Use the feed's image as the primary image
			images = append(images, img)
			
			// Add any additional images from the content that aren't the AA logo
			aaLogo := "cdnassets.aa.com.tr"
			for _, extractedImg := range extractedImages {
				if !strings.Contains(extractedImg, aaLogo) {
					images = append(images, extractedImg)
				}
			}
		} else if len(extractedImages) > 0 {
			// If no feed image, use the first extracted image as primary
			images = extractedImages
		}
		
		return content, images, nil
		
	default:
		return "", nil, fmt.Errorf("unsupported input type: %T", input)
	}
}

// extractFromURL fetches the URL and extracts content using a headless browser.
func (e *AAExtractor) extractFromURL(url string) (string, []string, error) {
	// Only process article URLs
	if !strings.Contains(url, "aa.com.tr/") {
		return "", nil, fmt.Errorf("not an AA article URL: %s", url)
	}

	// Set up the browser options
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/90.0.4430.212 Safari/537.36"),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)

	// Create a new context with the options
	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	// Create a new context with a timeout
	ctx, cancel := context.WithTimeout(allocCtx, 30*time.Second)
	defer cancel()

	// Create a new browser instance
	ctx, cancel = chromedp.NewContext(ctx)
	defer cancel()

	var htmlContent string

	// Run the browser and navigate to the URL
	err := chromedp.Run(ctx,
		// Navigate to the URL
		chromedp.Navigate(url),
		
		// Wait for the content to be loaded
		chromedp.WaitVisible(`div.detay-icerik`, chromedp.ByQuery),
		
		// Wait for the actual content to be loaded (not just the container)
		chromedp.WaitVisible(`div.detay-icerik p`, chromedp.ByQuery),
		
		// Add a small delay to ensure all dynamic content is loaded
		chromedp.Sleep(3 * time.Second),
		
		// Get the HTML content
		chromedp.OuterHTML("html", &htmlContent),
	)
	
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch URL with chromedp: %w", err)
	}

	// Parse the HTML with goquery
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Extract the main content
	var content string
	doc.Find("div.detay-icerik").Each(func(i int, s *goquery.Selection) {
		// Remove WhatsApp promotion div
		s.Find("div[style*='border:1px solid #0077b6']").Remove()
		// Remove subscription notice and related elements
		s.Find("span.detay-foto-editor, .detay-foto-editor, .detay-foto-editor + p").Remove()
		// Also remove any parent elements that might contain the subscription notice
		s.Find("a[href*='abonelik']").Parent().Remove()
		
		// Get the cleaned HTML content
		html, _ := s.Html()
		content = strings.TrimSpace(html)
		
		// Clean up any remaining unwanted text patterns
		unwantedPatterns := []string{
			`📲 Artık haberler size gelsin`,
			`AA'nın WhatsApp kanallarına katılın, önemli gelişmeler cebinize düşsün`,
			`Gündemdeki gelişmeler, özel haber, analiz, fotoğraf ve videolar için Anadolu Ajansı`,
			`Anlık gelişmeler için AA Canlı`,
			`Anadolu Ajansı web sitesinde, AA Haber Akış Sistemi \(HAS\) üzerinden abonelere sunulan haberler, özetlenerek yayımlanmaktadır\.`,
			`Abonelik için lütfen iletişime geçiniz\.`,
			`İlgili konular.*$`,
			`Bu haberi paylaşın`,
		}
		
		for _, pattern := range unwantedPatterns {
			re := regexp.MustCompile(pattern)
			content = re.ReplaceAllString(content, "")
		}
		
		// Clean up HTML entities and multiple spaces
		content = htmlpkg.UnescapeString(content)
		
		// Remove any remaining HTML tags
		tagRe := regexp.MustCompile(`<[^>]*>`)
		content = tagRe.ReplaceAllString(content, "")
		
		// Clean up whitespace
		content = strings.Join(strings.Fields(content), " ")
		content = strings.TrimSpace(content)
		content = strings.TrimSpace(content)
	})

	// If no content found, try alternative selectors
	if content == "" {
		doc.Find("div.news-content, article.content").Each(func(i int, s *goquery.Selection) {
			if html, err := s.Html(); err == nil {
				content = strings.TrimSpace(html)
			}
		})
	}

	// Extract images in order of priority
	var images []string

	// 1. Try to get the main article image (usually the largest one)
	doc.Find("img.detay-buyukFoto").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists && src != "" {
			src = strings.TrimSpace(src)
			if !strings.HasPrefix(src, "http") {
				src = "https://www.aa.com.tr" + src
			}
			images = append(images, src)
		}
	})

	// 2. Try Open Graph image (og:image)
	if len(images) == 0 {
		doc.Find("meta[property='og:image']").Each(func(i int, s *goquery.Selection) {
			if src, exists := s.Attr("content"); exists && src != "" {
				src = strings.TrimSpace(src)
				if !strings.HasPrefix(src, "http") {
					src = "https://www.aa.com.tr" + src
				}
				images = append(images, src)
			}
		})
	}

	// 3. Try Twitter image as fallback
	if len(images) == 0 {
		doc.Find("meta[name='twitter:image:src']").Each(func(i int, s *goquery.Selection) {
			if src, exists := s.Attr("content"); exists && src != "" {
				src = strings.TrimSpace(src)
				if !strings.HasPrefix(src, "http") {
					src = "https://www.aa.com.tr" + src
				}
				images = append(images, src)
			}
		})
	}

	// 4. Try any other images in the content
	if len(images) == 0 {
		doc.Find("div.detay-icerik img, div.news-content img, article.content img").Each(func(i int, s *goquery.Selection) {
			if src, exists := s.Attr("src"); exists && src != "" {
				src = strings.TrimSpace(src)
				// Skip AA logo and other non-article images
				if !strings.Contains(src, "cdnassets.aa.com.tr") && 
				   !strings.Contains(src, "pixel.quantserve.com") &&
				   !strings.Contains(src, "banner") &&
				   !strings.Contains(src, "logo") {
					if !strings.HasPrefix(src, "http") {
						src = "https://www.aa.com.tr" + src
					}
					images = append(images, src)
				}
			}
		})
	}

	return content, images, nil
}

// extractFromHTML extracts content from HTML using aa.com.tr specific selectors.
func (e *AAExtractor) extractFromHTML(reader io.Reader) (string, []string, error) {
	doc, err := goquery.NewDocumentFromReader(reader)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	// Extract main content
	var content string
	doc.Find("div.detay-icerik").Each(func(i int, s *goquery.Selection) {
		// Get the raw HTML content of the div
		html, _ := s.Html()
		content = strings.TrimSpace(html)
	})

	// Extract images from meta tags first (higher priority)
	images := e.extractImagesFromMeta(doc)
	
	// If no images found in meta, try to extract from content
	if len(images) == 0 {
		doc.Find("div.detay-icerik img").Each(func(i int, s *goquery.Selection) {
			if src, exists := s.Attr("src"); exists && src != "" {
				src = strings.TrimSpace(src)
				if !strings.HasPrefix(src, "http") {
					src = "https://www.aa.com.tr" + src
				}
				images = append(images, src)
			}
		})
	}

	return content, images, nil
}

// extractImagesFromMeta extracts image URLs from Open Graph and Twitter Card meta tags.
func (e *AAExtractor) extractImagesFromMeta(doc *goquery.Document) []string {
	var images []string

	// Check Open Graph image
	doc.Find("meta[property='og:image'], meta[name='twitter:image:src'], meta[itemprop='image']").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("content"); exists && src != "" {
			src = strings.TrimSpace(src)
			if !strings.HasPrefix(src, "http") {
				src = "https://www.aa.com.tr" + src
			}
			images = append(images, src)
		}
	})

	// Remove duplicates
	if len(images) > 0 {
		unique := make(map[string]bool)
		var result []string
		for _, img := range images {
			if !unique[img] {
				unique[img] = true
				result = append(result, img)
			}
		}
		return result
	}

	return images
}
