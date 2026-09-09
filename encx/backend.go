package encx

import (
	"context"
	"net/url"

	"github.com/skrashevich/encx-cli/encx/scenario"
)

// backend is the contract both Encounter engines must satisfy. Every exported
// Client method whose implementation differs between the ASP.NET engine and the
// Go backend REST API is declared here, so a missing implementation is a
// compile error rather than a silent fallback to the wrong engine.
//
// The interface grows story by story as method groups are ported; the parity
// test in engine_parity_test.go guards the exported surface against drift.
type backend interface {
	// Auth
	Login(ctx context.Context, login, password string, opts ...LoginOptions) (*LoginResponse, error)
	LoginComplete(ctx context.Context, login, password string, opts ...LoginOptions) error
	VerifyAdminSession(ctx context.Context) error

	// Game engine
	GetGameModel(ctx context.Context, gameId int, formValues ...url.Values) (*GameModel, error)
	GetGameModelLevel(ctx context.Context, gameId, levelNumber int) (*GameModel, error)
	SendCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*GameModel, error)
	SendBonusCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*GameModel, error)
	GetPenaltyHint(ctx context.Context, gameId, penaltyId int) (*GameModel, error)

	// Catalog and game pages
	GetGameList(ctx context.Context, page ...int) (*GameListResponse, error)
	GetDomainGames(ctx context.Context) ([]DomainGame, error)
	GetGameDetails(ctx context.Context, gameId int) (string, error)
	GetTimeoutToGame(ctx context.Context, gameId int) (*int, error)
	EnterGame(ctx context.Context, gameId int) (string, error)
	FetchResource(ctx context.Context, rawURL string, opts ...ResourceOptions) (*Resource, error)

	// Statistics and profile
	GetGameStatistics(ctx context.Context, gameId int) (*GameStatisticsResponse, error)
	GetProfile(ctx context.Context) (*Profile, error)

	// Teams
	GetTeamDetails(ctx context.Context, teamId int) (string, error)
	GetMyTeamDetails(ctx context.Context) (string, error)
	GetTeamManagementInfo(ctx context.Context, teamID int) (*TeamManagementInfo, error)
	GetTeamInvitations(ctx context.Context) ([]TeamInvitation, error)
	AcceptTeamInvitation(ctx context.Context, teamId int) error
	RejectTeamInvitation(ctx context.Context, teamID int) error
	RequestTeamMembership(ctx context.Context, teamName string) error
	InviteTeamMember(ctx context.Context, teamID int, login string) error
	RemoveTeamInvitation(ctx context.Context, teamID, userID int) error
	LeaveTeam(ctx context.Context, teamID int) error
	RenameTeam(ctx context.Context, teamID int, name string) error
	SetTeamSite(ctx context.Context, teamID int, site string) error
	SetTeamForum(ctx context.Context, teamID int, forum string) error

	// Admin level editor
	AdminGetGames(ctx context.Context) ([]AdminGame, error)
	AdminGetLevels(ctx context.Context, gameId int) ([]AdminLevel, error)
	AdminCreateLevels(ctx context.Context, gameId, count int) error
	AdminDeleteLevel(ctx context.Context, gameId, levelNum int) error
	AdminRenameLevels(ctx context.Context, gameId int, names map[int]string) error
	AdminSwapLevels(ctx context.Context, gameId, level1, level2 int) error
	AdminInsertLevel(ctx context.Context, gameId, src, dst int) error
	AdminCloneLevels(ctx context.Context, gameId, count, likeLevel int) error
	AdminGetTaskIds(ctx context.Context, gameId, levelNum int) ([]int, error)
	AdminGetTask(ctx context.Context, gameId, levelNum, taskId int) (*AdminTask, error)
	AdminCreateTask(ctx context.Context, gameId, levelNum int, t AdminTask) error
	AdminUpdateTask(ctx context.Context, gameId, levelNum, taskId int, t AdminTask) error
	AdminDeleteTask(ctx context.Context, gameId, levelNum, taskId int) error
	AdminGetHintIds(ctx context.Context, gameId, levelNum int) ([]int, error)
	AdminGetHint(ctx context.Context, gameId, levelNum, hintId int) (*AdminHint, error)
	AdminCreateHint(ctx context.Context, gameId, levelNum int, h AdminHint) error
	AdminUpdateHint(ctx context.Context, gameId, levelNum, hintId int, h AdminHint) error
	AdminDeleteHint(ctx context.Context, gameId, levelNum, hintId int) error
	AdminGetBonusIds(ctx context.Context, gameId, levelNum int) ([]int, error)
	AdminGetBonus(ctx context.Context, gameId, levelNum, bonusId int) (*AdminBonus, error)
	AdminCreateBonus(ctx context.Context, gameId, levelNum int, b AdminBonus) error
	AdminUpdateBonus(ctx context.Context, gameId, levelNum, bonusId int, b AdminBonus) error
	AdminDeleteBonus(ctx context.Context, gameId, levelNum, bonusId int) error
	AdminGetMessageIds(ctx context.Context, gameId, levelNum int) ([]int, error)
	AdminGetMessage(ctx context.Context, gameId, levelNum, messageId int) (*AdminGameMessage, error)
	AdminCreateMessage(ctx context.Context, gameId, levelID int, m AdminGameMessage) error
	AdminUpdateMessage(ctx context.Context, gameId, levelNum, messageId int, m AdminGameMessage) error
	AdminDeleteMessage(ctx context.Context, gameId, levelNum, messageId int) error
	AdminGetSectorRefs(ctx context.Context, gameId, levelNum int) ([]AdminSector, error)
	AdminGetSectorAnswers(ctx context.Context, gameId, levelNum int) ([]AdminSector, error)
	AdminCreateSector(ctx context.Context, gameId, levelNum int, s AdminSector) error
	AdminUpdateSector(ctx context.Context, gameId, levelNum, sectorId int, s AdminSector) error
	AdminDeleteSector(ctx context.Context, gameId, levelNum, sectorId int) error
	AdminAddSectorAnswers(ctx context.Context, gameId, levelNum, sectorId int, answers []string) error
	AdminClearLevelSectors(ctx context.Context, gameID, levelNum int) error
	AdminGetLevelSettings(ctx context.Context, gameId, levelNum int) (*AdminLevelSettings, error)
	AdminUpdateAutopass(ctx context.Context, gameId, levelNum int, s AdminLevelSettings) error
	AdminUpdateAnswerBlock(ctx context.Context, gameId, levelNum int, s AdminLevelSettings) error
	AdminUpdateSectorCompletion(ctx context.Context, gameId, levelNum, requiredCount int) error
	AdminGetComment(ctx context.Context, gameId, levelNum int) (name, comment string, err error)
	AdminUpdateComment(ctx context.Context, gameId, levelNum int, name, comment string) error

	// Admin game management
	AdminCreateGame(ctx context.Context, params AdminCreateGameParams) (int, error)
	AdminDeleteGame(ctx context.Context, gameId int) error
	AdminGetGameInfo(ctx context.Context, gameId int) (*AdminGameInfo, error)
	AdminUpdateGameInfo(ctx context.Context, gameId int, info AdminGameInfo) error
	AdminDeliverGame(ctx context.Context, gameId int) error
	AdminNotDeliverGame(ctx context.Context, gameId int) error
	AdminAwardPoints(ctx context.Context, gameId int) error
	AdminEndRatings(ctx context.Context, gameId int) error
	AdminCalculateIK(ctx context.Context, gameId int) error
	AdminGetCorrections(ctx context.Context, gameId int) ([]AdminCorrection, error)
	AdminAddCorrection(ctx context.Context, gameId int, corr AdminCorrectionAdd) error
	AdminDeleteCorrection(ctx context.Context, gameId int, correctionId string) error
	AdminGetTeams(ctx context.Context, gameId, levelNum int) ([]AdminTeam, error)
	AdminGetActionMonitor(ctx context.Context, gameId int) ([]AdminActionMonitorEntry, error)

	// Scenario export
	GetGameScenario(ctx context.Context, gameId int) (*scenario.Document, error)
	GetGameScenarioHTML(ctx context.Context, gameId int) (string, error)
}

