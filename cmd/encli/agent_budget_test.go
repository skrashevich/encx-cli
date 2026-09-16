package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// The numbers below are one real turn of this agent against its own provider:
// the system prompt and all 56 tool schemas, 29745 bytes, priced at 9474 prompt
// tokens. That is 3.14 bytes per token for the most English-heavy traffic the
// run ever sends; level content and question packs are Cyrillic and cost more
// bytes per token still.
const (
	measuredRequestBytes = 29745
	measuredPromptTokens = 9474
)

func TestBudgetCalibratorUsesTheProvidersOwnTokenCount(t *testing.T) {
	t.Parallel()
	var c agentBudgetCalibrator

	if got := c.byteBudget(); got != agentRequestByteBudget {
		t.Fatalf("before any usage report the budget must be the worst case %d, got %d",
			agentRequestByteBudget, got)
	}

	c.observe(measuredRequestBytes, measuredPromptTokens)
	want := int(agentRequestTokenBudget * agentMaxBytesPerToken)
	if got := c.byteBudget(); got != want {
		t.Fatalf("budget = %d, want %d (measured rate %.2f clamps to %.1f)",
			got, want, float64(measuredRequestBytes)/float64(measuredPromptTokens), agentMaxBytesPerToken)
	}

	// A provider that reports nothing leaves the budget where it was.
	c.observe(measuredRequestBytes, 0)
	c.observe(0, measuredPromptTokens)
	if got := c.byteBudget(); got != want {
		t.Fatalf("an empty usage report moved the budget to %d", got)
	}
}

// A rate between the floor and the ceiling has to be carried through as itself,
// not rounded to whichever clamp is nearer: every rate this workload has
// actually measured sits at the ceiling, so nothing else would catch a
// calibrator that simply returned the maximum.
func TestBudgetCalibratorCarriesARateBetweenTheClamps(t *testing.T) {
	t.Parallel()
	var c agentBudgetCalibrator

	c.observe(2000, 1000)
	if got, want := c.byteBudget(), int(agentRequestTokenBudget*2.0); got != want {
		t.Fatalf("budget = %d, want %d for two bytes per token", got, want)
	}
}

// The token budget assumes a 128K window, and this runtime also talks to
// GigaChat and to whatever sits behind a custom base URL. Rather than keep a
// table of windows per model, believe the provider when it says no.
func TestBudgetCalibratorShrinksOnARejectedRequest(t *testing.T) {
	t.Parallel()
	var c agentBudgetCalibrator
	c.observe(measuredRequestBytes, measuredPromptTokens)

	c.tooLarge(200_000)
	if got := c.byteBudget(); got != 100_000 {
		t.Fatalf("budget = %d, want half of the request the provider refused", got)
	}
	// A later, larger rejection does not undo a smaller ceiling.
	c.tooLarge(400_000)
	if got := c.byteBudget(); got != 100_000 {
		t.Fatalf("the ceiling rose to %d", got)
	}
	// Shrinking has a floor: a request that cannot hold the system prompt and
	// the tool schemas cannot run at all.
	c.tooLarge(1000)
	if got := c.byteBudget(); got != agentMinRequestByteBudget {
		t.Fatalf("budget = %d, want the floor %d", got, agentMinRequestByteBudget)
	}
}

// Cached input is billed at a discount by some providers, which reports fewer
// tokens than were read and makes a byte look far cheaper than it is. Buying
// context the window does not have gets the whole request rejected, so the
// calibrator keeps the smallest rate it has seen rather than the newest.
func TestBudgetCalibratorKeepsTheMostPessimisticRate(t *testing.T) {
	t.Parallel()
	var c agentBudgetCalibrator

	c.observe(100_000, 200) // 500 bytes per token: accounting, not text
	if got, want := c.byteBudget(), int(agentRequestTokenBudget*agentMaxBytesPerToken); got != want {
		t.Fatalf("an absurd rate must clamp to %d, got %d", want, got)
	}

	c.observe(100_000, 100_000) // one byte per token
	if got := c.byteBudget(); got != agentRequestByteBudget {
		t.Fatalf("the lower rate must win: budget = %d, want %d", got, agentRequestByteBudget)
	}

	c.observe(100_000, 200) // the discounted report comes back
	if got := c.byteBudget(); got != agentRequestByteBudget {
		t.Fatalf("a later inflated rate raised the budget to %d", got)
	}
}

