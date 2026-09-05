// Package agentmcp serves the Encounter engine toolset over the Model Context
// Protocol so a standalone PicoClaw instance (or any MCP client) can play
// through the same tools the in-app agent uses.
package agentmcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

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
		content := []mcp.Content{&mcp.TextContent{Text: result.ContentForLLM()}}
		return &mcp.CallToolResult{
			IsError: result.IsError,
			Content: append(content, imageContent(result.Media)...),
		}, nil
	}
}

// imageContent carries the pictures a tool attached into the MCP reply.
//
// The picture tools answer with an inline data URL, which is the form
// PicoClaw's providers turn into an image part. An MCP client expects an image
// content block instead, and without this conversion it would receive a tool
// result that talks about a picture it was never handed — which is exactly the
// failure these tools exist to remove.
func imageContent(media []string) []mcp.Content {
	var content []mcp.Content
	for _, inline := range media {
		mimeType, data, ok := decodeInlineImage(inline)
		if !ok {
			continue
		}
		content = append(content, &mcp.ImageContent{MIMEType: mimeType, Data: data})
	}
	return content
}

// decodeInlineImage splits a "data:image/png;base64,…" entry into its type and
// its bytes. Anything else — an audio clip, a media:// reference, a truncated
// string — is skipped rather than passed on as a content block the client
// cannot render.
func decodeInlineImage(inline string) (string, []byte, bool) {
	body, isDataURL := strings.CutPrefix(inline, "data:")
	if !isDataURL {
		return "", nil, false
	}
	header, encoded, found := strings.Cut(body, ",")
	if !found {
		return "", nil, false
	}
	mimeType, encoding, found := strings.Cut(header, ";")
	if !found || encoding != "base64" || !strings.HasPrefix(mimeType, "image/") {
		return "", nil, false
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 {
		return "", nil, false
	}
	return mimeType, data, true
}

func errorResult(message string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: message}},
	}
}
