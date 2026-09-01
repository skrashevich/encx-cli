package enapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// APIError is a failing response from the new engine.
//
// The backend answers with three shapes depending on the handler: a JSON error
// object, a localizable sentence key for UI strings, or bare text. All of them
// end up here so callers can branch on Status/Code instead of on the body.
type APIError struct {
	Method      string
	Path        string
	Status      int
	Code        int
	Err         string
	Message     string
	SentenceKey string
	FormatArgs  []string
	Body        string
}

func (e *APIError) Error() string {
	detail := e.Message
	if detail == "" {
		detail = e.Err
	}
	if detail == "" {
		detail = e.SentenceKey
	}
	if detail == "" {
		detail = e.Body
	}
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return fmt.Sprintf("enapi: %s %s: HTTP %d", e.Method, e.Path, e.Status)
	}
	return fmt.Sprintf("enapi: %s %s: HTTP %d: %s", e.Method, e.Path, e.Status, detail)
}

// Unauthorized reports whether the call failed because the session is missing
// or expired.
func (e *APIError) Unauthorized() bool {
	return e.Status == http.StatusUnauthorized
}

// Forbidden reports whether the session lacks the required site permission.
func (e *APIError) Forbidden() bool {
	return e.Status == http.StatusForbidden
}

// NotFound reports whether the addressed object does not exist.
func (e *APIError) NotFound() bool {
	return e.Status == http.StatusNotFound
}

// MissingHostError reports that no API host is known for a domain, so the
// request was not sent. It carries the remedy because the caller cannot guess
// it: the host is only derivable for Encounter's own zones.
type MissingHostError struct {
	Domain string
}

func (e *MissingHostError) Error() string {
	return "enapi: no API host is configured for " + e.Domain +
		": the domain is outside the Encounter zones, so the host cannot be derived — " +
		"set it explicitly (encx.WithAPIBaseURL, encli -api-base-url or ENCX_API_BASE_URL)"
}

// IsMissingHost reports whether err is a MissingHostError.
func IsMissingHost(err error) bool {
	var missing *MissingHostError
	return errors.As(err, &missing)
}

// AsAPIError extracts an *APIError from an error chain.
func AsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}

// IsStatus reports whether err is an APIError with the given HTTP status.
func IsStatus(err error, status int) bool {
	apiErr, ok := AsAPIError(err)
	return ok && apiErr.Status == status
}

// IsUnauthorized reports whether err is a 401 from the new engine.
func IsUnauthorized(err error) bool { return IsStatus(err, http.StatusUnauthorized) }

// IsNotFound reports whether err is a 404 from the new engine.
func IsNotFound(err error) bool { return IsStatus(err, http.StatusNotFound) }

// IsForbidden reports whether err is a 403 from the new engine.
func IsForbidden(err error) bool { return IsStatus(err, http.StatusForbidden) }

func parseAPIError(method, path string, status int, body []byte) *APIError {
	apiErr := &APIError{Method: method, Path: path, Status: status, Body: truncateBody(body)}

	var payload struct {
		Error       json.RawMessage `json:"error"`
		Message     string          `json:"message"`
		Code        int             `json:"code"`
		SentenceKey string          `json:"sentence_key"`
		FormatArgs  []string        `json:"format_args"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		// Plain-text errors ("Authorization required") stay in Body.
		return apiErr
	}
	apiErr.Code = payload.Code
	apiErr.Message = payload.Message
	apiErr.SentenceKey = payload.SentenceKey
	apiErr.FormatArgs = payload.FormatArgs
	apiErr.Err = decodeErrorField(payload.Error)
	return apiErr
}

// decodeErrorField reads the "error" member, which is a string in most
// handlers but an object in a few.
func decodeErrorField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text
	}
	return strings.TrimSpace(string(raw))
}

func truncateBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > maxErrorBodyBytes {
		return text[:maxErrorBodyBytes] + "…"
	}
	return text
}
