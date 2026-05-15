package main

import (
	"strings"
	"testing"
)

func TestIsMutationTool(t *testing.T) {
	t.Parallel()
	mutators := []string{
		"admin_create_sector", "admin_delete_level", "send_code", "enter", "login", "propose_admin_fix",
	}
	for _, name := range mutators {
		if !isMutationTool(name) {
			t.Fatalf("%q should be a mutation tool", name)
		}
	}
	readers := []string{"status", "admin_levels", "admin_level_content", "wikipedia_search", "read_local_file"}
	for _, name := range readers {
		if isMutationTool(name) {
			t.Fatalf("%q should not be a mutation tool", name)
		}
	}
}

func TestGetToolsForSessionReadonlyHidesMutations(t *testing.T) {
	t.Parallel()
	session := &llmSession{securityMode: SecurityModeReadonly}
	for _, tool := range getToolsForSession(session) {
		if isMutationTool(tool.Function.Name) {
			t.Fatalf("readonly mode should not expose %q", tool.Function.Name)
		}
	}
}

func TestSecurityBlocksMutationReadonly(t *testing.T) {
	t.Parallel()
	session := &llmSession{securityMode: SecurityModeReadonly}
	if !securityBlocksMutation(session, "admin_delete_level") {
		t.Fatal("expected admin_delete_level to be blocked")
	}
	if securityBlocksMutation(session, "status") {
		t.Fatal("status should not be blocked")
	}
}

func TestExecuteToolCallSafeReadonlyBlocksMutation(t *testing.T) {
	t.Parallel()
	session := &llmSession{securityMode: SecurityModeReadonly, preferRussian: true}
	result := executeToolCallSafe(t.Context(), &config{}, nil, session, "send_code", `{"game_id":1,"code":"X"}`)
	if !strings.Contains(result, "read-only") && !strings.Contains(result, "Только чтение") && !strings.Contains(result, "blocked") {
		t.Fatalf("expected readonly error, got %q", result)
	}
}

func TestParseAgentSecurityMode(t *testing.T) {
	t.Parallel()
	for _, mode := range []AgentSecurityMode{SecurityModeFull, SecurityModeReadonly, SecurityModeApprove} {
		got, ok := parseAgentSecurityMode(string(mode))
		if !ok || got != mode {
			t.Fatalf("parse %q: got %q ok=%v", mode, got, ok)
		}
	}
	if _, ok := parseAgentSecurityMode("nope"); ok {
		t.Fatal("expected invalid mode to fail")
	}
}
