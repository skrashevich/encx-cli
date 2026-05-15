package main

import "testing"

func TestFormatToolApprovalDetailsCreateLevels(t *testing.T) {
	t.Parallel()
	session := &llmSession{preferRussian: true}
	lines := formatToolApprovalDetails(session, "admin_create_levels", `{"game_id":32034,"count":1}`)
	if len(lines) < 2 {
		t.Fatalf("expected multiple detail lines, got %v", lines)
	}
	foundGame, foundCount := false, false
	for _, l := range lines {
		if l == "Игра #32034" {
			foundGame = true
		}
		if l == "Создать новых уровней: 1" {
			foundCount = true
		}
	}
	if !foundGame || !foundCount {
		t.Fatalf("details: %v", lines)
	}
}

func TestFormatToolApprovalActionStripsTimestamp(t *testing.T) {
	t.Parallel()
	action := formatToolApprovalAction(nil, "admin_create_levels", `{"game_id":1,"count":2}`)
	if action == "" {
		t.Fatal("empty action")
	}
	if len(action) > 4 && action[2] == ':' {
		t.Fatalf("should not start with timestamp, got %q", action)
	}
}
