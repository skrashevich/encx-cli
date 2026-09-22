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

func TestStampedUserMessageCarriesTheTime(t *testing.T) {
	fixed := time.Date(2026, 9, 7, 14, 7, 0, 0, time.FixedZone("MSK", 3*60*60))
	orig := systemPromptNow
	systemPromptNow = func() time.Time { return fixed }
	defer func() { systemPromptNow = orig }()

	stamped := stampedUserMessage("скопируй игру 82033")

	if !strings.Contains(stamped, "2026-09-07T14:07:00+03:00") {
		t.Errorf("message missing local RFC3339 timestamp:\n%s", stamped)
	}
	if !strings.Contains(stamped, "2026-09-07T11:07:00Z") {
		t.Errorf("message missing UTC timestamp:\n%s", stamped)
	}
	if !strings.HasSuffix(stamped, "скопируй игру 82033") {
		t.Errorf("the user's own text must survive verbatim at the end:\n%s", stamped)
	}

	// The rule that tells the model where to look for "now" has to point at the
	// stamp, or the stamp is decoration.
	prompt := buildSystemPrompt(&config{domain: "tech.en.cx"}, &llmSession{})
	if !strings.Contains(prompt, "authoritative") || !strings.Contains(prompt, "square brackets") {
		t.Errorf("prompt does not tell the model the time rides on the message:\n%s", prompt)
	}
}

