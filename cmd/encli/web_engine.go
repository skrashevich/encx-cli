package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

// webEngineProbeTimeout bounds the probe endpoint. encx caps its own probe at a
// few seconds, but an unreachable API host can still stall DNS and TLS, and the
// handler must not stay pinned open behind a browser tab the operator closed.
const webEngineProbeTimeout = 15 * time.Second

type engineEffectiveSettings struct {
	Engine     llmFieldSource `json:"engine"`
	APIBaseURL llmFieldSource `json:"api_base_url"`
}

// engineStoredSettings is the file's own content, which is what the form edits.
// It is reported separately from the effective values so a field shadowed by
// the environment still shows the operator what they saved.
type engineStoredSettings struct {
	Engine     string `json:"engine"`
	APIBaseURL string `json:"api_base_url"`
}

type engineSettingsPayload struct {
	Effective    engineEffectiveSettings `json:"effective"`
	Stored       engineStoredSettings    `json:"stored"`
	EnvOverrides []llmEnvOverride        `json:"env_overrides"`
	Defaults     map[string]string       `json:"defaults"`
	Engines      []string                `json:"engines"`
	SettingsPath string                  `json:"settings_path"`
	Error        string                  `json:"error,omitempty"`
}

// engineSettingsUpdate is the form submission.
//
// Both fields are replaced verbatim, so clearing one in the UI clears it in the
// file: an empty field is the operator handing the choice back to the
// environment, not a request to pin an empty value.
type engineSettingsUpdate struct {
	Engine     string `json:"engine"`
	APIBaseURL string `json:"api_base_url"`
}

// engineProbePayload answers "what does this domain actually run on?".
type engineProbePayload struct {
	Domain     string `json:"domain"`
	Detected   string `json:"detected"`
	Configured string `json:"configured"`
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
}

