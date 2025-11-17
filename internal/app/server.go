// internal/app/server.go
package app

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"gofull/internal/extractors"
	"gofull/internal/extractors/filters"
)

// Config holds server configuration
type Config struct {
	CacheTTL time.Duration
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		CacheTTL: 5 * time.Minute,
	}
}

// Server represents the HTTP server
type Server struct {
	mux          *http.ServeMux
	cache        *Cache
	extractorReg *extractors.Registry
	filterReg    *filters.FilterRegistry
}

// NewServer creates and configures a new server
func NewServer(cfg *Config) (*Server, error) {
	cache := NewCache(cfg.CacheTTL)

	// Setup extractor registry
	extractorReg := extractors.NewRegistry()

	// Register default extractor
	defaultExt := extractors.NewDefaultExtractor(nil)
	extractorReg.RegisterDefault(defaultExt)

	// Register domain-specific extractors
	dunyaExt := extractors.NewDunyaExtractor(nil)
	extractorReg.RegisterDomain("www.dunya.com", dunyaExt)
	extractorReg.RegisterDomain("dunya.com", dunyaExt)

	cnbceExt := extractors.NewCNBCEExtractor(nil)
	extractorReg.RegisterDomain("www.cnbce.com", cnbceExt)
	extractorReg.RegisterDomain("cnbce.com", cnbceExt)

	ntvExt := extractors.NewNTVExtractor(nil)
	extractorReg.RegisterDomain("www.ntv.com.tr", ntvExt)
	extractorReg.RegisterDomain("ntv.com.tr", ntvExt)

	// Register T24 extractor
	log.Println("\n=== BEFORE T24 Extractor Registration ===")
	for domain, extractor := range extractorReg.DomainExtractors() {
		log.Printf("Before: %s => %T\n", domain, extractor)
	}
	
	// Create a new T24 extractor with a custom HTTP client that follows redirects
	httpClient := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			log.Printf("Following redirect from %s to %s\n", via[0].URL, req.URL)
			return nil
		},
	}
	t24Ext := extractors.NewT24Extractor(httpClient)
	
	log.Println("\n=== REGISTERING T24 Extractor ===")
	log.Printf("T24 Extractor type: %T, value: %+v\n", t24Ext, t24Ext)
	
	// Register both with and without www
	log.Println("Registering T24 extractor for domains: t24.com.tr, www.t24.com.tr")
	extractorReg.RegisterDomain("www.t24.com.tr", t24Ext)
	extractorReg.RegisterDomain("t24.com.tr", t24Ext)
	
	// Verify registration
	log.Println("\n=== AFTER T24 Extractor Registration ===")
	registered := false
	for domain, extractor := range extractorReg.DomainExtractors() {
		log.Printf("After: %s => %T\n", domain, extractor)
		if domain == "t24.com.tr" || domain == "www.t24.com.tr" {
			registered = true
		}
	}
	if !registered {
		log.Println(" ERROR: T24 extractor was not registered! This is a critical error.")
		// Print the actual state of the domainExtractors map
		log.Println("Current domainExtractors state:")
		for domain, extractor := range extractorReg.DomainExtractors() {
			log.Printf("- %s => %T\n", domain, extractor)
		}
	} else {
		log.Println(" T24 extractor registered successfully")
	}
	log.Println("===================================\n")

	ekonomimExt := extractors.NewEkonomimExtractor(nil)
	log.Println("Registering EkonomimExtractor for domains:")
	for _, domain := range []string{
		"ekonomim.com",
		"www.ekonomim.com",
		"ekonomim.com.tr",
		"www.ekonomim.com.tr",
	} {
		extractorReg.RegisterDomain(domain, ekonomimExt)
		log.Printf("- %s\n", domain)
	}

	// ✅ Register Kisadalga extractor (the missing part)
	kisadalgaExt := extractors.NewKisadalgaExtractor(nil)
	for _, domain := range []string{
		"kisadalga.net",
		"www.kisadalga.net",
	} {
		extractorReg.RegisterDomain(domain, kisadalgaExt)
		log.Printf("Registered KisadalgaExtractor for %s\n", domain)
	}

	// Setup filter registry
	filterReg := filters.NewFilterRegistry()

	// dunya.com filters
	filterReg.Register(filters.URLFilter{
		Domain: "dunya.com",
		AllowedPaths: []string{
			"/finans/haberler/",
			"/sirketler/",
			"/sektorler/",
			"/ekonomi/",
		},
		BlockedPaths: []string{
			"/spor/",
			"/foto-galeri/",
			"/video-galeri/",
			"/gundem/",
			"/son-dakika/",
			"/dunya/",
			"/kultur-sanat/",
			"/kose-yazisi/",
		},
	})

	// ekonomim.com filters
	filterReg.Register(filters.URLFilter{
		Domain: "ekonomim.com",
		AllowedPaths: []string{
			"/sektorler/",
			"/sirketler/",
			"/ekonomi/",
			"/finans/",
		},
		BlockedPaths: []string{
			"/spor/",
			"/dunya/",
			"/foto-galeri/",
			"/gundem/",
			"/yasam/",
			"/yazar/",
			"/yazarlar/",
			"/son-dakika/",
			"/saglik/",
		},
	})

	// cnbce.com filters
	filterReg.Register(filters.URLFilter{
		Domain: "cnbce.com",
		BlockedPaths: []string{
			"/haberler",
			"/tv",
			"/art-e",
			"/gundem",
			"/son-dakika",
		},
	})

	// ntv.com.tr filters
	filterReg.Register(filters.URLFilter{
		Domain: "ntv.com.tr",
		AllowedPaths: []string{
			"/kultur-ve-sanat",
			"/dunya",
			"/teknoloji",
			"/turkiye",
			"/ntvpara",
			"/ekonomi",
		},
		BlockedPaths: []string{
			"/foto-galeri",
			"/galeri",
			"/dizi-haber",
			"/magazin",
			"/yasam",
			"/saglikli-yasam",
			"/yazarlar",
			"/video",
			"/son-dakika",
			"/gundem",
			"/video/turkiye",
		},
	})

	// kisadalga.net filters
	filterReg.Register(filters.URLFilter{
		Domain: "kisadalga.net",
		AllowedPaths: []string{
			"/haber/gundem/",
		},
	})

	srv := &Server{
		mux:          http.NewServeMux(),
		cache:        cache,
		extractorReg: extractorReg,
		filterReg:    filterReg,
	}

	srv.setupRoutes()
	return srv, nil
}