// legacyEngine routes calls to the ASP.NET implementation. Its methods forward
// to the legacy* methods on Client, which hold the original request logic.
type legacyEngine struct{ c *Client }

// newEngine routes calls to the Encounter Go Backend REST API.
type newEngine struct{ c *Client }

var (
	_ backend = (*legacyEngine)(nil)
	_ backend = (*newEngine)(nil)
)

// engine returns the backend serving the current request.
func (c *Client) engine(ctx context.Context) backend {
	if c.useNewEngine(ctx) {
		return c.modern
	}
	return c.legacy
}

func (e *legacyEngine) Login(ctx context.Context, login, password string, opts ...LoginOptions) (*LoginResponse, error) {
	return e.c.legacyLogin(ctx, login, password, opts...)
}

func (e *legacyEngine) LoginComplete(ctx context.Context, login, password string, opts ...LoginOptions) error {
	return e.c.legacyLoginComplete(ctx, login, password, opts...)
}

func (e *legacyEngine) VerifyAdminSession(ctx context.Context) error {
	return e.c.legacyVerifyAdminSession(ctx)
}

func (e *legacyEngine) GetGameModel(ctx context.Context, gameId int, formValues ...url.Values) (*GameModel, error) {
	return e.c.legacyGetGameModel(ctx, gameId, formValues...)
}

