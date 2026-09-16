package main

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// The budget is a token budget; bytes are what this process can measure without
// a tokenizer. Leave room for the response and the provider's message framing in
// a 128K window.
const agentRequestTokenBudget = 112 * 1024

const (
	// One byte per token is the worst case no text can beat, so it is what a run
	// spends until the provider says otherwise.
	agentMinBytesPerToken = 1.0
	// A deliberate margin, not a measurement: the real traffic here rates 3.14
	// for the tool schemas and 3.99 once the transcript fills with Cyrillic, so
	// the ceiling gives up a quarter of the window on purpose. It also bounds the
	// damage from a provider that bills cached input at a discount and so reports
	// far fewer tokens than it read.
	agentMaxBytesPerToken = 3.0
	// No rejection shrinks the budget below this: a request that cannot hold the
	// system prompt and the tool schemas cannot run at all.
	agentMinRequestByteBudget = 32 * 1024
	// Halving from the calibrated budget to that floor takes four steps.
	agentMaxContextShrinks = 4
)

// agentRequestByteBudget is the uncalibrated budget, used until the first usage
// report arrives.
//
// Spending a token budget in bytes is only correct for text where every token
// is one byte. This transcript is Russian JSON, where a token runs two to four:
// the run was handed about a quarter of the window it had. Thirty levels of
// 2.3 KB plus a 38 KB question pack — 107 KB of a 112 KB budget already holding
// 23 KB of tool schemas and a 6 KB system prompt — could not be held at once, so
// every turn excerpted results the model then re-read, which excerpted others.
// A live run spent six minutes re-reading the same thirty levels and never
// answered. The working set was never the problem: it is 30K tokens.
const agentRequestByteBudget = int(agentRequestTokenBudget * agentMinBytesPerToken)

// agentBudgetCalibrator converts the budget from tokens into bytes at the rate
// this run is actually paying, learned from the provider's own usage report for
// a request whose size we measured ourselves.
type agentBudgetCalibrator struct {
	// bytesPerToken is the smallest rate observed so far, or zero before the
	// first report. Smallest, not latest: an over-estimated rate buys context
	// the window does not have, and the request is rejected outright.
	bytesPerToken float64

	// ceiling is what the provider proved it will not accept, halved. The token
	// budget assumes a 128K window, and this runtime also talks to GigaChat and
	// to whatever sits behind a custom base URL, where the window can be 32K.
	// Rather than guess a window per model, believe the rejection.
	ceiling int
}

func (c *agentBudgetCalibrator) byteBudget() int {
	budget := agentRequestByteBudget
	if c.bytesPerToken > 0 {
		budget = int(agentRequestTokenBudget * c.bytesPerToken)
	}
	if c.ceiling > 0 && c.ceiling < budget {
		return c.ceiling
	}
	return budget
}

// tooLarge records a request the provider refused as too long for its window.
func (c *agentBudgetCalibrator) tooLarge(requestBytes int) {
	if requestBytes <= 0 {
		return
	}
	ceiling := requestBytes / 2
	if ceiling < agentMinRequestByteBudget {
		ceiling = agentMinRequestByteBudget
	}
	if c.ceiling == 0 || ceiling < c.ceiling {
		c.ceiling = ceiling
	}
}

// atFloor reports that shrinking has nothing left to give.
func (c *agentBudgetCalibrator) atFloor() bool {
	return c.ceiling > 0 && c.ceiling <= agentMinRequestByteBudget
}

func (c *agentBudgetCalibrator) observe(requestBytes, promptTokens int) {
	if requestBytes <= 0 || promptTokens <= 0 {
		return
	}
	// The package shadows the builtin min with an int one, so clamp by hand.
	rate := float64(requestBytes) / float64(promptTokens)
	if rate < agentMinBytesPerToken {
		rate = agentMinBytesPerToken
	}
	if rate > agentMaxBytesPerToken {
		rate = agentMaxBytesPerToken
	}
	if c.bytesPerToken == 0 || rate < c.bytesPerToken {
		c.bytesPerToken = rate
	}
}

