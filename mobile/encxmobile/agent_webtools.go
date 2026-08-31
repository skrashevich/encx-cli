package encxmobile

import (
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/tools"
)

const (
	// webSearchMaxResults keeps a search result set readable on a phone and cheap
	// in the model's context.
	webSearchMaxResults = 8
	// webFetchMaxChars bounds a fetched page. Encounter puzzles usually hinge on
	// a paragraph, not a whole site.
	webFetchMaxChars = 20000
	// webFetchLimitBytes stops a large download before it is converted to text.
	webFetchLimitBytes = 4 << 20
)

// registerWebTools adds PicoClaw's DuckDuckGo search and page reader.
//
// DuckDuckGo needs no API key, so it is the only search backend that works out
// of the box on a phone. Other PicoClaw tools are deliberately left out: the
// shell and process tools cannot run under iOS sandboxing, the hardware tools
// address GPIO buses that do not exist here, and cron/spawn/subagent need the
// agent runtime this embedding does not start.
//
// Registration failures are not fatal — the engine toolset is the point of the
// assistant, and losing web search should not stop a player mid-game.
func registerWebTools(registry *tools.ToolRegistry, observe func(tools.Tool) tools.Tool) {
	searchOptions := tools.WebSearchToolOptions{
		Provider:             "duckduckgo",
		DuckDuckGoEnabled:    true,
		DuckDuckGoMaxResults: webSearchMaxResults,
	}
	if search, err := tools.NewWebSearchTool(searchOptions); err != nil {
		logger.WarnCF("encxmobile", "web search tool unavailable", map[string]any{"error": err.Error()})
	} else {
		registry.Register(observe(search))
	}

	if fetch, err := tools.NewWebFetchTool(webFetchMaxChars, "markdown", webFetchLimitBytes); err != nil {
		logger.WarnCF("encxmobile", "web fetch tool unavailable", map[string]any{"error": err.Error()})
	} else {
		registry.Register(observe(fetch))
	}
}
