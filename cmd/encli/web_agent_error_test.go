package main

import (
	"strings"
	"testing"
)

func TestWebAgentErrorSurvivesReload(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	store := NewChatStore()
	snap := store.Create("demo.en.cx", 1, SecurityModeFull)
	h := &webHub{store: store, sse: newSSEHub()}
	thread, unlock, _ := store.LockThread(snap.ID)
	h.handleAgentEvent(snap.ID, thread, AgentEvent{Type: agentEventError, Message: "model output limit"})
	unlock()
	restored := NewChatStore()
	if err := restored.LoadFromDisk(); err != nil {
		t.Fatal(err)
	}
	snap, _ = restored.Get(snap.ID)
	if len(snap.Messages) != 1 || !strings.Contains(snap.Messages[0].Content, "model output limit") {
		t.Fatalf("error missing from transcript: %+v", snap.Messages)
	}
}