func (e *legacyEngine) GetGameModelLevel(ctx context.Context, gameId, levelNumber int) (*GameModel, error) {
	return e.c.legacyGetGameModelLevel(ctx, gameId, levelNumber)
}

func (e *legacyEngine) SendCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*GameModel, error) {
	return e.c.legacySendCode(ctx, gameId, levelId, levelNumber, code)
}

func (e *legacyEngine) SendBonusCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*GameModel, error) {
	return e.c.legacySendBonusCode(ctx, gameId, levelId, levelNumber, code)
}

func (e *legacyEngine) GetPenaltyHint(ctx context.Context, gameId, penaltyId int) (*GameModel, error) {
	return e.c.legacyGetPenaltyHint(ctx, gameId, penaltyId)
}

func (e *legacyEngine) GetGameList(ctx context.Context, page ...int) (*GameListResponse, error) {
	return e.c.legacyGetGameList(ctx, page...)
}

func (e *legacyEngine) GetDomainGames(ctx context.Context) ([]DomainGame, error) {
	return e.c.legacyGetDomainGames(ctx)
}

func (e *legacyEngine) GetGameDetails(ctx context.Context, gameId int) (string, error) {
	return e.c.legacyGetGameDetails(ctx, gameId)
}

func (e *legacyEngine) GetTimeoutToGame(ctx context.Context, gameId int) (*int, error) {
	return e.c.legacyGetTimeoutToGame(ctx, gameId)
}

func (e *legacyEngine) EnterGame(ctx context.Context, gameId int) (string, error) {
	return e.c.legacyEnterGame(ctx, gameId)
}

func (e *legacyEngine) FetchResource(ctx context.Context, rawURL string, opts ...ResourceOptions) (*Resource, error) {
	return e.c.fetchResource(ctx, e.c.baseURL(), rawURL, opts...)
}

func (e *legacyEngine) GetGameStatistics(ctx context.Context, gameId int) (*GameStatisticsResponse, error) {
	return e.c.legacyGetGameStatistics(ctx, gameId)
}

func (e *legacyEngine) GetProfile(ctx context.Context) (*Profile, error) {
	return e.c.legacyGetProfile(ctx)
}

func (e *legacyEngine) GetTeamDetails(ctx context.Context, teamId int) (string, error) {
	return e.c.legacyGetTeamDetails(ctx, teamId)
}

func (e *legacyEngine) GetMyTeamDetails(ctx context.Context) (string, error) {
	return e.c.legacyGetMyTeamDetails(ctx)
}

func (e *legacyEngine) GetTeamManagementInfo(ctx context.Context, teamID int) (*TeamManagementInfo, error) {
	return e.c.legacyGetTeamManagementInfo(ctx, teamID)
}

func (e *legacyEngine) GetTeamInvitations(ctx context.Context) ([]TeamInvitation, error) {
	return e.c.legacyGetTeamInvitations(ctx)
}

func (e *legacyEngine) AcceptTeamInvitation(ctx context.Context, teamId int) error {
	return e.c.legacyAcceptTeamInvitation(ctx, teamId)
}

func (e *legacyEngine) RejectTeamInvitation(ctx context.Context, teamID int) error {
	return e.c.legacyRejectTeamInvitation(ctx, teamID)
}

