package main

import (
	"strings"
	"testing"
)

func TestUserWantsLevelContentDetail(t *testing.T) {
	if !userWantsLevelContentDetail("дай сводку по сценарию игры") {
		t.Fatal("expected true for summary request")
	}
	if userWantsLevelContentDetail("сколько уровней в игре") {
		t.Fatal("expected false for count-only request")
	}
}

func TestRecordAndMissingLevels(t *testing.T) {
	s := &llmSession{preferRussian: true}
	recordLevelEnumeration(s, `{"count":3,"levels":[{"number":1},{"number":2},{"number":3}]}`)
	markLevelContentLoaded(s, "admin_level_content", `{"level_number":1}`)
	missing := missingLevelsForContentSummary(s, "сводка сценария")
	if len(missing) != 2 || missing[0] != 2 || missing[1] != 3 {
		t.Fatalf("missing: %v", missing)
	}
}

func TestMissingLevelsListOnlyQuery(t *testing.T) {
	s := &llmSession{}
	recordLevelEnumeration(s, `{"count":2,"levels":[{"number":1},{"number":2}]}`)
	missing := missingLevelsForContentSummary(s, "список уровней")
	if len(missing) != 0 {
		t.Fatalf("expected no gate for list-only: %v", missing)
	}
}

func TestAdminLevelsNoteInSummary(t *testing.T) {
	raw := `[{"number":1,"id":10,"name":"A"}]`
	out, ok := summarizeToolResult("admin_levels", raw)
	if !ok {
		t.Fatal("expected summary")
	}
	if !strings.Contains(out, "admin_level_content") {
		t.Fatalf("expected follow-up hint: %s", out)
	}
}

func TestToolResultLooksLikeError(t *testing.T) {
	if !toolResultLooksLikeError(`{"error":"nope"}`) {
		t.Fatal("expected error")
	}
	if toolResultLooksLikeError(`{"count":1}`) {
		t.Fatal("expected ok")
	}
}
