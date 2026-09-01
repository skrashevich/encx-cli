package surface

import (
	"strings"
	"unicode"
)

const (
	// cPrefix namespaces every generated C symbol.
	cPrefix = "encx_"
	// cMethodPrefix namespaces methods bound to the opaque client handle.
	cMethodPrefix = "encx_client_"
)

// SnakeCase converts a Go identifier to snake_case.
//
// A run of consecutive capitals is one word, so an acronym stays glued
// together: ExportHAR becomes export_har and APIBaseURL becomes api_base_url.
// When such a run is followed by a lowercase letter the last capital starts the
// next word instead, which is what splits HARSnapshot into har_snapshot.
func SnakeCase(name string) string {
	words := splitWords(name)
	for i, w := range words {
		words[i] = strings.ToLower(w)
	}
	return strings.Join(words, "_")
}

func splitWords(name string) []string {
	runes := []rune(name)
	var (
		words []string
		cur   []rune
	)
	for i, r := range runes {
		if unicode.IsUpper(r) && len(cur) > 0 {
			prevLower := !unicode.IsUpper(runes[i-1])
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if prevLower || nextLower {
				words = append(words, string(cur))
				cur = nil
			}
		}
		cur = append(cur, r)
	}
	if len(cur) > 0 {
		words = append(words, string(cur))
	}
	return words
}

// MethodCName is the C symbol for a method on the client handle.
func MethodCName(goName string) string { return cMethodPrefix + SnakeCase(goName) }

// FuncCName is the C symbol for a package-level function or constructor.
func FuncCName(goName string) string { return cPrefix + SnakeCase(goName) }