// One level of a real game: task text, name and comment, about 2.3 KB.
func levelContentResult(level int) string {
	task := strings.Repeat("Текст задания уровня с пояснением редактора. ", 25)
	return fmt.Sprintf(
		`{"level":%d,"name":"Уровень %d","comment":"Комментарий к предыдущему уровню.","tasks":[{"id":%d,"text":%q}]}`,
		level, level, 1000+level, task)
}

// thirtyLevelReview rebuilds the transcript of the run that hung: read the
// question pack, list the levels, then read the content of all thirty.
func thirtyLevelReview(t *testing.T) ([]providers.Message, []providers.ToolDefinition) {
	t.Helper()
	return levelReviewTranscript(t, 30)
}

func levelReviewTranscript(t *testing.T, levels int) ([]providers.Message, []providers.ToolDefinition) {
	t.Helper()
	session := &llmSession{}
	in := &AgentRunInput{Cfg: &config{}, Session: session, Tools: getToolsForSession(session)}
	registry, err := newPicoRegistry(in, AgentCallbacks{}, &agentRunStats{})
	if err != nil {
		t.Fatal(err)
	}

	history := []providers.Message{
		{Role: "system", Content: strings.Repeat("system prompt line\n", 330)}, // ~6 KB, as shipped
		{Role: "user", Content: "проверь что все комментарии соответствуют пакету"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "fetch", Type: "function",
			Function: &providers.FunctionCall{
				Name: "fetch_url", Arguments: `{"url":"https://db.chgk.info/tour/KNNPV1_u/print"}`}}}},
		// The question pack as fetch_url returns it, ~38 KB of extracted text.
		{Role: "tool", ToolCallID: "fetch", Content: strings.Repeat("Вопрос. Комментарий редактора к вопросу. ", 500)},
	}
	for level := 1; level <= levels; level++ {
		id := fmt.Sprintf("level-%d", level)
		history = append(history,
			providers.Message{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: id, Type: "function",
				Function: &providers.FunctionCall{
					Name:      "admin_level_content",
					Arguments: fmt.Sprintf(`{"game_id":82856,"level_number":%d}`, level)}}}},
			providers.Message{Role: "tool", ToolCallID: id, Content: levelContentResult(level)})
	}
	return history, registry.ToProviderDefs()
}

// The run this reproduces: a thirty-level game checked against the question pack
// it was built from. Nothing here is large — the whole working set is about 30K
// tokens — but spending a token budget in bytes made it four times too big to
// hold, and the model spent six minutes re-reading levels it had already read.
func TestThirtyLevelReviewFitsTheCalibratedBudget(t *testing.T) {
	t.Parallel()
	history, tools := thirtyLevelReview(t)

	var c agentBudgetCalibrator
	c.observe(measuredRequestBytes, measuredPromptTokens)
	bounded, err := boundedAgentMessages(history, tools, c.byteBudget())
	if err != nil {
		t.Fatalf("boundedAgentMessages: %v", err)
	}
	if len(bounded) != len(history) {
		t.Fatalf("turns were evicted from a %d-token working set: %d of %d messages left",
			measuredPromptTokens, len(bounded), len(history))
	}
	for i := range bounded {
		if bounded[i].Content != history[i].Content {
			t.Fatalf("message %d was trimmed; the model would read it again:\n%.120s",
				i, bounded[i].Content)
		}
	}
}

