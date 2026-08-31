package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/skrashevich/encx-cli/agentmcp"
	"github.com/skrashevich/encx-cli/agenttools"
	"github.com/skrashevich/encx-cli/encx"
)

// cmdMCP serves the engine toolset over MCP on stdin/stdout so an external agent
// such as PicoClaw can play through the same tools the iOS app uses.
//
// Everything the command prints goes to stderr: stdout carries the protocol.
func cmdMCP(ctx context.Context, cfg *config, client *encx.Client) {
	policy, err := parseMCPPolicy(cfg.mcpSecurity)
	if err != nil {
		fatal("%v", err)
	}

	opts := agenttools.Options{Policy: policy}
	if policy == agenttools.PolicyApprove {
		opts.Confirmer = agentmcp.NewElicitConfirmer()
	}
	catalog, err := agenttools.NewCatalog(client, opts)
	if err != nil {
		fatal("Failed to build the engine toolset: %v", err)
	}

	server, err := agentmcp.NewServer(catalog, version)
	if err != nil {
		fatal("Failed to build the MCP server: %v", err)
	}

	fmt.Fprintf(os.Stderr, "encli mcp: serving %d engine tools over stdio (domain %s, policy %s)\n",
		len(catalog.Tools()), cfg.domain, policy)
	if err := agentmcp.ServeStdio(ctx, server); err != nil && !errors.Is(err, context.Canceled) {
		fatal("MCP server stopped: %v", err)
	}
}

// parseMCPPolicy defaults to read-only: an MCP server is usually started
// unattended, and the safe default there is a toolset that cannot change the
// player's game.
func parseMCPPolicy(raw string) (agenttools.Policy, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return agenttools.PolicyReadonly, nil
	}
	policy, err := agenttools.ParsePolicy(trimmed)
	if err != nil {
		return "", fmt.Errorf("invalid -security value: %w", err)
	}
	return policy, nil
}