// The whole point of moving the clock onto the message: the system prompt — the
// rules plus the tool catalog behind it, some 6700 tokens — must be byte-identical
// from one message to the next, or none of it can be reused. It used to carry the
// time on its fourth line, which made every turn decode all of it again: on an
// Intel N150 that was over five minutes before the model said a word.
func TestBuildSystemPromptIsStableOverTime(t *testing.T) {
	orig := systemPromptNow
	defer func() { systemPromptNow = orig }()
	cfg := &config{domain: "tech.en.cx", gameId: 82033}

	systemPromptNow = func() time.Time { return time.Date(2026, 9, 7, 14, 7, 0, 0, time.UTC) }
	first := buildSystemPrompt(cfg, &llmSession{})

	systemPromptNow = func() time.Time { return time.Date(2026, 9, 8, 21, 43, 12, 0, time.UTC) }
	second := buildSystemPrompt(cfg, &llmSession{})

	if first != second {
		t.Fatalf("the system prompt changed with the clock, so no prefix can be cached:\n--- first ---\n%s\n--- second ---\n%s", first, second)
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
	runtime.afterToolResult("read_pdf_file", args, `{"content":"страница"}`)
	if !runtime.repeatedContentRead("read_pdf_file", args) {
		t.Fatal("the second identical read was not recognised as a repeat")
	}
	// Different arguments are a different slice of the document.
	if runtime.repeatedContentRead("read_pdf_file", `{"path":"scenario.pdf","page":2}`) {
		t.Fatal("a paged read was treated as a repeat of the whole-document read")
	}
	// Tools that do not carry documents are never deduplicated: calling them
	// again is how the agent observes a change it just made.
	args = `{"game_id":1}`
	runtime.afterToolResult("admin_levels", args, `{"count":1,"levels":[{"number":1}]}`)
	if runtime.repeatedContentRead("admin_levels", args) {
		t.Fatal("a non-content tool was deduplicated")
	}
}

// Level content is a document too. A review of thirty levels re-read all thirty
// every time the transcript was trimmed, and re-reading them is what trimmed it
// again; the run never got past the first pass.
func TestRuntimeRefusesARepeatedLevelContentRead(t *testing.T) {
	t.Parallel()
	runtime := &picoLegacyToolRuntime{delivered: map[string]struct{}{}}

	args := `{"game_id":82856,"level_number":1}`
	if runtime.repeatedContentRead("admin_level_content", args) {
		t.Fatal("the first read was treated as a repeat")
	}
	runtime.afterToolResult("admin_level_content", args, `{"level":1,"tasks":[]}`)
	if !runtime.repeatedContentRead("admin_level_content", args) {
		t.Fatal("the second identical read was not recognised as a repeat")
	}
	if runtime.repeatedContentRead("admin_level_content", `{"game_id":82856,"level_number":2}`) {
		t.Fatal("another level was treated as a repeat")
	}
	// The refusal is told to a model that has no page or offset to change, so it
	// must not demand different arguments it cannot supply.
	refusal := repeatedReadRefusal("admin_level_content")
	if strings.Contains(refusal, "offset=N") || strings.Contains(refusal, "page=N") {
		t.Fatalf("a level read cannot be paged: %s", refusal)
	}
}

// A level that failed is not a level that was delivered. Recording the memo at
// the point of the call meant a 429 on level 17 could never be retried, while
// the nudge that counts unread levels kept asking for it — the same loop this
// guard exists to break, in miniature.
func TestAFailedReadIsNotRememberedAsDelivered(t *testing.T) {
	t.Parallel()
	runtime := &picoLegacyToolRuntime{delivered: map[string]struct{}{}}

	args := `{"game_id":82856,"level_number":17}`
	runtime.afterToolResult("admin_level_content", args, `{"error":"HTTP 429: too many requests"}`)
	if runtime.repeatedContentRead("admin_level_content", args) {
		t.Fatal("a level that answered with an error cannot be asked for again")
	}
}

// The schema requires game_id and level_number, but models break schemas. Keying
// the memo on the raw argument string gave every spelling of one read — the game
// id dropped, a field invented — its own free pass through the guard.
func TestRepeatedLevelReadIsKeyedByWhatItAddresses(t *testing.T) {
	t.Parallel()
	runtime := &picoLegacyToolRuntime{
		input:     &AgentRunInput{Cfg: &config{gameId: 82856}},
		delivered: map[string]struct{}{},
	}

	runtime.afterToolResult("admin_level_content", `{"game_id":82856,"level_number":1}`, `{"level":1}`)
	if !runtime.repeatedContentRead("admin_level_content", `{"level_number":1}`) {
		t.Fatal("dropping the game id the session already knows slipped past the guard")
	}
	if !runtime.repeatedContentRead("admin_level_content", `{"game_id":82856,"level_number":1,"format":"full"}`) {
		t.Fatal("an argument the tool does not have slipped past the guard")
	}
}

// Reading a level back after editing it is how the agent checks its own work,
// so a mutation has to clear the memo. Documents read from disk or the web did
// not change because a game did, and forgetting those would disarm the guard
// where it is needed most: a build session is one long series of mutations.
func TestMutationReopensGameReadsButNotDocuments(t *testing.T) {
	t.Parallel()
	runtime := &picoLegacyToolRuntime{delivered: map[string]struct{}{}}

	level := `{"game_id":82856,"level_number":1}`
	pdf := `{"path":"scenario.pdf"}`
	runtime.afterToolResult("admin_level_content", level, `{"level":1}`)
	runtime.afterToolResult("read_pdf_file", pdf, `{"content":"страница"}`)

	// Through the same seam execute uses, so the wiring is covered too.
	runtime.afterToolResult("admin_set_comment", `{"game_id":82856,"level_number":1}`, `{"success":true}`)

	if runtime.repeatedContentRead("admin_level_content", level) {
		t.Fatal("a level edited in this run cannot be read back")
	}
	if !runtime.repeatedContentRead("read_pdf_file", pdf) {
		t.Fatal("writing to a game re-opened an unrelated document for re-reading")
	}
}

// A mutation that failed changed nothing, so it must not throw away reads that
// are still valid — and pay for them again.
func TestAFailedMutationKeepsTheMemo(t *testing.T) {
	t.Parallel()
	runtime := &picoLegacyToolRuntime{delivered: map[string]struct{}{}}

	level := `{"game_id":82856,"level_number":1}`
	runtime.afterToolResult("admin_level_content", level, `{"level":1}`)
	runtime.afterToolResult("admin_set_comment", level, `{"error":"Взнос не корректен"}`)

	if !runtime.repeatedContentRead("admin_level_content", level) {
		t.Fatal("a mutation that failed dropped a read that is still valid")
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
