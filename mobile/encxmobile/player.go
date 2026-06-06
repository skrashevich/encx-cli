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

// AcceptTeamInvitation accepts a team invitation.
func (c *EncClient) AcceptTeamInvitation(teamID int64) error {
	return c.client.AcceptTeamInvitation(c.bg(), int(teamID))
}

// ParseTeamLinks extracts team IDs and names from HTML. Returns JSON array of TeamInfo.
func ParseTeamLinks(html string) (string, error) {
	return marshalJSON(encx.ParseTeamLinks(html))
}
