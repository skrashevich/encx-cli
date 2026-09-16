package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

// newEngineSettingsTestServer wires the engine endpoints onto an isolated
// environment, so nothing these tests store can reach the developer's own
// configuration and no exported ENCX_* variable can decide their assertions.
func newEngineSettingsTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	_, srv := newEngineSettingsTestHub(t)
	return srv
}

// newEngineSettingsTestHub also hands back the hub, for the tests that have to
// look at the clients the registry hands out rather than only at the responses.
func newEngineSettingsTestHub(t *testing.T) (*webHub, *httptest.Server) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	silentEngineEnv(t)

	hub := &webHub{cfg: &config{}, registry: NewAuthRegistry(), store: NewChatStore(), sse: newSSEHub()}
	srv := httptest.NewTestServer(t, hub.newMux())
	srv.Start()
	return hub, srv
}

func engineSettingsRequest(t *testing.T, srv *httptest.Server, method, body string) (int, engineSettingsPayload, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = stringsReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+"/api/v1/engine/settings", reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s /api/v1/engine/settings: %v", method, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload engineSettingsPayload
	// An error response carries a different shape; every caller checks the status
	// before reading the payload, so a decode failure here is not fatal.
	_ = json.Unmarshal(raw, &payload)
	return res.StatusCode, payload, string(raw)
}

func TestWebEngineSettingsGetOnACleanMachine(t *testing.T) {
	srv := newEngineSettingsTestServer(t)

	status, payload, raw := engineSettingsRequest(t, srv, http.MethodGet, "")
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, body %s", status, raw)
	}
	if payload.SettingsPath != engineSettingsFile() {
		t.Errorf("settings_path = %q, want %q", payload.SettingsPath, engineSettingsFile())
	}
	if payload.Effective.Engine.Source != llmSourceDefault ||
		payload.Effective.Engine.Value != string(encx.DefaultEngineMode) {
		t.Errorf("effective engine = %+v, want the built-in default %q", payload.Effective.Engine, encx.DefaultEngineMode)
	}
	// The API host is derived from the domain when nothing names one, so there is
	// no default the panel could honestly display.
	if payload.Effective.APIBaseURL.Source != llmSourceUnset || payload.Effective.APIBaseURL.Value != "" {
		t.Errorf("effective api_base_url = %+v, want unset", payload.Effective.APIBaseURL)
	}
	if payload.Defaults["engine"] != string(encx.DefaultEngineMode) {
		t.Errorf("defaults = %+v", payload.Defaults)
	}
	if payload.Stored != (engineStoredSettings{}) {
		t.Errorf("stored = %+v, want empty", payload.Stored)
	}
	if len(payload.EnvOverrides) != 0 {
		t.Errorf("env_overrides = %+v, want none", payload.EnvOverrides)
	}
	for _, mode := range []encx.EngineMode{encx.EngineLegacy, encx.EngineNew, encx.EngineAuto} {
		if !slices.Contains(payload.Engines, string(mode)) {
			t.Errorf("engines %v does not offer %q", payload.Engines, mode)
		}
	}
}

func TestWebEngineSettingsPutRoundTrips(t *testing.T) {
	srv := newEngineSettingsTestServer(t)

	status, payload, raw := engineSettingsRequest(t, srv, http.MethodPut,
		`{"engine":"new","api_base_url":"https://api.example.com"}`)
	if status != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", status, raw)
	}
	want := engineStoredSettings{Engine: "new", APIBaseURL: "https://api.example.com"}
	if payload.Stored != want {
		t.Fatalf("stored after PUT = %+v, want %+v", payload.Stored, want)
	}

	status, payload, raw = engineSettingsRequest(t, srv, http.MethodGet, "")
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, body %s", status, raw)
	}
	if payload.Stored != want {
		t.Errorf("stored after GET = %+v, want %+v", payload.Stored, want)
	}
	if payload.Effective.Engine.Source != llmSourceSettings || payload.Effective.Engine.Value != "new" {
		t.Errorf("effective engine = %+v, want the stored value", payload.Effective.Engine)
	}
	if payload.Effective.APIBaseURL.Source != llmSourceSettings ||
		payload.Effective.APIBaseURL.Value != "https://api.example.com" {
		t.Errorf("effective api_base_url = %+v, want the stored value", payload.Effective.APIBaseURL)
	}
}

