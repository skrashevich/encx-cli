package encx

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// This wire shape follows GamePlayersPageResponse in the engine API. Keep
// player_id distinct from user_id/team_id; only player_id addresses mutations.
type apiGamePlayersPage struct {
	GameID     int  `json:"game_id"`
	CanView    bool `json:"can_view"`
	CanManage  bool `json:"has_game_management_permission"`
	Status     int  `json:"status_id"`
	Page       int  `json:"page"`
	TotalPages int  `json:"total_pages"`
	TotalRows  int  `json:"total_rows"`
	Players    []struct {
		PlayerID  int    `json:"player_id"`
		Name      string `json:"player_name"`
		UserID    int    `json:"user_id"`
		TeamID    int    `json:"team_id"`
		Status    int    `json:"status_id"`
		CanDelete bool   `json:"can_delete"`
	} `json:"players"`
}

func (e *newEngine) AdminGetPlayers(ctx context.Context, gameID int) ([]AdminGamePlayer, error) {
	players := []AdminGamePlayer{}
	seen := map[int]bool{}
	total := -1
	for page := 1; page <= 1000; page++ {
		var response apiGamePlayersPage
		q := url.Values{"status": {"-1"}, "sort": {"1"}, "page": {strconv.Itoa(page)}}
		if err := e.c.api().GetJSON(ctx, fmt.Sprintf("/games/%d/players", gameID), q, &response); err != nil {
			return nil, err
		}
		if response.GameID != gameID || !response.CanView || !response.CanManage || response.Status != -1 {
			return nil, fmt.Errorf("encx: full participant management view unavailable")
		}
		if response.Page != page || response.TotalRows < 0 || (total >= 0 && total != response.TotalRows) {
			return nil, fmt.Errorf("encx: inconsistent participant pagination")
		}
		total = response.TotalRows
		for _, p := range response.Players {
			if p.PlayerID <= 0 || seen[p.PlayerID] {
				return nil, fmt.Errorf("encx: missing or repeated participant ID in page %d", page)
			}
			seen[p.PlayerID] = true
			players = append(players, AdminGamePlayer{PlayerID: p.PlayerID, Name: p.Name, UserID: p.UserID, TeamID: p.TeamID, Status: p.Status, CanDelete: p.CanDelete})
		}
		if page >= response.TotalPages {
			if len(players) != total {
				return nil, fmt.Errorf("encx: incomplete participant list: read %d of %d", len(players), total)
			}
			return players, nil
		}
	}
	return nil, fmt.Errorf("encx: participant pagination exceeds 1000 pages")
}

func (e *newEngine) AdminDeletePlayer(ctx context.Context, gameID, playerID int) error {
	players, err := e.AdminGetPlayers(ctx, gameID)
	if err != nil {
		return err
	}
	if _, err = playerToDelete(players, playerID); err != nil {
		return err
	}
	req := struct {
		Confirm bool `json:"confirm"`
	}{Confirm: true}
	if err = e.c.api().PostJSON(ctx, fmt.Sprintf("/games/%d/players/%d/delete", gameID, playerID), req, nil); err != nil {
		return err
	}
	players, err = e.AdminGetPlayers(ctx, gameID)
	if err != nil {
		return fmt.Errorf("encx: deletion sent, verification failed: %w", err)
	}
	return verifyPlayerDeleted(players, playerID)
}
