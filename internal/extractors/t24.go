//go:build !excludeextractor_t24

package extractors

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

// RSSFeed represents the structure of an RSS feed
type RSSFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Title       string    `xml:"title"`
		Link        string    `xml:"link"`
		Description string    `xml:"description"`
		Items       []RSSItem `xml:"item"`
	} `xml:"channel"`
}

// RSSItem represents an item in an RSS feed
type RSSItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
	Enclosure   struct {
		URL string `xml:"url,attr"`
	} `xml:"enclosure"`
}

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
	var (
		images []string
		title  string
		urlStr string
	)
	
	// Print debug info about the input type
	fmt.Printf("T24Extractor.Extract called with type: %T, value: %+v\n", input, input)

	// Extract URL and title from input
	switch v := input.(type) {
	case string:
		urlStr = v
		// Check if this is an RSS feed URL
		if strings.Contains(urlStr, "/rss/") {
			return t.extractFromRSSFeed(urlStr)
		}
		// Handle regular article URL
		return t.extractFromURL(urlStr)
		
	case map[string]string:
		if t, ok := v["title"]; ok {
			title = t
		}
		if u, ok := v["url"]; ok {
			urlStr = u
		} else if u, ok := v["link"]; ok {
			urlStr = u
		}
		
		// If we have a URL, process it
		if urlStr != "" {
			if strings.Contains(urlStr, "/rss/") {
				return t.extractFromRSSFeed(urlStr)
			}
			return t.extractFromURL(urlStr)
		}
		
	case map[string]interface{}:
		// Extract title if available
		if t, ok := v["title"].(string); ok {
			title = t
		}
		
		// Extract URL from either 'url' or 'link' field
		if u, ok := v["url"].(string); ok && u != "" {
			urlStr = u
		} else if u, ok := v["link"].(string); ok && u != "" {
			urlStr = u
		}
		
		// If we have a URL, process it
		if urlStr != "" {
			if strings.Contains(urlStr, "/rss/") {
				return t.extractFromRSSFeed(urlStr)
			}
			return t.extractFromURL(urlStr)
		}
	}

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
				return title, images, nil // Return title as description if content extraction fails
			}
			// Prepend the enclosure image to the beginning of the images slice
			images = append(images, htmlImages...)
			// If content is empty, use title as description
			if strings.TrimSpace(content) == "" {
				return title, images, nil
			}
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
				return title, images, nil // Return title as description if content extraction fails
			}
			// Prepend the enclosure image to the beginning of the images slice
			images = append(images, htmlImages...)
			// If content is empty, use title as description
			if strings.TrimSpace(content) == "" {
				return title, images, nil
			}
			return content, images, nil
		}

	case string:
		// Input is a URL, fetch and extract content
		content, htmlImages, err := t.extractFromURL(v)
		if err != nil {
			return title, images, nil // Return title as description if content extraction fails
		}
		images = append(images, htmlImages...)
		// If content is empty, use title as description
		if strings.TrimSpace(content) == "" {
			return title, images, nil
		}
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

