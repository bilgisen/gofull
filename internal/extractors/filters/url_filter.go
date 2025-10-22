// internal/filters/url_filter.go
package filters

import (
	"strings"
)

// URLFilter defines filtering rules for a specific domain
type URLFilter struct {
	Domain       string
	AllowedPaths []string // If empty, allow all paths
	BlockedPaths []string // Takes priority over AllowedPaths
}

// FilterRegistry manages URL filtering rules
type FilterRegistry struct {
	filters []URLFilter
}

// NewFilterRegistry creates a new filter registry
func NewFilterRegistry() *FilterRegistry {
	return &FilterRegistry{
		filters: make([]URLFilter, 0),
	}
}

// Register adds a new URL filter
func (r *FilterRegistry) Register(filter URLFilter) {
	r.filters = append(r.filters, filter)
}

// ShouldProcess checks if a URL should be processed based on registered filters
func (r *FilterRegistry) ShouldProcess(urlStr string) bool {
	// Find matching filter for this URL's domain
	var matchedFilter *URLFilter
	for i := range r.filters {
		if strings.Contains(urlStr, r.filters[i].Domain) {
			matchedFilter = &r.filters[i]
			break
		}
	}

	// If no filter matches, allow processing
	if matchedFilter == nil {
		return true
	}

	// Check blocked paths first (highest priority)
	for _, blocked := range matchedFilter.BlockedPaths {
		if strings.Contains(urlStr, blocked) {
			return false
		}
	}

	// If no allowed paths specified, allow all (except blocked)
	if len(matchedFilter.AllowedPaths) == 0 {
		return true
	}

	// Check if URL matches any allowed path
	for _, allowed := range matchedFilter.AllowedPaths {
		if strings.Contains(urlStr, allowed) {
			return true
		}
	}

	// Doesn't match any allowed path
	return false
}