package encxmobile

import (
	"encoding/json"
	"fmt"

	"github.com/skrashevich/encx-cli/encx"
)

// AdminCreateGame creates a game and returns its id. paramsJSON must decode
// into encx.AdminCreateGameParams (title, description, game_type,
// start_datetime, finish_datetime, ...).
func (c *EncClient) AdminCreateGame(paramsJSON string) (int64, error) {
	var params encx.AdminCreateGameParams
	if err := json.Unmarshal([]byte(paramsJSON), &params); err != nil {
		return 0, fmt.Errorf("encxmobile: admin create game: %w", err)
	}
	id, err := c.client.AdminCreateGame(c.bg(), params)
	if err != nil {
		return 0, err
	}
	return int64(id), nil
}
