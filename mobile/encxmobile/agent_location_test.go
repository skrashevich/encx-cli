package encxmobile

import (
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/encx-cli/agenttools"
)

// newLocationTestSession builds a session with the location tool enabled.
func newLocationTestSession(t *testing.T, provider providers.LLMProvider) *AgentSession {
	t.Helper()
	cfg, policy, err := parseAgentConfig(`{"model":"scripted","policy":"readonly","location_tools":true}`)
	if err != nil {
		t.Fatalf("parseAgentConfig: %v", err)
	}
	session, err := newAgentSession(newFakeEngine(), cfg, policy, provider)
	if err != nil {
		t.Fatalf("newAgentSession: %v", err)
	}
	return session
}

func TestLocationToolDeliversTheHostPosition(t *testing.T) {
	provider := &scriptedProvider{responses: []*providers.LLMResponse{
		toolCallResponse(locationToolName, `{}`),
		{Content: "Вы на Красной площади."},
	}}
	session := newLocationTestSession(t, provider)
	delegate := &recordingDelegate{
		session:      session,
		locationJSON: `{"latitude":55.7539,"longitude":37.6208,"accuracy_m":8}`,
	}
	session.SetDelegate(delegate)

	reply, err := session.SendMessage("где я?")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "Вы на Красной площади." {
		t.Fatalf("unexpected reply: %q", reply)
	}

	types := delegate.eventTypes()
	if !hasEvent(types, "tool_started") || !hasEvent(types, "tool_finished") {
		t.Fatalf("the location call should be observed like any tool, got %v", types)
	}
	for _, event := range delegate.events {
		if event["type"] == "tool_finished" {
			if isError, _ := event["is_error"].(bool); isError {
				t.Fatalf("a resolved location should not be an error: %v", event)
			}
			result, _ := event["result"].(string)
			if !strings.Contains(result, "55.7539") {
				t.Fatalf("the position should reach the model verbatim, got %q", result)
			}
		}
	}
}

func TestLocationToolFailureDoesNotFailTheTurn(t *testing.T) {
	provider := &scriptedProvider{responses: []*providers.LLMResponse{
		toolCallResponse(locationToolName, `{}`),
		{Content: "Без геолокации не скажу."},
	}}
	session := newLocationTestSession(t, provider)
	delegate := &recordingDelegate{
		session:         session,
		locationFailure: "location permission denied",
	}
	session.SetDelegate(delegate)

	reply, err := session.SendMessage("где я?")
	if err != nil {
		t.Fatalf("a failed location read should stay inside the turn: %v", err)
	}
	if reply == "" {
		t.Fatal("the model should still answer after a failed location read")
	}

	sawError := false
	for _, event := range delegate.events {
		if event["type"] != "tool_finished" {
			continue
		}
		isError, _ := event["is_error"].(bool)
		result, _ := event["result"].(string)
		if isError && strings.Contains(result, "permission denied") {
			sawError = true
		}
	}
	if !sawError {
		t.Fatal("the failure message should reach the model as a tool error")
	}
}

func TestLocationToolIsOffByDefault(t *testing.T) {
	session := newTestSession(t, newFakeEngine(), agenttools.PolicyReadonly, &scriptedProvider{})
	for _, def := range session.registry.ToProviderDefs() {
		if def.Function.Name == locationToolName {
			t.Fatal("the location tool should not be registered without location_tools")
		}
	}
}

func TestResolveLocationWithoutARequestFails(t *testing.T) {
	session := newLocationTestSession(t, &scriptedProvider{})
	if err := session.ResolveLocation("call-404", `{"latitude":1}`); err == nil {
		t.Fatal("resolving an unknown request should fail")
	}
	if err := session.FailLocation("call-404", "whatever"); err == nil {
		t.Fatal("failing an unknown request should fail")
	}
	if err := session.ResolveLocation("call-404", "   "); err == nil {
		t.Fatal("an empty payload should be rejected")
	}
}
