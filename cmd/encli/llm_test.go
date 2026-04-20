package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestGetToolsIncludesAdminLevelContent(t *testing.T) {
	t.Parallel()

	for _, tool := range getTools(false) {
		if tool.Function.Name == "admin_level_content" {
			return
		}
	}

	t.Fatal("admin_level_content tool is not registered")
}

func TestPrintCommandHelpIncludesAdminLevelContent(t *testing.T) {
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
	}()

	printCommandHelp("admin-level-content")

	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(out), "admin-level-content") {
		t.Fatalf("help output does not mention command, got: %q", string(out))
	}
}

func TestGetToolsReviewModeAddsProposalAndBlocksMutations(t *testing.T) {
	t.Parallel()

	var hasProposal bool
	for _, tool := range getTools(true) {
		switch tool.Function.Name {
		case "propose_admin_fix":
			hasProposal = true
		case "admin_create_sector", "admin_delete_sector", "admin_set_comment":
			t.Fatalf("review mode should not expose mutation tool %q", tool.Function.Name)
		}
	}
	if !hasProposal {
		t.Fatal("review mode does not expose propose_admin_fix")
	}
}

func TestIsReviewApprovalPrompt(t *testing.T) {
	t.Parallel()

	if !isReviewApprovalPrompt("пройдись по уровням ещё раз, убедись что ответы залиты правильно") {
		t.Fatal("expected Russian review prompt to enable approval mode")
	}
	if isReviewApprovalPrompt("создай 3 новых уровня с бонусами") {
		t.Fatal("create prompt should not enable review approval mode")
	}
}

func TestParsePendingAdminFixInjectsGameID(t *testing.T) {
	t.Parallel()

	fix, err := parsePendingAdminFix(map[string]any{
		"title":   "Fix wrong answer",
		"summary": "Uploaded answer does not match the task",
		"steps": []any{
			map[string]any{
				"tool": "admin_create_sector",
				"arguments": map[string]any{
					"level_number": float64(2),
					"name":         "Password by regex",
					"answers":      []any{"Hunter2!"},
				},
			},
		},
	}, 82034)
	if err != nil {
		t.Fatalf("parsePendingAdminFix returned error: %v", err)
	}
	if got := getAnyInt(fix.Steps[0].Arguments["game_id"]); got != 82034 {
		t.Fatalf("expected injected game_id 82034, got %d", got)
	}
}
