package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const jevTestModify = `{"model":"jev-test","answers":{"intent":{"type":"choice","choice":"modify","confidence":0.95},"explicit_change":{"type":"noul","noul":0.95}}}`
const jevTestAudit = `{"model":"jev-test","answers":{"intent":{"type":"choice","choice":"audit","confidence":0.95},"explicit_change":{"type":"noul","noul":0.01}}}`
const jevTestAligned = `{"model":"jev-test","answers":{"alignment":{"type":"choice","choice":"within_scope","confidence":0.95},"unrequested_work":{"type":"noul","noul":0.01}}}`
const jevTestOutside = `{"model":"jev-test","answers":{"alignment":{"type":"choice","choice":"outside_scope","confidence":0.95},"unrequested_work":{"type":"noul","noul":0.95}}}`

func installJEVTransport(t *testing.T, classify, check string) *[]map[string]any {
	t.Helper()
	requests := new([]map[string]any)
	previous := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = previous })
	http.DefaultTransport = polzaRoutingTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != polzaBaseURL+"/systemone" {
			t.Fatalf("unexpected request to %s", r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		*requests = append(*requests, body)
		result := check
		if _, exists := body["questions"].(map[string]any)["intent"]; exists {
			result = classify
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(result))}, nil
	})
	return requests
}

func TestJEVActivationAndProviderSwitch(t *testing.T) {
	requests := installJEVTransport(t, jevTestModify, jevTestAligned)
	input := &AgentRunInput{Session: &llmSession{}, Messages: []llmMessage{{Role: "user", Content: "Измени подсказку"}}}
	startJEVTurn(t.Context(), AgentConfig{BaseURL: polzaBaseURL, APIKey: "test"}, input, AgentCallbacks{})
	if input.Session.jev == nil || len(*requests) != 1 {
		t.Fatal("Polza did not activate JEV")
	}
	for _, cfg := range []AgentConfig{
		{BaseURL: defaultLLMBaseURL, APIKey: "test"}, {BaseURL: "http://127.0.0.1/v1"},
		{BaseURL: polzaBaseURL, AuthMethod: authMethodCodex}, {BaseURL: polzaBaseURL, AuthMethod: authMethodGigaChat},
	} {
		startJEVTurn(t.Context(), cfg, input, AgentCallbacks{})
		if input.Session.jev != nil || len(*requests) != 1 {
			t.Fatalf("JEV leaked into provider %+v", cfg)
		}
	}
}

func TestJEVAuditBlocksDirectWriteAndQueuesFix(t *testing.T) {
	requests := installJEVTransport(t, jevTestAudit, jevTestAligned)
	input := &AgentRunInput{Cfg: &config{gameId: 42}, Session: &llmSession{}, Messages: []llmMessage{{Role: "user", Content: "Проверь подсказку"}}}
	startJEVTurn(t.Context(), AgentConfig{BaseURL: polzaBaseURL, APIKey: "test"}, input, AgentCallbacks{})
	r := &picoLegacyToolRuntime{input: input, stats: &agentRunStats{}}
	result := r.execute(t.Context(), "admin_set_comment", `{"game_id":42,"level_number":1,"text":"changed"}`)
	if !result.IsError || !strings.Contains(result.ForLLM, "jev_scope_propose") || len(*requests) != 1 {
		t.Fatalf("write was not stopped: %+v", result)
	}
	result = r.execute(t.Context(), "propose_admin_fix", `{"title":"Исправить подсказку","summary":"Опечатка","steps":[{"tool":"admin_set_comment","arguments":{"game_id":42,"level_number":1,"text":"fixed"}}]}`)
	if result.IsError || len(input.Session.pendingFixes) != 1 || len(*requests) != 2 {
		t.Fatalf("proposal was not queued: %+v", result)
	}
}

func TestJEVRejectsUnrelatedReadBeforeExecution(t *testing.T) {
	installJEVTransport(t, jevTestModify, jevTestOutside)
	input := &AgentRunInput{Cfg: &config{}, Session: &llmSession{}, Messages: []llmMessage{{Role: "user", Content: "Измени подсказку"}}}
	startJEVTurn(t.Context(), AgentConfig{BaseURL: polzaBaseURL, APIKey: "test"}, input, AgentCallbacks{})
	r := &picoLegacyToolRuntime{input: input, stats: &agentRunStats{}}
	result := r.execute(t.Context(), "list_local_dir", `{"path":"."}`)
	if !result.IsError || !strings.Contains(result.ForLLM, "jev_scope_deny") {
		t.Fatalf("unrelated read allowed: %+v", result)
	}
}

func TestJEVUnavailableRequiresOneApprovalAndRespectsDenial(t *testing.T) {
	installJEVTransport(t, `{}`, `{}`)
	input := &AgentRunInput{Cfg: &config{}, Session: &llmSession{}, Messages: []llmMessage{{Role: "user", Content: "Выйди из аккаунта"}}}
	startJEVTurn(t.Context(), AgentConfig{BaseURL: polzaBaseURL, APIKey: "test"}, input, AgentCallbacks{})
	approvals := 0
	r := &picoLegacyToolRuntime{input: input, stats: &agentRunStats{}, cb: AgentCallbacks{ApproveToolCall: func(_ context.Context, name, args string) (bool, error) { approvals++; return false, nil }}}
	result := r.execute(t.Context(), "logout", `{}`)
	if approvals != 1 || !strings.Contains(result.ForLLM, "user denied") {
		t.Fatalf("approval=%d result=%+v", approvals, result)
	}
	input.Session.securityMode = SecurityModeReadonly
	result = r.execute(t.Context(), "logout", `{}`)
	if approvals != 1 || !result.IsError || !strings.Contains(result.ForLLM, "read-only") {
		t.Fatalf("readonly bypass: %+v", result)
	}
}