// extractFromRSSFeed fetches and parses an RSS feed, then extracts content from each article
func (t *T24Extractor) extractFromRSSFeed(feedURL string) (string, []string, error) {
	fmt.Printf("Fetching RSS feed from: %s\n", feedURL)
	
	// Fetch the RSS feed
	resp, err := t.httpClient.Get(feedURL)
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch RSS feed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("failed to fetch RSS feed: %s", resp.Status)
	}

	// Read and parse the RSS feed
	var rssFeed RSSFeed
	decoder := xml.NewDecoder(resp.Body)
	if err := decoder.Decode(&rssFeed); err != nil {
		return "", nil, fmt.Errorf("failed to parse RSS feed: %v", err)
	}

	// Build the feed content
	var feedContent strings.Builder
	feedContent.WriteString(fmt.Sprintf("# %s\n\n", rssFeed.Channel.Title))
	feedContent.WriteString(fmt.Sprintf("%s\n\n", rssFeed.Channel.Description))

	var images []string

	// Limit the number of items to process
	maxItems := 10
	if len(rssFeed.Channel.Items) > maxItems {
		rssFeed.Channel.Items = rssFeed.Channel.Items[:maxItems]
	}

	// Process each item in the feed
	for i, item := range rssFeed.Channel.Items {
		// Add the item title and link
		feedContent.WriteString(fmt.Sprintf("## [%s](%s)\n", item.Title, item.Link))
		
		// Add publication date if available
		if item.PubDate != "" {
			pubDate, err := time.Parse(time.RFC1123Z, item.PubDate)
			if err == nil {
				feedContent.WriteString(fmt.Sprintf("*%s*\n\n", pubDate.Format("2006-01-02 15:04:05")))
			} else {
				feedContent.WriteString(fmt.Sprintf("*%s*\n\n", item.PubDate))
			}
		}

		// Add the item description
		feedContent.WriteString(fmt.Sprintf("%s\n\n", item.Description))

			// Try to get the best possible image
		var imageURL string
		
		// 1. Try to get Open Graph image from the article
		_, articleImages, err := t.extractFromURL(item.Link)
		if err == nil && len(articleImages) > 0 {
			imageURL = articleImages[0]
		} 
		
		// 2. Fall back to enclosure URL
		if imageURL == "" && item.Enclosure.URL != "" {
			imageURL = item.Enclosure.URL
		}
		
		// 3. Fall back to the first image in the description
		if imageURL == "" {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(item.Description))
			if err == nil {
				doc.Find("img").First().Each(func(i int, s *goquery.Selection) {
					if src, exists := s.Attr("src"); exists && src != "" {
						imageURL = src
					}
				})
			}
		}
		
		// Add the image if found
		if imageURL != "" {
			images = append(images, imageURL)
			feedContent.WriteString(fmt.Sprintf("![](%s)\n\n", imageURL))
		}
		
		// Add a separator between items
		if i < len(rssFeed.Channel.Items)-1 {
			feedContent.WriteString("---\n\n")
		}
	}

	return feedContent.String(), images, nil
}