func TestWebEngineSettingsPutRejectsAnUnknownEngine(t *testing.T) {
	srv := newEngineSettingsTestServer(t)

	status, _, raw := engineSettingsRequest(t, srv, http.MethodPut, `{"engine":"turbo"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("PUT status = %d, want 400, body %s", status, raw)
	}
	// The message is the only guidance the form shows, so it has to name the
	// engines the operator can actually pick.
	for _, mode := range []encx.EngineMode{encx.EngineLegacy, encx.EngineNew, encx.EngineAuto} {
		if !strings.Contains(raw, string(mode)) {
			t.Errorf("error %s does not name %q", raw, mode)
		}
	}
	if _, payload, _ := engineSettingsRequest(t, srv, http.MethodGet, ""); payload.Stored.Engine != "" {
		t.Errorf("a refused engine was stored anyway: %+v", payload.Stored)
	}
}

func TestWebEngineSettingsPutRejectsASchemelessAPIBaseURL(t *testing.T) {
	srv := newEngineSettingsTestServer(t)

	status, _, raw := engineSettingsRequest(t, srv, http.MethodPut, `{"api_base_url":"api.example.com"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("PUT status = %d, want 400, body %s", status, raw)
	}
	if !strings.Contains(raw, "http://") {
		t.Errorf("error %s does not explain the missing scheme", raw)
	}
}

func TestWebEngineSettingsDeleteReturnsToTheDefault(t *testing.T) {
	srv := newEngineSettingsTestServer(t)

	if status, _, raw := engineSettingsRequest(t, srv, http.MethodPut, `{"engine":"legacy"}`); status != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", status, raw)
	}
	status, payload, raw := engineSettingsRequest(t, srv, http.MethodDelete, "")
	if status != http.StatusOK {
		t.Fatalf("DELETE status = %d, body %s", status, raw)
	}
	if payload.Stored != (engineStoredSettings{}) {
		t.Errorf("stored after DELETE = %+v, want empty", payload.Stored)
	}
	if payload.Effective.Engine.Source != llmSourceDefault ||
		payload.Effective.Engine.Value != string(encx.DefaultEngineMode) {
		t.Errorf("effective engine after DELETE = %+v, want the built-in default", payload.Effective.Engine)
	}
}

// The environment outranks the file, so a stored engine that is not in force has
// to be reported rather than silently lost.
func TestWebEngineSettingsReportsTheEnvOverride(t *testing.T) {
	srv := newEngineSettingsTestServer(t)
	t.Setenv(encx.EngineEnvVar, "legacy")

	status, payload, raw := engineSettingsRequest(t, srv, http.MethodGet, "")
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, body %s", status, raw)
	}
	if len(payload.EnvOverrides) != 1 {
		t.Fatalf("env_overrides = %+v, want exactly the engine", payload.EnvOverrides)
	}
	got := payload.EnvOverrides[0]
	if got.Field != "engine" || got.Source != llmSourceEnv || got.EnvVar != encx.EngineEnvVar {
		t.Errorf("env override = %+v, want engine from %s", got, encx.EngineEnvVar)
	}
	if got.ShadowsStored {
		t.Errorf("shadows_stored = true with nothing stored: %+v", got)
	}

	if status, _, raw := engineSettingsRequest(t, srv, http.MethodPut, `{"engine":"new"}`); status != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", status, raw)
	}
	_, payload, raw = engineSettingsRequest(t, srv, http.MethodGet, "")
	if len(payload.EnvOverrides) != 1 || !payload.EnvOverrides[0].ShadowsStored {
		t.Fatalf("env_overrides = %+v, want shadows_stored once a value is saved; body %s", payload.EnvOverrides, raw)
	}
	if payload.Effective.Engine.Value != "legacy" {
		t.Errorf("effective engine = %+v, the environment must outrank the file", payload.Effective.Engine)
	}
}

