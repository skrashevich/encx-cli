package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newLLMSettingsTestServer wires the settings endpoints onto an isolated
// environment, so nothing these tests store can reach the developer's own
// configuration.
func newLLMSettingsTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	isolateLLMEnv(t)

	hub := &webHub{cfg: &config{}, registry: NewAuthRegistry(), store: NewChatStore(), sse: newSSEHub()}
	srv := httptest.NewTestServer(t, hub.newMux())
	srv.Start()
	return srv
}

// llmSettingsRequest performs one call and returns the status, the decoded
// payload and the raw body — the body is what the disclosure tests read.
func llmSettingsRequest(t *testing.T, srv *httptest.Server, method, body string) (int, llmSettingsPayload, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = stringsReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+"/api/v1/llm/settings", reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s /api/v1/llm/settings: %v", method, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload llmSettingsPayload
	// An error response carries a different shape; the caller checks the status
	// before reading the payload, so a decode failure here is not fatal.
	_ = json.Unmarshal(raw, &payload)
	return res.StatusCode, payload, string(raw)
}

func TestWebLLMSettingsGetReportsTheWholePanel(t *testing.T) {
	srv := newLLMSettingsTestServer(t)

	status, payload, raw := llmSettingsRequest(t, srv, http.MethodGet, "")
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, body %s", status, raw)
	}
	if payload.SettingsPath != llmSettingsFile() {
		t.Errorf("settings_path = %q, want %q", payload.SettingsPath, llmSettingsFile())
	}
	if payload.Defaults["base_url"] != defaultLLMBaseURL ||
		payload.Defaults["model"] != defaultLLMModel ||
		payload.Defaults["codex_model"] != defaultCodexModel {
		t.Errorf("defaults = %+v", payload.Defaults)
	}
	if payload.Effective.BaseURL.Source != llmSourceDefault || payload.Effective.BaseURL.Value != defaultLLMBaseURL {
		t.Errorf("effective base URL = %+v, want the built-in default", payload.Effective.BaseURL)
	}
	if payload.Effective.AuthMethod.Source != llmSourceUnset {
		t.Errorf("effective auth method = %+v, want unset on a clean machine", payload.Effective.AuthMethod)
	}
	if payload.Effective.APIKey.HasValue {
		t.Errorf("effective api key = %+v, want none", payload.Effective.APIKey)
	}
	if payload.Stored != (llmStoredSettings{}) {
		t.Errorf("stored = %+v, want empty", payload.Stored)
	}
	if len(payload.EnvOverrides) != 0 {
		t.Errorf("env_overrides = %+v, want none", payload.EnvOverrides)
	}
	if payload.Codex.SignedIn || payload.Codex.Path != codexAuthFile() {
		t.Errorf("codex = %+v", payload.Codex)
	}
	// Nothing is configured, so the agent summary has to carry the reason rather
	// than pretend a transport exists.
	if payload.Agent.Error == "" {
		t.Errorf("agent = %+v, want the missing-transport error", payload.Agent)
	}
	for _, method := range llmSettableAuthMethods {
		if !strings.Contains(raw, `"`+method+`"`) {
			t.Errorf("auth_methods %v does not offer %q", payload.AuthMethods, method)
		}
	}
}

// -web binds to localhost by default, but a key in the response would still land
// in every browser cache and devtools log that ever displayed the panel.
func TestWebLLMSettingsGetNeverDisclosesTheAPIKey(t *testing.T) {
	srv := newLLMSettingsTestServer(t)
	const key = "sk-secret-value-1234"
	writeTestLLMSettings(t, llmSettings{APIKey: key, AuthMethod: authMethodAPIKey})

	status, payload, raw := llmSettingsRequest(t, srv, http.MethodGet, "")
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, body %s", status, raw)
	}
	if strings.Contains(raw, "sk-secret") {
		t.Fatalf("the response discloses the stored key: %s", raw)
	}
	if !payload.Stored.HasAPIKey {
		t.Error("stored.has_api_key = false, the panel cannot tell a key exists")
	}
	if payload.Stored.APIKeyMasked != maskAPIKey(key) {
		t.Errorf("stored.api_key_masked = %q", payload.Stored.APIKeyMasked)
	}
	if !payload.Effective.APIKey.HasValue || payload.Effective.APIKey.Source != llmSourceSettings {
		t.Errorf("effective api key = %+v", payload.Effective.APIKey)
	}
}