func (e *legacyEngine) RequestTeamMembership(ctx context.Context, teamName string) error {
	return e.c.legacyRequestTeamMembership(ctx, teamName)
}

func (e *legacyEngine) InviteTeamMember(ctx context.Context, teamID int, login string) error {
	return e.c.legacyInviteTeamMember(ctx, teamID, login)
}

func (e *legacyEngine) RemoveTeamInvitation(ctx context.Context, teamID, userID int) error {
	return e.c.legacyRemoveTeamInvitation(ctx, teamID, userID)
}

func (e *legacyEngine) LeaveTeam(ctx context.Context, teamID int) error {
	return e.c.legacyLeaveTeam(ctx, teamID)
}

func (e *legacyEngine) RenameTeam(ctx context.Context, teamID int, name string) error {
	return e.c.legacyRenameTeam(ctx, teamID, name)
}

func (e *legacyEngine) SetTeamSite(ctx context.Context, teamID int, site string) error {
	return e.c.legacySetTeamSite(ctx, teamID, site)
}

func (e *legacyEngine) SetTeamForum(ctx context.Context, teamID int, forum string) error {
	return e.c.legacySetTeamForum(ctx, teamID, forum)
}

func (e *legacyEngine) AdminGetGames(ctx context.Context) ([]AdminGame, error) {
	return e.c.legacyAdminGetGames(ctx)
}
func (e *legacyEngine) AdminGetLevels(ctx context.Context, gameId int) ([]AdminLevel, error) {
	return e.c.legacyAdminGetLevels(ctx, gameId)
}
func (e *legacyEngine) AdminCreateLevels(ctx context.Context, gameId, count int) error {
	return e.c.legacyAdminCreateLevels(ctx, gameId, count)
}
func (e *legacyEngine) AdminDeleteLevel(ctx context.Context, gameId, levelNum int) error {
	return e.c.legacyAdminDeleteLevel(ctx, gameId, levelNum)
}
func (e *legacyEngine) AdminRenameLevels(ctx context.Context, gameId int, names map[int]string) error {
	return e.c.legacyAdminRenameLevels(ctx, gameId, names)
}
func (e *legacyEngine) AdminSwapLevels(ctx context.Context, gameId, level1, level2 int) error {
	return e.c.legacyAdminSwapLevels(ctx, gameId, level1, level2)
}
func (e *legacyEngine) AdminInsertLevel(ctx context.Context, gameId, src, dst int) error {
	return e.c.legacyAdminInsertLevel(ctx, gameId, src, dst)
}
func (e *legacyEngine) AdminCloneLevels(ctx context.Context, gameId, count, likeLevel int) error {
	return e.c.legacyAdminCloneLevels(ctx, gameId, count, likeLevel)
}
func (e *legacyEngine) AdminGetTaskIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	return e.c.legacyAdminGetTaskIds(ctx, gameId, levelNum)
}
func (e *legacyEngine) AdminGetTask(ctx context.Context, gameId, levelNum, taskId int) (*AdminTask, error) {
	return e.c.legacyAdminGetTask(ctx, gameId, levelNum, taskId)
}
func (e *legacyEngine) AdminCreateTask(ctx context.Context, gameId, levelNum int, t AdminTask) error {
	return e.c.legacyAdminCreateTask(ctx, gameId, levelNum, t)
}
func (e *legacyEngine) AdminUpdateTask(ctx context.Context, gameId, levelNum, taskId int, t AdminTask) error {
	return e.c.legacyAdminUpdateTask(ctx, gameId, levelNum, taskId, t)
}
func (e *legacyEngine) AdminDeleteTask(ctx context.Context, gameId, levelNum, taskId int) error {
	return e.c.legacyAdminDeleteTask(ctx, gameId, levelNum, taskId)
}
func (e *legacyEngine) AdminGetHintIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	return e.c.legacyAdminGetHintIds(ctx, gameId, levelNum)
}
func (e *legacyEngine) AdminGetHint(ctx context.Context, gameId, levelNum, hintId int) (*AdminHint, error) {
	return e.c.legacyAdminGetHint(ctx, gameId, levelNum, hintId)
}
func (e *legacyEngine) AdminCreateHint(ctx context.Context, gameId, levelNum int, h AdminHint) error {
	return e.c.legacyAdminCreateHint(ctx, gameId, levelNum, h)
}
func (e *legacyEngine) AdminUpdateHint(ctx context.Context, gameId, levelNum, hintId int, h AdminHint) error {
	return e.c.legacyAdminUpdateHint(ctx, gameId, levelNum, hintId, h)
}
func (e *legacyEngine) AdminDeleteHint(ctx context.Context, gameId, levelNum, hintId int) error {
	return e.c.legacyAdminDeleteHint(ctx, gameId, levelNum, hintId)
}
func (e *legacyEngine) AdminGetBonusIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	return e.c.legacyAdminGetBonusIds(ctx, gameId, levelNum)
}
func (e *legacyEngine) AdminGetBonus(ctx context.Context, gameId, levelNum, bonusId int) (*AdminBonus, error) {
	return e.c.legacyAdminGetBonus(ctx, gameId, levelNum, bonusId)
}
func (e *legacyEngine) AdminCreateBonus(ctx context.Context, gameId, levelNum int, b AdminBonus) error {
	return e.c.legacyAdminCreateBonus(ctx, gameId, levelNum, b)
}
func (e *legacyEngine) AdminUpdateBonus(ctx context.Context, gameId, levelNum, bonusId int, b AdminBonus) error {
	return e.c.legacyAdminUpdateBonus(ctx, gameId, levelNum, bonusId, b)
}
func (e *legacyEngine) AdminDeleteBonus(ctx context.Context, gameId, levelNum, bonusId int) error {
	return e.c.legacyAdminDeleteBonus(ctx, gameId, levelNum, bonusId)
}
func (e *legacyEngine) AdminGetMessageIds(ctx context.Context, gameId, levelNum int) ([]int, error) {
	return e.c.legacyAdminGetMessageIds(ctx, gameId, levelNum)
}
func (e *legacyEngine) AdminGetMessage(ctx context.Context, gameId, levelNum, messageId int) (*AdminGameMessage, error) {
	return e.c.legacyAdminGetMessage(ctx, gameId, levelNum, messageId)
}
func (e *legacyEngine) AdminCreateMessage(ctx context.Context, gameId, levelID int, m AdminGameMessage) error {
	return e.c.legacyAdminCreateMessage(ctx, gameId, levelID, m)
}
func (e *legacyEngine) AdminUpdateMessage(ctx context.Context, gameId, levelNum, messageId int, m AdminGameMessage) error {
	return e.c.legacyAdminUpdateMessage(ctx, gameId, levelNum, messageId, m)
}
func (e *legacyEngine) AdminDeleteMessage(ctx context.Context, gameId, levelNum, messageId int) error {
	return e.c.legacyAdminDeleteMessage(ctx, gameId, levelNum, messageId)
}
func (e *legacyEngine) AdminGetSectorRefs(ctx context.Context, gameId, levelNum int) ([]AdminSector, error) {
	return e.c.legacyAdminGetSectorRefs(ctx, gameId, levelNum)
}
func (e *legacyEngine) AdminGetSectorAnswers(ctx context.Context, gameId, levelNum int) ([]AdminSector, error) {
	return e.c.legacyAdminGetSectorAnswers(ctx, gameId, levelNum)
}
func (e *legacyEngine) AdminCreateSector(ctx context.Context, gameId, levelNum int, s AdminSector) error {
	return e.c.legacyAdminCreateSector(ctx, gameId, levelNum, s)
}
func (e *legacyEngine) AdminUpdateSector(ctx context.Context, gameId, levelNum, sectorId int, s AdminSector) error {
	return e.c.legacyAdminUpdateSector(ctx, gameId, levelNum, sectorId, s)
}
func (e *legacyEngine) AdminDeleteSector(ctx context.Context, gameId, levelNum, sectorId int) error {
	return e.c.legacyAdminDeleteSector(ctx, gameId, levelNum, sectorId)
}
func (e *legacyEngine) AdminAddSectorAnswers(ctx context.Context, gameId, levelNum, sectorId int, answers []string) error {
	return e.c.legacyAdminAddSectorAnswers(ctx, gameId, levelNum, sectorId, answers)
}
func (e *legacyEngine) AdminClearLevelSectors(ctx context.Context, gameID, levelNum int) error {
	return e.c.legacyAdminClearLevelSectors(ctx, gameID, levelNum)
}
func (e *legacyEngine) AdminGetLevelSettings(ctx context.Context, gameId, levelNum int) (*AdminLevelSettings, error) {
	return e.c.legacyAdminGetLevelSettings(ctx, gameId, levelNum)
}
func (e *legacyEngine) AdminUpdateAutopass(ctx context.Context, gameId, levelNum int, s AdminLevelSettings) error {
	return e.c.legacyAdminUpdateAutopass(ctx, gameId, levelNum, s)
}
func (e *legacyEngine) AdminUpdateAnswerBlock(ctx context.Context, gameId, levelNum int, s AdminLevelSettings) error {
	return e.c.legacyAdminUpdateAnswerBlock(ctx, gameId, levelNum, s)
}
func (e *legacyEngine) AdminUpdateSectorCompletion(ctx context.Context, gameId, levelNum, requiredCount int) error {
	return e.c.legacyAdminUpdateSectorCompletion(ctx, gameId, levelNum, requiredCount)
}
func (e *legacyEngine) AdminGetComment(ctx context.Context, gameId, levelNum int) (name, comment string, err error) {
	return e.c.legacyAdminGetComment(ctx, gameId, levelNum)
}
func (e *legacyEngine) AdminUpdateComment(ctx context.Context, gameId, levelNum int, name, comment string) error {
	return e.c.legacyAdminUpdateComment(ctx, gameId, levelNum, name, comment)
}

