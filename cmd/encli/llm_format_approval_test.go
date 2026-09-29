package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestToolApprovalActionUnicode(t *testing.T) {
	t.Parallel()
	session := &llmSession{preferRussian: true}
	for _, tc := range []struct{ name, args, want string }{
		{"admin_create_levels", `{"game_id":32055,"count":2}`, "Создаю 2 уровней"},
		{"admin_update_task", `{"game_id":32055,"text":"\u003cb\u003eПривет 🌍\u003c/b\u003e"}`, "Обновляю задание"},
		{"admin_rename_level", `{"name":"Земля — Луна 🌍"}`, "Переименовываю → Земля — Луна 🌍"},
		{"custom_tool", `{"text":"\u003cb\u003e\u041fривет \ud83c\udf0d\u003c/b\u003e","id":9007199254740993}`, `[custom_tool] {"id":9007199254740993,"text":"<b>Привет 🌍</b>"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := formatToolApprovalAction(session, tc.name, tc.args)
			if !utf8.ValidString(got) || strings.Contains(got, "�") || got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

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