// extractFromURL fetches the URL and extracts content.
func (t *T24Extractor) extractFromURL(articleURL string) (string, []string, error) {
	// Check if this is an RSS feed URL
	if strings.Contains(articleURL, "/rss/") {
		return t.extractFromRSSFeed(articleURL)
	}

	// Create a new request
	req, err := http.NewRequest("GET", articleURL, nil)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create request: %v", err)
	}

	// Set headers to mimic a browser
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "tr-TR,tr;q=0.9,en-US;q=0.8,en;q=0.7")

	// Send the request
	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("failed to fetch article: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("HTTP request failed with status: %s", resp.Status)
	}

	// Read the response body with proper encoding detection
	htmlContent, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read response body: %v", err)
	}

	return t.extractFromHTML(string(htmlContent))
}
func (t *T24Extractor) extractFromHTML(htmlContent string) (string, []string, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", nil, fmt.Errorf("error parsing HTML: %v", err)
	}

	// First, try to get Open Graph image (highest priority)
	ogImage, _ := doc.Find("meta[property='og:image']").Attr("content")
	if ogImage == "" {
		ogImage, _ = doc.Find("meta[property='og:image:secure_url']").Attr("content")
	}

	// Try to get Twitter image (second priority)
	twitterImage, _ := doc.Find("meta[name='twitter:image']").Attr("content")
	if twitterImage == "" {
		twitterImage, _ = doc.Find("meta[name='twitter:image:src']").Attr("content")
	}

	// Try to get the main content image (third priority)
	var contentImage string
	doc.Find("img").EachWithBreak(func(i int, s *goquery.Selection) bool {
		if src, exists := s.Attr("src"); exists && !strings.Contains(src, "logo") && 
		   !strings.Contains(src, "icon") && !strings.Contains(src, "pixel") {
			// Skip small images that are likely icons
			if width, exists := s.Attr("width"); exists {
				if w, err := strconv.Atoi(width); err == nil && w < 150 {
					return true // continue to next image
				}
			}
			if height, exists := s.Attr("height"); exists {
				if h, err := strconv.Atoi(height); err == nil && h < 150 {
					return true // continue to next image
				}
			}
			// Check if it's a content image (not an ad or icon)
			parentClass, _ := s.Parent().Attr("class")
			if !strings.Contains(parentClass, "ad") && !strings.Contains(src, "ad") {
				contentImage = src
				return false // break the loop
			}
		}
		return true // continue to next image
	})

	// First try the most reliable selector: property="articleBody"
	articleBody := doc.Find(`[property="articleBody"]`).First()
	
	// If not found, try looking for elements with class rowMarginZero
	if articleBody.Length() == 0 {
		articleBody = doc.Find(`.rowMarginZero`).First()
	}
	
	// If still not found, look for common article content containers
	if articleBody.Length() == 0 {
		articleBody = doc.Find(`article, .article, .content, .post-content, .entry-content`).First()
	}
	
	if articleBody.Length() == 0 {
		return "", nil, errors.New("article body not found - no matching selectors found")
	}

	// Clean up the content
	cleanContent(articleBody)

	// Process the content
	var contentBuilder strings.Builder
	
	// Add title if available
	title := doc.Find("h1").First().Text()
	if title != "" {
		contentBuilder.WriteString(title)
		contentBuilder.WriteString("\n\n")
	}

	// Process the main content - look for direct <p> and <h3> elements
	articleBody.Contents().Each(func(i int, s *goquery.Selection) {
		// Skip script and style elements
		if goquery.NodeName(s) == "script" || goquery.NodeName(s) == "style" {
			return
		}

		// Check if this is a direct child with the content we want
		if goquery.NodeName(s) == "p" || goquery.NodeName(s) == "h3" {
			text := strings.TrimSpace(s.Text())
			if text != "" {
				contentBuilder.WriteString(text)
				contentBuilder.WriteString("\n\n")
			}
		}

		// Also check if there's a nested div with the content
		s.Find("p, h3").Each(func(i int, inner *goquery.Selection) {
			// Skip social media and ad-related elements
			if inner.HasClass("social-icon") || inner.HasClass("pgn-native-d-ba") || 
			   inner.ParentsFiltered("div[style*='display:none']").Length() > 0 {
				return
			}

			text := strings.TrimSpace(inner.Text())
			if text == "" {
				return
			}

			// Skip common non-content text
			lowerText := strings.ToLower(text)
			if strings.HasPrefix(lowerText, "tıklayın") || 
			   strings.HasPrefix(lowerText, "haberin devamı") ||
			   strings.Contains(lowerText, "reklam") ||
			   strings.Contains(lowerText, "sponsor") ||
			   strings.Contains(lowerText, "etiketler:") {
				return
			}

			// Add the text to the content
			contentBuilder.WriteString(text)
			contentBuilder.WriteString("\n\n")
		})
	})

	// If we didn't find any content, try a more aggressive approach
	content := strings.TrimSpace(contentBuilder.String())
	if content == "" {
		// Look for any paragraph that has substantial content
		doc.Find("p").Each(func(i int, s *goquery.Selection) {
			text := strings.TrimSpace(s.Text())
			if len(text) > 50 { // Only include paragraphs with substantial content
				contentBuilder.WriteString(text)
				contentBuilder.WriteString("\n\n")
			}
		})
		content = strings.TrimSpace(contentBuilder.String())
	}

	// Extract images
	var images []string
	// Try to get Open Graph image first
	var articleOgImage string
	doc.Find("meta[property='og:image'], meta[property='og:image:secure_url']").Each(func(i int, s *goquery.Selection) {
		if content, exists := s.Attr("content"); exists && content != "" {
			articleOgImage = content
		}
	})

	// If no OG image, try Twitter image
	if articleOgImage == "" {
		doc.Find("meta[name='twitter:image']").Each(func(i int, s *goquery.Selection) {
			if content, exists := s.Attr("content"); exists && content != "" {
				articleOgImage = content
			}
		})
	}

	// If we found a meta image, use it as the first image
	if ogImage != "" {
		images = append([]string{ogImage}, images...)
	}

	// Also extract other images from the page
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		if src, exists := s.Attr("src"); exists && src != "" {
			// Skip social media icons, tracking pixels, and small images
			if strings.Contains(src, "pixel") || strings.Contains(src, "social") || 
			   strings.Contains(src, "icon") || strings.Contains(src, "logo") ||
			   strings.Contains(src, "avatar") || strings.Contains(src, "ad") {
				return
			}
			// Convert relative URLs to absolute
			if !strings.HasPrefix(src, "http") && !strings.HasPrefix(src, "//") {
				src = "https://t24.com.tr" + src
			}
			// Add images in order of priority
			for _, img := range []string{ogImage, twitterImage} {
				if img == "" && contentImage != "" {
					img = contentImage
				}
				if img == src {
					return
				}
			}
			images = append(images, src)
		}
	})

	return content, images, nil
}

