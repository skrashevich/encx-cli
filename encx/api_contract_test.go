package encx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newContractTestClient(serverURL string) *Client {
	host := strings.TrimPrefix(serverURL, "http://")
	return New(host, WithHTTP())
}

func TestLoginUsesDocumentedFormParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/login/signin" || r.URL.Query().Get("json") != "1" {
			t.Fatalf("url = %s, want /login/signin?json=1", r.URL.String())
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
			t.Fatalf("content-type = %q, want form", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("Login"); got != "player" {
			t.Fatalf("Login = %q", got)
		}
		if got := r.Form.Get("Password"); got != "secret" {
			t.Fatalf("Password = %q", got)
		}
		if got := r.Form.Get("ddlNetwork"); got != "2" {
			t.Fatalf("ddlNetwork = %q", got)
		}
		if got := r.Form.Get("MagicNumbers"); got != "1234" {
			t.Fatalf("MagicNumbers = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Error":0}`))
	}))
	defer server.Close()

	client := newContractTestClient(server.URL)
	resp, err := client.Login(t.Context(), "player", "secret", LoginOptions{Network: 2, MagicNumbers: "1234"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if resp.Error != 0 {
		t.Fatalf("Error = %d, want 0", resp.Error)
	}
}

func TestGetGameModelUsesDocumentedGET(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %q, want GET", r.Method)
		}
		if r.URL.Path != "/gameengines/encounter/play/2020" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("json") != "1" {
			t.Fatalf("missing json=1: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Event":0,"GameId":2020}`))
	}))
	defer server.Close()

	client := newContractTestClient(server.URL)
	model, err := client.GetGameModel(t.Context(), 2020)
	if err != nil {
		t.Fatalf("GetGameModel: %v", err)
	}
	if model.GameId != 2020 {
		t.Fatalf("GameId = %d, want 2020", model.GameId)
	}
}

func TestGetGameModelLevelAddsLevelQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %q, want GET", r.Method)
		}
		if got := r.URL.Query().Get("level"); got != "3" {
			t.Fatalf("level = %q, want 3", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Event":0,"GameId":2020}`))
	}))
	defer server.Close()

	client := newContractTestClient(server.URL)
	if _, err := client.GetGameModelLevel(t.Context(), 2020, 3); err != nil {
		t.Fatalf("GetGameModelLevel: %v", err)
	}
}

func TestSendCodeAndBonusUseDocumentedActions(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		seen = append(seen, r.Form.Encode())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Event":0,"GameId":2020}`))
	}))
	defer server.Close()

	client := newContractTestClient(server.URL)
	if _, err := client.SendCode(t.Context(), 2020, 1356, 2, "level-code"); err != nil {
		t.Fatalf("SendCode: %v", err)
	}
	if _, err := client.SendBonusCode(t.Context(), 2020, 1356, 2, "bonus-code"); err != nil {
		t.Fatalf("SendBonusCode: %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("requests = %d, want 2", len(seen))
	}
	if !strings.Contains(seen[0], "LevelAction.Answer=level-code") {
		t.Fatalf("level action form = %q", seen[0])
	}
	if !strings.Contains(seen[1], "BonusAction.Answer=bonus-code") {
		t.Fatalf("bonus action form = %q", seen[1])
	}
}

func TestLevelCanSubmitLevelAnswer(t *testing.T) {
	tests := []struct {
		name string
		in   *Level
		want bool
	}{
		{"nil", nil, false},
		{"normal", &Level{}, true},
		{"passed", &Level{IsPassed: true}, false},
		{"dismissed", &Level{Dismissed: true}, false},
		{"blocked", &Level{HasAnswerBlockRule: true, BlockDuration: 10}, false},
		{"block expired", &Level{HasAnswerBlockRule: true, BlockDuration: 0}, true},
	}
	for _, tt := range tests {
		if got := tt.in.CanSubmitLevelAnswer(); got != tt.want {
			t.Errorf("%s: CanSubmitLevelAnswer = %v, want %v", tt.name, got, tt.want)
		}
	}
}
