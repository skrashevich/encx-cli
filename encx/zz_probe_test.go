package encx

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestProbeCanManipulateOnStartedGame(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	fresh := scratchLevels(t, c, 1)
	for _, id := range []int{fresh, 26365, 26190, 25590, 25487} {
		var resp struct {
			GameID              int  `json:"game_id"`
			Started             bool `json:"started"`
			CanManipulateLevels bool `json:"can_manipulate_levels"`
			CanChangeSequence   bool `json:"can_change_levels_sequence"`
			StatusID            int  `json:"status_id"`
		}
		err := c.api().GetJSON(ctx, fmt.Sprintf("/admin/games/%d/levels", id), nil, &resp)
		label := "started game"
		if id == fresh {
			label = "fresh scratch"
		}
		t.Logf("%-14s game %-6d err=%v started=%v can_manipulate=%v can_change_seq=%v status=%d",
			label, id, err, resp.Started, resp.CanManipulateLevels, resp.CanChangeSequence, resp.StatusID)
	}
}

func TestProbeTeamMembersRoute(t *testing.T) {
	c := liveClient(t)
	ctx, cancel := liveContext(t)
	defer cancel()

	profile, err := c.GetProfile(ctx)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	var raw json.RawMessage
	err = c.api().GetJSON(ctx, fmt.Sprintf("/teams/%d/members", profile.TeamID), nil, &raw)
	if len(raw) > 700 {
		raw = raw[:700]
	}
	t.Logf("GET /teams/%d/members err=%v out=%s", profile.TeamID, err, raw)

	// Does /teams/{id} itself carry the members?
	var team json.RawMessage
	err = c.api().GetJSON(ctx, fmt.Sprintf("/teams/%d", profile.TeamID), nil, &team)
	if len(team) > 700 {
		team = team[:700]
	}
	t.Logf("GET /teams/%d err=%v out=%s", profile.TeamID, err, team)
}
