package extractors

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/google/uuid"
)

// NTVExtractor processes NTV RSS items and cleans HTML according to site-specific rules.
type NTVExtractor struct{}

func (e *NTVExtractor) CanHandle(url string) bool {
	return strings.Contains(url, "ntv.com.tr")
}

func (e *NTVExtractor) Extract(htmlContent string) (string, string, error) {
	// Parse HTML
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return "", "", fmt.Errorf("failed to parse HTML: %w", err)
	}

	// 1. Find primary image
	var primaryImage string
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		if cls, _ := s.Attr("class"); strings.Contains(cls, "type:primaryImage") {
			if src, ok := s.Attr("src"); ok {
				primaryImage = src
			}
		}
	})

	// 2. Remove non-primary images
	doc.Find("img").Each(func(i int, s *goquery.Selection) {
		if cls, _ := s.Attr("class"); !strings.Contains(cls, "type:primaryImage") {
			s.Remove()
		}
	})

	// 3. Convert <strong> tags with quote-like text to <h2>
	doc.Find("strong").Each(func(i int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if len(text) > 0 && strings.Count(text, " ") < 10 {
			// Looks like a subheading
			s.ReplaceWithHtml(fmt.Sprintf("<h2>%s</h2>", strings.Title(strings.ToLower(text))))
		}
	})

	// 4. Remove internal links but keep their text
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok && strings.Contains(href, "ntv.com.tr") {
			text := s.Text()
			s.ReplaceWithHtml(text)
		}
	})

	// 5. Clean slideshows but keep meaningful captions
	doc.Find("section.type\\:slideshow").Each(func(i int, s *goquery.Selection) {
		var captions []string
		s.Find("figcaption").Each(func(j int, fc *goquery.Selection) {
			text := strings.TrimSpace(fc.Text())
			// Only keep captions that contain numeric or TL/$ info
			if strings.Contains(text, "TL") || strings.Contains(text, "$") || strings.Contains(text, "Altın") {
				captions = append(captions, text)
			}
		})
		if len(captions) > 0 {
			joined := "<p>" + strings.Join(captions, "</p><p>") + "</p>"
			s.ReplaceWithHtml(joined)
		} else {
			s.Remove()
		}
	})

	// 6. Remove unwanted tags
	doc.Find("script, iframe, style, noscript").Remove()

	// 7. Serialize cleaned HTML
	var buf bytes.Buffer
	if err := goquery.Render(&buf, doc.Selection); err != nil {
		return "", "", fmt.Errorf("failed to render HTML: %w", err)
	}

	cleanHTML := buf.String()
	return primaryImage, cleanHTML, nil
}

// GenerateUniqueID creates a UUID for RSS items
func GenerateUniqueID() string {
	return uuid.New().String()
}
