// internal/app/server.go
package app

import (
	"encoding/json"
	"net/http"
	"time"

	"gofull/internal/extractors"
	"gofull/internal/fetch"
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

	// simple http client wrapper (retryable inside)
	hc := fetch.NewClient(fetch.ClientOptions{
		Timeout:   cfg.RequestTimeout,
		UserAgent: cfg.UserAgent,
	})

	c := NewCache(cfg.CacheDuration)

	// init extractors registry and register default extractor
	r := extractors.NewRegistry()
	// RegisterDefaultExtractor artık sadece http.Client alıyor (logger kaldırıldıysa)
	r.RegisterDefault(extractors.NewDefaultExtractor(hc.StandardClient()))

	// Register domain-specific extractors
	r.RegisterDomain("dunya.com", extractors.NewDunyaExtractor(hc.StandardClient()))

	// FeedHandler oluştur - Cache, Client ve Registry'yi geç
	fh := NewFeedHandler(c, hc.StandardClient(), r)

	// create server and mux
	s := &Server{
		cfg:         cfg,
		cache:       c,
		httpClient:  hc,
		extractors:  r, // Registry'yi Server yapısına da atadık (gelecekte lazım olabilir)
		feedHandler: fh,
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
	s.mux.Handle("/feed", s.feedHandler) // FeedHandler http.Handler arayüzünü uyguluyor
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
<html lang="en">
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
		.form-container {
			background: #f8f9fa;
			padding: 30px;
			border-radius: 12px;
			margin-bottom: 30px;
		}
		form {
			display: flex;
			flex-wrap: wrap;
			gap: 10px;
		}
		input[type="url"],
		input[type="number"] {
			padding: 15px;
			border: 2px solid #ddd;
			border-radius: 8px;
			font-size: 1em;
			transition: border-color 0.3s ease;
		}
		input[type="url"] {
			flex: 2;
			min-width: 250px;
		}
		input[type="number"] {
			width: 120px;
		}
		input:focus {
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
		code {
			background: #f1f3f4;
			padding: 3px 8px;
			border-radius: 4px;
			font-family: 'Courier New', monospace;
			font-size: 0.9em;
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

		<div class="form-container">
			<h2 style="margin-bottom: 20px; color: #333;">Try It Now</h2>
			<form action="/feed" method="get">
				<input type="url" name="url" placeholder="Enter RSS Feed URL (e.g., https://example.com/rss  )" required>
				<input type="number" name="limit" placeholder="Limit" min="1" max="100" value="10" title="Number of articles to fetch">
				<button type="submit">Generate</button>
			</form>
		</div>

		<div class="api-info">
			<h3>API Usage</h3>
			<p>GET endpoint:</p>
			<code>/feed?url={RSS_URL}&limit={NUMBER}</code><br>
			<p>Example: <code>/feed?url=https://news.ycombinator.com/rss&limit=15  </code></p>
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