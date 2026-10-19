package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/tools"
)

// Opt-in read-only provider smoke test: no Encounter tools, games, or chat
// history are touched. Uses the locally configured subscription credential.
func TestCodexSubscriptionLive(t *testing.T) {
	if os.Getenv("ENCLI_TEST_LIVE_CODEX") != "1" {
		t.Skip("set ENCLI_TEST_LIVE_CODEX=1 to exercise the subscription")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	p, err := newCodexProvider()
	if err != nil {
		t.Fatal(err)
	}
	messages := []providers.Message{{Role: "system", Content: "Call the probe tool exactly once. After receiving its result, reply exactly PROBE_OK."}, {Role: "user", Content: "Run the probe."}}
	tools := []providers.ToolDefinition{{Type: "function", Function: providers.ToolFunctionDefinition{Name: "probe", Description: "Read-only diagnostic probe with no side effects.", Parameters: map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}, "additionalProperties": false}}}}
	response, err := p.Chat(ctx, messages, tools, defaultCodexModel, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ToolCalls) != 1 || response.ToolCalls[0].Name != "probe" {
		t.Fatalf("expected probe call, got %+v", response)
	}
	messages = append(messages, providers.Message{Role: "assistant", ToolCalls: response.ToolCalls}, providers.Message{Role: "tool", ToolCallID: response.ToolCalls[0].ID, Content: "PROBE_OK"})
	response, err = p.Chat(ctx, messages, tools, defaultCodexModel, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.ToolCalls) != 0 || !strings.Contains(response.Content, "PROBE_OK") {
		t.Fatalf("expected final probe response, got %+v", response)
	}
}

// Inject one truncation after the probe has executed, then let the real model
// recover using the saved result. The live account need not waste its full
// output allowance to test the recovery path.
type limitedOnceLiveProvider struct {
	providers.LLMProvider
	injected bool
}

func (p *limitedOnceLiveProvider) Chat(ctx context.Context, messages []providers.Message, defs []providers.ToolDefinition, model string, options map[string]any) (*providers.LLMResponse, error) {
	if !p.injected && len(messages) > 0 && messages[len(messages)-1].Role == "tool" {
		p.injected = true
		return &providers.LLMResponse{FinishReason: "length"}, nil
	}
	return p.LLMProvider.Chat(ctx, messages, defs, model, options)
}

func TestOutputLimitSubscriptionLive(t *testing.T) {
	if os.Getenv("ENCLI_TEST_LIVE_CODEX") != "1" {
		t.Skip("set ENCLI_TEST_LIVE_CODEX=1 to exercise recovery with the subscription")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	delegate, err := newCodexProvider()
	if err != nil {
		t.Fatal(err)
	}
	injected := &limitedOnceLiveProvider{LLMProvider: delegate}
	p := &observedPicoProvider{delegate: injected, session: &llmSession{}, stats: &agentRunStats{}}
	probe := &outputLimitTestTool{name: "probe"}
	registry := tools.NewToolRegistry()
	registry.Register(probe)
	result, err := tools.RunToolLoop(ctx, tools.ToolLoopConfig{Provider: p, Model: defaultCodexModel, Tools: registry, MaxIterations: 5}, []providers.Message{
		{Role: "system", Content: "Call the read-only probe tool exactly once with no arguments. After receiving its result, reply exactly PROBE_OK. This is a diagnostic, do not create games."},
		{Role: "user", Content: "Run the probe."},
	}, "test", "test")
	if err != nil {
		t.Fatal(err)
	}
	if !injected.injected || probe.calls != 1 || !strings.Contains(result.Content, "PROBE_OK") {
		t.Fatalf("injected=%v probe calls=%d result=%+v", injected.injected, probe.calls, result)
	}
}

// Exercise a real response through the headerless keepalive path observed
// from Codex, without executing any Encounter tools.
func TestCodexSubscriptionLiveWithoutContentType(t *testing.T) {
	if os.Getenv("ENCLI_TEST_LIVE_CODEX") != "1" {
		t.Skip("set ENCLI_TEST_LIVE_CODEX=1 to exercise the subscription")
	}
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	intercepted := 0
	http.DefaultTransport = polzaRoutingTransport(func(r *http.Request) (*http.Response, error) {
		res, err := original.RoundTrip(r)
		if err == nil && r.URL.Path == "/backend-api/codex/responses" && res.StatusCode == http.StatusOK {
			intercepted++
			res.Header.Del("Content-Type")
			res.Body = &codexLiveReadCloser{Reader: io.MultiReader(strings.NewReader(": keepalive\n\n\n"), res.Body), Closer: res.Body}
		}
		return res, err
	})
	TestCodexSubscriptionLive(t)
	if intercepted != 2 {
		t.Fatalf("expected 2 headerless responses, got %d", intercepted)
	}
}

type codexLiveReadCloser struct {
	io.Reader
	io.Closer
}
