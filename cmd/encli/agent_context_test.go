package main

import (
	"encoding/json/v2"
	"fmt"
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

// assertToolCallsPaired checks the invariant every OpenAI-compatible provider
// enforces: a tool result must follow the assistant message that called it, and
// an assistant call must be answered. Trimming context by hand is exactly how
// that pairing gets broken.
func assertToolCallsPaired(t *testing.T, messages []providers.Message) {
	t.Helper()
	pending := map[string]bool{}
	for i, m := range messages {
		if m.Role == "tool" {
			if !pending[m.ToolCallID] {
				t.Fatalf("message %d: tool result %q has no matching call", i, m.ToolCallID)
			}
			delete(pending, m.ToolCallID)
			continue
		}
		if len(pending) > 0 {
			t.Fatalf("message %d: %d tool calls left unanswered", i, len(pending))
		}
		pending = map[string]bool{}
		for _, call := range m.ToolCalls {
			pending[call.ID] = true
		}
	}
	if len(pending) > 0 {
		t.Fatalf("transcript ends with %d unanswered tool calls", len(pending))
	}
}

// A long build — one fetched page plus a hundred admin calls — must degrade to
// a smaller request, never to a failed run: the game is already half-created by
// the time the transcript gets big.
func TestAgentDegradesInsteadOfFailingOnLongRun(t *testing.T) {
	page := strings.Repeat("вопрос и ответ из пакета ", 1600) // ~39 KB, as the real fetch_url result was
	history := []providers.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "создай игру, задания возьми из пакета"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "fetch1", Type: "function",
			Function: &providers.FunctionCall{Name: "fetch_url", Arguments: `{"url":"https://db.chgk.info/tour/x/print"}`}}}},
		{Role: "tool", ToolCallID: "fetch1", Content: page},
	}
	// 40 turns of five admin calls: the arguments alone outweigh the budget, so
	// excerpting tool results — all this used to be able to do — cannot save it.
	for turn := range 40 {
		var calls []providers.ToolCall
		for c := range 5 {
			id := fmt.Sprintf("call-%d-%d", turn, c)
			calls = append(calls, providers.ToolCall{ID: id, Type: "function", Function: &providers.FunctionCall{
				Name:      "admin_create_task",
				Arguments: fmt.Sprintf(`{"game_id":82856,"level_number":%d,"task":%q}`, turn+1, strings.Repeat("текст задания ", 40)),
			}})
		}
		history = append(history, providers.Message{Role: "assistant", ToolCalls: calls})
		for _, call := range calls {
			history = append(history, providers.Message{Role: "tool", ToolCallID: call.ID, Content: `{"success":true}`})
		}
	}
	originalArgs := history[4].ToolCalls[0].Function.Arguments

	bounded, err := boundedAgentMessages(history, nil, agentRequestByteBudget)
	if err != nil {
		t.Fatalf("a long run must degrade, not fail: %v", err)
	}
	b, err := json.Marshal(bounded)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > agentRequestByteBudget {
		t.Fatalf("request still oversized: %d", len(b))
	}
	assertToolCallsPaired(t, bounded)

	if bounded[0].Role != "system" || !strings.Contains(bounded[0].Content, "system prompt") {
		t.Fatalf("system prompt lost: %+v", bounded[0])
	}
	if !strings.Contains(string(b), "создай игру") {
		t.Fatal("the user's own instruction was dropped")
	}
	// The last turns are what the model must react to, so they stay verbatim.
	last := bounded[len(bounded)-1]
	if last.Role != "tool" || last.Content != `{"success":true}` {
		t.Fatalf("the newest tool result was altered: %+v", last)
	}
	if history[4].ToolCalls[0].Function.Arguments != originalArgs {
		t.Fatal("the stored transcript was edited through the shared FunctionCall pointer")
	}
	if history[3].Content != page {
		t.Fatal("stored source was destroyed")
	}
}

