package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type apiErrorProvider struct{ err error }

func (p apiErrorProvider) GetDefaultModel() string { return "test" }
func (p apiErrorProvider) Chat(context.Context, []providers.Message, []providers.ToolDefinition, string, map[string]any) (*providers.LLMResponse, error) {
	return nil, p.err
}

func TestChatWithWaitPreservesAPIErrorDetail(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", nil)
	for _, tc := range []struct{ name, body, detail string }{
		{"detail", `{"detail":"Model unavailable for this account"}`, "Model unavailable for this account"},
		{"non-json", "<html>gateway error</html>", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apiErr := &openai.Error{Request: req, Response: &http.Response{StatusCode: 400, Body: io.NopCloser(strings.NewReader(tc.body))}, StatusCode: 400}
			original := fmt.Errorf("codex API call: %w", apiErr)
			p := observedPicoProvider{delegate: apiErrorProvider{original}}
			_, err := p.chatWithWait(t.Context(), nil, nil, "test", nil)
			if !errors.Is(err, apiErr) {
				t.Fatalf("lost original error: %v", err)
			}
			if tc.detail != "" && !strings.Contains(err.Error(), tc.detail) {
				t.Fatalf("missing API detail: %v", err)
			}
			if tc.detail == "" && err != original {
				t.Fatalf("unexpected replacement: %v", err)
			}
		})
	}
}
