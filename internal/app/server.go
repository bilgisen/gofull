// FILE: internal/app/server.go
package app

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os" // os.Getenv için eklendi
	"time"

	"github.com/mmcdole/gofeed"

	"gofull/internal/extractors"
	"gofull/internal/fetch"
	// "gofull/internal/logger" // logger.Log ve logger.InitLogger için eklendi
)

// Config holds runtime settings for the server.
type Config struct {
	CacheDuration time.Duration
	UserAgent     string
	RequestTimeout time.Duration
}

// DefaultConfig returns sane defaults.
func DefaultConfig() *Config {
	return &Config{
		CacheDuration:  2 * time.Hour,
		UserAgent:      "Mozilla/5.0 (compatible; RSSFullTextBot/1.0)",
		RequestTimeout: 15 * time.Second,
	}
}

// Server is the application server.
type Server struct {
	cfg      *Config
	cache    *Cache
	httpClient *fetch.Client
	// extractor registry (domain -> extractor)
	extractors *extractors.Registry
	// FeedHandler eklendi
	feedHandler *FeedHandler
	mux      *http.ServeMux
	shutdown chan struct{}
}

// NewServer creates a new Server with provided config.
func NewServer(cfg *Config) (*Server, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	stdLogger := log.New(osStdout{}, "gofull: ", log.LstdFlags|log.Lmsgprefix)
	// logWrapper kullan
	logger := &logWrapper{Logger: stdLogger}

	// simple http client wrapper (retryable inside)
	hc := fetch.NewClient(fetch.ClientOptions{
		Timeout:   cfg.RequestTimeout,
		UserAgent: cfg.UserAgent,
	})

	c := NewCache(cfg.CacheDuration)

	// init extractors registry and register default extractor
	r := extractors.NewRegistry()
	// HATA: hc.StandardClient() kullan
	// HATA: logger wrapper kullan
	r.RegisterDefault(extractors.NewDefaultExtractor(hc.StandardClient())) // Değiştirildi

	// FeedHandler oluştur
	fp := gofeed.NewParser()
	fh := &FeedHandler{
		Client:     hc.StandardClient(), // fetch.Client değil, *http.Client -> fetch.Client.StandardClient() metodu ile al
		Registry:   r,
		Cache:      c,
		FeedParser: fp,
	}

	// create server and mux
	s := &Server{
		cfg:         cfg,
		cache:       c,
		httpClient:  hc,
		extractors:  r,
		feedHandler: fh, // Eklendi
		mux:         http.NewServeMux(),
		shutdown:    make(chan struct{}),
	}

	s.registerRoutes()
	return s, nil
}

// Run starts the HTTP server and background workers.
func (s *Server) Run(addr string) error {
	// start cache cleaner
	go s.cacheCleanerLoop()

	// create http server
	h := &http.Server{
		Addr:    addr,
		Handler: s.withCommonHeaders(s.mux),
	}

	// listen and serve
	if err := h.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/", s.handleHome)
	s.mux.HandleFunc("/feed", s.handleFeed) // handleFeed metodu artık var
	s.mux.HandleFunc("/health", s.handleHealth)
}

// withCommonHeaders adds CORS and common headers.
func (s *Server) withCommonHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Server", "gofull")
		h.ServeHTTP(w, r)
	})
}

