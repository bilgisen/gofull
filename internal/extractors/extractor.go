// FILE: internal/extractors/extractor.go
package extractors

import (
	"net/http"

	"github.com/google/uuid"
)

// Extractor extracts the main content and returns HTML + image URLs.
// The input can be a URL string or an HTML content string.
type Extractor interface {
	Extract(input any) (content string, images []string, err error)
}

// Registry holds registered extractors and default fallback.
type Registry struct {
	defaultExtractor Extractor
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) RegisterDefault(e Extractor) {
	r.defaultExtractor = e
}

// ForURL returns the best extractor for the URL. For now returns default.
func (r *Registry) ForURL(url string) Extractor {
	// future: match domain specific extractors
	if r.defaultExtractor != nil {
		return r.defaultExtractor
	}
	return &defaultExtractorStub{}
}

// defaultExtractorStub is a last-resort extractor.
type defaultExtractorStub struct{}

func (defaultExtractorStub) Extract(input any) (string, []string, error) {
	return "", nil, http.ErrNotSupported
}

// GenerateUniqueID creates a UUID for RSS items
func GenerateUniqueID() string {
	return uuid.New().String()
}