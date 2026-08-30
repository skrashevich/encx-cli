package encx

import (
	"errors"
	"fmt"
	"testing"
)

// A 2xx body we cannot parse means the engine took the request; a non-2xx one may never have
// arrived. Confusing the two silently destroys a player's answer, so pin both directions.
func TestIsUndecodableAcceptedUsesHTTPStatus(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status int
		want   bool
	}{
		{"200 accepted", 200, true},
		{"204 accepted", 204, true},
		{"299 accepted", 299, true},
		{"302 not accepted", 302, false},
		{"400 not accepted", 400, false},
		{"502 not accepted", 502, false},
		{"transport failure not accepted", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Non-HTML garbage: HTML is claimed by the session-expired branch first, on every
			// status, because Encounter serves its login page with HTTP 200.
			_, err := decodeGameModelJSON([]byte(`{"Level": "not an object"}`), tc.status, "game model")
			if err == nil {
				t.Fatal("expected a decode error")
			}
			if got := IsUndecodableAccepted(err); got != tc.want {
				t.Fatalf("IsUndecodableAccepted(HTTP %d) = %v, want %v (err=%v)", tc.status, got, tc.want, err)
			}
		})
	}
}

// A leading BOM or whitespace used to defeat the body[0] == '<' HTML check, turning a proxy error
// page into a "decode failure" that looked like a delivered answer.
func TestHTMLDetectionSurvivesLeadingBytes(t *testing.T) {
	t.Parallel()

	for _, prefix := range []string{"", "\ufeff", "\r\n", "  \t", "\n\ufeff "} {
		body := []byte(prefix + "<html><body>502 Bad Gateway</body></html>")
		_, err := decodeGameModelJSON(body, 502, "game model")
		if err == nil {
			t.Fatalf("prefix %q: expected an error", prefix)
		}
		var ue *UndecodableResponseError
		if errors.As(err, &ue) {
			t.Fatalf("prefix %q: HTML body must not surface as UndecodableResponseError, got %v", prefix, err)
		}
		if IsUndecodableAccepted(err) {
			t.Fatalf("prefix %q: HTML body must never count as accepted", prefix)
		}
	}
}

// Non-HTML garbage on a 2xx (e.g. a JSON error envelope) is the case that legitimately means
// "the engine answered, we just could not read it".
func TestJSONEnvelopeOn2xxIsAccepted(t *testing.T) {
	t.Parallel()

	_, err := decodeGameModelJSON([]byte(`{"Level": "not an object"}`), 200, "game model")
	if err == nil {
		t.Fatal("expected a decode error")
	}
	if !IsUndecodableAccepted(err) {
		t.Fatalf("2xx unparseable JSON should be accepted, got %v", err)
	}
	var ue *UndecodableResponseError
	if !errors.As(err, &ue) || ue.StatusCode != 200 {
		t.Fatalf("expected UndecodableResponseError with status 200, got %#v", err)
	}
	if ue.Unwrap() == nil {
		t.Fatal("expected the underlying json error to be wrapped")
	}
	if !errors.Is(fmt.Errorf("wrapped: %w", err), err) {
		t.Fatal("expected the error to survive wrapping")
	}
}

// Other error shapes on the send path must never be mistaken for a delivered answer.
func TestUnrelatedErrorsAreNotAccepted(t *testing.T) {
	t.Parallel()

	_, emptyErr := decodeGameModelJSON(nil, 200, "game model")
	for _, err := range []error{
		emptyErr,
		ErrAntiSpam,
		&AntiSpamError{URL: "https://t.en.cx/NotHumanRequest.aspx"},
		fmt.Errorf("encx: game request: context deadline exceeded"),
		errors.New("connection refused"),
	} {
		if IsUndecodableAccepted(err) {
			t.Fatalf("%v must not count as accepted", err)
		}
	}
}
