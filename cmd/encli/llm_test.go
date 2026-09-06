package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestGetToolsIncludesAdminLevelContent(t *testing.T) {
	t.Parallel()

	for _, tool := range getTools() {
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

// The start and the request settings are editable through the CLI on both
// engines, so the agent has to be able to reach them too.
func TestAdminUpdateGameToolOffersStartAndRequestSettings(t *testing.T) {
	t.Parallel()

	for _, tool := range getTools() {
		if tool.Function.Name != "admin_update_game" {
			continue
		}
		params := string(tool.Function.Parameters)
		for _, key := range []string{"start", "request_last_date", "moderated"} {
			if !strings.Contains(params, `"`+key+`"`) {
				t.Errorf("admin_update_game does not take %q: %s", key, params)
			}
		}
		return
	}
	t.Fatal("admin_update_game tool is not registered")
}

func TestModeratedArgReadsBothSpellings(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		raw         any
		want, wasOk bool
	}{
		{true, true, true},
		{false, false, true},
		{"true", true, true},
		{"0", false, true},
		{"maybe", false, false},
		{nil, false, false},
	} {
		got, ok := moderatedArg(tc.raw)
		if got != tc.want || ok != tc.wasOk {
			t.Errorf("moderatedArg(%#v) = %v, %v; want %v, %v", tc.raw, got, ok, tc.want, tc.wasOk)
		}
	}
}

func TestGetToolsExposesProposalAlongsideMutations(t *testing.T) {
	t.Parallel()

	found := map[string]bool{}
	for _, tool := range getTools() {
		found[tool.Function.Name] = true
	}
	for _, name := range []string{"propose_admin_fix", "admin_create_game", "admin_create_sector", "admin_set_comment"} {
		if !found[name] {
			t.Fatalf("tool %q is not exposed", name)
		}
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
