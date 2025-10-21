package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gorilla/feeds"
	"github.com/mmcdole/gofeed"
	"gofull/internal/extractors"
)

// FeedHandler orchestrates RSS/Atom feed fetching and content enrichment.
type FeedHandler struct {
	Client     *http.Client
	Registry   *extractors.Registry
	Cache      *Cache
	FeedParser *gofeed.Parser
}

// NewFeedHandler builds a ready-to-use FeedHandler with sane defaults.
func NewFeedHandler() *FeedHandler {
	client := &http.Client{Timeout: 20 * time.Second}
	reg := extractors.NewRegistry()
	reg.RegisterDefault(extractors.NewDefaultExtractor(client))

	return &FeedHandler{
		Client:     client,
		Registry:   reg,
		Cache:      NewCache(200), // in-memory cache capacity
		FeedParser: gofeed.NewParser(),
	}
}

// ProcessFeed fetches and processes a full feed concurrently.
func (h *FeedHandler) ProcessFeed(feedURL string) (*feeds.Feed, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	parsedFeed, err := h.FeedParser.ParseURLWithContext(feedURL, ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to parse feed: %w", err)
	}
	if parsedFeed == nil {
		return nil, fmt.Errorf("parsed feed is nil")
	}

	outputFeed := &feeds.Feed{
		Title:       parsedFeed.Title,
		Link:        &feeds.Link{Href: feedURL},
		Description: parsedFeed.Description,
		Author: &feeds.Author{
			Name:  parsedFeed.Author.Name,
			Email: parsedFeed.Author.Email,
		},
		Created: time.Now(),
	}

	// --- Concurrency setup ---
	var wg sync.WaitGroup
	itemChan := make(chan *feeds.Item, len(parsedFeed.Items))

	for _, item := range parsedFeed.Items {
		it := item // avoid closure capture bug
		wg.Add(1)
		go func() {
			defer wg.Done()

			ctxItem, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()

			processed, err := h.processItemWithCtx(ctxItem, it)
			if err != nil {
				log.Printf("[WARN] failed item: %s | %v", it.Link, err)
				return
			}
			itemChan <- processed
		}()
	}

	wg.Wait()
	close(itemChan)

	for it := range itemChan {
		outputFeed.Items = append(outputFeed.Items, it)
	}

	if len(outputFeed.Items) == 0 {
		return nil, errors.New("no valid items processed")
	}

	return outputFeed, nil
}

// processItemWithCtx extracts, cleans, caches and returns a feeds.Item.
func (h *FeedHandler) processItemWithCtx(ctx context.Context, item *gofeed.Item) (*feeds.Item, error) {
	if item == nil || item.Link == "" {
		return nil, fmt.Errorf("invalid item or missing link")
	}

	cacheKey := item.Link
	if cached, ok := h.Cache.Get(cacheKey); ok && cached != "" {
		return h.buildFeedItem(item, cached, nil), nil
	}

	domainExtractor := h.Registry.ForURL(item.Link)
	var contentHTML string
	var images []string
	var err error

	// Prefer feed-provided HTML if available
	if item.Content != "" && strings.Contains(item.Content, "<") {
		contentHTML, images, err = domainExtractor.Extract(map[string]string{"html": item.Content})
	} else {
		contentHTML, images, err = domainExtractor.Extract(item.Link)
	}

	if err != nil {
		return nil, fmt.Errorf("extract failed: %w", err)
	}
	if contentHTML == "" {
		return nil, errors.New("empty content")
	}

	h.Cache.Set(cacheKey, contentHTML)
	return h.buildFeedItem(item, contentHTML, images), nil
}

// buildFeedItem constructs a full feeds.Item ready for serialization.
func (h *FeedHandler) buildFeedItem(src *gofeed.Item, content string, images []string) *feeds.Item {
	item := &feeds.Item{
		Id:          extractors.GenerateUniqueID(),
		Title:       strings.TrimSpace(src.Title),
		Link:        &feeds.Link{Href: src.Link},
		Description: summarizeHTML(content, 300),
		Content:     content,
		Created:     coalesceTime(src.PublishedParsed, time.Now()),
		Updated:     coalesceTime(src.UpdatedParsed, coalesceTime(src.PublishedParsed, time.Now())),
	}

	if src.Author != nil && src.Author.Name != "" {
		item.Author = &feeds.Author{Name: src.Author.Name}
	}

	if len(images) > 0 {
		item.Enclosure = &feeds.Enclosure{
			Url:  images[0],
			Type: detectImageType(images[0]),
		}
	}

	return item
}

// summarizeHTML strips HTML and truncates plain text for feed description.
func summarizeHTML(html string, limit int) string {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}
	text := strings.Join(strings.Fields(doc.Text()), " ")
	if len(text) > limit {
		text = text[:limit] + "..."
	}
	return text
}

// detectImageType makes a best guess from URL extension.
func detectImageType(url string) string {
	switch {
	case strings.HasSuffix(url, ".png"):
		return "image/png"
	case strings.HasSuffix(url, ".webp"):
		return "image/webp"
	case strings.HasSuffix(url, ".gif"):
		return "image/gif"
	default:
		return "image/jpeg"
	}
}

// coalesceTime returns the first non-nil time pointer or fallback.
func coalesceTime(t1 *time.Time, fallback time.Time) time.Time {
	if t1 != nil {
		return *t1
	}
	return fallback
}
