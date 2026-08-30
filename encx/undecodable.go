package encx

import (
	"errors"
	"fmt"
)

// UndecodableResponseError means the server answered but the body could not be parsed into the
// expected model.
//
// StatusCode is what makes this error actionable. A 2xx means the request reached the engine and
// only the reply was unreadable — for an answer submission that means the answer was recorded, so
// resending it would duplicate it. A non-2xx means an intermediary (proxy, gateway) answered
// instead, so the request may never have reached the engine and must stay retryable.
type UndecodableResponseError struct {
	StatusCode int
	Context    string
	Err        error
}

func (e *UndecodableResponseError) Error() string {
	return fmt.Sprintf("encx: decode %s (HTTP %d): %v", e.Context, e.StatusCode, e.Err)
}

func (e *UndecodableResponseError) Unwrap() error { return e.Err }

// IsUndecodableAccepted reports whether err is an undecodable body on a 2xx response, i.e. the
// request definitely reached the engine and only the reply could not be read.
//
// Callers use this to decide that a submitted answer must NOT be resent. It deliberately returns
// false for non-2xx: mistaking a proxy error page for "delivered" would silently destroy a
// player's answer.
func IsUndecodableAccepted(err error) bool {
	var ue *UndecodableResponseError
	if !errors.As(err, &ue) {
		return false
	}
	return ue.StatusCode >= 200 && ue.StatusCode < 300
}
