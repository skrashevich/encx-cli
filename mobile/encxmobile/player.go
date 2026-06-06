package encxmobile

import (
	"github.com/skrashevich/encx-cli/encx"
)

// Login authenticates on the Encounter domain. Returns LoginResponse JSON.
func (c *EncClient) Login(login, password string) (string, error) {
	resp, err := c.client.Login(c.bg(), login, password)
	if err != nil {
		return "", err
	}
	return marshalJSON(resp)
}

// LoginWithCaptcha authenticates with CAPTCHA digits when Login returns Error==1.
func (c *EncClient) LoginWithCaptcha(login, password, magicNumbers string) (string, error) {
	resp, err := c.client.Login(c.bg(), login, password, encx.LoginOptions{MagicNumbers: magicNumbers})
	if err != nil {
		return "", err
	}
	return marshalJSON(resp)
}

// GetGameModel returns the current game state as GameModel JSON.
func (c *EncClient) GetGameModel(gameID int64) (string, error) {
	model, err := c.client.GetGameModel(c.bg(), int(gameID))
	if err != nil {
		return "", err
	}
	return marshalJSON(model)
}

// GetGameModelLevel returns the state for a specific level number in storm sequence games.
func (c *EncClient) GetGameModelLevel(gameID, levelNumber int64) (string, error) {
	model, err := c.client.GetGameModelLevel(c.bg(), int(gameID), int(levelNumber))
	if err != nil {
		return "", err
	}
	return marshalJSON(model)
}

// PingGame checks engine reachability with the code-send timeout.
func (c *EncClient) PingGame(gameID int64) (string, error) {
	ctx, cancel := c.codeSendCtx()
	defer cancel()
	model, err := c.client.GetGameModel(ctx, int(gameID))
	if err != nil {
		return "", err
	}
	return marshalJSON(model)
}

// SendCode submits an answer via LevelAction.Answer. Returns updated GameModel JSON.
func (c *EncClient) SendCode(gameID, levelID, levelNumber int64, code string) (string, error) {
	ctx, cancel := c.codeSendCtx()
	defer cancel()
	model, err := c.client.SendCode(ctx, int(gameID), int(levelID), int(levelNumber), code)
	if err != nil {
		return "", err
	}
	return marshalJSON(model)
}

// SendBonusCode submits a bonus answer via BonusAction.Answer. Returns updated GameModel JSON.
func (c *EncClient) SendBonusCode(gameID, levelID, levelNumber int64, code string) (string, error) {
	ctx, cancel := c.codeSendCtx()
	defer cancel()
	model, err := c.client.SendBonusCode(ctx, int(gameID), int(levelID), int(levelNumber), code)
	if err != nil {
		return "", err
	}
	return marshalJSON(model)
}

// GetPenaltyHint requests a penalty hint. Returns updated GameModel JSON.
func (c *EncClient) GetPenaltyHint(gameID, penaltyID int64) (string, error) {
	model, err := c.client.GetPenaltyHint(c.bg(), int(gameID), int(penaltyID))
	if err != nil {
		return "", err
	}
	return marshalJSON(model)
}

// GetGameList returns ComingGames and ActiveGames as JSON. page is 1-based (0 = first page).
func (c *EncClient) GetGameList(page int64) (string, error) {
	var list *encx.GameListResponse
	var err error
	if page > 0 {
		list, err = c.client.GetGameList(c.bg(), int(page))
	} else {
		list, err = c.client.GetGameList(c.bg())
	}
	if err != nil {
		return "", err
	}
	return marshalJSON(list)
}

// GetDomainGames returns games parsed from the domain main page as JSON array.
func (c *EncClient) GetDomainGames() (string, error) {
	games, err := c.client.GetDomainGames(c.bg())
	if err != nil {
		return "", err
	}
	return marshalJSON(games)
}

// GetGameStatistics returns full game statistics as JSON.
func (c *EncClient) GetGameStatistics(gameID int64) (string, error) {
	stats, err := c.client.GetGameStatistics(c.bg(), int(gameID))
	if err != nil {
		return "", err
	}
	return marshalJSON(stats)
}

// GetTimeoutToGame returns seconds until game start, or -1 if no counter is present.
func (c *EncClient) GetTimeoutToGame(gameID int64) (int64, error) {
	val, err := c.client.GetTimeoutToGame(c.bg(), int(gameID))
	if err != nil {
		return 0, err
	}
	if val == nil {
		return -1, nil
	}
	return int64(*val), nil
}

