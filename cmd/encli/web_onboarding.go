package main

import (
	"cmp"
	"fmt"
	"net/http"
	"time"
)

// onboardingStep is one item of the wizard's checklist. Its ID is what the
// browser keys on and its Title is a stable English identifier: the wizard's
// HTML already carries the Russian labels the operator reads, so translating
// here would only give the UI a second source of truth to drift from.
type onboardingStep struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Done   bool   `json:"done"`
	Detail string `json:"detail,omitempty"`
}

type onboardingStatusPayload struct {
	Required    bool             `json:"required"`
	Completed   bool             `json:"completed"`
	CompletedAt string           `json:"completed_at,omitempty"`
	Skipped     bool             `json:"skipped"`
	Steps       []onboardingStep `json:"steps"`
	Error       string           `json:"error,omitempty"`
}

func (h *webHub) httpOnboardingStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.onboardingStatusPayload())
}

func (h *webHub) httpOnboardingComplete(w http.ResponseWriter, r *http.Request) {
	var req authLoginBody
	if !readJSONBody(w, r, &req) {
		return
	}
	if !h.authenticateWebLogin(w, r, req) {
		return
	}

	state := onboardingState{
		Completed:   true,
		CompletedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveOnboardingState(state); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, h.onboardingStatusPayload())
}

func (h *webHub) httpOnboardingReset(w http.ResponseWriter, r *http.Request) {
	if err := resetOnboardingState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, h.onboardingStatusPayload())
}

// onboardingStatusPayload reports whether the wizard has to run and how far the
// two things it configures already are.
//
// A state file that will not parse is reported rather than propagated: refusing
// the request would make the wizard unreachable, and the operator would have no
// way left to configure the thing that wrote the broken file. An unreadable
// state counts as not completed, so the worst case is being asked once more.
func (h *webHub) onboardingStatusPayload() onboardingStatusPayload {
	state, loadErr := loadOnboardingState()

	payload := onboardingStatusPayload{
		Required:    !state.Completed,
		Completed:   state.Completed,
		CompletedAt: state.CompletedAt,
		Skipped:     state.Skipped,
		Steps: []onboardingStep{
			h.onboardingLLMStep(),
			h.onboardingAuthStep(),
		},
	}
	if loadErr != nil {
		payload.Error = loadErr.Error()
	}
	return payload
}

// onboardingLLMStep is done when resolveAgentConfig would hand the agent a
// working transport, which is the same question the first wizard step asks.
func (h *webHub) onboardingLLMStep() onboardingStep {
	step := onboardingStep{ID: "llm", Title: "LLM transport"}
	agentCfg, err := resolveAgentConfig(h.cfg)
	if err != nil {
		step.Detail = err.Error()
		return step
	}
	step.Done = true
	// An empty auth method is the plain API-key path; naming it keeps the detail
	// readable instead of starting with a blank.
	step.Detail = fmt.Sprintf("%s, model %s", cmp.Or(agentCfg.AuthMethod, authMethodAPIKey), agentCfg.Model)
	return step
}

// onboardingAuthStep is done as soon as one Encounter domain has a session; the
// wizard asks for one login, not for every domain the operator will ever use.
func (h *webHub) onboardingAuthStep() onboardingStep {
	step := onboardingStep{ID: "auth", Title: "en.cx login", Detail: "no Encounter session"}
	if h.registry == nil {
		return step
	}
	for _, st := range h.registry.ListStatus() {
		if st.HasSession {
			step.Done = true
			step.Detail = "signed in: " + st.Domain
			return step
		}
	}
	return step
}
