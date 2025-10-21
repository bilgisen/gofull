// FILE: internal/app/server.go
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
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
	logger   *log.Logger
	cache    *Cache
	httpClient *fetch.Client
	// extractor registry (domain -> extractor)
	extractors *extractors.Registry
	mux      *http.ServeMux
	shutdown chan struct{}
}

// NewServer creates a new Server with provided config.
func NewServer(cfg *Config) (*Server, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	logger := log.New(osStdout{}, "gofull: ", log.LstdFlags|log.Lmsgprefix)

	// simple http client wrapper (retryable inside)
	hc := fetch.NewClient(fetch.ClientOptions{
		Timeout:   cfg.RequestTimeout,
		UserAgent: cfg.UserAgent,
	})

	c := NewCache(cfg.CacheDuration)

	// init extractors registry and register default extractor
	r := extractors.NewRegistry()
	r.RegisterDefault(extractors.NewDefaultExtractor(hc, logger))

	// create server and mux
	s := &Server{
		cfg:       cfg,
		logger:    logger,
		cache:     c,
		httpClient: hc,
		extractors: r,
		mux:       http.NewServeMux(),
		shutdown:  make(chan struct{}),
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

	s.logger.Printf("🚀 Server starting on %s", addr)

	// listen and serve
	if err := h.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("/", s.handleHome)
	s.mux.HandleFunc("/feed", s.handleFeed)
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

// handleHome serves a minimal HTML page (kept small here).
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	html := `<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>gofull</title></head><body><h1>gofull — RSS Full Text Proxy</h1><p>See <a href="/feed">/feed</a> endpoint</p></body></html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
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
			s.logger.Printf("cache cleaned, size=%d", s.cache.Size())
		case <-s.shutdown:
			ticker.Stop()
			return
		}
	}
}

// Note: handleFeed is implemented in feed_handler.go within same package.

// osStdout implements io.Writer so we can wrap standard output logger without importing os everywhere.
type osStdout struct{}

func (osStdout) Write(p []byte) (n int, err error) {
	return fmt.Print(string(p))
}
