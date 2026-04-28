package creds

import "strings"

// RedactField returns "***" if the key indicates a credential field, otherwise the original value.
// Comparison is case-insensitive.
func RedactField(key, value string) string {
	k := strings.ToLower(key)
	if k == "authorization" {
		return "***"
	}
	if strings.Contains(k, "token") {
		return "***"
	}
	return value
}
