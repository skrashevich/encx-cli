package main

import (
	"encoding/json/v2"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// Use encoded bytes as a conservative token upper bound, including tool schemas.
// Leave room for the response and the provider's message framing in a 128K window.
const agentRequestByteBudget = 112 * 1024

// Bound only the outgoing request. The complete transcript remains on disk and
// in the tool loop; tool call IDs, arguments and user instructions stay intact.
func boundedAgentMessages(messages []providers.Message, tools []providers.ToolDefinition) ([]providers.Message, error) {
	size := func(ms []providers.Message) (int, error) {
		b, err := json.Marshal(struct {
			Messages []providers.Message
			Tools    []providers.ToolDefinition
		}{ms, tools})
		return len(b), err
	}
	n, err := size(messages)
	if err != nil {
		return nil, err
	}
	if n <= agentRequestByteBudget {
		return messages, nil
	}
	out := slices.Clone(messages)
	for i := range out {
		if out[i].Role != "tool" || len(out[i].Content) <= 320 {
			continue
		}
		content := out[i].Content
		end := 64
		for !utf8.ValidString(content[:end]) {
			end--
		}
		note := fmt.Sprintf("[Excerpt; omitted result of %d bytes to fit context. Full history kept. Re-read needed page/offset or level; do not infer omitted content.]\n%s", len(content), content[:end])
		// Do not replace a small result with a larger annotation.
		if len(note) >= len(content) {
			continue
		}
		out[i].Content = note
		n, err = size(out)
		if err != nil {
			return nil, err
		}
		if n <= agentRequestByteBudget {
			return out, nil
		}
	}
	return nil, fmt.Errorf("история и схемы инструментов превышают безопасный размер контекста; начните новый диалог с нужным ID игры и исходным файлом (полная история сохранена, размер запроса %d байт)", n)
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
