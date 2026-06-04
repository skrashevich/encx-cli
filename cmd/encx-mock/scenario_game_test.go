package main

import (
	"testing"
)

func TestResolveScenarioPath(t *testing.T) {
	t.Setenv("ENCX_MOCK_SCENARIO", "/from/env")

	if got := resolveScenarioPath(""); got != "/from/env" {
		t.Fatalf("empty flag: got %q, want /from/env", got)
	}
	if got := resolveScenarioPath("/from/flag"); got != "/from/flag" {
		t.Fatalf("flag set: got %q, want /from/flag", got)
	}
}
