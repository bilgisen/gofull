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
	// Debug: Print all registered domains at the start
	fmt.Println("\n=== [ForURL] Starting extractor selection ===")
	fmt.Println("Registered domains and their extractor types:")
	for domain, extractor := range r.domainExtractors {
		extractorType := fmt.Sprintf("%T", extractor)
		fmt.Printf("- %s => %s\n", domain, extractorType)
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		fmt.Printf("Error parsing URL '%s': %v\n", urlStr, err)
		if r.defaultExtractor != nil {
			fmt.Println("Using default extractor due to URL parse error")
			return r.defaultExtractor
		}
		fmt.Println("No default extractor available, using stub")
		return &defaultExtractorStub{}
	}

	// Clean and normalize the domain
	domain := strings.ToLower(parsedURL.Host)
	fmt.Printf("\nProcessing URL: %s\n", urlStr)
	fmt.Printf("Extracted domain: %s\n", domain)

	// Try exact match first
	if extractor, exists := r.domainExtractors[domain]; exists {
		fmt.Printf("✅ Found exact domain match: %s => %T\n", domain, extractor)
		return extractor
	}

	// Try with/without www
	var modifiedDomains []string
	if strings.HasPrefix(domain, "www.") {
		modifiedDomains = append(modifiedDomains, domain[4:]) // Try without www
	} else {
		modifiedDomains = append(modifiedDomains, "www."+domain) // Try with www

		// For subdomains, try parent domains
		parts := strings.Split(domain, ".")
		if len(parts) > 2 {
			modifiedDomains = append(modifiedDomains, strings.Join(parts[1:], ".")) // Try without subdomain
		}
	}

	// Try all modified domains
	for _, modDomain := range modifiedDomains {
		if extractor, exists := r.domainExtractors[modDomain]; exists {
			fmt.Printf("✅ Found modified domain match: %s => %T\n", modDomain, extractor)
			return extractor
		}
	}

	// Log why we're falling back to default
	if r.defaultExtractor != nil {
		fmt.Printf("⚠️  No specific extractor found for domain '%s', using default extractor\n", domain)
		return r.defaultExtractor
	}
	
	fmt.Printf("⚠️  No extractor found for domain '%s' and no default extractor set\n", domain)
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