// EnterGame registers the player in a game. Returns raw server response.
func (c *EncClient) EnterGame(gameID int64) (string, error) {
	return c.client.EnterGame(c.bg(), int(gameID))
}

// GetGameDetails returns the game details page HTML.
func (c *EncClient) GetGameDetails(gameID int64) (string, error) {
	return c.client.GetGameDetails(c.bg(), int(gameID))
}

// GetProfile returns the current user profile as JSON.
func (c *EncClient) GetProfile() (string, error) {
	profile, err := c.client.GetProfile(c.bg())
	if err != nil {
		return "", err
	}
	return marshalJSON(profile)
}

// GetTeamDetails returns team details page HTML.
func (c *EncClient) GetTeamDetails(teamID int64) (string, error) {
	return c.client.GetTeamDetails(c.bg(), int(teamID))
}

// GetMyTeamDetails returns the current user's team page HTML.
func (c *EncClient) GetMyTeamDetails() (string, error) {
	return c.client.GetMyTeamDetails(c.bg())
}

// GetTeamManagementInfo returns parsed team management info as JSON.
func (c *EncClient) GetTeamManagementInfo(teamID int64) (string, error) {
	info, err := c.client.GetTeamManagementInfo(c.bg(), int(teamID))
	if err != nil {
		return "", err
	}
	return marshalJSON(info)
}

// GetTeamInvitations returns team invitations addressed to the current user as JSON.
func (c *EncClient) GetTeamInvitations() (string, error) {
	invitations, err := c.client.GetTeamInvitations(c.bg())
	if err != nil {
		return "", err
	}
	return marshalJSON(invitations)
}

// AcceptTeamInvitation accepts a team invitation.
func (c *EncClient) AcceptTeamInvitation(teamID int64) error {
	return c.client.AcceptTeamInvitation(c.bg(), int(teamID))
}

// RejectTeamInvitation rejects a team invitation.
func (c *EncClient) RejectTeamInvitation(teamID int64) error {
	return c.client.RejectTeamInvitation(c.bg(), int(teamID))
}

// RequestTeamMembership sends a request to join a team by name.
func (c *EncClient) RequestTeamMembership(teamName string) error {
	return c.client.RequestTeamMembership(c.bg(), teamName)
}

// InviteTeamMember invites a user login into a team.
func (c *EncClient) InviteTeamMember(teamID int64, login string) error {
	return c.client.InviteTeamMember(c.bg(), int(teamID), login)
}

// RemoveTeamInvitation removes a pending invitation sent by a team.
func (c *EncClient) RemoveTeamInvitation(teamID, userID int64) error {
	return c.client.RemoveTeamInvitation(c.bg(), int(teamID), int(userID))
}

// LeaveTeam leaves a team when TeamDetails.aspx exposes a leave action.
func (c *EncClient) LeaveTeam(teamID int64) error {
	return c.client.LeaveTeam(c.bg(), int(teamID))
}

// RenameTeam renames a team.
func (c *EncClient) RenameTeam(teamID int64, name string) error {
	return c.client.RenameTeam(c.bg(), int(teamID), name)
}

// SetTeamSite updates the team website URL.
func (c *EncClient) SetTeamSite(teamID int64, site string) error {
	return c.client.SetTeamSite(c.bg(), int(teamID), site)
}

// SetTeamForum updates the team external forum URL.
func (c *EncClient) SetTeamForum(teamID int64, forum string) error {
	return c.client.SetTeamForum(c.bg(), int(teamID), forum)
}

// ParseTeamLinks extracts team IDs and names from HTML. Returns JSON array of TeamInfo.
func ParseTeamLinks(html string) (string, error) {
	return marshalJSON(encx.ParseTeamLinks(html))
}

// ParseTeamManagementInfo extracts team management info from HTML. Returns JSON.
func ParseTeamManagementInfo(html string, teamID int64) (string, error) {
	return marshalJSON(encx.ParseTeamManagementInfo(html, int(teamID)))
}

// ParseTeamInvitations extracts incoming team invitations from HTML. Returns JSON array.
func ParseTeamInvitations(html string) (string, error) {
	return marshalJSON(encx.ParseTeamInvitations(html))
}
