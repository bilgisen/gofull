package extractors

import (
	"errors"
	"net/url"

	"github.com/google/uuid"
)

// Extractor extracts main readable content (HTML + image URLs).
// The input can be either:
// - string (URL)
// - struct containing HTML content (map[string]string{"html": "<raw html>"})
type Extractor interface {
	Extract(input any) (content string, images []string, err error)
}

// Registry manages registered extractors and a default fallback.
type Registry struct {
	domainExtractors map[string]Extractor
	defaultExtractor  Extractor
}

// NewRegistry creates a new extractor registry.
func NewRegistry() *Registry {
	return &Registry{
		domainExtractors: make(map[string]Extractor),
	}
}

// RegisterDefault sets the default fallback extractor.
func (r *Registry) RegisterDefault(e Extractor) {
	r.defaultExtractor = e
}

// ForURL returns the best extractor for a given URL.
// It checks for domain-specific extractors first, then falls back to default.
func (r *Registry) ForURL(urlStr string) Extractor {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		// If URL parsing fails, return default extractor
		if r.defaultExtractor != nil {
			return r.defaultExtractor
		}
		return &defaultExtractorStub{}
	}

	// Check for domain-specific extractor
	domain := parsedURL.Host
	if extractor, exists := r.domainExtractors[domain]; exists {
		return extractor
	}

	// Fall back to default extractor
	if r.defaultExtractor != nil {
		return r.defaultExtractor
	}
	return &defaultExtractorStub{}
}

// RegisterDomain registers an extractor for a specific domain.
func (r *Registry) RegisterDomain(domain string, extractor Extractor) {
	if r.domainExtractors == nil {
		r.domainExtractors = make(map[string]Extractor)
	}
	r.domainExtractors[domain] = extractor
}

// defaultExtractorStub is used when no extractor is registered.
type defaultExtractorStub struct{}

func (defaultExtractorStub) Extract(input any) (string, []string, error) {
	return "", nil, errors.New("no extractor available")
}

// GenerateUniqueID returns a unique UUID string.
func GenerateUniqueID() string {
	return uuid.New().String()
}
