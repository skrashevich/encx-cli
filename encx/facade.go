package encx

import (
	"context"
	"net/url"
)

// This file holds the exported Client methods whose implementation depends on
// which Encounter engine serves the domain. Each one is a thin dispatch to the
// backend selected for the request, so callers never learn which engine answered.

// Login authenticates the user on the Encounter domain.
// On success (Error == 0) the session is stored in the client and used for
// subsequent requests: cookies on the legacy engine, a JWT on the new one.
//
// Optional LoginOptions can be passed to specify network or CAPTCHA digits.
func (c *Client) Login(ctx context.Context, login, password string, opts ...LoginOptions) (*LoginResponse, error) {
	return c.engine(ctx).Login(ctx, login, password, opts...)
}

// LoginComplete establishes a session that also works for game administration.
// On the legacy engine it signs in through Login.aspx and falls back to the
// JSON endpoint; on the new engine one JWT covers both.
func (c *Client) LoginComplete(ctx context.Context, login, password string, opts ...LoginOptions) error {
	return c.engine(ctx).LoginComplete(ctx, login, password, opts...)
}

// VerifyAdminSession reports whether the current session can reach the game
// administration API.
func (c *Client) VerifyAdminSession(ctx context.Context) error {
	return c.engine(ctx).VerifyAdminSession(ctx)
}

// GetGameModel retrieves the current game state.
// Passing form values is retained for backward compatibility and performs a
// POST action request; prefer SendCode, SendBonusCode, or GetPenaltyHint.
func (c *Client) GetGameModel(ctx context.Context, gameId int, formValues ...url.Values) (*GameModel, error) {
	return c.engine(ctx).GetGameModel(ctx, gameId, formValues...)
}

// GetGameModelLevel retrieves the state for a specific level number. This is
// used by storm sequence games where the engine accepts a level parameter.
func (c *Client) GetGameModelLevel(ctx context.Context, gameId, levelNumber int) (*GameModel, error) {
	return c.engine(ctx).GetGameModelLevel(ctx, gameId, levelNumber)
}

// SendCode submits a level answer (level, sectors, and bonuses when the level
// has no active answer block rule).
func (c *Client) SendCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*GameModel, error) {
	return c.engine(ctx).SendCode(ctx, gameId, levelId, levelNumber, code)
}

// SendBonusCode submits a bonus answer. Both engines keep this action separate
// from level answers so it still works while level answers are blocked.
func (c *Client) SendBonusCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*GameModel, error) {
	return c.engine(ctx).SendBonusCode(ctx, gameId, levelId, levelNumber, code)
}

// GetPenaltyHint requests a penalty hint by its ID.
func (c *Client) GetPenaltyHint(ctx context.Context, gameId, penaltyId int) (*GameModel, error) {
	return c.engine(ctx).GetPenaltyHint(ctx, gameId, penaltyId)
}

// GetGameList fetches the domain's coming and active games.
// An optional page number can be passed for pagination (1-based).
func (c *Client) GetGameList(ctx context.Context, page ...int) (*GameListResponse, error) {
	return c.engine(ctx).GetGameList(ctx, page...)
}

// GetDomainGames fetches the domain's list of available games.
func (c *Client) GetDomainGames(ctx context.Context) ([]DomainGame, error) {
	return c.engine(ctx).GetDomainGames(ctx)
}

// GetGameDetails fetches the game details document.
//
// The legacy engine returns the HTML page; the new engine has no HTML export
// and returns the structured details document as JSON instead.
func (c *Client) GetGameDetails(ctx context.Context, gameId int) (string, error) {
	return c.engine(ctx).GetGameDetails(ctx, gameId)
}

// GetTimeoutToGame reports the seconds remaining until the game starts, or nil
// when the engine does not publish a countdown.
func (c *Client) GetTimeoutToGame(ctx context.Context, gameId int) (*int, error) {
	return c.engine(ctx).GetTimeoutToGame(ctx, gameId)
}

// EnterGame registers the player in a game (application / fee confirmation).
func (c *Client) EnterGame(ctx context.Context, gameId int) (string, error) {
	return c.engine(ctx).EnterGame(ctx, gameId)
}

// GetGameStatistics fetches full game statistics: per-level results,
// player/team rankings and level metadata.
func (c *Client) GetGameStatistics(ctx context.Context, gameId int) (*GameStatisticsResponse, error) {
	return c.engine(ctx).GetGameStatistics(ctx, gameId)
}

// GetProfile fetches the signed-in user's profile.
func (c *Client) GetProfile(ctx context.Context) (*Profile, error) {
	return c.engine(ctx).GetProfile(ctx)
}