// agentRequestBytes measures what a request costs on the wire, tool schemas
// included: they are sent on every turn and are 23 KB of the budget here.
func agentRequestBytes(ms []providers.Message, tools []providers.ToolDefinition) (int, error) {
	b, err := json.Marshal(struct {
		Messages []providers.Message
		Tools    []providers.ToolDefinition
	}{ms, tools})
	return len(b), err
}

const (
	// A result or an argument list shorter than this cannot pay for the
	// annotation that would replace it.
	agentMinCompressibleBytes = 320
	// How much of a dropped tool result is kept as a lead, so the model can see
	// what the omitted payload was about.
	agentToolExcerptBytes = 64
	// Turns at the end of the transcript that are never evicted: the model has
	// to see what it just did, and the result it was called to react to.
	agentKeptTailTurns = 2
)

// Bound only the outgoing request. The complete transcript remains on disk and
// in the tool loop; tool call IDs, arguments and user instructions stay intact.
//
// Degradation is a ladder, cheapest loss first: excerpt big tool results, then
// drop the arguments of calls that already ran, then evict whole early turns.
// A long build (one fetched page plus a hundred admin calls) used to exhaust
// step one and abort the run mid-import with the game already half-filled, so
// running out of room must cost context, never the task.
func boundedAgentMessages(messages []providers.Message, tools []providers.ToolDefinition, budget int) ([]providers.Message, error) {
	if budget <= 0 {
		budget = agentRequestByteBudget
	}
	size := func(ms []providers.Message) (int, error) {
		return agentRequestBytes(ms, tools)
	}
	n, err := size(messages)
	if err != nil {
		return nil, err
	}
	if n <= budget {
		return messages, nil
	}

	out := slices.Clone(messages)
	groups := agentTurnGroups(out)
	// Everything from here on is off limits to every stage below.
	tailStart := len(out)
	if kept := min(agentKeptTailTurns, len(groups)); kept > 0 {
		tailStart = groups[len(groups)-kept][0]
	}

	// The tail is off limits here too. Excerpting the result the model was just
	// called to react to leaves it with a 64-byte stub of the page it asked for,
	// so it fetches the page again, the new result is excerpted again, and the
	// run spins: one loop re-fetched the same question pack eleven times,
	// shrinking max_bytes each round, and never saw a word of it.
	for i := range out[:tailStart] {
		if out[i].Role != "tool" || len(out[i].Content) <= agentMinCompressibleBytes {
			continue
		}
		content := out[i].Content
		end := agentToolExcerptBytes
		for !utf8.ValidString(content[:end]) {
			end--
		}
		// "Re-read what you need" is what this note used to say, and on a
		// transcript that no longer fits it is an instruction to thrash: every
		// re-read pushes out another result, which is then re-read in turn. Say
		// what it costs and how to finish instead.
		note := fmt.Sprintf("[Excerpt; omitted result of %d bytes: the transcript no longer fits the context. "+
			"Full history kept on disk. Reading this again costs another result, so first write what you "+
			"concluded from it into your reply, then move on; do not infer omitted content.]\n%s",
			len(content), content[:end])
		// Do not replace a small result with a larger annotation.
		if len(note) >= len(content) {
			continue
		}
		out[i].Content = note
		n, err = size(out)
		if err != nil {
			return nil, err
		}
		if n <= budget {
			return out, nil
		}
	}

	// The arguments of a call that already ran are recoverable from its result,
	// and a transcript of a hundred admin_create_task calls carries tens of
	// kilobytes of them.
	for i := range out[:tailStart] {
		trimmed, ok := agentTrimmedToolArguments(out[i])
		if !ok {
			continue
		}
		out[i] = trimmed
		n, err = size(out)
		if err != nil {
			return nil, err
		}
		if n <= budget {
			return out, nil
		}
	}

	// Last resort: drop early turns whole. A turn is an assistant message plus
	// the tool results answering it — split them and the provider rejects the
	// request for a tool result whose call is gone, or a call with no results.
	kept := make([]bool, len(out))
	for i := range kept {
		kept[i] = true
	}
	evicted := 0
	build := func() []providers.Message {
		res := make([]providers.Message, 0, len(out)+1)
		for i := range out {
			if kept[i] {
				res = append(res, out[i])
			}
		}
		return agentWithEvictionNote(res, evicted)
	}
	for _, group := range groups {
		if group[0] >= tailStart || agentGroupIsAnchor(out, group) {
			continue
		}
		for _, i := range group {
			kept[i] = false
		}
		evicted++
		candidate := build()
		n, err = size(candidate)
		if err != nil {
			return nil, err
		}
		if n <= budget {
			return candidate, nil
		}
	}
	return nil, fmt.Errorf("история и схемы инструментов превышают безопасный размер контекста; начните новый диалог с нужным ID игры и исходным файлом (полная история сохранена, размер запроса %d байт)", n)
}

