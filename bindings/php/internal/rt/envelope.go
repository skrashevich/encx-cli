package rt

import (
	"encoding/json"
	"fmt"
)

// envelope is the wire format every generated wrapper returns to PHP as a
// JSON string: {"ok":true,"value":...}, {"ok":true} (value omitted, not
// null), or {"ok":false,"error":"..."}.
type envelope struct {
	Ok    bool            `json:"ok"`
	Value json.RawMessage `json:"value,omitempty"`
	Error string          `json:"error,omitempty"`
}

// fallbackErrorJSON is returned when marshaling the envelope itself somehow
// fails, so callers never receive a broken JSON string or an empty one.
const fallbackErrorJSON = `{"ok":false,"error":"rt: failed to marshal response"}`

// OK serializes a successful envelope carrying v as the "value" field. If v
// cannot be marshaled to JSON (e.g. it contains a NaN float), OK returns a
// well-formed error envelope instead of broken JSON.
func OK(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return Failf("rt: failed to marshal value: %s", err.Error())
	}
	out, err := json.Marshal(envelope{Ok: true, Value: raw})
	if err != nil {
		return fallbackErrorJSON
	}
	return string(out)
}

// OKVoid returns a successful envelope with no "value" field at all (as
// opposed to a "value" of null).
func OKVoid() string {
	out, err := json.Marshal(envelope{Ok: true})
	if err != nil {
		return fallbackErrorJSON
	}
	return string(out)
}

// Fail serializes an error envelope from err. A nil err still produces a
// valid envelope, using a stable placeholder message.
func Fail(err error) string {
	if err == nil {
		return Failf("rt: unknown error")
	}
	return Failf("%s", err.Error())
}

// Failf serializes an error envelope from a formatted message. The message
// is JSON-string-escaped normally, so quotes, newlines, and non-ASCII text
// (e.g. Cyrillic) round-trip intact.
func Failf(format string, a ...any) string {
	out, err := json.Marshal(envelope{Ok: false, Error: fmt.Sprintf(format, a...)})
	if err != nil {
		return fallbackErrorJSON
	}
	return string(out)
}
