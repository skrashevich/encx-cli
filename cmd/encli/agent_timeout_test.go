package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
)

type stalledPicoProvider struct {
	calls int
	delay time.Duration
}

func (*stalledPicoProvider) GetDefaultModel() string { return "test" }
func (p *stalledPicoProvider) Chat(ctx context.Context, _ []providers.Message, _ []providers.ToolDefinition, _ string, _ map[string]any) (*providers.LLMResponse, error) {
	p.calls++

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(p.delay):
		return &providers.LLMResponse{Content: "готово", FinishReason: "stop"}, nil
	}
}

func TestModelSlowResponseDoesNotRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delegate := &stalledPicoProvider{delay: 4 * time.Minute}
		p := &observedPicoProvider{delegate: delegate, session: &llmSession{}, stats: &agentRunStats{}}
		response, err := p.Chat(t.Context(), []providers.Message{{Role: "user", Content: "hello"}}, nil, "test", nil)
		if err != nil || response == nil || response.Content != "готово" || delegate.calls != 1 {
			t.Fatalf("response=%v calls=%d err=%v", response, delegate.calls, err)
		}
	})
}

func TestModelTimeoutDoesNotResubmit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		delegate := &stalledPicoProvider{delay: time.Hour}
		p := &observedPicoProvider{delegate: delegate, session: &llmSession{preferRussian: true}, stats: &agentRunStats{}}
		retries := make(chan string, 10)
		p.cb.OnStatus = func(phase, message string) {
			if phase == "retry" {
				retries <- message
			}
		}
		started := time.Now()
		_, err := p.Chat(t.Context(), []providers.Message{{Role: "user", Content: "hello"}}, nil, "test", nil)
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "провайдер может продолжать") {
			t.Fatalf("err=%v", err)
		}
		if delegate.calls != 1 || time.Since(started) != 10*time.Minute || len(retries) != 0 {
			t.Fatalf("calls=%d elapsed=%s retries=%d", delegate.calls, time.Since(started), len(retries))
		}
	})
}

func TestTimeoutErrorsAreNotRetryable(t *testing.T) {
	for _, err := range []error{context.DeadlineExceeded, fmt.Errorf("request: %w", os.ErrDeadlineExceeded), &providers.FailoverError{Reason: providers.FailoverTimeout}, errors.New("HTTP 504 Gateway Timeout"), errors.New("context deadline exceeded"), errors.New("Client.Timeout exceeded while awaiting headers")} {
		if isRetryableLLMError(err) {
			t.Errorf("timeout is retryable: %v", err)
		}
	}
}

func TestModelCancellationDoesNotRetry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		time.AfterFunc(time.Second, cancel)
		delegate := &stalledPicoProvider{delay: time.Hour}
		p := &observedPicoProvider{delegate: delegate, session: &llmSession{}, stats: &agentRunStats{}}
		_, err := p.Chat(ctx, []providers.Message{{Role: "user", Content: "hello"}}, nil, "test", nil)
		if !errors.Is(err, context.Canceled) || delegate.calls != 1 {
			t.Fatalf("calls=%d err=%v", delegate.calls, err)
		}
	})
}
