package main

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// Exercise the actual PicoClaw/OpenAI SDK stack: keepalive comments and blank
// SSE frames must not become JSON responses or cause a model request retry.
func TestCodexStreamKeepalive(t *testing.T) {
	for _, contentType := range []string{"", "text/event-stream", "text/event-stream; charset=utf-8"} {
		t.Run("content-type="+contentType, func(t *testing.T) {
			for _, keepalive := range []string{"\n", ": keepalive\n\n", "event: ping\n\n"} {
				t.Run(strings.TrimSpace(keepalive), func(t *testing.T) {
					original := http.DefaultTransport
					t.Cleanup(func() { http.DefaultTransport = original })
					calls := 0
					http.DefaultTransport = polzaRoutingTransport(func(r *http.Request) (*http.Response, error) {
						calls++
						body := keepalive + "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-test\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}]}}\n\n" + keepalive
						return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
					})
					p := providers.NewCodexProviderWithTokenSource("test-token", "test-account", func() (string, string, error) { return "test-token", "test-account", nil })
					response, err := p.Chat(t.Context(), []providers.Message{{Role: "user", Content: "hello"}}, nil, "gpt-5.3-codex", nil)
					if err != nil || response == nil || response.Content != "ok" {
						t.Fatalf("response=%+v error=%v", response, err)
					}
					if calls != 1 {
						t.Fatalf("made %d requests, want 1", calls)
					}
				})
			}
		})
	}
}

// The former API model gpt-5.5 is rejected by ChatGPT accounts. This default
// was verified against the subscription catalog; explicit choices stay intact.
func TestCodexDefaultUsesSubscriptionModel(t *testing.T) {
	if got := codexModel(""); got != "gpt-5.6-sol" {
		t.Fatalf("subscription default = %q", got)
	}
	if got := codexModel("gpt-custom-choice"); got != "gpt-custom-choice" {
		t.Fatalf("explicit model overwritten: %q", got)
	}
}

func TestCodexStreamStillRejectsBrokenResponses(t *testing.T) {
	for _, body := range []string{
		": keepalive\n\n",
		"data: {\"type\":\"response.completed\"\n\n",
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n",
	} {
		t.Run(body, func(t *testing.T) {
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			calls := 0
			http.DefaultTransport = polzaRoutingTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			p := providers.NewCodexProvider("test-token", "test-account")
			response, err := p.Chat(t.Context(), nil, nil, defaultCodexModel, nil)
			if err == nil || response != nil {
				t.Fatalf("accepted incomplete stream: %+v, %v", response, err)
			}
			if calls != 1 {
				t.Fatalf("made %d requests, want 1", calls)
			}
		})
	}
}

func TestCodexStreamKeepalivePreservesToolCall(t *testing.T) {
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = polzaRoutingTransport(func(r *http.Request) (*http.Response, error) {
		body := ": keepalive\n\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-tool\",\"status\":\"completed\",\"output\":[{\"type\":\"function_call\",\"call_id\":\"call-probe\",\"name\":\"probe\",\"arguments\":\"{\\\"value\\\":42}\"}]}}\n\n: keepalive\n\n"
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream;charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	p := providers.NewCodexProvider("test-token", "test-account")
	response, err := p.Chat(t.Context(), nil, nil, defaultCodexModel, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.FinishReason != "tool_calls" || len(response.ToolCalls) != 1 {
		t.Fatalf("response=%+v", response)
	}
	call := response.ToolCalls[0]
	if call.ID != "call-probe" || call.Name != "probe" || call.Arguments["value"] != float64(42) {
		t.Fatalf("tool call changed: %+v", call)
	}
}
