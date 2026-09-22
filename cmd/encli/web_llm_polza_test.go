package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json/v2"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func polzaTestHub(t *testing.T, handler http.HandlerFunc) *webHub {
	t.Helper()
	isolateLLMEnv(t)
	t.Setenv(llmSettingsFileEnvVar, filepath.Join(t.TempDir(), "settings.json"))
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)
	m := newPolzaManager()
	m.tokenURL = upstream.URL + "/token"
	m.balanceURL = upstream.URL + "/balance"
	m.modelsURL = upstream.URL + "/models"
	t.Cleanup(m.close)
	return &webHub{polza: m}
}
func polzaCall(t *testing.T, h *webHub, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	h.newMux().ServeHTTP(w, req)
	return w
}
func TestPolzaPKCESingleUsePersistenceAndSecrets(t *testing.T) {
	var calls atomic.Int32
	var verifier string
	h := polzaTestHub(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]string
		if err := json.UnmarshalRead(r.Body, &body); err != nil {
			t.Error(err)
		}
		verifier = body["code_verifier"]
		if body["grant_type"] != "authorization_code" || body["code"] != "accepted-code" || !strings.HasPrefix(body["callback_url"], "http://127.0.0.1:") {
			t.Errorf("wrong token request: %v", body)
		}
		fmt.Fprint(w, `{"key":"polza-secret-test-key","user_id":"user1"}`)
	})
	f, err := h.polza.start("deepseek/deepseek-v4-flash-0731")
	if err != nil {
		t.Fatal(err)
	}
	auth, _ := url.Parse(f.authorizeURL)
	q := auth.Query()
	if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("state") != f.state || q.Get("app_name") != "encli" {
		t.Fatalf("wrong authorize URL: %s", f.authorizeURL)
	}
	for _, input := range []string{"accepted-code", f.redirectURI + "?code=accepted-code", f.redirectURI + "?code=accepted-code&state=wrong", "https://attacker.invalid/?code=accepted-code&state=" + f.state, f.redirectURI + "?code=accepted-code&state=" + f.state + "&state=" + f.state} {
		if h.polza.submit(f, input) == nil {
			t.Errorf("accepted invalid callback: %s", input)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid callback reached token endpoint")
	}
	callback := f.redirectURI + "?code=accepted-code&state=" + f.state
	if err := h.polza.submit(f, callback); err != nil {
		t.Fatal(err)
	}
	if err := h.polza.submit(f, callback); err == nil {
		t.Fatal("replay accepted")
	}
	if calls.Load() != 1 {
		t.Fatalf("exchanges: %d", calls.Load())
	}
	hash := sha256.Sum256([]byte(verifier))
	if q.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(hash[:]) {
		t.Fatal("PKCE challenge mismatch")
	}
	stored, err := loadLLMSettings()
	if err != nil {
		t.Fatal(err)
	}
	if stored.APIKey != "polza-secret-test-key" || stored.AuthMethod != authMethodAPIKey || stored.BaseURL != polzaBaseURL || stored.Model != "deepseek/deepseek-v4-flash-0731" {
		t.Fatalf("incorrect saved configuration: %+v", stored)
	}
	w := polzaCall(t, h, "GET", "/api/v1/llm/polza/login/"+f.id, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"success"`) {
		t.Fatal(w.Body.String())
	}
	for _, secret := range []string{stored.APIKey, verifier} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatal("secret leaked in status")
		}
	}
	if f.verifier != "" {
		t.Fatal("verifier retained after completion")
	}
}
func TestPolzaBalanceConnectZeroAndForeignKey(t *testing.T) {
	var calls atomic.Int32
	h := polzaTestHub(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer own-polza-key" {
			t.Error("wrong key")
		}
		fmt.Fprint(w, `{"amount":"0.00000000","available":"0.00000000"}`)
	})
	if err := saveLLMSettings(llmSettings{AuthMethod: authMethodAPIKey, BaseURL: "https://foreign.invalid/v1", APIKey: "foreign-secret"}); err != nil {
		t.Fatal(err)
	}
	w := polzaCall(t, h, "POST", "/api/v1/llm/polza/connect", `{"model":"deepseek/deepseek-v4-flash-0731"}`)
	if w.Code != 400 || calls.Load() != 0 {
		t.Fatal("foreign key reused")
	}
	w = polzaCall(t, h, "POST", "/api/v1/llm/polza/check", `{}`)
	if w.Code != 409 || calls.Load() != 0 {
		t.Fatal("foreign effective key sent")
	}
	w = polzaCall(t, h, "POST", "/api/v1/llm/polza/connect", `{"api_key":"own-polza-key","model":"deepseek/deepseek-v4-flash-0731"}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"connected":true`) || !strings.Contains(w.Body.String(), `"available":"0.00000000"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	stored, _ := loadLLMSettings()
	if stored.APIKey != "own-polza-key" {
		t.Fatal("zero balance key not saved")
	}
	w = polzaCall(t, h, "POST", "/api/v1/llm/polza/connect", `{"model":"deepseek/deepseek-v4-flash-0731"}`)
	if w.Code != 200 {
		t.Fatal("own key reuse failed", w.Body.String())
	}
	t.Setenv("LLM_BASE_URL", "https://override.invalid/v1")
	before := calls.Load()
	w = polzaCall(t, h, "POST", "/api/v1/llm/polza/check", `{}`)
	if w.Code != 409 || calls.Load() != before || !strings.Contains(w.Body.String(), "LLM_BASE_URL") {
		t.Fatal("override not honored safely")
	}
}
func TestPolzaInvalidBalanceAndRedirectNeverPersist(t *testing.T) {
	for _, response := range []string{`{"amount":"12","available":"NaN"}`, `{"amount":"12"}`, `{"amount":12,"available":12}`} {
		t.Run(response, func(t *testing.T) {
			h := polzaTestHub(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, response) })
			w := polzaCall(t, h, "POST", "/api/v1/llm/polza/connect", `{"api_key":"secret","model":"test/model"}`)
			if w.Code != 502 {
				t.Fatal(w.Code, w.Body.String())
			}
			stored, _ := loadLLMSettings()
			if stored.APIKey != "" {
				t.Fatal("invalid balance saved key")
			}
		})
	}
	t.Run("redirect", func(t *testing.T) {
		var leaked atomic.Bool
		dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
		t.Cleanup(dest.Close)
		h := polzaTestHub(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", dest.URL)
			w.WriteHeader(307)
			fmt.Fprint(w, "secret upstream response")
		})
		w := polzaCall(t, h, "POST", "/api/v1/llm/polza/connect", `{"api_key":"secret","model":"test/model"}`)
		if w.Code != 502 || leaked.Load() || strings.Contains(w.Body.String(), "secret") {
			t.Fatal("redirect or error leaked credentials", w.Body.String())
		}
		f, err := h.polza.start("test/model")
		if err != nil {
			t.Fatal(err)
		}
		if h.polza.submit(f, f.redirectURI+"?state="+f.state+"&code=secret-code") == nil || leaked.Load() {
			t.Fatal("token redirect followed")
		}
	})
}
func TestPolzaCancelInFlightAndReplay(t *testing.T) {
	arrived := make(chan struct{})
	release := make(chan struct{})
	h := polzaTestHub(t, func(w http.ResponseWriter, r *http.Request) {
		close(arrived)
		<-release
		fmt.Fprint(w, `{"key":"late-key"}`)
	})
	f, err := h.polza.start("test/model")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- h.polza.submit(f, f.redirectURI+"?state="+f.state+"&code=code") }()
	<-arrived
	if h.polza.submit(f, f.redirectURI+"?state="+f.state+"&code=code") == nil {
		t.Error("concurrent replay accepted")
	}
	w := polzaCall(t, h, "DELETE", "/api/v1/llm/polza/login/"+f.id, "")
	close(release)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if <-done == nil {
		t.Fatal("cancelled exchange succeeded")
	}
	stored, _ := loadLLMSettings()
	if stored.APIKey != "" {
		t.Fatal("cancelled exchange persisted key")
	}
	u, _ := url.Parse(f.redirectURI)
	conn, err := net.DialTimeout("tcp", u.Host, 100*time.Millisecond)
	if err == nil {
		conn.Close()
		t.Fatal("cancel left listener open")
	}
}
func TestPolzaTimeoutRetentionAndModels(t *testing.T) {
	h := polzaTestHub(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[{"id":"audio","type":"audio","top_provider":{"supported_parameters":["tools"]}},{"id":"no-tools","type":"chat"},{"id":"another/tool-model","type":"chat","top_provider":{"supported_parameters":["tools"]}},{"id":"deepseek/deepseek-v4-flash-0731","name":"DeepSeek V4 Flash 0731","type":"chat","top_provider":{"supported_parameters":["tools"]}}]}`)
	})
	w := polzaCall(t, h, "GET", "/api/v1/llm/polza/models", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "audio") || strings.Contains(w.Body.String(), "no-tools") || !strings.Contains(w.Body.String(), `"default_model":"deepseek/deepseek-v4-flash-0731"`) {
		t.Fatal(w.Body.String())
	}
	h.polza.ttl = 10 * time.Millisecond
	h.polza.retention = 20 * time.Millisecond
	f, err := h.polza.start("test/model")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for h.polza.get(f.id) != nil {
		select {
		case <-deadline:
			t.Fatal("expired flow retained")
		case <-time.After(5 * time.Millisecond):
		}
	}
	if f.snapshot()["status"] != "error" {
		t.Fatal("timeout not reported")
	}
}

func TestPolzaDenialAndNewAttemptCancelOld(t *testing.T) {
	h := polzaTestHub(t, func(w http.ResponseWriter, r *http.Request) { t.Error("denial reached upstream") })
	f, err := h.polza.start("test/model")
	if err != nil {
		t.Fatal(err)
	}
	if h.polza.submit(f, f.redirectURI+"?state="+f.state+"&error=access_denied&error_description=secret-text") == nil {
		t.Fatal("denial succeeded")
	}
	snap := f.snapshot()
	if snap["status"] != "error" || strings.Contains(fmt.Sprint(snap), "secret-text") {
		t.Fatal("denial not sanitized or settled")
	}
	old, err := h.polza.start("test/model")
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.polza.start("test/model")
	if err != nil {
		t.Fatal(err)
	}
	if old.snapshot()["status"] != "error" {
		t.Fatal("new attempt did not cancel previous")
	}
}
func TestPolzaAmbientKeyNeverLeaksToNewProvider(t *testing.T) {
	var calls atomic.Int32
	h := polzaTestHub(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"amount":"1","available":"1"}`)
	})
	t.Setenv("LLM_API_KEY", "old-provider-secret")
	if err := saveLLMSettings(llmSettings{AuthMethod: authMethodAPIKey, BaseURL: polzaBaseURL, APIKey: "polza-key", Model: "test/model"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"connect", "login", "check"} {
		w := polzaCall(t, h, "POST", "/api/v1/llm/polza/"+path, `{"api_key":"polza-key","model":"test/model"}`)
		if w.Code != 409 || !strings.Contains(w.Body.String(), "LLM_API_KEY") {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("foreign environment key sent to Polza")
	}
	t.Setenv("LLM_BASE_URL", polzaBaseURL)
	w := polzaCall(t, h, "POST", "/api/v1/llm/polza/check", `{}`)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "LLM_API_KEY") || calls.Load() != 1 {
		t.Fatal("explicit matching environment not honored", w.Body.String())
	}
}
func TestPolzaLocalCallback(t *testing.T) {
	h := polzaTestHub(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"key":"callback-key"}`) })
	f, err := h.polza.start("test/model")
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), "GET", f.redirectURI+"?state="+f.state+"&code=test", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || f.snapshot()["status"] != "success" {
		t.Fatal("callback did not finish login")
	}
}
