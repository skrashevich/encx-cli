package encxmobile

import (
	"context"
	"encoding/json"
	"fmt"

	toolshared "github.com/sipeed/picoclaw/pkg/tools/shared"
	"github.com/skrashevich/encx-cli/agenttools"
	"github.com/skrashevich/encx-cli/internal/jevguard"
)

// eventResultLimit caps the tool output copied into a progress event. The UI
// shows an activity line, not the payload; the model still receives the full
// result.
const eventResultLimit = 2000

// callIDKey carries the per-execution identifier from the observation layer down
// to the confirmation gate.
//
// PicoClaw runs every tool call of one iteration in parallel goroutines, so a
// turn can have several confirmations in flight. Both the progress events and
// the confirmation request must name the same call or the host cannot tell them
// apart.
type callIDKey struct{}

func withCallID(ctx context.Context, callID string) context.Context {
	return context.WithValue(ctx, callIDKey{}, callID)
}

func callIDFrom(ctx context.Context) (string, bool) {
	callID, ok := ctx.Value(callIDKey{}).(string)
	return callID, ok && callID != ""
}

// observedTool reports a tool's lifecycle to the host while delegating the work
// to the catalog tool underneath.
type observedTool struct {
	inner   toolshared.Tool
	session *AgentSession
}

func (s *AgentSession) observe(tool toolshared.Tool) toolshared.Tool {
	return &observedTool{inner: tool, session: s}
}

func (t *observedTool) Name() string { return t.inner.Name() }

func (t *observedTool) Description() string { return t.inner.Description() }

func (t *observedTool) Parameters() map[string]any { return t.inner.Parameters() }

func (t *observedTool) Execute(ctx context.Context, args map[string]any) *toolshared.ToolResult {
	turn := t.session.currentTurn()
	callID := t.session.nextCallID()
	ctx = withCallID(ctx, callID)

	t.session.emit(turn, map[string]any{
		"type":    "tool_started",
		"call_id": callID,
		"tool":    t.inner.Name(),
		"args":    args,
	})

	var result *toolshared.ToolResult
	t.session.mu.Lock()
	evaluation := t.session.jevTurn
	t.session.mu.Unlock()
	mutating := false
	if tool, ok := t.inner.(interface{ Mutating() bool }); ok {
		mutating = tool.Mutating()
	}
	// Readonly remains an unconditional catalog restriction. No JEV call or
	// extra approval can turn it into permission to execute a mutation.
	if evaluation != nil && !(mutating && t.session.catalog.Policy() == agenttools.PolicyReadonly) {
		decision := evaluation.Check(ctx, jevguard.Call{Name: t.Name(), Arguments: args, Mutating: mutating, ContentMutation: mutating})
		t.session.emit(turn, map[string]any{"type": "jev_tool", "call_id": callID, "tool": t.Name(), "action": decision.Action, "model": decision.Model, "cost_rub": decision.CostRub})
		switch decision.Action {
		case "deny", "propose":
			result = toolshared.ErrorResult(decision.Reason)
		case "confirm":
			// The approve catalog already confirms this exact call. Full access
			// needs an extra confirmation when semantic scope is uncertain.
			if t.session.catalog.Policy() != agenttools.PolicyApprove {
				allowed, err := (sessionConfirmer{session: t.session}).ConfirmToolCall(ctx, agenttools.ConfirmRequest{Tool: t.Name(), Args: args})
				if err != nil {
					result = toolshared.ErrorResult(err.Error()).WithError(err)
				} else if !allowed {
					result = toolshared.ErrorResult("The user declined this action. Do not retry it.")
				}
			}
		}
	}
	if result == nil {
		result = t.inner.Execute(ctx, args)
		if evaluation != nil && mutating {
			evaluation.ForgetEvidence()
		}
	}
	if evaluation != nil && !mutating && result != nil && !result.IsError {
		evaluation.Remember(t.Name(), args, result.ForLLM)
	}

	event := map[string]any{
		"type":    "tool_finished",
		"call_id": callID,
		"tool":    t.inner.Name(),
	}
	if result != nil {
		event["is_error"] = result.IsError
		event["result"] = truncateForEvent(result.ForLLM)
	}
	t.session.emit(turn, event)
	return result
}

func truncateForEvent(text string) string {
	runes := []rune(text)
	if len(runes) <= eventResultLimit {
		return text
	}
	return string(runes[:eventResultLimit]) + "…"
}

func (s *AgentSession) currentTurn() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.turn
}

// nextCallID returns a fresh identifier for one tool execution.
func (s *AgentSession) nextCallID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callSeq++
	return fmt.Sprintf("call-%d", s.callSeq)
}

// emit delivers a progress event to the host. Tool calls run in parallel inside
// one turn, so the delegate can be invoked from several goroutines.
func (s *AgentSession) emit(turn int64, payload map[string]any) {
	s.mu.Lock()
	delegate := s.delegate
	s.mu.Unlock()
	if delegate == nil {
		return
	}

	payload["turn"] = turn
	encoded, err := json.Marshal(payload)
	if err != nil {
		return
	}
	delegate.OnEvent(string(encoded))
}
