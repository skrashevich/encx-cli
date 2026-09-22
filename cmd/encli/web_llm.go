package main

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"
)

// The -web settings panel configures the two transports an operator can set up
// from a browser: an OpenAI-compatible provider, and a ChatGPT subscription.
// GigaChat stays on the environment for now; it is still accepted here so the
// API and resolveAgentConfig agree on what a valid auth method is.
var llmSettableAuthMethods = []string{authMethodAPIKey, authMethodCodex, authMethodGigaChat}

// llmAPIKeyStatus describes the key without disclosing it. The panel needs to
// know that a key exists and which one, not what it is: -web binds to localhost
// by default but the response would otherwise put the key in every browser cache
// and devtools log that ever displayed the panel.
type llmAPIKeyStatus struct {
	HasValue bool   `json:"has_value"`
	Masked   string `json:"masked,omitempty"`
	Source   string `json:"source"`
	EnvVar   string `json:"env_var,omitempty"`
}

type llmEffectiveSettings struct {
	AuthMethod llmFieldSource  `json:"auth_method"`
	BaseURL    llmFieldSource  `json:"base_url"`
	Model      llmFieldSource  `json:"model"`
	APIKey     llmAPIKeyStatus `json:"api_key"`
}

// llmStoredSettings is the file's own content, which is what the form edits.
// It is reported separately from the effective values so a field shadowed by
// the environment still shows the operator what they saved.
type llmStoredSettings struct {
	AuthMethod   string `json:"auth_method"`
	BaseURL      string `json:"base_url"`
	Model        string `json:"model"`
	HasAPIKey    bool   `json:"has_api_key"`
	APIKeyMasked string `json:"api_key_masked,omitempty"`
}

// llmEnvOverride is one field the panel cannot change from here.
type llmEnvOverride struct {
	Field string `json:"field"`
	// Source is env or flag; EnvVar names the variable when there is one.
	Source string `json:"source"`
	EnvVar string `json:"env_var,omitempty"`
	// ShadowsStored marks the case worth a loud badge: something IS saved for
	// this field and it is not the value in use.
	ShadowsStored bool `json:"shadows_stored"`
}

// envOverrideField pairs one resolved field with what the settings file holds
// for it, which is all collectEnvOverrides needs in order to judge it.
type envOverrideField struct {
	Field  string
	Source llmFieldSource
	Stored string
}

// collectEnvOverrides picks out the fields a settings panel cannot change from
// where it is. Both the LLM and the engine panel ask this same question, so it
// is answered once: a panel may only offer to edit what it stores, and a value
// that arrived from a flag or the environment therefore has to be explained
// rather than silently lost to the form.
func collectEnvOverrides(fields []envOverrideField) []llmEnvOverride {
	var out []llmEnvOverride
	for _, f := range fields {
		if !llmSourceIsAmbient(f.Source.Source) {
			continue
		}
		out = append(out, llmEnvOverride{
			Field:         f.Field,
			Source:        f.Source.Source,
			EnvVar:        f.Source.EnvVar,
			ShadowsStored: strings.TrimSpace(f.Stored) != "",
		})
	}
	return out
}

