package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestBuildSystemPromptStampsCurrentTime(t *testing.T) {
	fixed := time.Date(2026, 9, 7, 14, 7, 0, 0, time.FixedZone("MSK", 3*60*60))
	orig := systemPromptNow
	systemPromptNow = func() time.Time { return fixed }
	defer func() { systemPromptNow = orig }()

	prompt := buildSystemPrompt(&config{domain: "tech.en.cx"}, &llmSession{})

	if !strings.Contains(prompt, "2026-09-07T14:07:00+03:00") {
		t.Fatalf("prompt missing local RFC3339 timestamp:\n%s", prompt)
	}
	if !strings.Contains(prompt, "2026-09-07T11:07:00Z") {
		t.Fatalf("prompt missing UTC timestamp:\n%s", prompt)
	}
	if !strings.Contains(prompt, "authoritative") {
		t.Fatalf("prompt missing time-handling rule:\n%s", prompt)
	}
}

// A document read by a tool used to vanish when the turn ended: only the final
// assistant text was written back, so the next turn saw the model's paraphrase
// and nothing else.
func TestRunAgentLoopKeepsToolHistory(t *testing.T) {
	// Not parallel: this drives a real tool execution, and executeToolCallSafe
	// swaps process-global os.Stdout and agentMode. Production serialises that
	// through legacyToolExecutionMu, but TestExecuteToolCallSafe* calls it
	// directly without the lock, so the two must not overlap.
	provider := &scriptedPicoProvider{responses: []*providers.LLMResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []providers.ToolCall{{
				ID:        "call-1",
				Type:      "function",
				Name:      "read_pdf_file",
				Arguments: map[string]any{"path": "scenario.pdf"},
			}},
		},
		{Content: "готово", FinishReason: "stop"},
	}}
	input := &AgentRunInput{
		Cfg:      &config{},
		Session:  &llmSession{},
		Messages: []llmMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "прочитай"}},
		Tools: []llmTool{{Type: "function", Function: llmFunction{
			Name:       "read_pdf_file",
			Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}}},
	}
	if _, err := runAgentLoop(context.Background(), AgentConfig{
		Model: "scripted", Provider: provider,
	}, input, AgentCallbacks{}); err != nil {
		t.Fatalf("runAgentLoop: %v", err)
	}

	roles := make([]string, 0, len(input.Messages))
	for _, m := range input.Messages {
		roles = append(roles, m.Role)
	}
	want := []string{"system", "user", "assistant", "tool", "assistant"}
	if strings.Join(roles, ",") != strings.Join(want, ",") {
		t.Fatalf("roles = %v, want %v", roles, want)
	}

	call := input.Messages[2]
	if len(call.ToolCalls) != 1 || call.ToolCalls[0].Function.Name != "read_pdf_file" {
		t.Fatalf("assistant tool call = %#v", call.ToolCalls)
	}
	if call.ToolCalls[0].ID != "call-1" || call.ToolCalls[0].Type != "function" {
		t.Fatalf("tool call identity = %#v", call.ToolCalls[0])
	}
	if !strings.Contains(call.ToolCalls[0].Function.Arguments, "scenario.pdf") {
		t.Fatalf("arguments = %q, want the path preserved", call.ToolCalls[0].Function.Arguments)
	}
	if input.Messages[3].ToolCallID != "call-1" {
		t.Fatalf("tool result = %#v, want it bound to the call", input.Messages[3])
	}
	if input.Messages[3].Content == "" {
		t.Fatal("the tool result reached the model but was not kept in the conversation")
	}
}

