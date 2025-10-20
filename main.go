// main.go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-shiori/go-readability"
	"github.com/gorilla/feeds"
	"github.com/mmcdole/gofeed"
)

// Cache structure
type CachedContent struct {
	Content   string
	Timestamp time.Time
}

type Server struct {
	cache     map[string]CachedContent
	cacheMux  sync.RWMutex
	cacheTime time.Duration
}

func NewServer() *Server {
	return &Server{
		cache:     make(map[string]CachedContent),
		cacheTime: 2 * time.Hour, // Cache for 2 hours
	}
}

// Fetch full text with readability
func (s *Server) fetchFullText(articleURL string) string {
	// Check cache first
	s.cacheMux.RLock()
	if cached, exists := s.cache[articleURL]; exists {
		if time.Since(cached.Timestamp) < s.cacheTime {
			s.cacheMux.RUnlock()
			log.Printf("Cache hit: %s", articleURL)
			return cached.Content
		}
	}
	s.cacheMux.RUnlock()

	log.Printf("Fetching: %s", articleURL)

	// Fetch with timeout
	article, err := readability.FromURL(articleURL, 15*time.Second)
	if err != nil {
		log.Printf("Error fetching %s: %v", articleURL, err)
		return fmt.Sprintf("⚠️ Failed to fetch content from %s\n\nError: %v", articleURL, err)
	}

	content := article.TextContent
	if content == "" {
		content = "⚠️ No content could be extracted from this article."
	}

	// Cache the result
	s.cacheMux.Lock()
	s.cache[articleURL] = CachedContent{
		Content:   content,
		Timestamp: time.Now(),
	}
	s.cacheMux.Unlock()

	return content
}

// Clean old cache entries
func (s *Server) cleanCache() {
	ticker := time.NewTicker(1 * time.Hour)
	for range ticker.C {
		s.cacheMux.Lock()
		now := time.Now()
		for url, cached := range s.cache {
			if now.Sub(cached.Timestamp) > s.cacheTime {
				delete(s.cache, url)
			}
		}
		s.cacheMux.Unlock()
		log.Printf("Cache cleaned. Current size: %d", len(s.cache))
	}
}

