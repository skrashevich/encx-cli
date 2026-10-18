package main

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
)

func testClaudeToken() string { return "sk-ant-oat01-" + strings.Repeat("x", 90) }

func TestClaudeCredentialLifecycle(t *testing.T) {
	isolateLLMEnv(t)
	if _, err := loadClaudeCredential(); !errors.Is(err, errNoClaudeCredential) {
		t.Fatalf("missing credential: %v", err)
	}
	token := testClaudeToken()
	if err := saveClaudeCredential(" \n" + token + "\n"); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{claudeAuthFile(): 0600, filepath.Dir(claudeAuthFile()): 0700} {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != mode {
			t.Fatalf("permissions %s: %v", path, st.Mode())
		}
	}
	if credentialPathCollides(claudeAuthFile()) {
		t.Fatal("credential exposed as Encounter session")
	}
	for _, invalid := range []string{"", "sk-ant-api03-" + strings.Repeat("x", 90), "sk-ant-oat01-short", token + "\nextra"} {
		if err := saveClaudeCredential(invalid); err == nil {
			t.Fatal("accepted invalid token")
		}
	}
	cred, err := loadClaudeCredential()
	if err != nil || cred.AccessToken != token {
		t.Fatalf("previous token lost: %v", err)
	}
	if err := os.Chmod(claudeAuthFile(), 0644); err != nil {
		t.Fatal(err)
	}
	if err := saveClaudeCredential(token); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(claudeAuthFile())
	if st.Mode().Perm() != 0600 {
		t.Fatal("replacement didn't repair permissions")
	}
	for range 2 {
		if err := deleteClaudeCredential(); err != nil {
			t.Fatal(err)
		}
	}
	if hasClaudeCredential() {
		t.Fatal("still configured after logout")
	}
}

