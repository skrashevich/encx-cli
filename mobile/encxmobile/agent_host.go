package encxmobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// AuthMethodOnDevice runs the model in the host process instead of calling a
// provider. The host drives the conversation and calls InvokeTool; the engine
// toolset, its policy gate, confirmations, pacing and caching are unchanged.
const AuthMethodOnDevice = "on-device"

// hostToolDescription is one tool as the host needs to see it.
type hostToolDescription struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Mutating    bool           `json:"mutating"`
}

// ToolCatalogJSON lists the engine tools for a host-driven loop.
//
// Apple's on-device model runs inside the app and cannot be reached through a
// provider, so the host owns the conversation and asks for the toolset instead.
func (s *AgentSession) ToolCatalogJSON() (string, error) {
	tools := s.catalog.Tools()
	described := make([]hostToolDescription, 0, len(tools))
	for _, tool := range tools {
		described = append(described, hostToolDescription{
			Name:        tool.Name(),
			Description: tool.Description(),
			Parameters:  tool.Parameters(),
			Mutating:    tool.Mutating(),
		})
	}
	return marshalJSON(described)
}

// SystemPrompt is the instruction text describing the engine and the active
// access policy. A host-driven loop has to supply it itself.
func (s *AgentSession) SystemPrompt() string {
	return s.systemPrompt
}

// BeginHostTurn opens a host-driven turn and returns its number.
//
// It mirrors what SendMessage does for the built-in loop: it claims the session,
// bumps the turn counter so confirmations can be matched, and drops cached reads
// so a new question sees fresh state.
func (s *AgentSession) BeginHostTurn() (int64, error) {
	ctx, cancel := context.WithCancel(context.Background())

	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		cancel()
		return 0, errors.New("encxmobile: a turn is already running")
	}
	s.cancel = cancel
	s.hostCtx = ctx
	s.turn++
	turn := s.turn
	s.mu.Unlock()

	s.catalog.InvalidateCache()
	s.emit(turn, map[string]any{"type": "turn_started"})
	return turn, nil
}

// EndHostTurn closes a host-driven turn. content is the reply the host produced;
// an empty string reports the turn as failed with reason.
func (s *AgentSession) EndHostTurn(content string, reason string) {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.hostCtx = nil
	turn := s.turn
	s.failPendingLocked()
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	if strings.TrimSpace(content) == "" {
		s.emit(turn, map[string]any{"type": "turn_failed", "error": reason})
		return
	}
	s.emit(turn, map[string]any{"type": "turn_finished", "content": content})
}

// RecordHostExchange appends a completed host-driven exchange to the transcript
// the session remembers.
func (s *AgentSession) RecordHostExchange(userMessage, assistantMessage string) {
	if strings.TrimSpace(userMessage) == "" || strings.TrimSpace(assistantMessage) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history,
		hostMessage("user", userMessage),
		hostMessage("assistant", assistantMessage),
	)
}

// InvokeTool runs one engine tool by name and returns its JSON result.
//
// This is the same path the built-in loop takes: the policy gate runs first, so
// a mutating call still needs the player's confirmation, and pacing and caching
// still apply.
func (s *AgentSession) InvokeTool(name string, argsJSON string) (string, error) {
	tool, ok := s.catalog.Lookup(strings.TrimSpace(name))
	if !ok {
		return "", fmt.Errorf("encxmobile: unknown tool %q", name)
	}

	args := map[string]any{}
	if trimmed := strings.TrimSpace(argsJSON); trimmed != "" && trimmed != "null" {
		if err := json.Unmarshal([]byte(trimmed), &args); err != nil {
			return "", fmt.Errorf("encxmobile: arguments for %s are not a JSON object: %w", name, err)
		}
	}

	turn := s.currentTurn()
	callID := s.nextCallID()
	ctx := withCallID(s.hostContext(), callID)

	s.emit(turn, map[string]any{
		"type":    "tool_started",
		"call_id": callID,
		"tool":    tool.Name(),
		"args":    args,
	})

	result := tool.Execute(ctx, args)

	event := map[string]any{
		"type":    "tool_finished",
		"call_id": callID,
		"tool":    tool.Name(),
	}
	if result != nil {
		event["is_error"] = result.IsError
		event["result"] = truncateForEvent(result.ForLLM)
	}
	s.emit(turn, event)

	if result == nil {
		return "", fmt.Errorf("encxmobile: %s returned no result", name)
	}
	if result.IsError {
		return "", errors.New(result.ForLLM)
	}
	return result.ForLLM, nil
}

// hostContext returns the context of the running host turn so Cancel() reaches
// tools the host started.
func (s *AgentSession) hostContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hostCtx != nil {
		return s.hostCtx
	}
	return context.Background()
}