func TestWebLLMSettingsPutStoresTheTransport(t *testing.T) {
	srv := newLLMSettingsTestServer(t)

	status, payload, raw := llmSettingsRequest(t, srv, http.MethodPut,
		`{"auth_method":"APIKEY","base_url":" https://api.example.com/v1 ","model":" gpt-4o ","api_key":"sk-secret-value-1234"}`)
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", status, raw)
	}
	if strings.Contains(raw, "sk-secret") {
		t.Fatalf("the PUT response echoes the key back: %s", raw)
	}
	want := llmStoredSettings{
		AuthMethod:   authMethodAPIKey,
		BaseURL:      "https://api.example.com/v1",
		Model:        "gpt-4o",
		HasAPIKey:    true,
		APIKeyMasked: maskAPIKey("sk-secret-value-1234"),
	}
	if payload.Stored != want {
		t.Fatalf("stored = %+v, want %+v", payload.Stored, want)
	}

	// The file is what the next process reads, so the response is not enough.
	onDisk, err := loadLLMSettings()
	if err != nil {
		t.Fatalf("loadLLMSettings: %v", err)
	}
	if onDisk.APIKey != "sk-secret-value-1234" || onDisk.BaseURL != "https://api.example.com/v1" || onDisk.Model != "gpt-4o" {
		t.Fatalf("settings on disk = %+v", onDisk)
	}
	// And the agent summary must now describe a working transport.
	if payload.Agent.Error != "" {
		t.Fatalf("agent = %+v, want no error once a key is stored", payload.Agent)
	}
	if payload.Agent.BaseURL != "https://api.example.com/v1" || payload.Agent.Model != "gpt-4o" {
		t.Fatalf("agent = %+v", payload.Agent)
	}
}

func TestWebLLMSettingsPutRejectsBadInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"unknown auth method", `{"auth_method":"chatgpt"}`, "unknown LLM auth method"},
		{"base url scheme", `{"base_url":"ftp://x"}`, "http://"},
		// url.Parse accepts a bare word as a relative reference, so what refuses
		// this is the scheme check, not the parser.
		{"base url without a scheme", `{"base_url":"не-url"}`, "base URL"},
		{"base url with no host", `{"base_url":"https://"}`, "no host"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newLLMSettingsTestServer(t)

			status, _, raw := llmSettingsRequest(t, srv, http.MethodPut, tc.body)
			if status != http.StatusBadRequest {
				t.Fatalf("PUT status = %d, want 400 (body %s)", status, raw)
			}
			if !strings.Contains(raw, tc.want) {
				t.Errorf("error body = %s, want it to mention %q", raw, tc.want)
			}
			// A refused save must leave the file untouched.
			if _, err := os.Stat(llmSettingsFile()); !os.IsNotExist(err) {
				t.Errorf("a refused PUT created the settings file (stat err = %v)", err)
			}
		})
	}
}