func (e *legacyEngine) AdminCreateGame(ctx context.Context, params AdminCreateGameParams) (int, error) {
	return e.c.legacyAdminCreateGame(ctx, params)
}

func (e *legacyEngine) AdminDeleteGame(ctx context.Context, gameId int) error {
	return e.c.legacyAdminDeleteGame(ctx, gameId)
}

func (e *legacyEngine) AdminGetGameInfo(ctx context.Context, gameId int) (*AdminGameInfo, error) {
	return e.c.legacyAdminGetGameInfo(ctx, gameId)
}

func (e *legacyEngine) AdminUpdateGameInfo(ctx context.Context, gameId int, info AdminGameInfo) error {
	return e.c.legacyAdminUpdateGameInfo(ctx, gameId, info)
}

func (e *legacyEngine) AdminDeliverGame(ctx context.Context, gameId int) error {
	return e.c.legacyAdminDeliverGame(ctx, gameId)
}

func (e *legacyEngine) AdminNotDeliverGame(ctx context.Context, gameId int) error {
	return e.c.legacyAdminNotDeliverGame(ctx, gameId)
}

func (e *legacyEngine) AdminAwardPoints(ctx context.Context, gameId int) error {
	return e.c.legacyAdminAwardPoints(ctx, gameId)
}

