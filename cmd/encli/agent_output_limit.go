package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// Retry only the unexecuted model step, never the tool loop or whole user turn.
const agentMaxOutputLimitRetries = 3

type agentOutputLimitError struct{ reason string }

func (e *agentOutputLimitError) Error() string {
	return fmt.Sprintf("модель не завершила ответ: достигнут лимит токенов (finish_reason=%s); вызовы инструментов из обрезанного ответа не выполнялись", e.reason)
}

func (p *observedPicoProvider) Chat(ctx context.Context, messages []providers.Message, toolDefs []providers.ToolDefinition, model string, options map[string]any) (*providers.LLMResponse, error) {
	// Recovery instructions belong only to this request. Keep the original
	// transcript for persistence, including completed tool calls and results.
	defer func() { p.seen = messages }()
	request := messages
	for attempt := 0; ; attempt++ {
		response, err := p.chat(ctx, request, toolDefs, model, options)
		var limited *agentOutputLimitError
		if !errors.As(err, &limited) {
			return response, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if attempt == agentMaxOutputLimitRetries {
			return nil, fmt.Errorf("модель исчерпала лимит ответа после %d попыток продолжения; выполненные действия сохранены: %w", agentMaxOutputLimitRetries, err)
		}
		emitStatus(p.cb, "continue", p.session.reviewText(
			"Output limit reached; continuing the unfinished step in smaller parts…",
			"Достигнут лимит ответа; продолжаю незавершённый шаг небольшими частями…",
		))
		// No content or tool calls from the truncated response are trusted as
		// complete. Rebuild from the SAME history, with one bounded instruction;
		// never splice incomplete JSON or accumulate draft scenario text.
		request = make([]providers.Message, 0, len(messages)+1)
		request = append(request, providers.Message{Role: "system", Content: fmt.Sprintf(
			"The previous response hit the output token limit and was discarded (recovery %d/%d). "+
				"NONE of the tool calls in that discarded response were executed. Completed tool calls and results in the conversation remain authoritative: do not repeat them or recreate existing games or levels. "+
				"Continue the unfinished task from those results. Make at most ONE tool call per response, with concise arguments and no lengthy planning prose. "+
				"Build a game incrementally: create the game once, then individual levels, tasks, answers and hints in separate calls, preserving requested content and numbering. "+
				"Do not generate the whole scenario in one response. If an individual operation is too large, use supported incremental operations; never invent parameters or silently omit required content. "+
				"If no tools remain necessary, return a concise complete final answer, not a continuation of the discarded draft.", attempt+1, agentMaxOutputLimitRetries)})
		request = append(request, messages...)
	}
}