// GetTeamDetails fetches the team page.
//
// The legacy engine returns HTML; the new engine returns the team document as
// JSON, since it publishes no HTML page.
func (c *Client) GetTeamDetails(ctx context.Context, teamId int) (string, error) {
	return c.engine(ctx).GetTeamDetails(ctx, teamId)
}

// GetMyTeamDetails fetches the signed-in user's own team page.
func (c *Client) GetMyTeamDetails(ctx context.Context) (string, error) {
	return c.engine(ctx).GetMyTeamDetails(ctx)
}

// GetTeamManagementInfo reports what the current session may do with a team.
func (c *Client) GetTeamManagementInfo(ctx context.Context, teamID int) (*TeamManagementInfo, error) {
	return c.engine(ctx).GetTeamManagementInfo(ctx, teamID)
}

// GetTeamInvitations fetches team invitations addressed to the current user.
func (c *Client) GetTeamInvitations(ctx context.Context) ([]TeamInvitation, error) {
	return c.engine(ctx).GetTeamInvitations(ctx)
}

// AcceptTeamInvitation accepts a team invitation by team ID.
func (c *Client) AcceptTeamInvitation(ctx context.Context, teamId int) error {
	return c.engine(ctx).AcceptTeamInvitation(ctx, teamId)
}

// RejectTeamInvitation rejects a team invitation by team ID.
func (c *Client) RejectTeamInvitation(ctx context.Context, teamID int) error {
	return c.engine(ctx).RejectTeamInvitation(ctx, teamID)
}

// RequestTeamMembership sends a request to join the named team.
func (c *Client) RequestTeamMembership(ctx context.Context, teamName string) error {
	return c.engine(ctx).RequestTeamMembership(ctx, teamName)
}

// InviteTeamMember invites a user login into the specified team.
func (c *Client) InviteTeamMember(ctx context.Context, teamID int, login string) error {
	return c.engine(ctx).InviteTeamMember(ctx, teamID, login)
}

// RemoveTeamInvitation withdraws an invitation the captain sent.
func (c *Client) RemoveTeamInvitation(ctx context.Context, teamID, userID int) error {
	return c.engine(ctx).RemoveTeamInvitation(ctx, teamID, userID)
}

// LeaveTeam removes the current user from the team.
func (c *Client) LeaveTeam(ctx context.Context, teamID int) error {
	return c.engine(ctx).LeaveTeam(ctx, teamID)
}

// RenameTeam changes the team name.
func (c *Client) RenameTeam(ctx context.Context, teamID int, name string) error {
	return c.engine(ctx).RenameTeam(ctx, teamID, name)
}

// SetTeamSite sets the team web site.
func (c *Client) SetTeamSite(ctx context.Context, teamID int, site string) error {
	return c.engine(ctx).SetTeamSite(ctx, teamID, site)
}

// SetTeamForum sets the team forum link.
func (c *Client) SetTeamForum(ctx context.Context, teamID int, forum string) error {
	return c.engine(ctx).SetTeamForum(ctx, teamID, forum)
}