func TestClaudeEnvironmentAndStatus(t *testing.T) {
	isolateLLMEnv(t)
	fileToken := testClaudeToken()
	if err := saveClaudeCredential(fileToken); err != nil {
		t.Fatal(err)
	}
	envToken := fileToken + "env"
	t.Setenv(claudeTokenEnvVar, envToken)
	cred, err := loadClaudeCredential()
	if err != nil || cred.AccessToken != envToken {
		t.Fatalf("env priority: %v", err)
	}
	if err := deleteClaudeCredential(); err != nil {
		t.Fatal(err)
	}
	status := claudeStatus()
	if !status.Configured || status.Source != "env" {
		t.Fatalf("status: %+v", status)
	}
	raw, _ := json.Marshal(status)
	if strings.Contains(string(raw), envToken) {
		t.Fatal("status discloses token")
	}
	t.Setenv(claudeTokenEnvVar, "bad")
	if _, err := loadClaudeCredential(); err == nil {
		t.Fatal("invalid env silently ignored")
	}
	t.Setenv(claudeTokenEnvVar, "")
	if err := os.WriteFile(claudeAuthFile(), []byte(`{"access_token":`+fileToken), 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(claudeStatus())
	if strings.Contains(string(raw), fileToken) {
		t.Fatal("parse error discloses token")
	}
}

func TestClaudeConfigResolution(t *testing.T) {
	isolateLLMEnv(t)
	if _, err := resolveAgentConfig(&config{llmAuth: authMethodClaude}); !errors.Is(err, errNoClaudeCredential) {
		t.Fatalf("missing token: %v", err)
	}
	if err := saveClaudeCredential(testClaudeToken()); err != nil {
		t.Fatal(err)
	}
	cfg, err := resolveAgentConfig(nil)
	if err != nil || cfg.AuthMethod != authMethodClaude || cfg.Model != defaultClaudeModel {
		t.Fatalf("auto: %+v %v", cfg, err)
	}
	t.Setenv("LLM_BASE_URL", "http://localhost:9999/v1")
	cfg, err = resolveAgentConfig(nil)
	if err != nil || cfg.AuthMethod == authMethodClaude {
		t.Fatalf("hijacked proxy: %+v %v", cfg, err)
	}
	t.Setenv("LLM_API_KEY", "other-key")
	t.Setenv("LLM_MODEL", "anthropic/claude-opus-4.6")
	cfg, err = resolveAgentConfig(&config{llmAuth: authMethodClaude})
	if err != nil || cfg.Model != "claude-opus-4-6" || cfg.APIKey != "" || cfg.BaseURL != "" {
		t.Fatalf("explicit: %+v %v", cfg, err)
	}
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_API_KEY", "")
	writeTestCodexCredential(t, &codexCredential{AccessToken: "codex"})
	cfg, err = resolveAgentConfig(nil)
	if err != nil || cfg.AuthMethod != authMethodCodex {
		t.Fatalf("changed Codex precedence: %+v %v", cfg, err)
	}
	t.Setenv("LLM_AUTH", authMethodClaude)
	cfg, err = resolveAgentConfig(nil)
	if err != nil || cfg.AuthMethod != authMethodClaude {
		t.Fatalf("env: %+v %v", cfg, err)
	}
	pricing := resolveAgentPricing(t.Context(), cfg)
	if pricing == nil || !pricing.isSubscription || pricing.subscriptionName != "Claude" {
		t.Fatalf("pricing: %+v", pricing)
	}
	if resolveAgentContextWindow(t.Context(), cfg) != 0 {
		t.Fatal("subscription called API-key catalog")
	}
	for _, name := range []string{"", "openrouter/free", "openai/gpt-5.5"} {
		if claudeModel(name) != defaultClaudeModel {
			t.Fatalf("bad default for %q", name)
		}
	}
}

func TestClaudeProviderStreamingToolRoundTrip(t *testing.T) {
	isolateLLMEnv(t)
	if err := saveClaudeCredential(testClaudeToken()); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		expected := testClaudeToken()
		if call == 2 {
			expected += "new"
		}
		if r.URL.Path != "/v1/messages" || r.Header.Get("Authorization") != "Bearer "+expected || r.Header.Get("X-Api-Key") != "" || r.Header.Get("Anthropic-Beta") != "oauth-2025-04-20" {
			t.Error("incorrect Claude endpoint/authentication")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Error(err)
		}
		if request["stream"] != true || request["model"] != defaultClaudeModel {
			t.Errorf("request: %s", body)
		}
		if call == 2 && (!strings.Contains(string(body), `"tool_result"`) || !strings.Contains(string(body), `"tool_use_id":"tool_1"`) || !strings.Contains(string(body), `"42"`)) {
			t.Errorf("missing tool result: %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		emit := func(event string) {
			var e struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal([]byte(event), &e); err != nil {
				t.Error(err)
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, event)
		}
		emit(`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-6","content":[],"usage":{"input_tokens":12,"output_tokens":0}}}`)
		if call == 1 {
			emit(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"tool_1","name":"lookup","input":{}}}`)
			emit(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"id\":42}"}}`)
			emit(`{"type":"content_block_stop","index":0}`)
			emit(`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":5}}`)
		} else {
			emit(`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
			emit(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Готово"}}`)
			emit(`{"type":"content_block_stop","index":0}`)
			emit(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`)
		}
		emit(`{"type":"message_stop"}`)
	}))
	defer srv.Close()
	p, err := newClaudeProviderAt(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	messages := []providers.Message{{Role: "system", Content: "Use tools"}, {Role: "user", Content: "Find 42"}}
	tools := []providers.ToolDefinition{{Type: "function", Function: providers.ToolFunctionDefinition{Name: "lookup", Description: "Lookup", Parameters: map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "integer"}}}}}}
	resp, err := p.Chat(t.Context(), messages, tools, defaultClaudeModel, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.FinishReason != "tool_calls" || len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "lookup" || resp.ToolCalls[0].Arguments["id"] != float64(42) {
		t.Fatalf("tool call: %+v", resp)
	}
	messages = append(messages, providers.Message{Role: "assistant", ToolCalls: resp.ToolCalls}, providers.Message{Role: "tool", ToolCallID: resp.ToolCalls[0].ID, Content: "42"})
	if err := saveClaudeCredential(testClaudeToken() + "new"); err != nil {
		t.Fatal(err)
	}
	resp, err = p.Chat(t.Context(), messages, tools, defaultClaudeModel, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Готово" || resp.Usage.PromptTokens != 12 || resp.Usage.CompletionTokens != 5 {
		t.Fatalf("response: %+v", resp)
	}
	if err := deleteClaudeCredential(); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Chat(t.Context(), messages, tools, defaultClaudeModel, map[string]any{}); err == nil {
		t.Fatal("logout ignored by existing provider")
	}
	if calls.Load() != 2 {
		t.Fatal("sent request after logout")
	}
	t.Setenv("ANTHROPIC_API_KEY", "paid-api-key")
	if _, err := newClaudeProviderAt(srv.URL); err == nil {
		t.Fatal("API key billing fallback allowed")
	}
}

func TestClaudeWebLoginAndLogout(t *testing.T) {
	srv := newLLMSettingsTestServer(t)
	request := func(path, body string, intended int) string {
		t.Helper()
		res, err := http.Post(srv.URL+"/api/v1/llm/claude/"+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != intended {
			t.Fatalf("%s: %d %s", path, res.StatusCode, raw)
		}
		if strings.Contains(string(raw), testClaudeToken()) {
			t.Fatal("secret returned to browser")
		}
		return string(raw)
	}
	request("login", `{"token":"bad"}`, 400)
	request("login", `{"token":"`+testClaudeToken()+`"}`, 200)
	status, payload, raw := llmSettingsRequest(t, srv, http.MethodPut, `{"auth_method":"claude","model":"claude-opus-4-6"}`)
	if status != 200 || !payload.Claude.Configured || payload.Agent.AuthMethod != authMethodClaude || payload.Agent.Model != "claude-opus-4-6" {
		t.Fatalf("settings: %s", raw)
	}
	if strings.Contains(raw, testClaudeToken()) {
		t.Fatal("secret in settings payload")
	}
	request("login", `{"token":"invalid"}`, 400)
	if !hasClaudeCredential() {
		t.Fatal("failed login destroyed existing token")
	}
	request("logout", `{}`, 200)
	if hasClaudeCredential() {
		t.Fatal("logout not persisted")
	}
}

// The subscription endpoint rejects otherwise valid OAuth requests without its
// required first system block. Preserve the agent prompt and cache boundaries.
func TestClaudeProviderSubscriptionSystemBlock(t *testing.T) {
	isolateLLMEnv(t)
	if err := saveClaudeCredential(testClaudeToken()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		system []providers.Message
		want   []string
	}{
		{name: "no system"},
		{name: "plain system", system: []providers.Message{{Role: "system", Content: "Encounter instructions"}}, want: []string{"Encounter instructions"}},
		{name: "structured system", system: []providers.Message{{Role: "system", Content: "fallback must not replace parts", SystemParts: []providers.ContentBlock{{Type: "text", Text: "Static rules", CacheControl: &providers.CacheControl{Type: "ephemeral"}}, {Type: "text", Text: "Current game"}}}}, want: []string{"Static rules", "Current game"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req struct {
					System []struct {
						Text         string         `json:"text"`
						CacheControl map[string]any `json:"cache_control"`
					} `json:"system"`
				}
				if err := json.UnmarshalRead(r.Body, &req); err != nil {
					t.Error(err)
				}
				if len(req.System) != len(tc.want)+1 || req.System[0].Text != "You are a Claude agent, built on Anthropic's Claude Agent SDK." {
					t.Errorf("missing subscription system block: %+v", req.System)
					http.Error(w, "missing subscription system block", http.StatusBadRequest)
					return
				}
				for i, want := range tc.want {
					if req.System[i+1].Text != want {
						t.Errorf("agent prompt changed: %+v", req.System)
					}
				}
				if tc.name == "structured system" && req.System[1].CacheControl["type"] != "ephemeral" {
					t.Error("lost prompt cache boundary")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-5-5\",\"content\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			}))
			defer srv.Close()
			provider, err := newClaudeProviderAt(srv.URL)
			if err != nil {
				t.Fatal(err)
			}
			messages := append(tc.system, providers.Message{Role: "user", Content: "Hi"})
			before, err := json.Marshal(messages)
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				if _, err := provider.Chat(t.Context(), messages, nil, "claude-sonnet-5-5", nil); err != nil {
					t.Error(err)
				}
			}
			after, err := json.Marshal(messages)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("provider modified the caller's message history")
			}
		})
	}
}
