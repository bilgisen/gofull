package extractors

import (
	"errors"

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
	defaultExtractor Extractor
}

// NewRegistry creates a new extractor registry.
func NewRegistry() *Registry {
	return &Registry{}
}

// RegisterDefault sets the default fallback extractor.
func (r *Registry) RegisterDefault(e Extractor) {
	r.defaultExtractor = e
}

// ForURL returns the best extractor for a given URL.
// For now, it always returns the default extractor.
func (r *Registry) ForURL(url string) Extractor {
	if r.defaultExtractor != nil {
		return r.defaultExtractor
	}
	return &defaultExtractorStub{}
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
