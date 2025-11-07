// internal/app/server.go
package app

import (
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
			"/gundem/",
		},
		BlockedPaths: []string{
			"/spor/",
			"/foto-galeri/",
			"/video-galeri/",
		},
	})

	// ekonomim.com filters
	filterReg.Register(filters.URLFilter{
		Domain: "ekonomim.com",
		AllowedPaths: []string{
			"/sektorler/",
		},
		BlockedPaths: []string{
			"/spor/",
			"/dunya/",
			"/foto-galeri/",
			"/finans/",
			"/gundem/",
			"/yasam/",
			"/ekonomi/",
			"/sirketler/",
			"/yazar/",
			"/yazarlar/",
			"/son-dakika/",
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
			"/sporskor",
		},
		BlockedPaths: []string{
			"/foto-galeri",
			"/galeri",
			"/dizi-haber",
			"/magazin",
			"/yasam",
			"/saglikli-yasam",
			"/yazarlar",
			"//video",
			"/turkiye",
			"/son-dakika",
			"/gundem",
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
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>RSS Full-Text Proxy with Filtering</title></head>
<body><h1>🚀 RSS Full-Text Proxy</h1><p>Convert RSS feeds to full-text with smart filtering</p></body>
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