// The wizard is reached after boot() has already fetched a catalog, so a client
// for the domain is cached with the pre-wizard options before the operator saves
// anything. Engine options are fixed at encx.New, so unless the save drops that
// client every later request keeps the old engine and the derived API host while
// the panel reports the saved ones as being in force.
func TestWebEngineSettingsSaveReachesAlreadyCachedClients(t *testing.T) {
	hub, srv := newEngineSettingsTestHub(t)
	const domain = "quest.en.cx"

	cached := hub.registry.Get(domain, encOptsFromConfig(hub.cfg))
	if cached.EngineMode() != encx.DefaultEngineMode {
		t.Fatalf("cached client engine = %q, want the default %q", cached.EngineMode(), encx.DefaultEngineMode)
	}
	if cached.APIBaseURL() != "https://api.en.cx" {
		t.Fatalf("cached client api_base_url = %q, want the host derived from the domain", cached.APIBaseURL())
	}

	if status, _, raw := engineSettingsRequest(t, srv, http.MethodPut,
		`{"engine":"new","api_base_url":"https://api.selfhosted.internal"}`); status != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", status, raw)
	}

	saved := hub.registry.Get(domain, encOptsFromConfig(hub.cfg))
	if saved.EngineMode() != encx.EngineNew {
		t.Errorf("engine after PUT = %q, want the saved %q", saved.EngineMode(), encx.EngineNew)
	}
	if saved.APIBaseURL() != "https://api.selfhosted.internal" {
		t.Errorf("api_base_url after PUT = %q, want the saved host", saved.APIBaseURL())
	}
	// The old client is untouched on purpose: a request already in flight keeps
	// running against the host it started on.
	if cached.APIBaseURL() != "https://api.en.cx" {
		t.Errorf("the dropped client was mutated: api_base_url = %q", cached.APIBaseURL())
	}

	if status, _, raw := engineSettingsRequest(t, srv, http.MethodDelete, ""); status != http.StatusOK {
		t.Fatalf("DELETE status = %d, body %s", status, raw)
	}
	cleared := hub.registry.Get(domain, encOptsFromConfig(hub.cfg))
	if cleared.EngineMode() != encx.DefaultEngineMode {
		t.Errorf("engine after DELETE = %q, want the default %q", cleared.EngineMode(), encx.DefaultEngineMode)
	}
	if cleared.APIBaseURL() != "https://api.en.cx" {
		t.Errorf("api_base_url after DELETE = %q, want the derived host again", cleared.APIBaseURL())
	}
}

func TestWebEngineProbeRequiresADomain(t *testing.T) {
	srv := newEngineSettingsTestServer(t)

	res, err := http.Get(srv.URL + "/api/v1/engine/probe")
	if err != nil {
		t.Fatalf("GET probe: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("probe without a domain = %d, want 400, body %s", res.StatusCode, raw)
	}
}

// Client.Engine() returns the configured mode without asking anyone when that
// mode is not auto, so a probe that reused the configured client would only ever
// echo the operator's choice back at them. A domain outside the Encounter zones
// has no API host worth asking and resolves to legacy without a request, which
// makes the distinction testable offline.
func TestWebEngineProbeReportsTheHostNotTheConfiguredMode(t *testing.T) {
	srv := newEngineSettingsTestServer(t)
	if status, _, raw := engineSettingsRequest(t, srv, http.MethodPut, `{"engine":"new"}`); status != http.StatusOK {
		t.Fatalf("PUT status = %d, body %s", status, raw)
	}

	res, err := http.Get(srv.URL + "/api/v1/engine/probe?domain=quest.example.invalid")
	if err != nil {
		t.Fatalf("GET probe: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("probe status = %d, body %s", res.StatusCode, raw)
	}
	var got engineProbePayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("parse %s: %v", raw, err)
	}
	if !got.OK || got.Error != "" {
		t.Fatalf("probe = %+v, want a performed probe", got)
	}
	if got.Domain != "quest.example.invalid" {
		t.Errorf("domain = %q, want the queried one", got.Domain)
	}
	if got.Configured != string(encx.EngineNew) {
		t.Errorf("configured = %q, want the stored selection", got.Configured)
	}
	if got.Detected != string(encx.EngineLegacy) {
		t.Errorf("detected = %q, want the host's actual engine", got.Detected)
	}
}
