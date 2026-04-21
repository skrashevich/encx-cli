package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func callLLM(ctx context.Context, apiURL, apiKey, model string, messages []llmMessage, tools []llmTool) (*llmResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 600*time.Second)
	defer cancel()

	req := llmRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	debugf("llm http request: url=%s model=%s messages=%d tools=%d bytes=%d", apiURL, model, len(messages), len(tools), len(body))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	// Progress indicator so the user knows we're waiting for the LLM.
	waitDone := make(chan struct{})
	defer close(waitDone)
	go func() {
		start := time.Now()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(os.Stderr, "[llm] ожидание ответа... %s\n", time.Since(start).Round(time.Second))
			case <-waitDone:
				return
			}
		}
	}()

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	debugf("llm http response: status=%d bytes=%d", resp.StatusCode, len(respBody))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, parseLLMErrorMessage(respBody))
	}

	var llmResp llmResponse
	if err := json.Unmarshal(respBody, &llmResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if llmResp.Error != nil {
		return nil, fmt.Errorf("%s", llmResp.Error.Message)
	}

	return &llmResp, nil
}

func isRetryableLLMError(err error) bool {
	s := err.Error()
	// Permanent API errors — never retry.
	for _, perm := range []string{"unknown provider", "invalid model", "model not found", "unauthorized", "forbidden"} {
		if strings.Contains(strings.ToLower(s), perm) {
			return false
		}
	}
	return strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "HTTP 429") ||
		strings.Contains(s, "HTTP 502") ||
		strings.Contains(s, "HTTP 503") ||
		strings.Contains(s, "HTTP 504") ||
		strings.Contains(s, "connection reset") ||
		strings.Contains(s, "EOF")
}

func parseLLMErrorMessage(respBody []byte) string {
	var apiErr llmErrorEnvelope
	if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Error != nil && apiErr.Error.Message != "" {
		return apiErr.Error.Message
	}
	if msg := strings.TrimSpace(string(respBody)); msg != "" {
		return msg
	}
	return "empty error response"
}