// The form never receives the key, so an empty one means "leave it alone" and
// clearing has to be an explicit act.
func TestWebLLMSettingsPutKeepsTheKeyUnlessCleared(t *testing.T) {
	srv := newLLMSettingsTestServer(t)
	writeTestLLMSettings(t, llmSettings{APIKey: "sk-secret-value-1234", Model: "file/model"})

	status, payload, raw := llmSettingsRequest(t, srv, http.MethodPut,
		`{"auth_method":"apikey","base_url":"https://api.example.com/v1","model":"gpt-4o","api_key":""}`)
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", status, raw)
	}
	if !payload.Stored.HasAPIKey {
		t.Fatal("a form that never saw the key erased it")
	}
	if onDisk, _ := loadLLMSettings(); onDisk.APIKey != "sk-secret-value-1234" {
		t.Fatalf("stored key = %q, want the one the form could not resend", onDisk.APIKey)
	}

	status, payload, raw = llmSettingsRequest(t, srv, http.MethodPut,
		`{"auth_method":"apikey","base_url":"https://api.example.com/v1","model":"gpt-4o","clear_api_key":true}`)
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", status, raw)
	}
	if payload.Stored.HasAPIKey || payload.Stored.APIKeyMasked != "" {
		t.Fatalf("stored = %+v, want the key cleared", payload.Stored)
	}
	if onDisk, _ := loadLLMSettings(); onDisk.APIKey != "" {
		t.Fatalf("stored key = %q, want it cleared on disk", onDisk.APIKey)
	}

	// The non-secret fields are replaced verbatim, so clearing one in the UI
	// hands the choice back to the environment.
	status, payload, raw = llmSettingsRequest(t, srv, http.MethodPut, `{"auth_method":"","base_url":"","model":""}`)
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", status, raw)
	}
	if payload.Stored != (llmStoredSettings{}) {
		t.Fatalf("stored = %+v, want every field cleared", payload.Stored)
	}
}

// The panel that exists to fix an unreadable file must be able to open, and
// saving over it must replace it rather than be refused.
func TestWebLLMSettingsSurvivesABrokenFile(t *testing.T) {
	srv := newLLMSettingsTestServer(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv(llmSettingsFileEnvVar, path)
	if err := os.WriteFile(path, []byte("not json at all"), 0600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	status, payload, raw := llmSettingsRequest(t, srv, http.MethodGet, "")
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, body %s", status, raw)
	}
	if payload.Error == "" {
		t.Error("the payload hides the parse failure the panel has to report")
	}

	status, payload, raw = llmSettingsRequest(t, srv, http.MethodPut, `{"auth_method":"apikey","api_key":"sk-secret-value-1234"}`)
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", status, raw)
	}
	if payload.Error != "" {
		t.Errorf("payload error = %q, want the file replaced", payload.Error)
	}
	if !payload.Stored.HasAPIKey {
		t.Errorf("stored = %+v, want the new key", payload.Stored)
	}
}

func TestWebLLMSettingsDeleteResetsThePanel(t *testing.T) {
	srv := newLLMSettingsTestServer(t)
	writeTestLLMSettings(t, llmSettings{AuthMethod: authMethodAPIKey, APIKey: "sk-secret-value-1234"})

	status, payload, raw := llmSettingsRequest(t, srv, http.MethodDelete, "")
	if status != http.StatusOK {
		t.Fatalf("DELETE status = %d, body %s", status, raw)
	}
	if payload.Stored != (llmStoredSettings{}) {
		t.Fatalf("stored = %+v, want empty after a reset", payload.Stored)
	}
	if _, err := os.Stat(llmSettingsFile()); !os.IsNotExist(err) {
		t.Fatalf("the settings file survived the reset (stat err = %v)", err)
	}

	// Resetting a machine that never configured anything is not an error.
	status, _, raw = llmSettingsRequest(t, srv, http.MethodDelete, "")
	if status != http.StatusOK {
		t.Fatalf("second DELETE status = %d, body %s", status, raw)
	}
}