// cleanContent removes unwanted elements from the article content.
func cleanContent(s *goquery.Selection) {
	// First remove all tables and their content
	s.Find("table, tbody, thead, tfoot, tr, td, th").Each(func(i int, el *goquery.Selection) {
		// Replace table cells with their text content
		if el.Is("td, th") {
			el.AfterHtml(el.Text())
		}
		el.Remove()
	})

	// Remove script and style elements
	s.Find(`script, style, iframe, noscript, 
		.ad-container, .ad, .advertisement, .social-share, 
		.related-news, .tags, .comments, .author-info, 
		.article-footer, .article-meta, .article-share, 
		.article-tags, .article-related, .article-comments, 
		.article-author, .article-date, .article-category, 
		.article-source, .article-url, .article-title, 
		.article-image, .article-video, .article-audio, 
		.article-gallery, .article-pagination, .article-navigation,
		.article-recommend, .article-popular, .article-most-read,
		.recommended-news, .popular-news, .most-read-news,
		.social-buttons, .share-buttons, .social-media-buttons,
		.fb-like, .twitter-tweet, .instagram-media,
		.newsletter, .subscription, .signup-form,
		.comment-section, .comment-form, .comment-list,
		.pagination, .page-navigation, .pager,
		.breadcrumb, .breadcrumbs, .site-map,
		.footer, .site-footer, .main-footer,
		.header, .site-header, .main-header,
		.sidebar, .side-bar, .widget-area,
		.modal, .popup, .lightbox,
		[class*='cookie'], [id*='cookie'],
		[class*='gdpr'], [id*='gdpr'],
		[class*='privacy'], [id*='privacy'],
		[class*='banner'], [id*='banner'],
		[class*='popup'], [id*='popup'],
		[class*='modal'], [id*='modal'],
		[class*='overlay'], [id*='overlay'],
		[class*='notification'], [id*='notification']`).Remove()

	// Remove empty elements
	s.Find("p:empty, div:empty, span:empty, a:empty").Remove()

	// Remove elements that only contain whitespace
	s.Find("p, div, span").FilterFunction(func(i int, s *goquery.Selection) bool {
		return strings.TrimSpace(s.Text()) == ""
	}).Remove()

	// Clean up attributes
	s.Find("*").Each(func(i int, el *goquery.Selection) {
		el.RemoveAttr("style")
		el.RemoveAttr("class")
		el.RemoveAttr("id")
		el.RemoveAttr("data-*")
		el.RemoveAttr("on*")
	})

	// Remove inline styles and scripts
	s.Find("[style], [onclick], [onload], [onerror]").RemoveAttr("style onclick onload onerror")

	// Clean up links
	s.Find("a").Each(func(i int, el *goquery.Selection) {
		// Remove tracking parameters from URLs
		if href, exists := el.Attr("href"); exists {
			// Convert relative URLs to absolute
			if strings.HasPrefix(href, "/") {
				el.SetAttr("href", "https://t24.com.tr"+href)
			}
			
			// Clean up tracking parameters
			u, err := url.Parse(el.AttrOr("href", ""))
			if err == nil {
				q := u.Query()
				// Remove common tracking parameters
				for _, param := range []string{"utm_", "ref_", "source", "campaign", "medium", "content"} {
					for k := range q {
						if strings.Contains(strings.ToLower(k), param) {
							q.Del(k)
						}
					}
				}
				u.RawQuery = q.Encode()
				el.SetAttr("href", u.String())
			}
		}
	})

	// Clean up images
	s.Find("img").Each(func(i int, el *goquery.Selection) {
		// Convert relative URLs to absolute
		if src, exists := el.Attr("src"); exists && strings.HasPrefix(src, "/") {
			el.SetAttr("src", "https://t24.com.tr"+src)
		}
		
		// Remove tracking parameters from image URLs
		if _, exists := el.Attr("src"); exists {
			u, err := url.Parse(el.AttrOr("src", ""))
			if err == nil {
				q := u.Query()
				// Remove common image tracking parameters
				for _, param := range []string{"w=", "h=", "quality=", "resize", "crop", "fit"} {
					for k := range q {
						if strings.Contains(strings.ToLower(k), param) {
							q.Del(k)
						}
					}
				}
				u.RawQuery = q.Encode()
				el.SetAttr("src", u.String())
			}
		}
	})
	
	// Remove any remaining script and style tags that might have been missed
	s.Find("script, style, noscript, iframe, frame, object, embed, param, video, audio, source, track, canvas, svg, math").Remove()
	
	// Remove any elements that might contain scripts or styles
	s.Find("*[onclick], *[onload], *[onerror], *[onmouseover], *[onmouseout], *[onmousedown], *[onmouseup]").RemoveAttr("onclick onload onerror onmouseover onmouseout onmousedown onmouseup")
	
	// Remove any elements with suspicious attributes
	s.Find("*[data-], *[ng-], *[v-], *[x-], *[@*], *[:*]").Remove()
	
	// Remove any elements that are likely ads or trackers
	s.Find("ins.adsbygoogle, .ad, .advertisement, [id*='ad-'], [class*='ad-'], [id*='banner'], [class*='banner'], [id*='sponsor'], [class*='sponsor']").Remove()
	
	// Remove any elements that are likely social media embeds
	s.Find(".fb-post, .fb-like, .fb-comments, .twitter-tweet, .instagram-media, .tiktok-embed").Remove()
	
	// Remove any elements that are likely newsletter signups
	s.Find(".newsletter, .newsletter-form, .signup-form, .email-signup").Remove()
	
	// Remove any elements that are likely related content
	s.Find(".related, .related-posts, .related-articles, .more-news, .read-more").Remove()
	
	// Remove any elements that are likely navigation or pagination
	s.Find(".pagination, .page-navigation, .pager, .nav-links").Remove()
	
	// Remove any elements that are likely breadcrumbs
	s.Find(".breadcrumb, .breadcrumbs").Remove()
	
	// Remove any elements that are likely headers or footers
	s.Find("header, footer, .header, .footer, .site-header, .site-footer, .main-header, .main-footer").Remove()
	
	// Remove any elements that are likely sidebars
	s.Find("aside, .sidebar, .side-bar, .widget-area").Remove()
	
	// Remove any elements that are likely modals or popups
	s.Find(".modal, .popup, .lightbox, .overlay").Remove()
	
	// Remove any elements that are likely cookie or GDPR notices
	s.Find(".cookie-consent, .gdpr-banner, .privacy-banner, [class*='cookie'], [id*='cookie'], [class*='gdpr'], [id*='gdpr'], [class*='privacy'], [id*='privacy']").Remove()
	
	// Remove any elements that are likely banners or notifications
	s.Find(".banner, .notification, .alert, .notice, .message, [role='alert'], [role='status'], [role='banner']").Remove()
	
	// Remove any elements that are likely tooltips or popovers
	s.Find("[data-toggle='tooltip'], [data-toggle='popover'], [data-bs-toggle='tooltip'], [data-bs-toggle='popover'], .tooltip, .popover").Remove()
	
	// Remove any elements that are likely loading placeholders
	s.Find(".loading, .loader, .spinner, .skeleton, .shimmer").Remove()
	
	// Remove any elements that are likely lazy-loaded
	s.Find("[data-src], [data-lazy], [lazyload], .lazy, .lazyload").Remove()
	
	// Remove any elements that are likely hidden or offscreen
	s.Find("[hidden], [aria-hidden='true'], .hidden, .d-none, .invisible, .sr-only, .visually-hidden").Remove()
	
	// Remove any elements that are likely empty containers
	s.Find("div:empty, span:empty, p:empty, a:empty, li:empty, td:empty, th:empty").Remove()
	
	// Remove any elements that only contain whitespace
	s.Find("p, div, span, li, td, th").FilterFunction(func(i int, s *goquery.Selection) bool {
		return strings.TrimSpace(s.Text()) == ""
	}).Remove()
}

// Ensure T24Extractor implements Extractor interface
var _ Extractor = (*T24Extractor)(nil)

// Compile time check to ensure this file is being compiled
var _ = "T24Extractor is being compiled"