func (h *webHub) httpEngineSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.httpGetEngineSettings(w, r)
	case http.MethodPut:
		h.httpPutEngineSettings(w, r)
	case http.MethodDelete:
		h.httpDeleteEngineSettings(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (h *webHub) httpGetEngineSettings(w http.ResponseWriter, r *http.Request) {
	payload, err := h.engineSettingsPayload()
	if err != nil {
		// A settings file that does not parse still has to be reportable, or the
		// panel that exists to fix it cannot open.
		payload.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *webHub) httpPutEngineSettings(w http.ResponseWriter, r *http.Request) {
	var req engineSettingsUpdate
	if !readJSONBody(w, r, &req) {
		return
	}
	next := engineSettings{
		Engine:     strings.TrimSpace(req.Engine),
		APIBaseURL: strings.TrimSpace(req.APIBaseURL),
	}
	// Validated here as well as in saveEngineSettings so a typo comes back as a
	// 400 the form can show on the offending field, rather than a 500.
	if err := validateEngineMode(next.Engine); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateEngineAPIBaseURL(next.APIBaseURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := saveEngineSettings(next); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.applyEngineSettingsToClients()
	payload, err := h.engineSettingsPayload()
	if err != nil {
		payload.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *webHub) httpDeleteEngineSettings(w http.ResponseWriter, r *http.Request) {
	if err := deleteEngineSettings(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.applyEngineSettingsToClients()
	payload, err := h.engineSettingsPayload()
	if err != nil {
		payload.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, payload)
}

// applyEngineSettingsToClients makes a saved or deleted engine setting take
// effect without a restart.
//
// Cached clients are dropped rather than adjusted in place. ForEachClient plus
// SetEngine would only fix the engine half: the API host is fixed at encx.New,
// so a client cached before the change would keep pointing at the derived host
// while this endpoint truthfully reports the new one as being in force. Dropping
// costs only the rebuild, because AuthRegistry.Get reloads the session from disk
// on a miss, so the operator stays signed in.
//
// A request already in flight keeps its old client and finishes against the old
// host. That is intended: the alternative is cancelling work the operator did
// not ask to cancel, and every request started after the save uses the new
// settings.
func (h *webHub) applyEngineSettingsToClients() {
	if h.registry == nil {
		return
	}
	h.registry.DropClients()
}

// engineConfig returns the hub's config, substituting an empty one when the hub
// was built without any.
//
// Both engine endpoints go through it so they agree on whether a config is
// required: engineSettingsPayload only reads two fields and tolerated a nil,
// while the probe path reaches cfg.insecure through encOptsFromConfig and would
// panic on the same hub.
func (h *webHub) engineConfig() *config {
	if h.cfg != nil {
		return h.cfg
	}
	return &config{}
}

// engineSettingsPayload assembles what the panel renders: the stored form
// values, the values actually in force, and where each of those came from.
func (h *webHub) engineSettingsPayload() (engineSettingsPayload, error) {
	stored, loadErr := loadEngineSettings()

	cfg := h.engineConfig()
	engineField := resolveEngineSettingField(cfg.engine, engineEnvVars, stored.Engine, string(encx.DefaultEngineMode))
	// The API host has no built-in default: when nobody names one it is derived
	// from the domain, which this endpoint does not know. Reporting it as unset
	// is honest, where naming a host here would be a guess the operator would
	// read as a setting.
	baseField := resolveEngineSettingField(cfg.apiBaseURL, engineAPIBaseURLEnvVars, stored.APIBaseURL, "")

	payload := engineSettingsPayload{
		Effective: engineEffectiveSettings{
			Engine:     engineField,
			APIBaseURL: baseField,
		},
		Stored: engineStoredSettings{
			Engine:     stored.Engine,
			APIBaseURL: stored.APIBaseURL,
		},
		Defaults: map[string]string{
			"engine": string(encx.DefaultEngineMode),
		},
		Engines:      []string{string(encx.EngineLegacy), string(encx.EngineNew), string(encx.EngineAuto)},
		SettingsPath: engineSettingsFile(),
	}

	payload.EnvOverrides = collectEnvOverrides([]envOverrideField{
		{"engine", engineField, stored.Engine},
		{"api_base_url", baseField, stored.APIBaseURL},
	})
	return payload, loadErr
}

func (h *webHub) httpEngineProbe(w http.ResponseWriter, r *http.Request) {
	domain := strings.TrimSpace(r.URL.Query().Get("domain"))
	if domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "domain query required"})
		return
	}

	// A settings file that does not parse is not worth failing the probe over:
	// loadEngineSettings hands back empty settings with the error, so the
	// resolution below still reports the engine that would actually be used.
	// The settings endpoint is where that error belongs.
	payload, _ := h.engineSettingsPayload()
	result := engineProbePayload{
		Domain:     domain,
		Configured: payload.Effective.Engine.Value,
	}

	ctx, cancel := context.WithTimeout(r.Context(), webEngineProbeTimeout)
	defer cancel()
	detected, err := probeEngineForDomain(ctx, h.engineConfig(), domain)
	if err != nil {
		result.Error = err.Error()
		writeJSON(w, http.StatusOK, result)
		return
	}
	result.OK = true
	result.Detected = string(detected)
	writeJSON(w, http.StatusOK, result)
}

// probeEngineForDomain asks the API host which backend serves domain.
//
// The client is built here instead of taken from h.registry because the probe
// has to run in auto mode — Client.Engine() short-circuits and returns the
// configured mode without asking anyone when that mode is legacy or new — and
// calling SetEngine on a registry client would leave every later request from
// that cached client resolving by probe instead of by the operator's choice.
// A throwaway client cannot have that effect. It also needs no session: the
// probe asks the API host about the domain, not about the operator.
func probeEngineForDomain(ctx context.Context, cfg *config, domain string) (encx.EngineMode, error) {
	opts := append(encOptsFromConfig(cfg), encx.WithEngine(encx.EngineAuto))
	client := encx.New(domain, opts...)

	// Client.Engine() resolves against context.Background(), so the deadline is
	// watched here instead. The probe goroutine reports into a buffered channel
	// and is bounded by encx's own probe timeout, so abandoning it leaks nothing.
	done := make(chan encx.EngineMode, 1)
	go func() { done <- client.Engine() }()
	select {
	case mode := <-done:
		return mode, nil
	case <-ctx.Done():
		return "", fmt.Errorf("engine probe for %s: %w", domain, ctx.Err())
	}
}
