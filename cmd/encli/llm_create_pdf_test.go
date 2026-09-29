package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

func TestCreateScenarioPDFStoresLocalWholeAndPartialArtifacts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/session":
			_, _ = w.Write([]byte(`{"user_id":1}`))
		case "/games/42/scenario":
			_, _ = w.Write([]byte(`{"game":{"id":42,"title":"Тестовая игра"},"levels":[{"level_id":1,"level_number":1,"level_name":"Первый","tasks":[{"task_id":1,"task_text":"Задание один"}]},{"level_id":2,"level_number":2,"level_name":"Второй","tasks":[{"task_id":2,"task_text":"Задание два"}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	client := encx.New(host, encx.WithHTTP(), encx.WithAPIBaseURL(server.URL), encx.WithEngine(encx.EngineNew), encx.WithAdminDelay(0))
	client.SetAPIToken("test-token")
	store := NewChatStore()
	chat := store.Create(host, 42, SecurityModeReadonly)
	session := &llmSession{webChatID: chat.ID, securityMode: SecurityModeReadonly}
	hub := &webHub{cfg: &config{}, registry: NewAuthRegistry(), store: store, sse: newSSEHub()}

	for _, tc := range []struct {
		args   string
		levels int
	}{
		{`{"game_id":42}`, 2},
		{`{"game_id":42,"from_level":2,"to_level":2}`, 1},
	} {
		raw := executeToolCallSafe(t.Context(), &config{domain: host}, client, session, "create_scenario_pdf", tc.args)
		var result struct {
			URL    string `json:"url"`
			Levels int    `json:"levels"`
			Error  string `json:"error"`
		}
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			t.Fatalf("tool response %q: %v", raw, err)
		}
		if result.Error != "" || result.Levels != tc.levels || !validPDFArtifactURL(chat.ID, result.URL) {
			t.Fatalf("unexpected result: %+v", result)
		}
		req := httptest.NewRequest(http.MethodGet, result.URL, nil)
		w := httptest.NewRecorder()
		hub.newMux().ServeHTTP(w, req)
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF-")) {
			t.Fatalf("local PDF delivery: status=%d type=%q", w.Code, w.Header().Get("Content-Type"))
		}
		if u, err := url.Parse(result.URL); err != nil || u.IsAbs() || u.Host != "" {
			t.Fatalf("artifact URL must be local: %q", result.URL)
		}
	}
}

func TestScenarioPDFReplyUsesLocalArtifactURL(t *testing.T) {
	link := "/api/v1/chats/1840cf2226446dc0/artifacts/0123456789abcdef0123456789abcdef.pdf"
	if !validPDFArtifactURL("1840cf2226446dc0", link) {
		t.Fatal("valid local PDF URL rejected")
	}
	message := includeLocalPDFLink("Скачать [сценарий](https://svk.en.cx/api/v1/scenario/82913/pdf?domain=svk.en.cx)", link)
	if strings.Contains(message, "https://svk.en.cx/api/v1/scenario") || !strings.Contains(message, "]("+link+")") {
		t.Fatalf("PDF reply points at wrong host: %q", message)
	}
}

func TestPDFToolsAndInstructionsAreWebChatOnly(t *testing.T) {
	cli := &llmSession{securityMode: SecurityModeReadonly}
	for _, tool := range getToolsForSession(cli) {
		if tool.Function.Name == "create_pdf" || tool.Function.Name == "create_scenario_pdf" {
			t.Fatalf("PDF artifact tool exposed outside WebUI: %s", tool.Function.Name)
		}
	}
	if strings.Contains(buildSystemPrompt(&config{}, cli), "create_scenario_pdf") {
		t.Fatal("CLI prompt advertises a WebUI-only tool")
	}
	web := &llmSession{webChatID: "1840cf2226446dc0", securityMode: SecurityModeReadonly}
	seen := map[string]bool{}
	for _, tool := range getToolsForSession(web) {
		seen[tool.Function.Name] = true
	}
	if !seen["create_pdf"] || !seen["create_scenario_pdf"] || !strings.Contains(buildSystemPrompt(&config{}, web), "create_scenario_pdf") {
		t.Fatal("WebUI PDF tools or guidance missing")
	}
}