func (s *Server) setupRoutes() {
	feedHandler := NewFeedHandler(s.cache, nil, s.extractorReg, s.filterReg)
	s.mux.HandleFunc("/", s.handleHome)
	s.mux.Handle("/feed", feedHandler)
	s.mux.HandleFunc("/health", s.handleHealth)
	
	// Add extract endpoint
	s.mux.HandleFunc("/extract", func(w http.ResponseWriter, r *http.Request) {
		url := r.URL.Query().Get("url")
		if url == "" {
			http.Error(w, "Missing 'url' parameter", http.StatusBadRequest)
			return
		}

		// Extract content using the extractor registry
		extractor := s.extractorReg.ForURL(url)
		content, _, err := extractor.Extract(url)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error extracting content: %v", err), http.StatusInternalServerError)
			return
		}

		// Return the extracted content as plain text
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(content))
	})
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>RSS Full-Text Proxy with Filtering</title>
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
        h1 { font-size: 2.5em; color: #667eea; margin-bottom: 10px; }
        .subtitle { color: #666; margin-bottom: 30px; font-size: 1.1em; }
        code {
            background: #f5f5f5;
            padding: 15px;
            display: block;
            border-radius: 8px;
            overflow-x: auto;
            margin: 10px 0;
            border-left: 4px solid #667eea;
        }
        input[type="url"] {
            width: 100%;
            padding: 15px;
            border: 2px solid #ddd;
            border-radius: 8px;
            font-size: 1em;
            margin-bottom: 15px;
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
            font-weight: 600;
        }
        button:hover { transform: translateY(-2px); }
    </style>
</head>
<body>
    <div class="container">
        <h1>🚀 RSS Full-Text Proxy</h1>
        <p class="subtitle">Convert RSS feeds to full-text with smart filtering</p>

        <h2>Usage</h2>
        <code>GET /feed?url={RSS_URL}&limit={NUMBER}</code>

        <h2>Try It</h2>
        <form action="/feed" method="get">
            <input type="url" name="url" placeholder="RSS Feed URL" required>
            <div class="input-row">
                <input type="number" name="limit" value="10" min="1" max="50">
                <button type="submit">Generate</button>
            </div>
        </form>
    </div>
</body>
</html>`
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok","service":"RSS Full-Text Proxy"}`))
}

func (s *Server) Run(addr string) error {
	log.Printf("🚀 Server starting on %s", addr)
	return http.ListenAndServe(addr, s.mux)
}
