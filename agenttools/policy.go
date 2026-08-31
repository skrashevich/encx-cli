package agenttools

import (
	"context"
	"fmt"
)

// Policy decides what an agent is allowed to do with the engine.
type Policy string

const (
	// PolicyReadonly hides mutating tools from the catalog and refuses them if
	// they are called anyway.
	PolicyReadonly Policy = "readonly"
	// PolicyApprove exposes mutating tools but routes every call through a
	// Confirmer first. This is the default.
	PolicyApprove Policy = "approve"
	// PolicyFull lets the agent mutate engine state without asking.
	PolicyFull Policy = "full"
)

// DefaultPolicy is applied when a caller leaves Options.Policy empty.
const DefaultPolicy = PolicyApprove

// ParsePolicy converts a textual policy name, accepting the empty string as the
// default.
func ParsePolicy(s string) (Policy, error) {
	switch Policy(s) {
	case "":
		return DefaultPolicy, nil
	case PolicyReadonly, PolicyApprove, PolicyFull:
		return Policy(s), nil
	default:
		return "", fmt.Errorf("agenttools: unknown policy %q (want readonly, approve or full)", s)
	}
}

func (p Policy) normalized() Policy {
	if p == "" {
		return DefaultPolicy
	}
	return p
}

// ConfirmRequest describes a mutating call awaiting the user's decision.
type ConfirmRequest struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args,omitempty"`
}

// Confirmer approves or declines a mutating tool call. Implementations are
// expected to block until the user answers or ctx is cancelled.
type Confirmer interface {
	ConfirmToolCall(ctx context.Context, req ConfirmRequest) (bool, error)
}

// ConfirmerFunc adapts a function to the Confirmer interface.
type ConfirmerFunc func(ctx context.Context, req ConfirmRequest) (bool, error)

// ConfirmToolCall implements Confirmer.
func (f ConfirmerFunc) ConfirmToolCall(ctx context.Context, req ConfirmRequest) (bool, error) {
	return f(ctx, req)
}

// gate holds the shared authorization state of a catalog.
type gate struct {
	policy    Policy
	confirmer Confirmer
}

// authorize reports whether a mutating call may proceed. The returned string is
// the refusal text for the LLM when it may not.
func (g *gate) authorize(ctx context.Context, name string, args map[string]any) (bool, string) {
	switch g.policy {
	case PolicyFull:
		return true, ""
	case PolicyReadonly:
		return false, fmt.Sprintf(
			"Tool %q is unavailable: the engine access policy is read-only. "+
				"Describe the action for the user instead of performing it.", name)
	case PolicyApprove:
		if g.confirmer == nil {
			return false, fmt.Sprintf(
				"Tool %q needs user approval but no confirmation channel is configured.", name)
		}
		approved, err := g.confirmer.ConfirmToolCall(ctx, ConfirmRequest{Tool: name, Args: args})
		if err != nil {
			return false, fmt.Sprintf("Approval for %q could not be obtained: %v", name, err)
		}
		if !approved {
			return false, fmt.Sprintf(
				"The user declined %q. Do not retry it; ask what to do instead.", name)
		}
		return true, ""
	default:
		return false, fmt.Sprintf("Tool %q is unavailable: unknown access policy %q.", name, g.policy)
	}
}