// agentTurnGroups splits the transcript into units that must live or die
// together: any message, followed by every tool result that answers it.
func agentTurnGroups(ms []providers.Message) [][]int {
	var groups [][]int
	for i := 0; i < len(ms); i++ {
		group := []int{i}
		for i+1 < len(ms) && ms[i+1].Role == "tool" {
			i++
			group = append(group, i)
		}
		groups = append(groups, group)
	}
	return groups
}

// agentGroupIsAnchor reports the turns that are never evicted: the system
// prompt and anything the user typed. Those are the task itself — losing them
// costs more than any amount of tool history.
func agentGroupIsAnchor(ms []providers.Message, group []int) bool {
	for _, i := range group {
		if ms[i].Role == "system" || ms[i].Role == "user" {
			return true
		}
	}
	return false
}

// agentTrimmedToolArguments replaces oversized arguments of already-issued tool
// calls, leaving the call id, type and name so the result still has its pair.
// The message is copied down to the FunctionCall pointer: the caller's
// transcript is the saved history and must not be edited through a shared
// pointer.
func agentTrimmedToolArguments(m providers.Message) (providers.Message, bool) {
	var changed bool
	calls := slices.Clone(m.ToolCalls)
	for j := range calls {
		fn := calls[j].Function
		if fn == nil || len(fn.Arguments) <= agentMinCompressibleBytes {
			continue
		}
		replaced := *fn
		replaced.Arguments = fmt.Sprintf(
			`{"_omitted":"%d bytes of arguments; this call already ran, its result is below"}`, len(fn.Arguments))
		calls[j].Function = &replaced
		changed = true
	}
	if !changed {
		return m, false
	}
	m.ToolCalls = calls
	return m, true
}

// agentWithEvictionNote records the gap in the system prompt rather than as a
// message of its own, so the transcript keeps its role order intact.
func agentWithEvictionNote(ms []providers.Message, evicted int) []providers.Message {
	if evicted == 0 {
		return ms
	}
	// Same rule as the excerpt note: on a transcript that no longer fits, "read
	// it again" is an instruction to thrash, and a read the guard has already
	// answered is refused anyway.
	note := fmt.Sprintf("\n[Context trimmed: %d earlier tool steps were removed from this request. "+
		"The full history is kept outside the request; do not infer what they contained. "+
		"Write your conclusions into your reply as you go — re-reading evicts something else.]", evicted)
	if len(ms) > 0 && ms[0].Role == "system" {
		out := slices.Clone(ms)
		out[0].Content += note
		return out
	}
	return append([]providers.Message{{Role: "system", Content: strings.TrimSpace(note)}}, ms...)
}

func validateAgentResponse(response *providers.LLMResponse) error {
	if response == nil {
		return fmt.Errorf("LLM provider returned an empty response")
	}
	switch response.FinishReason {
	case "length", "max_tokens", "max_output_tokens":
		return fmt.Errorf("модель не завершила ответ: достигнут лимит токенов (finish_reason=%s); изменения инструментов не повторялись", response.FinishReason)
	case "error", "failed", "canceled", "cancelled", "content_filter":
		return fmt.Errorf("модель не завершила ответ (finish_reason=%s)", response.FinishReason)
	}
	if len(response.ToolCalls) == 0 && strings.TrimSpace(response.Content) == "" {
		return fmt.Errorf("модель вернула пустой ответ без вызовов инструментов (finish_reason=%s)", response.FinishReason)
	}
	return nil
}
