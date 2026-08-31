// Package agentmcp serves the Encounter engine toolset over the Model Context
// Protocol so a standalone PicoClaw instance (or any MCP client) can play
// through the same tools the in-app agent uses.
package agentmcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/skrashevich/encx-cli/agenttools"
)

// ServerName is the implementation name advertised to MCP clients.
const ServerName = "encx-engine"

// NewServer publishes a catalog as an MCP server.
//
// Under agenttools.PolicyApprove every mutating call is put to the client
// through MCP elicitation, which is the protocol's equivalent of the in-app
// confirmation sheet. Clients that do not support elicitation get a refusal
// rather than a silent mutation.
func NewServer(catalog *agenttools.Catalog, version string) (*mcp.Server, error) {
	if catalog == nil {
		return nil, errors.New("agentmcp: catalog is required")
	}
	if version == "" {
		version = "dev"
	}

	server := mcp.NewServer(&mcp.Implementation{Name: ServerName, Version: version}, nil)
	for _, tool := range catalog.Tools() {
		readOnly := !tool.Mutating()
		definition := &mcp.Tool{
			Name:        tool.Name(),
			Description: tool.Description(),
			InputSchema: tool.Parameters(),
			Annotations: &mcp.ToolAnnotations{ReadOnlyHint: readOnly},
		}
		server.AddTool(definition, toolHandler(tool))
	}
	return server, nil
}

// ServeStdio runs the server over stdin/stdout until the client disconnects or
// ctx is cancelled.
func ServeStdio(ctx context.Context, server *mcp.Server) error {
	return server.Run(ctx, &mcp.StdioTransport{})
}

func toolHandler(tool *agenttools.Tool) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := map[string]any{}
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return errorResult(fmt.Sprintf("%s: arguments are not a JSON object: %v", tool.Name(), err)), nil
			}
		}

		// The catalog asks its Confirmer during Execute; carry the session so the
		// confirmer can elicit from this specific client.
		result := tool.Execute(withSession(ctx, req.Session), args)
		if result == nil {
			return errorResult(tool.Name() + " returned no result"), nil
		}
		return &mcp.CallToolResult{
			IsError: result.IsError,
			Content: []mcp.Content{&mcp.TextContent{Text: result.ContentForLLM()}},
		}, nil
	}
}

func errorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
	}
}