// The continuation guard is registered hidden, so it never appears in the tool
// catalog. Persisting its synthetic call would leave later turns referring to a
// function the model has no definition for.
func TestLLMMessagesFromDropsTheContinuationGuard(t *testing.T) {
	t.Parallel()
	got := llmMessagesFrom([]providers.Message{
		{Role: "user", Content: "проверь уровни"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{
			ID: "nudge-1", Name: levelReviewNudgeTool, Arguments: map[string]any{"message": "дозагрузи"},
		}}},
		{Role: "tool", ToolCallID: "nudge-1", Content: "дозагрузи"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{
			ID: "call-2", Name: "admin_levels", Arguments: map[string]any{"game_id": 5},
		}}},
		{Role: "tool", ToolCallID: "call-2", Content: `{"count":3}`},
	})

	if len(got) != 3 {
		t.Fatalf("messages = %#v, want the guard pair dropped", got)
	}
	for _, m := range got {
		for _, call := range m.ToolCalls {
			if call.Function.Name == levelReviewNudgeTool {
				t.Fatal("the hidden guard survived into the persisted conversation")
			}
		}
	}
	if got[1].ToolCalls[0].Function.Name != "admin_levels" {
		t.Fatalf("kept call = %#v", got[1].ToolCalls)
	}
	if got[1].ToolCalls[0].Function.Arguments != `{"game_id":5}` {
		t.Fatalf("arguments = %q, want them recovered from the map", got[1].ToolCalls[0].Function.Arguments)
	}
	if got[2].ToolCallID != "call-2" {
		t.Fatalf("result = %#v", got[2])
	}
}

// A turn that made no tool call must not gain phantom entries.
func TestRunAgentLoopPlainAnswerKeepsConversationFlat(t *testing.T) {
	t.Parallel()
	provider := &scriptedPicoProvider{responses: []*providers.LLMResponse{
		{Content: "привет", FinishReason: "stop"},
	}}
	input := &AgentRunInput{
		Cfg:      &config{},
		Session:  &llmSession{},
		Messages: []llmMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "привет"}},
	}
	if _, err := runAgentLoop(context.Background(), AgentConfig{
		Model: "scripted", Provider: provider,
	}, input, AgentCallbacks{}); err != nil {
		t.Fatalf("runAgentLoop: %v", err)
	}
	if len(input.Messages) != 3 {
		t.Fatalf("messages = %#v, want system, user, assistant", input.Messages)
	}
}

// An identical content read returns identical bytes, and those bytes now stay
// in the conversation for the rest of the run. Paying for them twice once blew
// past a provider's context limit.
func TestRuntimeAnswersARepeatedContentReadFromTheEarlierResult(t *testing.T) {
	t.Parallel()
	runtime := &picoLegacyToolRuntime{delivered: map[string]struct{}{}}

	args := `{"path":"scenario.pdf"}`
	if runtime.repeatedContentRead("read_pdf_file", args) {
		t.Fatal("the first read was treated as a repeat")
	}
	if !runtime.repeatedContentRead("read_pdf_file", args) {
		t.Fatal("the second identical read was not recognised as a repeat")
	}
	// Different arguments are a different slice of the document.
	if runtime.repeatedContentRead("read_pdf_file", `{"path":"scenario.pdf","page":2}`) {
		t.Fatal("a paged read was treated as a repeat of the whole-document read")
	}
	// Tools that do not carry documents are never deduplicated: calling them
	// again is how the agent observes a change it just made.
	if runtime.repeatedContentRead("admin_levels", `{"game_id":1}`) ||
		runtime.repeatedContentRead("admin_levels", `{"game_id":1}`) {
		t.Fatal("a non-content tool was deduplicated")
	}
}