type llmCodexStatus struct {
	SignedIn  bool   `json:"signed_in"`
	AccountID string `json:"account_id,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Expired   bool   `json:"expired"`
	Error     string `json:"error,omitempty"`
	Path      string `json:"path"`
}

// llmAgentSummary is what resolveAgentConfig would hand the agent right now,
// so the panel can show the outcome rather than make the operator infer it.
type llmAgentSummary struct {
	AuthMethod string `json:"auth_method"`
	Model      string `json:"model"`
	BaseURL    string `json:"base_url"`
	Error      string `json:"error,omitempty"`
}

type llmSettingsPayload struct {
	Effective    llmEffectiveSettings `json:"effective"`
	Stored       llmStoredSettings    `json:"stored"`
	EnvOverrides []llmEnvOverride     `json:"env_overrides"`
	Defaults     map[string]string    `json:"defaults"`
	AuthMethods  []string             `json:"auth_methods"`
	Codex        llmCodexStatus       `json:"codex"`
	Agent        llmAgentSummary      `json:"agent"`
	SettingsPath string               `json:"settings_path"`
	Error        string               `json:"error,omitempty"`
}

// llmSettingsUpdate is the form submission.
//
// The non-secret fields are replaced verbatim, so clearing one in the UI clears
// it in the file. The key cannot work that way: the form never receives it, so
// an empty string means "leave it alone" and clearing is an explicit act.
type llmSettingsUpdate struct {
	AuthMethod  string `json:"auth_method"`
	BaseURL     string `json:"base_url"`
	Model       string `json:"model"`
	APIKey      string `json:"api_key"`
	ClearAPIKey bool   `json:"clear_api_key"`
}

func (h *webHub) httpLLMSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.httpGetLLMSettings(w, r)
	case http.MethodPut:
		h.httpPutLLMSettings(w, r)
	case http.MethodDelete:
		h.httpDeleteLLMSettings(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
	}
}

func (h *webHub) httpGetLLMSettings(w http.ResponseWriter, r *http.Request) {
	payload, err := h.llmSettingsPayload()
	if err != nil {
		// A settings file that does not parse still has to be reportable, or the
		// panel that exists to fix it cannot open.
		payload.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *webHub) httpPutLLMSettings(w http.ResponseWriter, r *http.Request) {
	var req llmSettingsUpdate
	if !readJSONBody(w, r, &req) {
		return
	}
	authMethod := strings.ToLower(strings.TrimSpace(req.AuthMethod))
	if authMethod != "" && !slices.Contains(llmSettableAuthMethods, authMethod) {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": fmt.Sprintf("unknown LLM auth method %q: use %s", req.AuthMethod, strings.Join(llmSettableAuthMethods, ", ")),
		})
		return
	}
	if err := validateLLMBaseURL(req.BaseURL); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Start from what is stored so the key survives a form that never saw it.
	// A file that does not parse is replaced rather than refused: the operator
	// came here to set the transport, and there is nothing worth preserving.
	current, err := loadLLMSettings()
	if err != nil {
		debugf("llm settings: replacing an unreadable file: %v", err)
		current = llmSettings{}
	}
	next := llmSettings{
		AuthMethod: authMethod,
		BaseURL:    strings.TrimSpace(req.BaseURL),
		Model:      strings.TrimSpace(req.Model),
		APIKey:     current.APIKey,
	}
	switch {
	case req.ClearAPIKey:
		next.APIKey = ""
	case strings.TrimSpace(req.APIKey) != "":
		next.APIKey = strings.TrimSpace(req.APIKey)
	}

	if err := saveLLMSettings(next); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	payload, err := h.llmSettingsPayload()
	if err != nil {
		payload.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, payload)
}

func (h *webHub) httpDeleteLLMSettings(w http.ResponseWriter, r *http.Request) {
	if err := deleteLLMSettings(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	payload, err := h.llmSettingsPayload()
	if err != nil {
		payload.Error = err.Error()
	}
	writeJSON(w, http.StatusOK, payload)
}

// llmSettingsPayload assembles what the panel renders: the stored form values,
// the values actually in force, and where each of those came from.
func (h *webHub) llmSettingsPayload() (llmSettingsPayload, error) {
	stored, loadErr := loadLLMSettings()

	var explicitAuth string
	if h.cfg != nil {
		explicitAuth = h.cfg.llmAuth
	}
	authField := resolveLLMField(explicitAuth, llmAuthEnvVars, stored.AuthMethod, "")
	baseField := resolveLLMField("", llmBaseURLEnvVars, stored.BaseURL, defaultLLMBaseURL)
	modelField := resolveLLMField("", llmModelEnvVars, stored.Model, defaultLLMModel)
	keyField := resolveLLMField("", llmAPIKeyEnvVars, stored.APIKey, "")

	payload := llmSettingsPayload{
		Effective: llmEffectiveSettings{
			AuthMethod: authField,
			BaseURL:    baseField,
			Model:      modelField,
			APIKey: llmAPIKeyStatus{
				HasValue: keyField.Value != "",
				Masked:   maskAPIKey(keyField.Value),
				Source:   keyField.Source,
				EnvVar:   keyField.EnvVar,
			},
		},
		Stored: llmStoredSettings{
			AuthMethod:   stored.AuthMethod,
			BaseURL:      stored.BaseURL,
			Model:        stored.Model,
			HasAPIKey:    stored.APIKey != "",
			APIKeyMasked: maskAPIKey(stored.APIKey),
		},
		Defaults: map[string]string{
			"base_url":    defaultLLMBaseURL,
			"model":       defaultLLMModel,
			"codex_model": defaultCodexModel,
		},
		AuthMethods:  llmSettableAuthMethods,
		Codex:        codexStatusForWeb(),
		SettingsPath: llmSettingsFile(),
	}

	payload.EnvOverrides = collectEnvOverrides([]envOverrideField{
		{"auth_method", authField, stored.AuthMethod},
		{"base_url", baseField, stored.BaseURL},
		{"model", modelField, stored.Model},
		{"api_key", keyField, stored.APIKey},
	})

	agentCfg, agentErr := resolveAgentConfig(h.cfg)
	payload.Agent = llmAgentSummary{
		AuthMethod: agentCfg.AuthMethod,
		Model:      agentCfg.Model,
		BaseURL:    agentCfg.BaseURL,
	}
	if agentErr != nil {
		payload.Agent.Error = agentErr.Error()
	}
	return payload, loadErr
}

// codexStatusForWeb reports the stored ChatGPT sign-in for the panel. A missing
// credential is a state, not a failure, so only a broken file carries an error.
func codexStatusForWeb() llmCodexStatus {
	status := llmCodexStatus{Path: codexAuthFile()}
	cred, err := loadCodexCredential()
	if err != nil {
		if !errors.Is(err, errNoCodexCredential) {
			status.Error = err.Error()
		}
		return status
	}
	status.SignedIn = true
	status.AccountID = cred.AccountID
	if !cred.ExpiresAt.IsZero() {
		status.ExpiresAt = cred.ExpiresAt.Format(time.RFC3339)
		status.Expired = time.Now().After(cred.ExpiresAt)
	}
	return status
}
