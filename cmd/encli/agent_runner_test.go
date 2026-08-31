package main

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
)

type scriptedPicoProvider struct {
	mu        sync.Mutex
	responses []*providers.LLMResponse
	calls     [][]providers.Message
	toolDefs  [][]providers.ToolDefinition
}

func (p *scriptedPicoProvider) GetDefaultModel() string { return "scripted" }

func (p *scriptedPicoProvider) Chat(
	_ context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, append([]providers.Message(nil), messages...))
	p.toolDefs = append(p.toolDefs, append([]providers.ToolDefinition(nil), tools...))
	response := p.responses[0]
	p.responses = p.responses[1:]
	return response, nil
}

func TestRunAgentLoopUsesPicoClawProvider(t *testing.T) {
	t.Parallel()
	provider := &scriptedPicoProvider{responses: []*providers.LLMResponse{{
		Content:      "готово",
		FinishReason: "stop",
		Usage:        &providers.UsageInfo{PromptTokens: 7, CompletionTokens: 3},
	}}}
	input := &AgentRunInput{
		Cfg:      &config{},
		Session:  &llmSession{preferRussian: true},
		Messages: []llmMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "сделай"}},
	}
	var events []AgentEvent
	_, err := runAgentLoop(context.Background(), AgentConfig{
		BaseURL:  "http://127.0.0.1:1/v1",
		Model:    "scripted",
		Provider: provider,
	}, input, AgentCallbacks{OnEvent: func(event AgentEvent) { events = append(events, event) }})
	if err != nil {
		t.Fatalf("runAgentLoop: %v", err)
	}
	if len(provider.calls) != 1 || len(provider.calls[0]) != 2 {
		t.Fatalf("provider calls = %#v", provider.calls)
	}
	if got := input.Messages[len(input.Messages)-1]; got.Role != "assistant" || got.Content != "готово" {
		t.Fatalf("assistant message = %#v", got)
	}
	var sawAnswer, sawReport, sawDone bool
	for _, event := range events {
		switch event.Type {
		case agentEventAssistantText:
			sawAnswer = event.Text == "готово"
		case agentEventReport:
			sawReport = strings.Contains(event.Report, "Токены:           10")
		case agentEventDone:
			sawDone = true
		}
	}
	if !sawAnswer || !sawReport || !sawDone {
		t.Fatalf("events missing answer=%v report=%v done=%v: %#v", sawAnswer, sawReport, sawDone, events)
	}
}

func TestRunAgentLoopExecutesLegacyToolThroughPicoClaw(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LLM_FILES_ROOT", root)
	provider := &scriptedPicoProvider{responses: []*providers.LLMResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []providers.ToolCall{{
				ID:        "call-1",
				Name:      "list_local_dir",
				Arguments: map[string]any{"path": "."},
			}},
		},
		{Content: "каталог прочитан", FinishReason: "stop"},
	}}
	definition := llmTool{Type: "function", Function: llmFunction{
		Name:        "list_local_dir",
		Description: "List a local directory",
		Parameters:  []byte(`{"type":"object","properties":{"path":{"type":"string"}}}`),
	}}
	input := &AgentRunInput{
		Cfg:      &config{},
		Session:  &llmSession{},
		Messages: []llmMessage{{Role: "user", Content: "list"}},
		Tools:    []llmTool{definition},
	}
	var events []AgentEvent
	_, err := runAgentLoop(context.Background(), AgentConfig{
		BaseURL:  "http://127.0.0.1:1/v1",
		Model:    "scripted",
		Provider: provider,
	}, input, AgentCallbacks{OnEvent: func(event AgentEvent) { events = append(events, event) }})
	if err != nil {
		t.Fatalf("runAgentLoop: %v", err)
	}
	if len(provider.calls) != 2 {
		t.Fatalf("provider call count = %d, want 2", len(provider.calls))
	}
	secondCall := provider.calls[1]
	if len(secondCall) < 3 || secondCall[len(secondCall)-1].Role != "tool" ||
		!strings.Contains(secondCall[len(secondCall)-1].Content, `"count": 0`) {
		t.Fatalf("second provider messages = %#v", secondCall)
	}
	var sawStart, sawFinish bool
	for _, event := range events {
		if event.Type == agentEventToolStart && event.ToolName == "list_local_dir" {
			sawStart = true
		}
		if event.Type == agentEventToolDone && event.ToolName == "list_local_dir" {
			sawFinish = true
		}
	}
	if !sawStart || !sawFinish {
		t.Fatalf("tool events missing start=%v finish=%v: %#v", sawStart, sawFinish, events)
	}
}

func TestRunAgentLoopKeepsApprovalGateWithPicoClaw(t *testing.T) {
	provider := &scriptedPicoProvider{responses: []*providers.LLMResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []providers.ToolCall{{
				ID:        "call-approval",
				Name:      "send_code",
				Arguments: map[string]any{"game_id": 42, "code": "alpha"},
			}},
		},
		{Content: "действие отменено", FinishReason: "stop"},
	}}
	definition := llmTool{Type: "function", Function: llmFunction{
		Name:        "send_code",
		Description: "Submit a code",
		Parameters: []byte(`{"type":"object","properties":{"game_id":{"type":"integer"},"code":{"type":"string"}},` +
			`"required":["game_id","code"]}`),
	}}
	input := &AgentRunInput{
		Cfg:      &config{},
		Session:  &llmSession{securityMode: SecurityModeApprove},
		Messages: []llmMessage{{Role: "user", Content: "отправь код"}},
		Tools:    []llmTool{definition},
	}
	approvalCalls := 0
	_, err := runAgentLoop(context.Background(), AgentConfig{
		BaseURL:  "http://127.0.0.1:1/v1",
		Model:    "scripted",
		Provider: provider,
	}, input, AgentCallbacks{ApproveToolCall: func(_ context.Context, name, args string) (bool, error) {
		approvalCalls++
		if name != "send_code" || !strings.Contains(args, `"code":"alpha"`) {
			t.Fatalf("approval request = %s(%s)", name, args)
		}
		return false, nil
	}})
	if err != nil {
		t.Fatalf("runAgentLoop: %v", err)
	}
	if approvalCalls != 1 {
		t.Fatalf("approval calls = %d, want 1", approvalCalls)
	}
	toolResult := provider.calls[1][len(provider.calls[1])-1]
	if toolResult.Role != "tool" || !strings.Contains(toolResult.Content, "user denied") {
		t.Fatalf("declined tool result = %#v", toolResult)
	}
}
