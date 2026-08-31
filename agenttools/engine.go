package agenttools

import (
	"context"
	"net/url"

	"github.com/skrashevich/encx-cli/encx"
)

// Engine is the slice of the Encounter client the tool catalog depends on.
// Narrowing the dependency keeps the tools testable without a live domain.
type Engine interface {
	GetDomainGames(ctx context.Context) ([]encx.DomainGame, error)
	GetGameList(ctx context.Context, page ...int) (*encx.GameListResponse, error)
	GetGameModel(ctx context.Context, gameId int, formValues ...url.Values) (*encx.GameModel, error)
	GetGameModelLevel(ctx context.Context, gameId, levelNumber int) (*encx.GameModel, error)
	GetGameStatistics(ctx context.Context, gameId int) (*encx.GameStatisticsResponse, error)
	GetTimeoutToGame(ctx context.Context, gameId int) (*int, error)
	GetProfile(ctx context.Context) (*encx.Profile, error)
	GetTeamManagementInfo(ctx context.Context, teamID int) (*encx.TeamManagementInfo, error)
	FetchResource(ctx context.Context, rawURL string, opts ...encx.ResourceOptions) (*encx.Resource, error)
	EnterGame(ctx context.Context, gameId int) (string, error)
	SendCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*encx.GameModel, error)
	SendBonusCode(ctx context.Context, gameId, levelId, levelNumber int, code string) (*encx.GameModel, error)
	GetPenaltyHint(ctx context.Context, gameId, penaltyId int) (*encx.GameModel, error)
}

var _ Engine = (*encx.Client)(nil)

// gameModel keeps the engine's central payload type readable in tool plumbing.
type gameModel = encx.GameModel
