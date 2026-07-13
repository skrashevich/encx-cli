package encx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestGetGameModelLevelOmitsNonPositiveLevelQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %q, want GET", r.Method)
		}
		if _, ok := r.URL.Query()["level"]; ok {
			t.Fatalf("unexpected level query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Event":0,"GameId":2020}`))
	}))
	defer server.Close()

	client := newContractTestClient(server.URL)
	if _, err := client.GetGameModelLevel(t.Context(), 2020, 0); err != nil {
		t.Fatalf("GetGameModelLevel: %v", err)
	}
}

func TestGetGameModelRetainsLegacyPostAndNoFormGet(t *testing.T) {
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm: %v", err)
			}
			if got := r.PostForm.Get("LegacyAction"); got != "1" {
				t.Fatalf("LegacyAction = %q, want 1", got)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Event":0,"GameId":2020}`))
	}))
	defer server.Close()

	client := newContractTestClient(server.URL)
	if _, err := client.GetGameModel(t.Context(), 2020, url.Values{"LegacyAction": {"1"}}); err != nil {
		t.Fatalf("legacy GetGameModel POST: %v", err)
	}
	if _, err := client.GetGameModel(t.Context(), 2020); err != nil {
		t.Fatalf("GetGameModel GET: %v", err)
	}
	if got, want := strings.Join(methods, ","), "POST,GET"; got != want {
		t.Fatalf("methods = %q, want %q", got, want)
	}
}

func TestSendCodeAndBonusUseExactDocumentedForms(t *testing.T) {
	var seen []url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		seen = append(seen, r.PostForm)
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
	wantLevel := url.Values{
		"LevelId":            {"1356"},
		"LevelNumber":        {"2"},
		"LevelAction.Answer": {"level-code"},
	}
	if got := seen[0]; !valuesEqual(got, wantLevel) {
		t.Fatalf("level action form = %q, want %q", got.Encode(), wantLevel.Encode())
	}
	wantBonus := url.Values{
		"LevelId":            {"1356"},
		"LevelNumber":        {"2"},
		"BonusAction.Answer": {"bonus-code"},
	}
	if got := seen[1]; !valuesEqual(got, wantBonus) {
		t.Fatalf("bonus action form = %q, want %q", got.Encode(), wantBonus.Encode())
	}
}

func TestSendCodeReportsAntiBotRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/NotHumanRequest.aspx?return=redacted")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	client := newContractTestClient(server.URL)
	_, err := client.SendCode(t.Context(), 2020, 1356, 2, "level-code")
	if !errors.Is(err, ErrAntiSpam) {
		t.Fatalf("SendCode error = %v, want ErrAntiSpam", err)
	}
}

func valuesEqual(got, want url.Values) bool {
	return got.Encode() == want.Encode()
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
