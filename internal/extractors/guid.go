package extractors

import (
	"crypto/sha256"
	"encoding/hex"
)

// GenerateGUIDFromURL creates a deterministic GUID from a URL using SHA-256 hashing.
// It returns a 32-character hexadecimal string.
func GenerateGUIDFromURL(url string) string {
	hash := sha256.Sum256([]byte(url))
	return hex.EncodeToString(hash[:])
}
