package encxmobile

import (
	"encoding/json/v2"
	"fmt"

	"github.com/skrashevich/encx-cli/encx"
)

// ResolveEngine returns engine and definitive as JSON. Only confirmed auto
// selections may be persisted; a temporary legacy fallback must remain retryable.
func (c *EncClient) ResolveEngine() (string, error) {
	return marshalJSON(c.client.ResolveEngine(c.bg()))
}

// LevelSectorProgress returns total, done, required and remaining as JSON for a
// Level JSON object. A null level or no answer requirement has remaining=null.
func LevelSectorProgress(levelJSON string) (string, error) {
	var level *encx.Level
	if err := json.Unmarshal([]byte(levelJSON), &level); err != nil {
		return "", fmt.Errorf("encxmobile: decode level for sector progress: %w", err)
	}
	return marshalJSON(level.SectorProgress())
}
