package mockllm

import (
	"net/http"
	"strings"
)

// headersMatch checks if all expected header matches are satisfied by the actual HTTP headers.
// Returns true if expected is nil or empty (backward compatible).
// All entries must match (AND semantics).
func headersMatch(expected []HeaderMatch, actual http.Header) bool {
	if len(expected) == 0 {
		return true
	}
	for _, h := range expected {
		actualValue := actual.Get(h.Name)
		matchType := h.MatchType
		if matchType == "" {
			matchType = MatchTypeExact
		}
		switch matchType {
		case MatchTypeExact:
			if actualValue != h.Value {
				return false
			}
		case MatchTypeContains:
			if !strings.Contains(actualValue, h.Value) {
				return false
			}
		default:
			return false
		}
	}
	return true
}