// Handle RSS feed generation
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	sourceURL := r.URL.Query().Get("url")
	if sourceURL == "" {
		http.Error(w, `{"error": "Missing 'url' parameter"}`, http.StatusBadRequest)
		return
	}

	// Parse limit parameter
	limit := 10
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 50 {
			limit = l
		}
	}

	// Decode URL if encoded
	decodedURL, err := url.QueryUnescape(sourceURL)
	if err != nil {
		decodedURL = sourceURL
	}

	log.Printf("Processing feed: %s (limit: %d)", decodedURL, limit)

	// Parse original RSS feed
	fp := gofeed.NewParser()
	fp.UserAgent = "Mozilla/5.0 (compatible; RSSFullTextBot/1.0)"
	
	feed, err := fp.ParseURL(decodedURL)
	if err != nil {
		log.Printf("Error parsing feed: %v", err)
		http.Error(w, fmt.Sprintf(`{"error": "Failed to parse RSS feed: %v"}`, err), http.StatusBadRequest)
		return
	}

	if len(feed.Items) == 0 {
		http.Error(w, `{"error": "No items found in feed"}`, http.StatusNotFound)
		return
	}

	// Create new feed
	newFeed := &feeds.Feed{
		Title:       feed.Title + " - Full Text",
		Link:        &feeds.Link{Href: feed.Link},
		Description: fmt.Sprintf("Full-text version of %s", feed.Title),
		Author:      &feeds.Author{Name: "RSS Full-Text Proxy"},
		Created:     time.Now(),
	}

	if feed.Image != nil {
		newFeed.Image = &feeds.Image{
			Url:   feed.Image.URL,
			Title: feed.Image.Title,
		}
	}

	// Process each item
	processed := 0
	for i, item := range feed.Items {
		if i >= limit {
			break
		}

		if item.Link == "" {
			log.Printf("Skipping item without link: %s", item.Title)
			continue
		}

		log.Printf("[%d/%d] Processing: %s", i+1, limit, item.Title)

		// Fetch full text
		fullText := s.fetchFullText(item.Link)

		// Parse publication date
		pubDate := time.Now()
		if item.PublishedParsed != nil {
			pubDate = *item.PublishedParsed
		}

		// Create content with full text
		content := fmt.Sprintf(`<div style="font-family: system-ui, -apple-system, sans-serif; line-height: 1.6; max-width: 800px;">
<p style="background: #f0f0f0; padding: 10px; border-radius: 5px;">
	<strong>📄 Original Article:</strong> <a href="%s" target="_blank">%s</a>
</p>
<hr style="margin: 20px 0; border: none; border-top: 1px solid #ddd;">
<div style="white-space: pre-wrap; color: #333;">%s</div>
</div>`, item.Link, item.Link, fullText)

		// Add to new feed
		feedItem := &feeds.Item{
			Title:       item.Title,
			Link:        &feeds.Link{Href: item.Link},
			Description: content,
			Content:     content,
			Created:     pubDate,
		}

		if item.Author != nil {
			feedItem.Author = &feeds.Author{Name: item.Author.Name}
		}

		newFeed.Items = append(newFeed.Items, feedItem)
		processed++
	}

	log.Printf("Successfully processed %d/%d articles", processed, len(feed.Items))

	// Generate RSS XML
	rss, err := newFeed.ToRss()
	if err != nil {
		http.Error(w, `{"error": "Failed to generate RSS"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(rss))
}

// Home page handler
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>RSS Full-Text Proxy</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { 
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            min-height: 100vh;
            padding: 20px;
        }
        .container {
            max-width: 900px;
            margin: 0 auto;
            background: white;
            border-radius: 20px;
            padding: 40px;
            box-shadow: 0 20px 60px rgba(0,0,0,0.3);
        }
        h1 { 
            font-size: 2.5em;
            color: #667eea;
            margin-bottom: 10px;
        }
        .subtitle {
            color: #666;
            margin-bottom: 30px;
            font-size: 1.1em;
        }
        .section {
            margin: 30px 0;
        }
        h2 {
            color: #333;
            margin-bottom: 15px;
            font-size: 1.5em;
        }
        code {
            background: #f5f5f5;
            padding: 15px;
            display: block;
            border-radius: 8px;
            overflow-x: auto;
            font-size: 0.9em;
            border-left: 4px solid #667eea;
        }
        ul {
            list-style: none;
            padding-left: 0;
        }
        li {
            padding: 8px 0;
            padding-left: 25px;
            position: relative;
        }
        li:before {
            content: "→";
            position: absolute;
            left: 0;
            color: #667eea;
            font-weight: bold;
        }
        .form-group {
            background: #f8f9fa;
            padding: 30px;
            border-radius: 12px;
            margin-top: 20px;
        }
        input[type="url"] {
            width: 100%;
            padding: 15px;
            border: 2px solid #ddd;
            border-radius: 8px;
            font-size: 1em;
            margin-bottom: 15px;
            transition: border-color 0.3s;
        }
        input[type="url"]:focus {
            outline: none;
            border-color: #667eea;
        }
        .input-row {
            display: flex;
            gap: 10px;
            align-items: center;
        }
        input[type="number"] {
            width: 100px;
            padding: 15px;
            border: 2px solid #ddd;
            border-radius: 8px;
            font-size: 1em;
        }
        button {
            padding: 15px 40px;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            border: none;
            border-radius: 8px;
            font-size: 1em;
            cursor: pointer;
            transition: transform 0.2s, box-shadow 0.2s;
            font-weight: 600;
        }
        button:hover {
            transform: translateY(-2px);
            box-shadow: 0 10px 25px rgba(102, 126, 234, 0.4);
        }
        .stats {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
            gap: 15px;
            margin: 20px 0;
        }
        .stat-card {
            background: #f8f9fa;
            padding: 20px;
            border-radius: 10px;
            text-align: center;
        }
        .stat-value {
            font-size: 2em;
            font-weight: bold;
            color: #667eea;
        }
        .stat-label {
            color: #666;
            font-size: 0.9em;
            margin-top: 5px;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>🚀 RSS Full-Text Proxy</h1>
        <p class="subtitle">Convert any RSS feed to full-text articles</p>

        <div class="stats">
            <div class="stat-card">
                <div class="stat-value">∞</div>
                <div class="stat-label">Feeds Supported</div>
            </div>
            <div class="stat-card">
                <div class="stat-value">2h</div>
                <div class="stat-label">Cache Duration</div>
            </div>
            <div class="stat-card">
                <div class="stat-value">50</div>
                <div class="stat-label">Max Articles</div>
            </div>
        </div>

        <div class="section">
            <h2>📖 How to Use</h2>
            <code>GET /feed?url={RSS_URL}&limit={NUMBER}</code>
        </div>

        <div class="section">
            <h2>🔗 Example</h2>
            <code>https://your-app.railway.app/feed?url=https://techcrunch.com/feed/&limit=10</code>
        </div>

        <div class="section">
            <h2>⚙️ Parameters</h2>
            <ul>
                <li><strong>url</strong> (required) - RSS feed URL</li>
                <li><strong>limit</strong> (optional) - Number of articles (default: 10, max: 50)</li>
            </ul>
        </div>

        <div class="form-group">
            <h2 style="margin-bottom: 20px;">🧪 Try It Now</h2>
            <form action="/feed" method="get">
                <input type="url" name="url" placeholder="Enter RSS Feed URL (e.g., https://techcrunch.com/feed/)" required>
                <div class="input-row">
                    <input type="number" name="limit" placeholder="Limit" value="10" min="1" max="50">
                    <button type="submit">Generate Full-Text Feed</button>
                </div>
            </form>
        </div>

        <div class="section">
            <h2>✨ Features</h2>
            <ul>
                <li>Automatic full-text extraction from any article</li>
                <li>Smart caching for better performance</li>
                <li>Preserves original feed metadata</li>
                <li>Clean, readable output</li>
                <li>No ads or tracking</li>
            </ul>
        </div>

        <hr style="margin: 40px 0; border: none; border-top: 1px solid #ddd;">
        <p style="text-align: center; color: #999; font-size: 0.9em;">
            Powered by Go + Readability | <a href="/health" style="color: #667eea;">Health Check</a>
        </p>
    </div>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// Health check endpoint
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":     "ok",
		"service":    "RSS Full-Text Proxy",
		"version":    "1.0.0",
		"cache_size": len(s.cache),
		"timestamp":  time.Now().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

func main() {
	server := NewServer()

	// Start cache cleanup goroutine
	go server.cleanCache()

	// Routes
	http.HandleFunc("/", server.handleHome)
	http.HandleFunc("/feed", server.handleFeed)
	http.HandleFunc("/health", server.handleHealth)

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("🚀 Server starting on port %s...", port)
	log.Printf("📍 Access at http://localhost:%s", port)
	
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}