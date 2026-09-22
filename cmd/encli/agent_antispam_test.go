package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/encx-cli/encx"
)

func TestAgentAntiSpamPausesBatchAndPreservesHistory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Location", "/NotHumanRequest.aspx?return=%2f")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	cfg := &config{domain: strings.TrimPrefix(srv.URL, "http://")}
	client := encx.New(cfg.domain, encx.WithHTTP(), encx.WithEngine(encx.EngineLegacy))
	saveSession(cfg, client)
	provider := &scriptedPicoProvider{responses: []*providers.LLMResponse{
		{FinishReason: "tool_calls", ToolCalls: []providers.ToolCall{
			{ID: "first", Name: "admin_games", Arguments: map[string]any{}},
			{ID: "second", Name: "admin_games", Arguments: map[string]any{}},
		}},
		{Content: "unexpected extra model request", FinishReason: "stop"},
	}}
	input := &AgentRunInput{Cfg: cfg, Client: client, Session: &llmSession{preferRussian: true}, Tools: getTools(), Messages: []llmMessage{{Role: "user", Content: "Покажи игры"}}}
	_, err := runAgentLoop(t.Context(), AgentConfig{Provider: provider, AuthMethod: authMethodCodex}, input, AgentCallbacks{})
	if err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("sent %d requests after challenge; want 1", got)
	}
	if len(provider.calls) != 1 {
		t.Errorf("model was called again after challenge")
	}
	count := 0
	skipped := 0
	for _, m := range input.Messages {
		if m.Role != "tool" {
			continue
		}
		count++
		var result map[string]any
		if err := json.Unmarshal([]byte(m.Content), &result); err != nil {
			t.Fatal(err)
		}
		if result["code"] != "antispam_required" || result["verification_url"] != srv.URL+"/NotHumanRequest.aspx?return=%2f" {
			t.Errorf("lost challenge: %s", m.Content)
		}
		if result["skipped"] == true {
			skipped++
		}
	}
	if count != 2 {
		t.Errorf("lost batch history: %d results", count)
	}
	if skipped != 1 {
		t.Errorf("want one unexecuted call marked skipped, got %d", skipped)
	}
	final := input.Messages[len(input.Messages)-1].Content
	if !strings.Contains(final, srv.URL+"/NotHumanRequest.aspx") {
		t.Errorf("missing verification link: %s", final)
	}
	// A new user turn must be allowed to check again after manual verification.
	input.Messages = append(input.Messages, llmMessage{Role: "user", Content: "Проверку прошёл, продолжай"})
	next := &scriptedPicoProvider{responses: []*providers.LLMResponse{{Content: "продолжаю", FinishReason: "stop"}}}
	_, err = runAgentLoop(t.Context(), AgentConfig{Provider: next, AuthMethod: authMethodCodex}, input, AgentCallbacks{})
	if err != nil || len(next.calls) != 1 {
		t.Fatalf("next turn still blocked: calls=%d err=%v", len(next.calls), err)
	}
}

func TestAgentAntiSpamInsideSectorUpdate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writes.Add(1)
		}
		if r.URL.Path == "/Administration/Games/LevelManager.aspx" {
			_, _ = w.Write([]byte("<html>Administration</html>"))
			return
		}
		w.Header().Set("Location", "/NotHumanRequest.aspx?return=%2f")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	cfg := &config{domain: strings.TrimPrefix(srv.URL, "http://")}
	client := encx.New(cfg.domain, encx.WithHTTP(), encx.WithEngine(encx.EngineLegacy))
	saveSession(cfg, client)
	session := &llmSession{}
	raw := executeToolCallSafe(t.Context(), cfg, client, session, "admin_update_sector", `{"game_id":82908,"level_number":4,"sector_id":3528293,"answers":["фактор"]}`)
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if result["code"] != "antispam_required" || session.antiSpamResult == "" {
		t.Fatalf("sector read failure lost anti-spam type: %s", raw)
	}
	if writes.Load() != 0 {
		t.Fatal("updated sector without reading current state")
	}
}

func TestAgentLoginRejectsPlaceholderWithoutChangingCredentials(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); w.WriteHeader(http.StatusForbidden) }))
	defer srv.Close()
	cfg := &config{domain: strings.TrimPrefix(srv.URL, "http://"), login: "original", password: "original-password"}
	client := encx.New(cfg.domain, encx.WithHTTP(), encx.WithEngine(encx.EngineLegacy))
	result := executeToolCallSafe(t.Context(), cfg, client, &llmSession{}, "login", `{"login":"user","password":"__ASK_USER__"}`)
	if requests.Load() != 0 {
		t.Error("sent a fabricated password to the server")
	}
	if cfg.login != "original" || cfg.password != "original-password" {
		t.Error("placeholder overwrote credentials")
	}
	if !toolResultLooksLikeError(result) {
		t.Fatalf("expected actionable error: %s", result)
	}
}
