package main

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type toolProgressKey struct{}

// reportToolProgress keeps intermediate status out of the captured JSON result.
func reportToolProgress(ctx context.Context, english, russian string) {
	if report, ok := ctx.Value(toolProgressKey{}).(func(string, string)); ok {
		report(english, russian)
	}
}

// runWithToolProgress joins the heartbeat before returning, so a completed tool
// cannot overwrite the status of the next tool or the final assistant response.
func runWithToolProgress(ctx context.Context, cb AgentCallbacks, session *llmSession, name string, interval time.Duration, run func(context.Context) string) string {
	started := time.Now()
	var mu sync.Mutex
	detail := session.reviewText("Waiting for operation", "Ожидание выполнения операции")
	publish := func() {
		message := fmt.Sprintf("%s · %s · %s", name, time.Since(started).Round(time.Second), detail)
		if cb.OnStatus != nil {
			cb.OnStatus("tool", message)
		} else {
			stderrAgentf(cb, "%s\n", message)
		}
	}
	report := func(english, russian string) {
		mu.Lock()
		defer mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		detail = session.reviewText(english, russian)
		publish()
	}
	ctx = context.WithValue(ctx, toolProgressKey{}, report)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				mu.Lock()
				if ctx.Err() == nil {
					publish()
				}
				mu.Unlock()
			}
		}
	}()
	defer func() { close(stop); <-done }()
	return run(ctx)
}
