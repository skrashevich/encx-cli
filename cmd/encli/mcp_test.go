package main

import (
	"testing"

	"github.com/skrashevich/encx-cli/agenttools"
)

func TestParseMCPPolicyDefaultsToReadonly(t *testing.T) {
	for _, input := range []string{"", "   "} {
		policy, err := parseMCPPolicy(input)
		if err != nil {
			t.Fatalf("parseMCPPolicy(%q): %v", input, err)
		}
		if policy != agenttools.PolicyReadonly {
			t.Fatalf("an unattended MCP server should default to read-only, got %q", policy)
		}
	}
}

func TestParseMCPPolicyAcceptsExplicitModes(t *testing.T) {
	for input, want := range map[string]agenttools.Policy{
		"readonly": agenttools.PolicyReadonly,
		"approve":  agenttools.PolicyApprove,
		"full":     agenttools.PolicyFull,
		"  full  ": agenttools.PolicyFull,
	} {
		policy, err := parseMCPPolicy(input)
		if err != nil {
			t.Fatalf("parseMCPPolicy(%q): %v", input, err)
		}
		if policy != want {
			t.Fatalf("parseMCPPolicy(%q) = %q, want %q", input, policy, want)
		}
	}
}

func TestParseMCPPolicyRejectsUnknownMode(t *testing.T) {
	if _, err := parseMCPPolicy("yolo"); err == nil {
		t.Fatal("an unknown -security value should be rejected")
	}
}
