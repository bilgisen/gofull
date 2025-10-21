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
	"gofull/internal/logger" // logger.Log ve logger.InitLogger için eklendi
)

// init fonksiyonu, logger'ı başlatmak için app paketine eklendi.
func init() {
	logger.InitLogger(os.Getenv("APP_ENV")) // global logger.Log'u başlatır
}

// logWrapper wraps *log.Logger to match interface{ Printf(...interface{}) }
type logWrapper struct {
	*log.Logger
}

func (lw *logWrapper) Printf(v ...interface{}) {
	lw.Logger.Printf(fmt.Sprint(v...)) // fmt.Sprint ile tüm argümanları tek string haline getir
}

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
	logger   *logWrapper // *log.Logger yerine logWrapper
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
	r.RegisterDefault(extractors.NewDefaultExtractor(hc.StandardClient(), logger)) // Değiştirildi

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
		logger:      logger,
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

	s.logger.Printf("🚀 Server starting on %s", addr)

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

// handleHome serves a minimal HTML page (kept small here).
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	html := `<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>gofull</title></head><body><h1>gofull — RSS Full Text Proxy</h1><p>See <a href="/feed">/feed</a> endpoint</p></body></html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
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
		s.logger.Printf("Error processing feed %s: %v", feedURL, err)
		http.Error(w, "Failed to process feed", http.StatusInternalServerError)
		return
	}

	// Assuming feed is a *feeds.Feed, serialize it to XML for RSS/Atom
	atom, err := feed.ToAtom()
	if err != nil {
		s.logger.Printf("Error serializing feed to Atom %s: %v", feedURL, err)
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
			s.logger.Printf("cache cleaned, size=%d", s.cache.Size())
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