// The same transcript at the uncalibrated budget, which is what every run used
// before the provider's token count was consulted. It is kept as the record of
// what the trim does when the working set genuinely does not fit: the levels
// the model still needs are replaced by a 64-byte lead, and the note left in
// their place must not send it back to re-read them.
func TestTrimTellsTheModelToRecordFindingsRatherThanReread(t *testing.T) {
	t.Parallel()
	history, tools := thirtyLevelReview(t)

	bounded, err := boundedAgentMessages(history, tools, agentRequestByteBudget)
	if err != nil {
		t.Fatalf("boundedAgentMessages: %v", err)
	}
	// Measured the way boundedAgentMessages measures it: the tool schemas are
	// 23 KB of every request, and comparing the messages alone against the budget
	// is an assertion that cannot fail.
	sent, err := agentRequestBytes(bounded, tools)
	if err != nil {
		t.Fatal(err)
	}
	if sent > agentRequestByteBudget {
		t.Fatalf("request still oversized: %d", sent)
	}

	var excerpted int
	for _, m := range bounded {
		if m.Role == "tool" && strings.HasPrefix(m.Content, "[Excerpt;") {
			excerpted++
		}
	}
	if excerpted == 0 {
		t.Fatal("the uncalibrated budget is supposed to be too small for this transcript")
	}
	note := ""
	for _, m := range bounded {
		if strings.HasPrefix(m.Content, "[Excerpt;") {
			note = m.Content
			break
		}
	}
	if strings.Contains(strings.ToLower(note), "re-read") {
		t.Fatalf("the trim note still asks for a re-read, which is what looped: %.200s", note)
	}
	if !strings.Contains(note, "write what you") {
		t.Fatalf("the trim note must say how to make progress instead: %.200s", note)
	}
}

// A provider that refuses the first refusals times, reporting the size it was
// given each time, then answers.
type oversizedRequestProvider struct {
	refusals int
	sizes    []int
}

func (p *oversizedRequestProvider) GetDefaultModel() string { return "small-window" }

func (p *oversizedRequestProvider) Chat(
	_ context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	sent, err := agentRequestBytes(messages, tools)
	if err != nil {
		return nil, err
	}
	p.sizes = append(p.sizes, sent)
	if len(p.sizes) <= p.refusals {
		return nil, fmt.Errorf("HTTP 400: This model's maximum context length is 32768 tokens")
	}
	return &providers.LLMResponse{
		Content:      "готово",
		FinishReason: "stop",
		Usage:        &providers.UsageInfo{PromptTokens: sent / 3},
	}, nil
}

// The budget assumes a 128K window; this runtime also talks to models with 32K.
// Shrinking has to be able to reach such a window, and it cannot if each halving
// spends one of the three network retries — the run would die of "failed after 3
// attempts" without ever sending a request the model could accept.
func TestRunShrinksUntilTheModelAcceptsTheRequest(t *testing.T) {
	// Not parallel: runAgentLoop drives real tool machinery and process globals.
	// Four times the levels of the run that hung, so there is still room to
	// shrink into after three refusals.
	history, _ := levelReviewTranscript(t, 120)
	session := &llmSession{agentBytesPerToken: agentMaxBytesPerToken}
	input := &AgentRunInput{
		Cfg:      &config{},
		Session:  session,
		Messages: llmMessagesFrom(history),
		Tools:    getToolsForSession(session),
	}
	provider := &oversizedRequestProvider{refusals: 3}

	if _, err := runAgentLoop(context.Background(), AgentConfig{
		BaseURL:  "http://127.0.0.1:1/v1",
		Model:    "small-window",
		Provider: provider,
	}, input, AgentCallbacks{}); err != nil {
		t.Fatalf("runAgentLoop: %v", err)
	}

	if len(provider.sizes) != provider.refusals+1 {
		t.Fatalf("requests = %v, want %d attempts", provider.sizes, provider.refusals+1)
	}
	for i := 1; i < len(provider.sizes); i++ {
		if provider.sizes[i] >= provider.sizes[i-1] {
			t.Fatalf("request %d did not shrink after a refusal: %v", i, provider.sizes)
		}
	}
	// What the run learned about the window outlives the run: without it the
	// next user message rediscovers it by having two more requests refused.
	if session.agentRequestCeiling <= 0 || session.agentRequestCeiling >= agentRequestByteBudget {
		t.Fatalf("session ceiling = %d, want the learned limit", session.agentRequestCeiling)
	}
}
