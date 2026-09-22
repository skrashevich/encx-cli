package main

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
)

type polzaRoutingTransport func(*http.Request) (*http.Response, error)

func (f polzaRoutingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPicoProviderPolzaExcludesRelace(t *testing.T) {
	// This test replaces the transport to inspect the real serialized provider request.
	// Keep it non-parallel and never send the test credentials to an external service.
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, endpoint := range []string{polzaBaseURL, polzaBaseURL + "/", defaultLLMBaseURL, "https://example.com/v1"} {
		t.Run(endpoint, func(t *testing.T) {
			called := false
			http.DefaultTransport = polzaRoutingTransport(func(r *http.Request) (*http.Response, error) {
				called = true
				body, err := io.ReadAll(r.Body)
				if err != nil {
					return nil, err
				}
				var payload map[string]jsontext.Value
				if err := json.Unmarshal(body, &payload); err != nil {
					return nil, err
				}
				if endpoint == polzaBaseURL || endpoint == polzaBaseURL+"/" {
					var routing struct {
						Ignore []string `json:"ignore"`
						Sort   string   `json:"sort"`
					}
					if err := json.Unmarshal(payload["provider"], &routing); err != nil {
						t.Errorf("provider routing missing: %s", body)
					}
					if len(routing.Ignore) != 2 || routing.Ignore[0] != "Relace" || routing.Ignore[1] != "relace/fp4" {
						t.Errorf("ignore = %v, want [Relace relace/fp4]", routing.Ignore)
					}
					if routing.Sort != "throughput" {
						t.Errorf("sort = %q, want throughput", routing.Sort)
					}
				} else if _, exists := payload["provider"]; exists {
					t.Errorf("Polza routing leaked to %s", endpoint)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)), Request: r}, nil
			})
			p, err := newPicoProvider(AgentConfig{BaseURL: endpoint, APIKey: "test", Model: "test/model"})
			if err != nil {
				t.Fatal(err)
			}
			response, err := p.Chat(t.Context(), []providers.Message{{Role: "user", Content: "hello"}}, nil, "test/model", nil)
			if err != nil || response == nil || response.Content != "ok" {
				t.Fatalf("response=%v err=%v", response, err)
			}
			if !called {
				t.Fatal("no HTTP request")
			}
		})
	}
}
