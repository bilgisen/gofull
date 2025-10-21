package app

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/feeds"
	"github.com/mmcdole/gofeed"

	"gofull/internal/extractors"
)

// FeedHandler processes RSS/Atom feeds and rebuilds them with cleaned article content.
type FeedHandler struct {
	Client     *http.Client
	Registry   *extractors.Registry
	Cache      *Cache
	FeedParser *gofeed.Parser
}

// ProcessFeed fetches a feed URL, extracts and cleans each item, and returns a unified feed.
func (h *FeedHandler) ProcessFeed(feedURL string) (*feeds.Feed, error) {
	parsedFeed, err := h.FeedParser.ParseURL(feedURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse feed: %w", err)
	}

	// Nil check for parsedFeed
	if parsedFeed == nil {
		return nil, fmt.Errorf("parsed feed is nil")
	}

	outputFeed := &feeds.Feed{
		Title:       parsedFeed.Title,
		Link:        &feeds.Link{Href: feedURL},
		Description: parsedFeed.Description,
	}

	for _, item := range parsedFeed.Items {
		processed, err := h.processItem(item)
		if err != nil {
			continue
		}
		outputFeed.Items = append(outputFeed.Items, processed)
	}

	return outputFeed, nil
}

// processItem extracts article content, cleans it with the appropriate extractor, and builds a feeds.Item.
func (h *FeedHandler) processItem(item *gofeed.Item) (*feeds.Item, error) {
	if item.Link == "" {
		return nil, fmt.Errorf("item missing link")
	}

	// Get extractor based on item link (domain-based selection)
	domainExtractor := h.Registry.ForURL(item.Link)

	var contentHTML string
	var images []string
	var err error

	// Prefer feed content if available (as HTML)
	if item.Content != "" {
		contentHTML, images, err = domainExtractor.Extract(item.Content)
	} else {
		contentHTML, images, err = domainExtractor.Extract(item.Link)
	}
	if err != nil {
		return nil, fmt.Errorf("extract failed: %w", err)
	}

	// Create a clean item with UUID
	feedItem := &feeds.Item{
		Id:          extractors.GenerateUniqueID(),
		Title:       item.Title,
		Link:        &feeds.Link{Href: item.Link},
		Description: summarizeHTML(contentHTML, 300),
		Content:     contentHTML,
	}

	// Set created time if available
	if item.PublishedParsed != nil {
		feedItem.Created = *item.PublishedParsed
	} else {
		feedItem.Created = time.Now()
	}

	// Set updated time if available
	if item.UpdatedParsed != nil {
		feedItem.Updated = *item.UpdatedParsed
	} else if item.PublishedParsed != nil {
		feedItem.Updated = *item.PublishedParsed
	} else {
		feedItem.Updated = time.Now()
	}

	// Add author if available
	if item.Author != nil && item.Author.Name != "" {
		feedItem.Author = &feeds.Author{Name: item.Author.Name}
	}

	// Add image if found (first image)
	if len(images) > 0 {
		feedItem.Enclosure = &feeds.Enclosure{
			Url:  images[0],
			Type: "image/jpeg",
		}
	}

	return feedItem, nil
}

// summarizeHTML creates a text summary from HTML content by stripping tags and limiting length.
func summarizeHTML(html string, limit int) string {
	plain := stripTags(html)
	if len(plain) > limit {
		return plain[:limit] + "..."
	}
	return plain
}

// stripTags removes basic HTML tags for generating text summary.
func stripTags(input string) string {
	replacer := strings.NewReplacer("<p>", " ", "</p>", " ", "<br>", " ", "</br>", " ")
	text := replacer.Replace(input)
	for _, tag := range []string{"<strong>", "</strong>", "<em>", "</em>", "<h2>", "</h2>"} {
		text = strings.ReplaceAll(text, tag, "")
	}
	return strings.TrimSpace(text)
}