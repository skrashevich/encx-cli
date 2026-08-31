package agenttools

import (
	"context"
	"net/url"
	"sync"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

// DefaultRequestInterval is the floor between engine requests. It matches the
// pacing the iOS app already applies to its own traffic.
const DefaultRequestInterval = 350 * time.Millisecond

// Paced wraps an Engine so concurrent tools cannot burst requests at Encounter.
//
// An LLM runtime executes the tool calls of one turn in parallel, so a single
// question can fire half a dozen engine requests at once. Encounter answers a
// burst with its anti-spam page rather than JSON, which surfaces to the player
// as a broken session on an account that is perfectly fine.
type Paced struct {
	engine   Engine
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
}

// NewPaced spaces out requests to engine. A non-positive interval means
// DefaultRequestInterval.
func NewPaced(engine Engine, interval time.Duration) *Paced {
	if interval <= 0 {
		interval = DefaultRequestInterval
	}
	return &Paced{engine: engine, interval: interval}
}

var _ Engine = (*Paced)(nil)

// wait reserves the next slot and sleeps outside the lock, so callers queue in
// order without blocking each other's bookkeeping.
func (p *Paced) wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	next := p.last.Add(p.interval)
	var delay time.Duration
	if !p.last.IsZero() && next.After(now) {
		delay = next.Sub(now)
		p.last = next
	} else {
		p.last = now
	}
	p.mu.Unlock()

	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (p *Paced) GetDomainGames(ctx context.Context) ([]encx.DomainGame, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return p.engine.GetDomainGames(ctx)
}

func (p *Paced) GetGameList(ctx context.Context, page ...int) (*encx.GameListResponse, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return p.engine.GetGameList(ctx, page...)
}

func (p *Paced) GetGameModel(
	ctx context.Context, gameId int, formValues ...url.Values,
) (*encx.GameModel, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return p.engine.GetGameModel(ctx, gameId, formValues...)
}

func (p *Paced) GetGameModelLevel(ctx context.Context, gameId, levelNumber int) (*encx.GameModel, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return p.engine.GetGameModelLevel(ctx, gameId, levelNumber)
}

func (p *Paced) GetGameStatistics(ctx context.Context, gameId int) (*encx.GameStatisticsResponse, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return p.engine.GetGameStatistics(ctx, gameId)
}

func (p *Paced) GetTimeoutToGame(ctx context.Context, gameId int) (*int, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return p.engine.GetTimeoutToGame(ctx, gameId)
}

func (p *Paced) GetProfile(ctx context.Context) (*encx.Profile, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return p.engine.GetProfile(ctx)
}

func (p *Paced) GetTeamManagementInfo(ctx context.Context, teamID int) (*encx.TeamManagementInfo, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return p.engine.GetTeamManagementInfo(ctx, teamID)
}

func (p *Paced) FetchResource(
	ctx context.Context, rawURL string, opts ...encx.ResourceOptions,
) (*encx.Resource, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return p.engine.FetchResource(ctx, rawURL, opts...)
}

func (p *Paced) EnterGame(ctx context.Context, gameId int) (string, error) {
	if err := p.wait(ctx); err != nil {
		return "", err
	}
	return p.engine.EnterGame(ctx, gameId)
}

func (p *Paced) SendCode(
	ctx context.Context, gameId, levelId, levelNumber int, code string,
) (*encx.GameModel, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return p.engine.SendCode(ctx, gameId, levelId, levelNumber, code)
}

func (p *Paced) SendBonusCode(
	ctx context.Context, gameId, levelId, levelNumber int, code string,
) (*encx.GameModel, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return p.engine.SendBonusCode(ctx, gameId, levelId, levelNumber, code)
}

func (p *Paced) GetPenaltyHint(ctx context.Context, gameId, penaltyId int) (*encx.GameModel, error) {
	if err := p.wait(ctx); err != nil {
		return nil, err
	}
	return p.engine.GetPenaltyHint(ctx, gameId, penaltyId)
}
