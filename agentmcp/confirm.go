package agentmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skrashevich/encx-cli/agenttools"
)

type sessionKey struct{}

func withSession(ctx context.Context, session *mcp.ServerSession) context.Context {
	if session == nil {
		return ctx
	}
	return context.WithValue(ctx, sessionKey{}, session)
}

func sessionFrom(ctx context.Context) *mcp.ServerSession {
	session, _ := ctx.Value(sessionKey{}).(*mcp.ServerSession)
	return session
}

// elicitConfirmer authorizes mutating calls by asking the connected MCP client.
type elicitConfirmer struct{}

// NewElicitConfirmer returns a Confirmer that puts mutating calls to the MCP
// client through elicitation. Pass it as agenttools.Options.Confirmer when
// building a catalog for PolicyApprove.
func NewElicitConfirmer() agenttools.Confirmer { return elicitConfirmer{} }

// ConfirmToolCall implements agenttools.Confirmer.
func (elicitConfirmer) ConfirmToolCall(ctx context.Context, req agenttools.ConfirmRequest) (bool, error) {
	session := sessionFrom(ctx)
	if session == nil {
		return false, errors.New("no MCP session is attached to this call")
	}

	result, err := session.Elicit(ctx, &mcp.ElicitParams{
		Message: confirmationMessage(req),
		RequestedSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"confirm": map[string]any{
					"type":        "boolean",
					"description": "Set to true to let the call reach the Encounter engine.",
				},
			},
			"required": []string{"confirm"},
		},
	})
	if err != nil {
		// Clients without elicitation support answer with a protocol error. Treat
		// that as "not authorized" rather than letting the mutation through.
		return false, fmt.Errorf("the client could not confirm the call: %w", err)
	}
	if result.Action != "accept" {
		return false, nil
	}
	confirmed, _ := result.Content["confirm"].(bool)
	return confirmed, nil
}

func confirmationMessage(req agenttools.ConfirmRequest) string {
	var b strings.Builder
	b.WriteString("The agent wants to change Encounter game state by calling ")
	b.WriteString(req.Tool)
	if len(req.Args) > 0 {
		if encoded, err := json.Marshal(req.Args); err == nil {
			b.WriteString(" with ")
			b.Write(encoded)
		}
	}
	b.WriteString(". Confirm?")
	return b.String()
}
