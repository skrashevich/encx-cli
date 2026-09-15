package main

import (
	"encoding/json/v2"
	"github.com/sipeed/picoclaw/pkg/providers"
	"os"
	"strings"
	"testing"
)

func TestAgentBoundsRequestWithoutLosingStoredHistory(t *testing.T) {
	p := &scriptedPicoProvider{responses: []*providers.LLMResponse{{Content: "game 32055", FinishReason: "stop"}}}
	large := strings.Repeat("сценарий HTML ", 30000)
	history := []llmMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "import scenario"},
		{Role: "assistant", ToolCalls: []llmToolCall{{ID: "read1", Type: "function", Function: llmToolCallFunction{Name: "read_local_file", Arguments: `{"path":"scenario.html"}`}}}},
		{Role: "tool", ToolCallID: "read1", Content: large},
		{Role: "assistant", ToolCalls: []llmToolCall{{ID: "create1", Type: "function", Function: llmToolCallFunction{Name: "admin_create_game", Arguments: `{"title":"game"}`}}}},
		{Role: "tool", ToolCallID: "create1", Content: `{"game_id":32055,"success":true}`}, {Role: "user", Content: "дай ссылку"}}
	in := &AgentRunInput{Cfg: &config{}, Session: &llmSession{}, Messages: history}
	_, err := runAgentLoop(t.Context(), AgentConfig{Model: "scripted", Provider: p}, in, AgentCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p.calls[0])
	if len(b) > 100*1024 {
		t.Fatalf("request still oversized: %d", len(b))
	}
	if !strings.Contains(string(b), "32055") || !strings.Contains(string(b), "дай ссылку") {
		t.Fatal("lost created game or latest request")
	}
	if !strings.Contains(string(b), "omitted") {
		t.Fatal("omitted source was not identified")
	}
	if in.Messages[3].Content != large {
		t.Fatal("stored source was destroyed")
	}
}

func TestAgentRejectsEmptyAndIncompleteResponses(t *testing.T) {
	for _, reason := range []string{"stop", "length", "error", "canceled"} {
		t.Run(reason, func(t *testing.T) {
			p := &scriptedPicoProvider{responses: []*providers.LLMResponse{{FinishReason: reason}}}
			in := &AgentRunInput{Cfg: &config{}, Session: &llmSession{}, Messages: []llmMessage{{Role: "user", Content: "?"}}}
			var reported bool
			_, err := runAgentLoop(t.Context(), AgentConfig{Model: "scripted", Provider: p}, in, AgentCallbacks{OnEvent: func(ev AgentEvent) { reported = reported || ev.Type == agentEventError }})
			if err == nil || !reported {
				t.Fatalf("silent completion: err=%v reported=%v", err, reported)
			}
		})
	}
}

func TestSavedDialogRequestOffline(t *testing.T) {
	path := os.Getenv("ENCLI_CONTEXT_TEST_CHAT")
	if path == "" {
		t.Skip("set ENCLI_CONTEXT_TEST_CHAT to check a saved transcript offline")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved persistedChat
	if err = json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	session := &llmSession{}
	in := &AgentRunInput{Cfg: &config{}, Session: session, Tools: getToolsForSession(session)}
	registry, err := newPicoRegistry(in, AgentCallbacks{}, &agentRunStats{})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(picoMessages(saved.Messages))
	defs, _ := json.Marshal(registry.ToProviderDefs())
	t.Log("RAW", len(raw), "TOOLS", len(defs))
	messages, err := boundedAgentMessages(picoMessages(saved.Messages), registry.ToProviderDefs())
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(messages)
	t.Log("BOUNDED_MESSAGE_BYTES", len(b))
}
