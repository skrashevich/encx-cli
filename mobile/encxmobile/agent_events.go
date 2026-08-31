package encxmobile

import (
	"context"
	"encoding/json"
	"fmt"

	toolshared "github.com/sipeed/picoclaw/pkg/tools/shared"
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

	result := t.inner.Execute(ctx, args)

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