// A key exported into a shell or a container is the deployment speaking. The
// panel cannot override it, so it has to say so instead of losing silently.
func TestWebLLMSettingsReportsEnvOverrides(t *testing.T) {
	srv := newLLMSettingsTestServer(t)
	writeTestLLMSettings(t, llmSettings{APIKey: "sk-secret-value-1234", Model: "file/model"})
	t.Setenv("LLM_API_KEY", "sk-env-value-9999")

	status, payload, raw := llmSettingsRequest(t, srv, http.MethodGet, "")
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, body %s", status, raw)
	}
	if strings.Contains(raw, "sk-env") || strings.Contains(raw, "sk-secret") {
		t.Fatalf("the response discloses a key: %s", raw)
	}

	var override *llmEnvOverride
	for i, o := range payload.EnvOverrides {
		if o.Field == "api_key" {
			override = &payload.EnvOverrides[i]
		}
		if o.Field == "model" {
			t.Errorf("model is reported as overridden by %q, but nothing exports it", o.EnvVar)
		}
	}
	if override == nil {
		t.Fatalf("env_overrides = %+v, want the shadowed api_key", payload.EnvOverrides)
	}
	if override.Source != llmSourceEnv || override.EnvVar != "LLM_API_KEY" {
		t.Errorf("override = %+v, want it to name the variable", *override)
	}
	if !override.ShadowsStored {
		t.Error("shadows_stored = false, but a stored key is being ignored")
	}
	// The stored value still has to be visible: it is what the form edits.
	if !payload.Stored.HasAPIKey {
		t.Error("stored.has_api_key = false while a key is saved")
	}
	if payload.Effective.APIKey.Source != llmSourceEnv || payload.Effective.APIKey.EnvVar != "LLM_API_KEY" {
		t.Errorf("effective api key = %+v", payload.Effective.APIKey)
	}
}

// Nothing stored means there is nothing to shadow, so the badge must stay off.
func TestWebLLMSettingsEnvOverrideWithoutStoredValue(t *testing.T) {
	srv := newLLMSettingsTestServer(t)
	t.Setenv("LLM_BASE_URL", "https://env.example.com/v1")
	t.Setenv("LLM_API_KEY", "sk-env-value-9999")

	status, payload, raw := llmSettingsRequest(t, srv, http.MethodGet, "")
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, body %s", status, raw)
	}
	found := 0
	for _, o := range payload.EnvOverrides {
		found++
		if o.ShadowsStored {
			t.Errorf("override %+v claims to shadow a stored value, but nothing is stored", o)
		}
	}
	if found != 2 {
		t.Fatalf("env_overrides = %+v, want base_url and api_key", payload.EnvOverrides)
	}
}

// The panel cannot edit what --llm-auth pinned either, so that is reported the
// same way — with no variable to name.
func TestWebLLMSettingsReportsTheFlagOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	isolateLLMEnv(t)
	writeTestLLMSettings(t, llmSettings{AuthMethod: authMethodAPIKey, APIKey: "sk-secret-value-1234"})

	hub := &webHub{cfg: &config{llmAuth: authMethodCodex}, registry: NewAuthRegistry(), store: NewChatStore(), sse: newSSEHub()}
	srv := httptest.NewTestServer(t, hub.newMux())
	srv.Start()

	status, payload, raw := llmSettingsRequest(t, srv, http.MethodGet, "")
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, body %s", status, raw)
	}
	if payload.Effective.AuthMethod.Source != llmSourceFlag || payload.Effective.AuthMethod.Value != authMethodCodex {
		t.Fatalf("effective auth method = %+v", payload.Effective.AuthMethod)
	}
	var found bool
	for _, o := range payload.EnvOverrides {
		if o.Field != "auth_method" {
			continue
		}
		found = true
		if o.Source != llmSourceFlag || o.EnvVar != "" {
			t.Errorf("override = %+v, want a flag with no variable", o)
		}
		if !o.ShadowsStored {
			t.Error("shadows_stored = false, but a stored auth method is being ignored")
		}
	}
	if !found {
		t.Fatalf("env_overrides = %+v, want the flag-pinned auth method", payload.EnvOverrides)
	}
}

func TestWebLLMSettingsRejectsOtherMethods(t *testing.T) {
	srv := newLLMSettingsTestServer(t)

	status, _, raw := llmSettingsRequest(t, srv, http.MethodPost, `{}`)
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405 (body %s)", status, raw)
	}
}