// A transcript of many small steps has nothing left to excerpt or trim — every
// message is already below the compressible floor — so the only way to fit it
// is to evict whole turns.
func TestAgentEvictsEarlyTurnsWhenNothingIsCompressible(t *testing.T) {
	history := []providers.Message{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "залей уровни"},
	}
	for turn := range 1200 {
		id := fmt.Sprintf("call-%d", turn)
		history = append(history,
			providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: id, Type: "function",
				Function: &providers.FunctionCall{Name: "admin_set_autopass", Arguments: `{"game_id":82856,"level":1}`}}}},
			providers.Message{Role: "tool", ToolCallID: id, Content: `{"success":true}`})
	}

	bounded, err := boundedAgentMessages(history, nil, agentRequestByteBudget)
	if err != nil {
		t.Fatalf("eviction must keep the run alive: %v", err)
	}
	b, err := json.Marshal(bounded)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > agentRequestByteBudget {
		t.Fatalf("request still oversized: %d", len(b))
	}
	if len(bounded) >= len(history) {
		t.Fatalf("nothing was evicted: %d of %d messages", len(bounded), len(history))
	}
	assertToolCallsPaired(t, bounded)
	if bounded[0].Role != "system" || !strings.Contains(bounded[0].Content, "Context trimmed") {
		t.Fatalf("the gap must be declared in the system prompt: %+v", bounded[0])
	}
	// Same rule as the excerpt note: on a transcript that no longer fits, asking
	// for a re-read is asking the run to thrash. The note may name re-reading as
	// the cost — it must not name it as the remedy.
	if strings.Contains(strings.ToLower(bounded[0].Content), "re-read the ") {
		t.Fatalf("the eviction note still asks for a re-read: %s", bounded[0].Content)
	}
	if !strings.Contains(bounded[0].Content, "Write your conclusions") {
		t.Fatalf("the eviction note must say how to make progress: %s", bounded[0].Content)
	}
	if !strings.Contains(string(b), "залей уровни") {
		t.Fatal("the user's own instruction was evicted")
	}
	if last := bounded[len(bounded)-1]; last.ToolCallID != "call-1199" {
		t.Fatalf("the newest turn must survive, got %+v", last)
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
	messages, err := boundedAgentMessages(picoMessages(saved.Messages), registry.ToProviderDefs(), agentRequestByteBudget)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(messages)
	t.Log("BOUNDED_MESSAGE_BYTES", len(b), "MESSAGES", len(messages), "of", len(saved.Messages),
		"EVICTED_TURNS", strings.Contains(messages[0].Content, "Context trimmed"))
	assertToolCallsPaired(t, messages)
}

// The page the model just asked for must survive the trim. Excerpting it left
// the model a 64-byte stub of its own fetch, so it fetched again — eleven times
// on one real run, shrinking max_bytes each round and never reading a word.
func TestAgentKeepsTheFreshlyFetchedPage(t *testing.T) {
	page := strings.Repeat("Вопрос 1. Комментарий: пояснение редактора. ", 900) // ~39 KB
	var history []providers.Message
	history = append(history,
		providers.Message{Role: "system", Content: "system prompt"},
		providers.Message{Role: "user", Content: "возьми комментарии из пакета"})
	// Enough earlier work to blow the budget on its own.
	for turn := range 60 {
		id := fmt.Sprintf("old-%d", turn)
		history = append(history,
			providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{{
				ID: id, Type: "function", Function: &providers.FunctionCall{
					Name:      "admin_create_task",
					Arguments: fmt.Sprintf(`{"game_id":1,"level_number":%d,"task":%q}`, turn, strings.Repeat("текст ", 60)),
				}}}},
			providers.Message{Role: "tool", ToolCallID: id, Content: strings.Repeat("результат уровня ", 60)})
	}
	// The newest turn: the fetch the model is about to read.
	history = append(history,
		providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{{
			ID: "fetch-now", Type: "function", Function: &providers.FunctionCall{
				Name: "fetch_url", Arguments: `{"url":"https://db.chgk.info/tour/KNNPV1_u/print"}`}}}},
		providers.Message{Role: "tool", ToolCallID: "fetch-now", Content: page})

	bounded, err := boundedAgentMessages(history, nil, agentRequestByteBudget)
	if err != nil {
		t.Fatalf("must degrade, not fail: %v", err)
	}
	b, err := json.Marshal(bounded)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > agentRequestByteBudget {
		t.Fatalf("request still oversized: %d", len(b))
	}
	assertToolCallsPaired(t, bounded)

	last := bounded[len(bounded)-1]
	if last.ToolCallID != "fetch-now" {
		t.Fatalf("the newest tool result is not last: %+v", last)
	}
	if last.Content != page {
		t.Fatalf("the freshly fetched page was trimmed to %d bytes; the model would refetch it",
			len(last.Content))
	}
}