// A cancelled run has already changed the game on the server. Dropping its
// history does not undo that, it only hides it: a run stopped after creating
// four levels once left a conversation that read as "never answered", and the
// next message started the whole load over on top of those levels.
func TestRunAgentLoopKeepsHistoryWhenCancelled(t *testing.T) {
	// Not parallel: drives a real tool execution, which swaps process globals.
	ctx, cancel := context.WithCancel(context.Background())
	provider := &cancellingPicoProvider{cancel: cancel, responses: []*providers.LLMResponse{
		{
			FinishReason: "tool_calls",
			ToolCalls: []providers.ToolCall{{
				ID: "call-1", Type: "function", Name: "read_pdf_file",
				Arguments: map[string]any{"path": "scenario.pdf"},
			}},
		},
	}}
	input := &AgentRunInput{
		Cfg:      &config{},
		Session:  &llmSession{},
		Messages: []llmMessage{{Role: "system", Content: "system"}, {Role: "user", Content: "залей сценарий"}},
		Tools: []llmTool{{Type: "function", Function: llmFunction{
			Name:       "read_pdf_file",
			Parameters: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`),
		}}},
	}
	if _, err := runAgentLoop(ctx, AgentConfig{Model: "scripted", Provider: provider},
		input, AgentCallbacks{}); err == nil {
		t.Fatal("runAgentLoop returned no error for a cancelled run")
	}

	roles := make([]string, 0, len(input.Messages))
	for _, m := range input.Messages {
		roles = append(roles, m.Role)
	}
	if strings.Join(roles, ",") != "system,user,assistant,tool,assistant" {
		t.Fatalf("roles = %v, want the interrupted work kept plus a closing note", roles)
	}
	if len(input.Messages[2].ToolCalls) != 1 {
		t.Fatalf("the tool call made before cancellation was dropped: %#v", input.Messages[2])
	}
	note := input.Messages[len(input.Messages)-1].Content
	if !strings.Contains(note, "прервано пользователем") {
		t.Fatalf("note = %q, want it to say the user stopped the run", note)
	}
	if !strings.Contains(note, "Не продолжай") {
		t.Fatalf("note = %q, want it to forbid silently resuming", note)
	}
}

func TestInterruptedRunNoteDistinguishesFailureFromCancellation(t *testing.T) {
	t.Parallel()
	if note := interruptedRunNote(errors.New("LLM API error after 3 attempts")); strings.Contains(note, "пользователем") {
		t.Fatalf("note = %q, want a plain failure not reported as a cancellation", note)
	} else if !strings.Contains(note, "проверь текущее состояние") {
		t.Fatalf("note = %q, want it to ask for a state check before further changes", note)
	}
	if note := interruptedRunNote(fmt.Errorf("wrapped: %w", context.Canceled)); !strings.Contains(note, "пользователем") {
		t.Fatalf("note = %q, want a wrapped cancellation recognised", note)
	}
}

// cancellingPicoProvider cancels the run from inside the loop, the way the web
// UI's cancel button does while tools are still executing.
type cancellingPicoProvider struct {
	cancel    context.CancelFunc
	responses []*providers.LLMResponse
	calls     int
}

func (p *cancellingPicoProvider) GetDefaultModel() string { return "scripted" }

func (p *cancellingPicoProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	_ []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	if p.calls >= len(p.responses) {
		p.cancel()
		return nil, context.Canceled
	}
	response := p.responses[p.calls]
	p.calls++
	return response, nil
}

// "Wipe the game" once wiped the game and then rebuilt it, because the rule
// that forbids stopping partway had no boundary and the earlier, interrupted
// request still looked owed.
func TestSystemPromptScopesTheTaskToTheLatestMessage(t *testing.T) {
	t.Parallel()
	prompt := buildSystemPrompt(&config{domain: "demo.en.cx"}, &llmSession{})
	if !strings.Contains(prompt, "SCOPE: the user's LATEST message defines the task") {
		t.Fatal("the system prompt no longer scopes the task to the latest message")
	}
	scope := strings.Index(prompt, "- SCOPE:")
	complete := strings.Index(prompt, "- ALWAYS COMPLETE THE FULL TASK")
	if scope < 0 || complete < 0 || scope > complete {
		t.Fatalf("SCOPE at %d, ALWAYS COMPLETE at %d: the boundary must be read first", scope, complete)
	}
	if !strings.Contains(prompt, "interrupted, cancelled, or replaced") {
		t.Fatal("the prompt does not tell the model to leave an interrupted request alone")
	}
}