// handleHome serves a modern HTML interface for the RSS Full-Text Proxy.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	homeHTML := `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Gofull - RSS Full-Text Proxy</title>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
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
            font-size: 2.8em;
            color: #667eea;
            margin-bottom: 10px;
            text-align: center;
        }
        .subtitle {
            color: #666;
            margin-bottom: 40px;
            font-size: 1.2em;
            text-align: center;
        }
        .feature-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
            gap: 30px;
            margin-bottom: 40px;
        }
        .feature-card {
            background: #f8f9fa;
            padding: 25px;
            border-radius: 12px;
            border-left: 4px solid #667eea;
        }
        .feature-card h3 {
            color: #333;
            margin-bottom: 10px;
            font-size: 1.3em;
        }
        .feature-card p {
            color: #666;
            line-height: 1.6;
        }
        code {
            background: #f1f3f4;
            padding: 3px 8px;
            border-radius: 4px;
            font-family: 'Courier New', monospace;
            font-size: 0.9em;
        }
        .form-container {
            background: #f8f9fa;
            padding: 30px;
            border-radius: 12px;
            margin-bottom: 30px;
        }
        input[type="url"] {
            width: 100%;
            padding: 15px;
            border: 2px solid #ddd;
            border-radius: 8px;
            font-size: 1em;
            margin-bottom: 15px;
            transition: border-color 0.3s ease;
        }
        input[type="url"]:focus {
            outline: none;
            border-color: #667eea;
        }
        button {
            padding: 15px 40px;
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            border: none;
            border-radius: 8px;
            font-size: 1em;
            cursor: pointer;
            font-weight: 600;
            transition: all 0.3s ease;
        }
        button:hover {
            transform: translateY(-2px);
            box-shadow: 0 5px 15px rgba(102, 126, 234, 0.4);
        }
        .api-info {
            background: #e8f4f8;
            padding: 20px;
            border-radius: 8px;
            border-left: 4px solid #667eea;
        }
        .footer {
            text-align: center;
            color: #666;
            margin-top: 30px;
            font-size: 0.9em;
        }
    </style>
</head>
<body>
    <div class="container">
        <h1>🚀 RSS Full-Text Proxy</h1>
        <p class="subtitle">Transform any RSS feed into full-text articles with intelligent content extraction</p>

        <div class="feature-grid">
            <div class="feature-card">
                <h3>🔍 Smart Extraction</h3>
                <p>Uses advanced algorithms to extract full article content from RSS feeds, including text, images, and metadata.</p>
            </div>
            <div class="feature-card">
                <h3>⚡ High Performance</h3>
                <p>Built with Go for maximum speed and efficiency. Includes intelligent caching for optimal response times.</p>
            </div>
            <div class="feature-card">
                <h3>🌐 Universal Support</h3>
                <p>Works with any RSS/Atom feed from any website. Supports multiple content extraction strategies.</p>
            </div>
        </div>

        <div class="form-container">
            <h2 style="margin-bottom: 20px; color: #333;">Try It Now</h2>
            <form action="/feed" method="get" style="display: flex; gap: 10px;">
                <input type="url" name="url" placeholder="Enter RSS Feed URL (e.g., https://example.com/rss)" required style="flex: 1;">
                <button type="submit">Generate Full-Text Feed</button>
            </form>
        </div>

        <div class="api-info">
            <h3>API Usage</h3>
            <p>Use the endpoint directly: <code>GET /feed?url={RSS_URL}</code></p>
            <p>Example: <code>/feed?url=https://news.ycombinator.com/rss</code></p>
        </div>

        <div class="footer">
            <p>Built with ❤️ using Go | Open Source RSS Full-Text Proxy</p>
        </div>
    </div>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(homeHTML))
}

// handleFeed handles the /feed endpoint.
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	feedURL := r.URL.Query().Get("url")
	if feedURL == "" {
		http.Error(w, "Missing 'url' query parameter", http.StatusBadRequest)
		return
	}

	feed, err := s.feedHandler.ProcessFeed(feedURL)
	if err != nil {
		http.Error(w, "Failed to process feed", http.StatusInternalServerError)
		return
	}

	// Assuming feed is a *feeds.Feed, serialize it to XML for RSS/Atom
	atom, err := feed.ToAtom()
	if err != nil {
		http.Error(w, "Failed to serialize feed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/atom+xml; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(atom))
}

// handleHealth returns JSON health information.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "ok",
		"service":   "gofull",
		"cache_size": s.cache.Size(),
		"timestamp": time.Now().Format(time.RFC3339),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

// cacheCleanerLoop periodically triggers cache cleanup.
func (s *Server) cacheCleanerLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	for {
		select {
		case <-ticker.C:
			s.cache.Cleanup()
		case <-s.shutdown:
			ticker.Stop()
			return
		}
	}
}

// osStdout implements io.Writer so we can wrap standard output logger without importing os everywhere.
type osStdout struct{}

func (osStdout) Write(p []byte) (n int, err error) {
	return fmt.Print(string(p))
}