func (e *legacyEngine) AdminEndRatings(ctx context.Context, gameId int) error {
	return e.c.legacyAdminEndRatings(ctx, gameId)
}

func (e *legacyEngine) AdminCalculateIK(ctx context.Context, gameId int) error {
	return e.c.legacyAdminCalculateIK(ctx, gameId)
}

func (e *legacyEngine) AdminGetCorrections(ctx context.Context, gameId int) ([]AdminCorrection, error) {
	return e.c.legacyAdminGetCorrections(ctx, gameId)
}

func (e *legacyEngine) AdminAddCorrection(ctx context.Context, gameId int, corr AdminCorrectionAdd) error {
	return e.c.legacyAdminAddCorrection(ctx, gameId, corr)
}

func (e *legacyEngine) AdminDeleteCorrection(ctx context.Context, gameId int, correctionId string) error {
	return e.c.legacyAdminDeleteCorrection(ctx, gameId, correctionId)
}

func (e *legacyEngine) AdminGetTeams(ctx context.Context, gameId, levelNum int) ([]AdminTeam, error) {
	return e.c.legacyAdminGetTeams(ctx, gameId, levelNum)
}

func (e *legacyEngine) AdminGetActionMonitor(ctx context.Context, gameId int) ([]AdminActionMonitorEntry, error) {
	return e.c.legacyAdminGetActionMonitor(ctx, gameId)
}
