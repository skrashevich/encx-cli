package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

func extractToolError(result string) string {
	var payload map[string]any
	if !decodeJSON(result, &payload) {
		return ""
	}
	errMsg, _ := payload["error"].(string)
	return strings.TrimSpace(errMsg)
}

func cloneAnyMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}

// bareUnicodeRe matches bare uXXXX sequences (without leading \) that some
// LLM API proxies produce instead of proper \uXXXX JSON escapes.
var bareUnicodeRe = regexp.MustCompile(`u([0-9a-fA-F]{4})`)

func decodeBareUnicodeEscapes(s string) string {
	if s == "" {
		return s
	}
	return bareUnicodeRe.ReplaceAllStringFunc(s, func(m string) string {
		r, err := strconv.ParseInt(m[1:], 16, 32)
		if err != nil {
			return m
		}
		return string(rune(r))
	})
}

func getAnyString(v any) string {
	s, _ := v.(string)
	return decodeBareUnicodeEscapes(s)
}

// moderatedArg reads a flag a model may have written as a JSON boolean or as
// its spelling in a string. It reports whether the value was one at all, so an
// absent or unreadable flag is left to the game's current setting.
func moderatedArg(v any) (value, ok bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		return parseBoolArg(val)
	}
	return false, false
}

func getAnyInt(v any) int {
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case string:
		n, _ := strconv.Atoi(val)
		return n
	default:
		return 0
	}
}

func looksLikeRussian(s string) bool {
	for _, r := range s {
		if r >= 'А' && r <= 'я' || r == 'ё' || r == 'Ё' {
			return true
		}
	}
	return false
}

func (s *llmSession) reviewText(english, russian string) string {
	if s != nil && s.preferRussian {
		return russian
	}
	return english
}

func summarizeDebugArgs(argsJSON string) string {
	if argsJSON == "" {
		return "{}"
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return summarizeDebugText(argsJSON, 0)
	}
	if _, ok := args["password"]; ok {
		args["password"] = "<redacted>"
	}
	data, err := json.Marshal(args)
	if err != nil {
		return summarizeDebugText(argsJSON, 0)
	}
	return summarizeDebugText(string(data), 0)
}
