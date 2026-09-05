package genphp

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/skrashevich/encx-cli/bindings/php/internal/surface"
)

// bareKey matches an array-shape key that needs no quoting.
var bareKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// arrayShape renders the PHPDoc type of a decoded JSON result struct.
//
// The keys and their types are spelled out rather than collapsed into
// array<string, mixed>, so a caller reading $snapshot['JSON'] is checked
// against the Go struct by any static analyser, and so a field rename shows up
// as a diff in the committed PHP file. A field carrying `json:"-"` never
// reaches the JSON and is therefore absent here; `omitempty` makes the key
// optional, which the trailing ? on the key name says.
func arrayShape(fields []surface.Field) string {
	var keys []string
	for _, f := range fields {
		if f.JSONName == "" {
			continue
		}
		key := f.JSONName
		if !bareKey.MatchString(key) {
			key = strconv.Quote(key)
		}
		if f.OmitEmpty {
			key += "?"
		}
		keys = append(keys, key+": "+phpDocType(f.Type))
	}
	if len(keys) == 0 {
		// Every field was dropped by its json tag, so the object is empty
		// and there is no shape worth spelling out.
		return "array<string, mixed>"
	}
	return "array{" + strings.Join(keys, ", ") + "}"
}

// phpDocType maps the Go spelling of a JSON-bindable field type onto the PHP
// value json_decode produces for it, decoding into associative arrays.
//
// The input is one of the forms surface accepts: a basic identifier, a slice
// of an accepted type, or a map of them. Anything else would have been
// rejected before the model reached this emitter, so it degrades to mixed
// rather than failing the build over a type it merely cannot name.
func phpDocType(goType string) string {
	if elem, ok := strings.CutPrefix(goType, "[]"); ok {
		// encoding/json writes a byte slice as a base64 string, not as a
		// list of numbers.
		if elem == "byte" || elem == "uint8" {
			return "string"
		}
		return "list<" + phpDocType(elem) + ">"
	}
	if rest, ok := strings.CutPrefix(goType, "map["); ok {
		if key, value, found := cutMapKey(rest); found {
			return "array<" + phpDocType(key) + ", " + phpDocType(value) + ">"
		}
		return "array<array-key, mixed>"
	}
	switch goType {
	case "string":
		return "string"
	case "int", "int64", "byte", "uint8":
		return "int"
	case "float64":
		return "float"
	case "bool":
		return "bool"
	}
	return "mixed"
}

// cutMapKey splits the text after "map[" into the key type and the value type,
// matching the bracket that opens the key so that a nested map key does not
// end the scan early.
func cutMapKey(s string) (key, value string, ok bool) {
	depth := 0
	for i, r := range s {
		switch r {
		case '[':
			depth++
		case ']':
			if depth == 0 {
				return s[:i], s[i+1:], true
			}
			depth--
		}
	}
	return "", "", false
}
