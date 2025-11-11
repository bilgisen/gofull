package extractors

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

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
	defaultExtractor Extractor
}

// DomainExtractors returns a copy of the domain extractors map for debugging purposes.
func (r *Registry) DomainExtractors() map[string]Extractor {
	result := make(map[string]Extractor, len(r.domainExtractors))
	for k, v := range r.domainExtractors {
		result[k] = v
	}
	return result
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
	// Debug: Print all registered domains at the start
	fmt.Println("\n=== [ForURL] Starting extractor selection ===")
	fmt.Println("URL being processed:", urlStr)
	
	// Print all registered domains and their extractor types
	fmt.Println("\nRegistered domains and their extractor types:")
	for domain, extractor := range r.domainExtractors {
		extractorType := fmt.Sprintf("%T", extractor)
		fmt.Printf("- %s => %s\n", domain, extractorType)
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		fmt.Printf("❌ Error parsing URL '%s': %v\n", urlStr, err)
		if r.defaultExtractor != nil {
			fmt.Println("⚠️  Using default extractor due to URL parse error")
			return r.defaultExtractor
		}
		fmt.Println("❌ No default extractor available, using stub")
		return &defaultExtractorStub{}
	}

	// Clean and normalize the domain
	domain := strings.ToLower(parsedURL.Host)
	// Remove port if present
	domain = strings.Split(domain, ":")[0]
	
	// Remove www. prefix for consistent matching
	if strings.HasPrefix(domain, "www.") {
		domain = domain[4:]
	}
	
	fmt.Printf("\n🔍 Processing URL: %s\n", urlStr)
	fmt.Printf("🔗 Extracted domain: %s\n", domain)

	// Try exact match first (with and without www)
	for _, d := range []string{domain, "www." + domain} {
		if extractor, exists := r.domainExtractors[d]; exists {
			fmt.Printf("✅ Found exact domain match: %s => %T\n", d, extractor)
			return extractor
		}
	}

	// Try subdomain matches (e.g., for 'sub.example.com', try 'example.com')
	parts := strings.Split(domain, ".")
	for i := 1; i < len(parts)-1; i++ {
		parentDomain := strings.Join(parts[i:], ".")
		for _, d := range []string{parentDomain, "www." + parentDomain} {
			if extractor, exists := r.domainExtractors[d]; exists {
				fmt.Printf("✅ Found parent domain match: %s => %T (from %s)\n", d, extractor, domain)
				return extractor
			}
		}
	}

	// Log detailed debug info if no match found
	fmt.Println("\n🔍 Debug Info:")
	fmt.Printf("Looking for domain: %s (and variations)\n", domain)
	fmt.Println("Registered domains:")
	for d := range r.domainExtractors {
		fmt.Printf("- %s\n", d)
	}

	// Log why we're falling back to default
	if r.defaultExtractor != nil {
		fmt.Printf("\n⚠️  No specific extractor found for domain '%s' or its variations, using default extractor\n", domain)
		return r.defaultExtractor
	}
	
	fmt.Printf("\n❌ No extractor found for domain '%s' and no default extractor set\n", domain)
	return &defaultExtractorStub{}
}

// RegisterDomain registers an extractor for a specific domain.
func (r *Registry) RegisterDomain(domain string, extractor Extractor) {
	domain = strings.ToLower(domain)
	fmt.Printf("🔧 Registering extractor for domain: %s => %T\n", domain, extractor)
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