func (c *Client) AdminGetGames(ctx context.Context) ([]AdminGame, error) {
	return c.engine(ctx).AdminGetGames(ctx)
}
func (c *Client) AdminGetLevels(ctx context.Context, gameId int) ([]AdminLevel, error) {
	return c.engine(ctx).AdminGetLevels(ctx, gameId)
}
func (c *Client) AdminCreateLevels(ctx context.Context, gameId, count int) error {
	return c.engine(ctx).AdminCreateLevels(ctx, gameId, count)
}
func (c *Client) AdminDeleteLevel(ctx context.Context, gameId, levelNum int) error {
	return c.engine(ctx).AdminDeleteLevel(ctx, gameId, levelNum)
}
func (c *Client) AdminRenameLevels(ctx context.Context, gameId int, names map[int]string) error {
	return c.engine(ctx).AdminRenameLevels(ctx, gameId, names)
}
func (c *Client) AdminSwapLevels(ctx context.Context, gameId, level1, level2 int) error {
	return c.engine(ctx).AdminSwapLevels(ctx, gameId, level1, level2)
}
func (c *Client) AdminInsertLevel(ctx context.Context, gameId, src, dst int) error {
	return c.engine(ctx).AdminInsertLevel(ctx, gameId, src, dst)
}
func (c *Client) AdminCloneLevels(ctx context.Context, gameId, count, likeLevel int) error {
	return c.engine(ctx).AdminCloneLevels(ctx, gameId, count, likeLevel)
}
func (c *Client) AdminGetTaskIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	return c.engine(ctx).AdminGetTaskIds(ctx, gameId, levelNum)
}
func (c *Client) AdminGetTask(ctx context.Context, gameId, levelNum, taskId int) (*AdminTask, error) {
	return c.engine(ctx).AdminGetTask(ctx, gameId, levelNum, taskId)
}
func (c *Client) AdminCreateTask(ctx context.Context, gameId, levelNum int, t AdminTask) error {
	return c.engine(ctx).AdminCreateTask(ctx, gameId, levelNum, t)
}
func (c *Client) AdminUpdateTask(ctx context.Context, gameId, levelNum, taskId int, t AdminTask) error {
	return c.engine(ctx).AdminUpdateTask(ctx, gameId, levelNum, taskId, t)
}
func (c *Client) AdminDeleteTask(ctx context.Context, gameId, levelNum, taskId int) error {
	return c.engine(ctx).AdminDeleteTask(ctx, gameId, levelNum, taskId)
}
func (c *Client) AdminGetHintIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	return c.engine(ctx).AdminGetHintIds(ctx, gameId, levelNum)
}
func (c *Client) AdminGetHint(ctx context.Context, gameId, levelNum, hintId int) (*AdminHint, error) {
	return c.engine(ctx).AdminGetHint(ctx, gameId, levelNum, hintId)
}
func (c *Client) AdminCreateHint(ctx context.Context, gameId, levelNum int, h AdminHint) error {
	return c.engine(ctx).AdminCreateHint(ctx, gameId, levelNum, h)
}
func (c *Client) AdminUpdateHint(ctx context.Context, gameId, levelNum, hintId int, h AdminHint) error {
	return c.engine(ctx).AdminUpdateHint(ctx, gameId, levelNum, hintId, h)
}
func (c *Client) AdminDeleteHint(ctx context.Context, gameId, levelNum, hintId int) error {
	return c.engine(ctx).AdminDeleteHint(ctx, gameId, levelNum, hintId)
}
func (c *Client) AdminGetBonusIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	return c.engine(ctx).AdminGetBonusIds(ctx, gameId, levelNum)
}
func (c *Client) AdminGetBonus(ctx context.Context, gameId, levelNum, bonusId int) (*AdminBonus, error) {
	return c.engine(ctx).AdminGetBonus(ctx, gameId, levelNum, bonusId)
}
func (c *Client) AdminCreateBonus(ctx context.Context, gameId, levelNum int, b AdminBonus) error {
	return c.engine(ctx).AdminCreateBonus(ctx, gameId, levelNum, b)
}
func (c *Client) AdminUpdateBonus(ctx context.Context, gameId, levelNum, bonusId int, b AdminBonus) error {
	return c.engine(ctx).AdminUpdateBonus(ctx, gameId, levelNum, bonusId, b)
}
func (c *Client) AdminDeleteBonus(ctx context.Context, gameId, levelNum, bonusId int) error {
	return c.engine(ctx).AdminDeleteBonus(ctx, gameId, levelNum, bonusId)
}
func (c *Client) AdminGetMessageIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	return c.engine(ctx).AdminGetMessageIds(ctx, gameId, levelNum)
}
func (c *Client) AdminGetMessage(ctx context.Context, gameId, levelNum, messageId int) (*AdminGameMessage, error) {
	return c.engine(ctx).AdminGetMessage(ctx, gameId, levelNum, messageId)
}
func (c *Client) AdminCreateMessage(ctx context.Context, gameId, levelID int, m AdminGameMessage) error {
	return c.engine(ctx).AdminCreateMessage(ctx, gameId, levelID, m)
}
func (c *Client) AdminUpdateMessage(ctx context.Context, gameId, levelNum, messageId int, m AdminGameMessage) error {
	return c.engine(ctx).AdminUpdateMessage(ctx, gameId, levelNum, messageId, m)
}
func (c *Client) AdminDeleteMessage(ctx context.Context, gameId, levelNum, messageId int) error {
	return c.engine(ctx).AdminDeleteMessage(ctx, gameId, levelNum, messageId)
}
func (c *Client) AdminGetSectorRefs(ctx context.Context, gameId, levelNum int) ([]AdminSector, error) {
	return c.engine(ctx).AdminGetSectorRefs(ctx, gameId, levelNum)
}
func (c *Client) AdminGetSectorAnswers(ctx context.Context, gameId, levelNum int) ([]AdminSector, error) {
	return c.engine(ctx).AdminGetSectorAnswers(ctx, gameId, levelNum)
}
func (c *Client) AdminCreateSector(ctx context.Context, gameId, levelNum int, s AdminSector) error {
	return c.engine(ctx).AdminCreateSector(ctx, gameId, levelNum, s)
}
func (c *Client) AdminUpdateSector(ctx context.Context, gameId, levelNum, sectorId int, s AdminSector) error {
	return c.engine(ctx).AdminUpdateSector(ctx, gameId, levelNum, sectorId, s)
}
func (c *Client) AdminDeleteSector(ctx context.Context, gameId, levelNum, sectorId int) error {
	return c.engine(ctx).AdminDeleteSector(ctx, gameId, levelNum, sectorId)
}
func (c *Client) AdminAddSectorAnswers(ctx context.Context, gameId, levelNum, sectorId int, answers []string) error {
	return c.engine(ctx).AdminAddSectorAnswers(ctx, gameId, levelNum, sectorId, answers)
}
func (c *Client) AdminClearLevelSectors(ctx context.Context, gameID, levelNum int) error {
	return c.engine(ctx).AdminClearLevelSectors(ctx, gameID, levelNum)
}
func (c *Client) AdminGetLevelSettings(ctx context.Context, gameId, levelNum int) (*AdminLevelSettings, error) {
	return c.engine(ctx).AdminGetLevelSettings(ctx, gameId, levelNum)
}
func (c *Client) AdminUpdateAutopass(ctx context.Context, gameId, levelNum int, s AdminLevelSettings) error {
	return c.engine(ctx).AdminUpdateAutopass(ctx, gameId, levelNum, s)
}
func (c *Client) AdminUpdateAnswerBlock(ctx context.Context, gameId, levelNum int, s AdminLevelSettings) error {
	return c.engine(ctx).AdminUpdateAnswerBlock(ctx, gameId, levelNum, s)
}
func (c *Client) AdminUpdateSectorCompletion(ctx context.Context, gameId, levelNum, requiredCount int) error {
	return c.engine(ctx).AdminUpdateSectorCompletion(ctx, gameId, levelNum, requiredCount)
}
func (c *Client) AdminGetComment(ctx context.Context, gameId, levelNum int) (name, comment string, err error) {
	return c.engine(ctx).AdminGetComment(ctx, gameId, levelNum)
}
func (c *Client) AdminUpdateComment(ctx context.Context, gameId, levelNum int, name, comment string) error {
	return c.engine(ctx).AdminUpdateComment(ctx, gameId, levelNum, name, comment)
}

