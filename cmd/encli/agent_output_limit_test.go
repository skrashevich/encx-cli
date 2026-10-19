package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/tools"
	toolshared "github.com/sipeed/picoclaw/pkg/tools/shared"
)

type outputLimitTestTool struct {
	name  string
	calls int
}

func (t *outputLimitTestTool) Name() string      { return t.name }
func (*outputLimitTestTool) Description() string { return "Test game mutation" }
func (*outputLimitTestTool) Parameters() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{"game_id": map[string]any{"type": "integer"}}}
}
func (t *outputLimitTestTool) Execute(context.Context, map[string]any) *toolshared.ToolResult {
	t.calls++
	return toolshared.SilentResult(`{"success":true,"game_id":32055,"level_number":1}`)
}

func TestOutputLimitContinuesGameWithoutRepeatingMutations(t *testing.T) {
	for _, reason := range []string{"length", "max_tokens", "max_output_tokens"} {
		t.Run(reason, func(t *testing.T) {
			call := func(id, name string) providers.ToolCall {
				return providers.ToolCall{ID: id, Name: name, Arguments: map[string]any{"game_id": 32055}}
			}
			delegate := &scriptedPicoProvider{responses: []*providers.LLMResponse{
				{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{call("created", "admin_create_game")}},
				{FinishReason: reason, Content: "unfinished draft", ToolCalls: []providers.ToolCall{call("discarded", "admin_create_game")}, Usage: &providers.UsageInfo{CompletionTokens: 99}},
				{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{call("level", "admin_create_levels")}},
				{FinishReason: reason}, // A later step gets its own recovery budget.
				{FinishReason: "stop", Content: "Игра 32055 создана"},
			}}
			p := &observedPicoProvider{delegate: delegate, session: &llmSession{}, stats: &agentRunStats{}}
			game := &outputLimitTestTool{name: "admin_create_game"}
			level := &outputLimitTestTool{name: "admin_create_levels"}
			registry := tools.NewToolRegistry()
			registry.Register(game)
			registry.Register(level)
			result, err := tools.RunToolLoop(t.Context(), tools.ToolLoopConfig{Provider: p, Model: "scripted", Tools: registry, MaxIterations: 10}, []providers.Message{{Role: "user", Content: "Создай игру"}}, "test", "test")
			if err != nil {
				t.Fatal(err)
			}
			if result.Content != "Игра 32055 создана" || game.calls != 1 || level.calls != 1 {
				t.Fatalf("result=%+v creates=%d levels=%d", result, game.calls, level.calls)
			}
			for _, messages := range delegate.calls {
				assertToolCallsPaired(t, messages)
			}
			if len(delegate.calls) != 5 {
				t.Fatalf("calls=%d", len(delegate.calls))
			}
			recovery := delegate.calls[2]
			var savedResult, guidance bool
			for _, m := range recovery {
				savedResult = savedResult || (m.Role == "tool" && m.ToolCallID == "created" && strings.Contains(m.Content, "32055"))
				guidance = guidance || (m.Role == "system" && strings.Contains(m.Content, "output token limit"))
				for _, c := range m.ToolCalls {
					if c.ID == "discarded" {
						t.Fatal("truncated tool call leaked into continuation")
					}
				}
			}
			if !savedResult || !guidance {
				t.Fatalf("missing prior result=%v or recovery guidance=%v", savedResult, guidance)
			}
			_, _, turns, _, _, completion := p.stats.snapshot()
			if turns != 5 || completion != 99 {
				t.Fatalf("truncated responses not accounted: turns=%d completion=%d", turns, completion)
			}
			for _, m := range p.seen {
				if m.Role == "system" && strings.Contains(m.Content, "output token limit") {
					t.Fatal("recovery prompt polluted stored history")
				}
			}
		})
	}
}

type alwaysLimitedProvider struct {
	calls  int
	cancel context.CancelFunc
}

func (*alwaysLimitedProvider) GetDefaultModel() string { return "test" }
func (p *alwaysLimitedProvider) Chat(context.Context, []providers.Message, []providers.ToolDefinition, string, map[string]any) (*providers.LLMResponse, error) {
	p.calls++
	if p.cancel != nil {
		p.cancel()
	}
	return &providers.LLMResponse{FinishReason: "length"}, nil
}

func TestOutputLimitRecoveryIsBounded(t *testing.T) {
	d := &alwaysLimitedProvider{}
	p := &observedPicoProvider{delegate: d, session: &llmSession{}, stats: &agentRunStats{}}
	_, err := p.Chat(t.Context(), []providers.Message{{Role: "user", Content: "создай игру"}}, nil, "test", nil)
	if err == nil || d.calls != 4 {
		t.Fatalf("calls=%d err=%v", d.calls, err)
	}
}

func TestOutputLimitRecoveryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	d := &alwaysLimitedProvider{cancel: cancel}
	p := &observedPicoProvider{delegate: d, session: &llmSession{}, stats: &agentRunStats{}}
	_, err := p.Chat(ctx, []providers.Message{{Role: "user", Content: "создай игру"}}, nil, "test", nil)
	if err == nil || d.calls != 1 {
		t.Fatalf("calls=%d err=%v", d.calls, err)
	}
}

func TestOutputLimitPreservesPersistedGameHistory(t *testing.T) {
	delegate := &scriptedPicoProvider{responses: []*providers.LLMResponse{
		{FinishReason: "length", Content: "unfinished draft", ToolCalls: []providers.ToolCall{{ID: "unexecuted", Name: "admin_create_game", Arguments: map[string]any{}}}},
		{FinishReason: "stop", Content: "Игра 32055 готова"},
	}}
	input := &AgentRunInput{Cfg: &config{}, Session: &llmSession{}, Messages: []llmMessage{
		{Role: "user", Content: "Создай игру"},
		{Role: "assistant", ToolCalls: []llmToolCall{{ID: "created", Type: "function", Function: llmToolCallFunction{Name: "admin_create_game", Arguments: `{"title":"IT"}`}}}},
		{Role: "tool", ToolCallID: "created", Content: `{"game_id":32055,"success":true}`},
	}}
	var failures int
	_, err := runAgentLoop(t.Context(), AgentConfig{Model: "test", Provider: delegate}, input, AgentCallbacks{OnEvent: func(e AgentEvent) {
		if e.Type == agentEventError {
			failures++
		}
	}})
	if err != nil || failures != 0 {
		t.Fatalf("err=%v failures=%d", err, failures)
	}
	if len(input.Messages) != 4 || input.Messages[2].ToolCallID != "created" || input.Messages[3].Content != "Игра 32055 готова" {
		t.Fatalf("unexpected history: %+v", input.Messages)
	}
	for _, m := range input.Messages {
		if strings.Contains(m.Content, "unfinished draft") || strings.Contains(m.Content, "output token limit") {
			t.Fatal("discarded response or temporary recovery prompt persisted")
		}
	}
}
