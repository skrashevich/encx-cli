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

// newOnboardingTestServer isolates everything the wizard reads: the home
// directory the sessions live in, the LLM transport, the engine selection and
// the state file itself. Without all four a developer who has encli configured
// would see the opposite verdict from a machine that does not.
func newOnboardingTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	isolateLLMEnv(t)
	silentEngineEnv(t)
	t.Setenv(onboardingFileEnvVar, filepath.Join(t.TempDir(), "onboarding", "state.json"))

	hub := &webHub{cfg: &config{useHTTP: true, engine: "legacy"}, registry: NewAuthRegistry(), store: NewChatStore(), sse: newSSEHub()}
	srv := httptest.NewTestServer(t, hub.newMux())
	srv.Start()
	return srv
}

func onboardingRequest(t *testing.T, srv *httptest.Server, method, path, body string) (int, onboardingStatusPayload, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = stringsReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var payload onboardingStatusPayload
	_ = json.Unmarshal(raw, &payload)
	return res.StatusCode, payload, string(raw)
}

func onboardingStepByID(t *testing.T, payload onboardingStatusPayload, id string) onboardingStep {
	t.Helper()
	for _, s := range payload.Steps {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no %q step in %+v", id, payload.Steps)
	return onboardingStep{}
}

func TestWebOnboardingRequiredOnACleanMachine(t *testing.T) {
	srv := newOnboardingTestServer(t)

	status, payload, raw := onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", "")
	if status != http.StatusOK {
		t.Fatalf("GET status = %d, body %s", status, raw)
	}
	if !payload.Required || payload.Completed {
		t.Fatalf("payload = %+v, want the wizard required", payload)
	}
	if len(payload.Steps) != 2 {
		t.Fatalf("steps = %+v, want llm and auth", payload.Steps)
	}
	for _, id := range []string{"llm", "auth"} {
		if step := onboardingStepByID(t, payload, id); step.Done {
			t.Errorf("step %q = %+v, want not done on a bare machine", id, step)
		}
	}
}

// The wizard must stay closed across restarts, so the verdict has to survive on
// disk rather than only in the response the browser happened to see.
func TestWebOnboardingCompletePersists(t *testing.T) {
	srv := newOnboardingTestServer(t)

	// Completion validates credentials before persisting the onboarding state.
	status, payload, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete", onboardingCredentials(t))
	if status != http.StatusOK {
		t.Fatalf("POST complete status = %d, body %s", status, raw)
	}
	if payload.Required || !payload.Completed || payload.CompletedAt == "" {
		t.Fatalf("payload = %+v, want completed with a timestamp", payload)
	}

	data, err := os.ReadFile(onboardingFile())
	if err != nil {
		t.Fatalf("read %s: %v", onboardingFile(), err)
	}
	var onDisk onboardingState
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("parse %s: %v", onboardingFile(), err)
	}
	if !onDisk.Completed || onDisk.CompletedAt == "" || onDisk.Skipped {
		t.Fatalf("state on disk = %+v, want completed and not skipped", onDisk)
	}

	_, payload, raw = onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", "")
	if payload.Required {
		t.Fatalf("GET after complete = %s, want required=false", raw)
	}
}

func TestWebOnboardingRejectsMissingCredentials(t *testing.T) {
	srv := newOnboardingTestServer(t)
	for _, body := range []string{"", "{}", `{"skipped":true}`, `{"domain":"demo.en.cx","login":"player"}`} {
		status, _, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete", body)
		if status != http.StatusBadRequest {
			t.Fatalf("status=%d body=%s", status, raw)
		}
	}
	state, err := loadOnboardingState()
	if err != nil || state.Completed {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func TestWebOnboardingRejectsInvalidCredentials(t *testing.T) {
	srv := newOnboardingTestServer(t)
	body := strings.Replace(onboardingCredentials(t), "secret", "wrong", 1)
	status, _, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete", body)
	if status != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", status, raw)
	}
	state, err := loadOnboardingState()
	if err != nil || state.Completed {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func onboardingCredentials(t *testing.T) string {
	t.Helper()
	upstream := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/login/signin" || r.Method != http.MethodPost {
			t.Errorf("unexpected login request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Form.Get("Login") != "player" || r.Form.Get("Password") != "secret" {
			_, _ = w.Write([]byte(`{"Error":1}`))
			return
		}
		_, _ = w.Write([]byte(`{"Error":0}`))
	}))
	upstream.Start()
	body, err := json.Marshal(authLoginBody{Domain: strings.TrimPrefix(upstream.URL, "http://"), Login: "player", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestWebOnboardingResetMakesItRequiredAgain(t *testing.T) {
	srv := newOnboardingTestServer(t)

	if status, _, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete", onboardingCredentials(t)); status != http.StatusOK {
		t.Fatalf("POST complete status = %d, body %s", status, raw)
	}
	status, payload, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/reset", "")
	if status != http.StatusOK {
		t.Fatalf("POST reset status = %d, body %s", status, raw)
	}
	if !payload.Required || payload.Completed || payload.CompletedAt != "" {
		t.Fatalf("payload after reset = %+v, want the wizard required again", payload)
	}
	if _, err := os.Stat(onboardingFile()); !os.IsNotExist(err) {
		t.Fatalf("stat %s after reset = %v, want the file gone", onboardingFile(), err)
	}
}

func TestWebOnboardingLLMStepFollowsTheTransport(t *testing.T) {
	srv := newOnboardingTestServer(t)

	_, payload, _ := onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", "")
	step := onboardingStepByID(t, payload, "llm")
	if step.Done {
		t.Fatalf("llm step = %+v, want not done with no transport configured", step)
	}
	if step.Detail == "" {
		t.Errorf("llm step = %+v, want the reason it cannot run", step)
	}

	t.Setenv("LLM_API_KEY", "sk-test-key")
	t.Setenv("LLM_MODEL", "test-model")
	_, payload, _ = onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", "")
	step = onboardingStepByID(t, payload, "llm")
	if !step.Done {
		t.Fatalf("llm step = %+v, want done once a key and a model are exported", step)
	}
	if step.Detail == "" {
		t.Errorf("llm step = %+v, want the method and model named", step)
	}
}

// The state file sits next to the LLM and engine settings and is written the
// same way, so it has to earn the same guarantee they are checked for: the
// wizard's verdict is not something another local account gets to read or
// rewrite.
func TestSaveOnboardingStateFilePermissions(t *testing.T) {
	// Use a nested path so the directory mode is the one saveOnboardingState
	// creates rather than the temp directory's.
	path := filepath.Join(t.TempDir(), "onboarding", "state.json")
	t.Setenv(onboardingFileEnvVar, path)

	if err := saveOnboardingState(onboardingState{Completed: true, CompletedAt: "2024-01-01T00:00:00Z"}); err != nil {
		t.Fatalf("save onboarding state: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat state: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("state mode = %v, want 0600", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat state dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0700 {
		t.Fatalf("state dir mode = %v, want 0700", perm)
	}
}
