package main

import (
	"context"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

func TestToolProgressHeartbeatAndCompletion(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var statuses []string
		cb := AgentCallbacks{OnStatus: func(phase, message string) {
			if phase != "tool" {
				t.Errorf("phase = %q", phase)
			}
			statuses = append(statuses, message)
		}, Stderrf: func(string, ...any) { t.Error("UI progress must not also emit stderr") }}
		result := runWithToolProgress(t.Context(), cb, &llmSession{preferRussian: true}, "admin_import_scenario", 5*time.Second, func(ctx context.Context) string {
			reportToolProgress(ctx, "Level 2/3", "Уровень 2/3")
			time.Sleep(6 * time.Second)
			return `{"verified":true}`
		})
		if result != `{"verified":true}` {
			t.Fatalf("result contaminated: %s", result)
		}
		if len(statuses) != 2 || !strings.Contains(statuses[1], "5s · Уровень 2/3") {
			t.Fatalf("statuses = %v", statuses)
		}
		time.Sleep(10 * time.Second)
		if len(statuses) != 2 {
			t.Fatalf("late status after completion: %v", statuses)
		}
	})
}

func TestToolProgressCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var statuses []string
		runWithToolProgress(ctx, AgentCallbacks{OnStatus: func(_, message string) { statuses = append(statuses, message) }}, &llmSession{}, "slow_tool", time.Second, func(ctx context.Context) string {
			reportToolProgress(ctx, "Reading", "Чтение")
			cancel()
			time.Sleep(3 * time.Second)
			reportToolProgress(ctx, "Late update", "Позднее обновление")
			return "cancelled"
		})
		if len(statuses) != 1 {
			t.Fatalf("updates after cancellation: %v", statuses)
		}
	})
}

func TestToolProgressStopsOnPanic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		func() {
			defer func() {
				if recover() == nil {
					t.Error("expected panic")
				}
			}()
			runWithToolProgress(t.Context(), AgentCallbacks{OnStatus: func(_, _ string) { calls++ }}, &llmSession{}, "broken_tool", time.Second, func(context.Context) string { panic("failed") })
		}()
		time.Sleep(3 * time.Second)
		if calls != 0 {
			t.Fatalf("heartbeat after panic: %d", calls)
		}
	})
}