func (c *Client) AdminGetGameInfo(ctx context.Context, gameId int) (*AdminGameInfo, error) {
	return c.engine(ctx).AdminGetGameInfo(ctx, gameId)
}

func (c *Client) AdminUpdateGameInfo(ctx context.Context, gameId int, info AdminGameInfo) error {
	return c.engine(ctx).AdminUpdateGameInfo(ctx, gameId, info)
}

func (c *Client) AdminDeliverGame(ctx context.Context, gameId int) error {
	return c.engine(ctx).AdminDeliverGame(ctx, gameId)
}

func (c *Client) AdminNotDeliverGame(ctx context.Context, gameId int) error {
	return c.engine(ctx).AdminNotDeliverGame(ctx, gameId)
}

func (c *Client) AdminAwardPoints(ctx context.Context, gameId int) error {
	return c.engine(ctx).AdminAwardPoints(ctx, gameId)
}

func (c *Client) AdminEndRatings(ctx context.Context, gameId int) error {
	return c.engine(ctx).AdminEndRatings(ctx, gameId)
}

func (c *Client) AdminCalculateIK(ctx context.Context, gameId int) error {
	return c.engine(ctx).AdminCalculateIK(ctx, gameId)
}

func (c *Client) AdminGetCorrections(ctx context.Context, gameId int) ([]AdminCorrection, error) {
	return c.engine(ctx).AdminGetCorrections(ctx, gameId)
}

func (c *Client) AdminAddCorrection(ctx context.Context, gameId int, corr AdminCorrectionAdd) error {
	return c.engine(ctx).AdminAddCorrection(ctx, gameId, corr)
}

func (c *Client) AdminDeleteCorrection(ctx context.Context, gameId int, correctionId string) error {
	return c.engine(ctx).AdminDeleteCorrection(ctx, gameId, correctionId)
}

func (c *Client) AdminGetTeams(ctx context.Context, gameId, levelNum int) ([]AdminTeam, error) {
	return c.engine(ctx).AdminGetTeams(ctx, gameId, levelNum)
}

func (c *Client) AdminGetActionMonitor(ctx context.Context, gameId int) ([]AdminActionMonitorEntry, error) {
	return c.engine(ctx).AdminGetActionMonitor(ctx, gameId)
}
