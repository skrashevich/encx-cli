package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
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

	hub := &webHub{cfg: &config{}, registry: NewAuthRegistry(), store: NewChatStore(), sse: newSSEHub()}
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
	if len(payload.Steps) != 3 {
		t.Fatalf("steps = %+v, want llm, engine and auth", payload.Steps)
	}
	for _, id := range []string{"llm", "engine", "auth"} {
		if step := onboardingStepByID(t, payload, id); step.Done {
			t.Errorf("step %q = %+v, want not done on a bare machine", id, step)
		}
	}
}

// The wizard must stay closed across restarts, so the verdict has to survive on
// disk rather than only in the response the browser happened to see.
func TestWebOnboardingCompletePersists(t *testing.T) {
	srv := newOnboardingTestServer(t)

	// The Finish button sends no body; that is the happy path, not a bad request.
	status, payload, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete", "")
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

func TestWebOnboardingCompleteSkippedRoundTrips(t *testing.T) {
	srv := newOnboardingTestServer(t)

	status, payload, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete", `{"skipped":true}`)
	if status != http.StatusOK {
		t.Fatalf("POST complete status = %d, body %s", status, raw)
	}
	if !payload.Skipped || payload.Required {
		t.Fatalf("payload = %+v, want a skipped completion", payload)
	}
	_, payload, raw = onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", "")
	if !payload.Skipped || payload.Required {
		t.Fatalf("GET after skip = %s, want skipped and not required", raw)
	}
}

func TestWebOnboardingResetMakesItRequiredAgain(t *testing.T) {
	srv := newOnboardingTestServer(t)

	if status, _, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete", ""); status != http.StatusOK {
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

// The built-in default is not a choice, so only an engine the operator actually
// selected closes the step.
func TestWebOnboardingEngineStepFollowsTheChoice(t *testing.T) {
	srv := newOnboardingTestServer(t)

	_, payload, _ := onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", "")
	if step := onboardingStepByID(t, payload, "engine"); step.Done {
		t.Fatalf("engine step = %+v, want not done while nothing is configured", step)
	}

	t.Setenv(encx.EngineEnvVar, "new")
	_, payload, _ = onboardingRequest(t, srv, http.MethodGet, "/api/v1/onboarding", "")
	step := onboardingStepByID(t, payload, "engine")
	if !step.Done {
		t.Fatalf("engine step = %+v, want done once %s selects one", step, encx.EngineEnvVar)
	}
	if step.Detail == "" {
		t.Errorf("engine step = %+v, want the value and its source named", step)
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

// -web holds the download back until the operator has been shown the choice of
// transport, and finishing the wizard is that moment: the start it skipped on a
// bare machine has to happen here, or someone who left local inference as it was
// pays for it on their first message instead.
func TestWebOnboardingCompleteStartsTheLocalDownload(t *testing.T) {
	srv := newOnboardingTestServer(t)

	// Before the wizard has offered the choice, the gate holds the download back.
	if localPrefetchInvited(&config{}) {
		t.Fatal("a bare machine reports the choice of transport as already offered")
	}

	var started int
	startLocalPrefetch = func(_ context.Context, _ *config, trigger localPrefetchTrigger, _ func(string)) {
		if trigger != prefetchWhenInvited {
			t.Errorf("trigger = %v, want the gated one", trigger)
		}
		started++
	}

	if status, payload, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete", ""); status != http.StatusOK {
		t.Fatalf("POST complete status = %d, body %s", status, raw)
	} else if !payload.Completed {
		t.Fatalf("payload = %+v, want the wizard completed", payload)
	}

	if started != 1 {
		t.Errorf("finishing the wizard started the download %d times, want once", started)
	}
	if !localPrefetchInvited(&config{}) {
		t.Error("the gate still holds after the wizard finished")
	}
}

// An operator who used the wizard to configure a cloud provider gets the
// opposite: whatever was already coming down for local inference is abandoned
// rather than paid for in full.
func TestWebOnboardingCompleteStopsAPointlessDownload(t *testing.T) {
	srv := newOnboardingTestServer(t)
	t.Setenv("LLM_API_KEY", "sk-test-key")

	stopped := 0
	localPrefetchMu.Lock()
	localPrefetchCancel = func() { stopped++ }
	localPrefetchFor = localConfigFrom(llmSettings{})
	localPrefetchMu.Unlock()

	if status, _, raw := onboardingRequest(t, srv, http.MethodPost, "/api/v1/onboarding/complete", ""); status != http.StatusOK {
		t.Fatalf("POST complete status = %d, body %s", status, raw)
	}
	if stopped != 1 {
		t.Errorf("the local download was stopped %d times, want once", stopped)
	}
}
