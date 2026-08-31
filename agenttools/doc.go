// Package agenttools exposes the Encounter engine to LLM agents as a catalog of
// callable tools.
//
// The catalog is the single source of truth shared by every agent surface in this
// repository: the PicoClaw agent embedded into the iOS app through gomobile
// (mobile/encxmobile) and the MCP server served by encli. Each tool implements
// PicoClaw's toolshared.Tool interface, so a catalog can be registered directly
// into a *tools.ToolRegistry.
//
// Unlike the legacy agent path in cmd/encli, tools here are free of CLI coupling:
// they never write to stdout, never panic to report failure, and never mutate
// package-level state. Results are returned as JSON so the same tool works for an
// in-app chat, an MCP client, and a test.
//
// Engine mutations (submitting codes, taking penalty hints, joining a game) are
// gated by a Policy. Under PolicyApprove — the default — every mutating call has
// to be confirmed through a Confirmer before it reaches the engine.
package agenttools
