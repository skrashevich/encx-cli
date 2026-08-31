package agenttools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/tools"
)

// Options configures a catalog.
type Options struct {
	// Policy decides what the agent may do with the engine. Empty means
	// DefaultPolicy.
	Policy Policy
	// Confirmer authorizes mutating calls under PolicyApprove.
	Confirmer Confirmer
	// ReadCacheTTL memoizes read-tool results for this long. Zero disables
	// caching. Callers that drive discrete turns should also call
	// InvalidateCache between them.
	ReadCacheTTL time.Duration
}

// Catalog is the engine toolset bound to one engine and one access policy.
type Catalog struct {
	policy Policy
	cache  *readCache
	all    []*Tool
	byName map[string]*Tool
}

// NewCatalog builds the engine toolset.
func NewCatalog(engine Engine, opts Options) (*Catalog, error) {
	if engine == nil {
		return nil, errors.New("agenttools: engine is required")
	}
	policy, err := ParsePolicy(string(opts.Policy))
	if err != nil {
		return nil, err
	}
	if policy == PolicyApprove && opts.Confirmer == nil {
		return nil, errors.New("agenttools: policy \"approve\" requires a Confirmer")
	}

	g := &gate{policy: policy, confirmer: opts.Confirmer}
	catalog := &Catalog{
		policy: policy,
		cache:  newReadCache(opts.ReadCacheTTL),
		byName: map[string]*Tool{},
	}
	catalog.add(readTools(engine, g)...)
	catalog.add(mutatingTools(engine, g)...)
	return catalog, nil
}

func (c *Catalog) add(list ...*Tool) {
	for _, tool := range list {
		tool.cache = c.cache
		c.all = append(c.all, tool)
		c.byName[tool.name] = tool
	}
}

// InvalidateCache drops every memoized read.
//
// Callers that serve discrete requests should call this between them: the game
// moves while the player reads, and answering a new question from a cached level
// is worse than a slower fresh read.
func (c *Catalog) InvalidateCache() { c.cache.clear() }

// Policy returns the access policy the catalog was built with.
func (c *Catalog) Policy() Policy { return c.policy }

// Tools returns the tools an agent may see. Under PolicyReadonly the mutating
// tools are withheld so the model is never tempted to call them.
func (c *Catalog) Tools() []*Tool {
	exposed := make([]*Tool, 0, len(c.all))
	for _, tool := range c.all {
		if tool.mutating && c.policy == PolicyReadonly {
			continue
		}
		exposed = append(exposed, tool)
	}
	return exposed
}

// All returns every tool the catalog knows about, including the ones the policy
// hides.
func (c *Catalog) All() []*Tool { return append([]*Tool(nil), c.all...) }

// Lookup finds a tool by name, ignoring policy visibility.
func (c *Catalog) Lookup(name string) (*Tool, bool) {
	tool, ok := c.byName[name]
	return tool, ok
}

// Register adds the exposed tools to a PicoClaw registry.
func (c *Catalog) Register(registry *tools.ToolRegistry) {
	if registry == nil {
		return
	}
	for _, tool := range c.Tools() {
		registry.Register(tool)
	}
}

// SystemPromptAddendum describes the engine and the active policy to the model.
func (c *Catalog) SystemPromptAddendum() string {
	var b strings.Builder
	b.WriteString("You control an Encounter (en.cx) game account through the enc_* tools. ")
	b.WriteString("Game state changes constantly, so read the current state before reasoning about it ")
	b.WriteString("instead of relying on earlier turns.\n")
	b.WriteString("Level content often carries the task in a picture. When a level reports images, " +
		"call enc_view_image on them — you can see the picture itself, so never tell the user you " +
		"are unable to open a link from the engine.\n")
	switch c.policy {
	case PolicyReadonly:
		b.WriteString("Access policy: READ-ONLY. You cannot submit codes, take penalty hints or join games. " +
			"Explain what should be done and let the user do it.")
	case PolicyApprove:
		b.WriteString("Access policy: APPROVAL REQUIRED. Submitting a code, taking a penalty hint or joining a game " +
			"is shown to the user for confirmation first. Never assume such an action succeeded until the tool returns. " +
			"If the user declines, do not retry the same call.")
	case PolicyFull:
		b.WriteString("Access policy: FULL. Mutating calls run immediately. Penalty hints cost time and wrong codes " +
			"can trigger answer blocks, so state your intent before acting.")
	}
	return b.String()
}

const (
	toolProfile       = "enc_profile"
	toolTeam          = "enc_team"
	toolDomainGames   = "enc_domain_games"
	toolGameList      = "enc_game_list"
	toolGameTimeout   = "enc_game_timeout"
	toolGameState     = "enc_game_state"
	toolLevel         = "enc_level"
	toolActionLog     = "enc_action_log"
	toolViewImage     = "enc_view_image"
	toolStatistics    = "enc_game_statistics"
	toolSendCode      = "enc_send_code"
	toolSendBonusCode = "enc_send_bonus_code"
	toolPenaltyHint   = "enc_take_penalty_hint"
	toolEnterGame     = "enc_enter_game"
)

func readTools(engine Engine, g *gate) []*Tool {
	return []*Tool{
		{
			name:        toolProfile,
			description: "Read the signed-in player's profile: login, name, rank, team and points.",
			parameters:  schema(nil),
			gate:        g,
			run: func(ctx context.Context, _ arguments) (any, error) {
				return engine.GetProfile(ctx)
			},
		},
		{
			name: toolTeam,
			description: "Read the player's team: name, pending invitations and available management actions. " +
				"Defaults to the team from the player's profile.",
			parameters: schema(map[string]any{
				"team_id": intProp("Team ID. Omit to use the team from the player's profile."),
			}),
			gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				teamID, ok := args.optionalInt("team_id")
				if !ok {
					profile, err := engine.GetProfile(ctx)
					if err != nil {
						return nil, fmt.Errorf("resolve team from profile: %w", err)
					}
					if profile == nil || profile.TeamID == 0 {
						return nil, errors.New("the profile has no team; pass team_id explicitly")
					}
					teamID = profile.TeamID
				}
				return engine.GetTeamManagementInfo(ctx, teamID)
			},
		},
		{
			name:        toolDomainGames,
			description: "List the games advertised on the domain's front page (title and game ID only).",
			parameters:  schema(nil),
			gate:        g,
			run: func(ctx context.Context, _ arguments) (any, error) {
				return engine.GetDomainGames(ctx)
			},
		},
		{
			name:        toolGameList,
			description: "List upcoming and active games with schedule and type details.",
			parameters: schema(map[string]any{
				"page": intProp("Page number of the game list. Omit for the first page."),
			}),
			gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				var list *encxGameList
				var err error
				if page, ok := args.optionalInt("page"); ok {
					list, err = fetchGameList(ctx, engine, page)
				} else {
					list, err = fetchGameList(ctx, engine)
				}
				if err != nil {
					return nil, err
				}
				return list, nil
			},
		},
		{
			name:        toolGameTimeout,
			description: "Seconds remaining until a game starts. Null means the game is not pending a start.",
			parameters: schema(map[string]any{
				"game_id": intProp("Game ID."),
			}, "game_id"),
			gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				seconds, err := engine.GetTimeoutToGame(ctx, gameID)
				if err != nil {
					return nil, err
				}
				return map[string]any{"game_id": gameID, "seconds_to_start": seconds}, nil
			},
		},
		{
			name: toolGameState,
			description: "Read the current state of a game: title, team, level list and the active level with its " +
				"tasks, sectors, bonuses, hints and organizer messages.",
			parameters: schema(map[string]any{
				"game_id": intProp("Game ID."),
			}, "game_id"),
			gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				model, err := engine.GetGameModel(ctx, gameID)
				if err != nil {
					return nil, err
				}
				return newGameStateView(model), nil
			},
		},
		{
			name: toolLevel,
			description: "Read one level in detail. Without level_number this returns the active level; with it, the " +
				"requested level of a storm-sequence game.",
			parameters: schema(map[string]any{
				"game_id":      intProp("Game ID."),
				"level_number": intProp("Level number. Omit for the active level."),
			}, "game_id"),
			gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				model, err := loadLevelModel(ctx, engine, args)
				if err != nil {
					return nil, err
				}
				if model.Level == nil {
					return nil, errors.New("the game has no active level right now")
				}
				return newLevelView(model.Level), nil
			},
		},
		{
			name:        toolActionLog,
			description: "Read the codes submitted on a level, with correctness and penalties.",
			parameters: schema(map[string]any{
				"game_id":      intProp("Game ID."),
				"level_number": intProp("Level number. Omit for the active level."),
			}, "game_id"),
			gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				model, err := loadLevelModel(ctx, engine, args)
				if err != nil {
					return nil, err
				}
				if model.Level == nil {
					return nil, errors.New("the game has no active level right now")
				}
				return map[string]any{
					"game_id":      model.GameId,
					"level_number": model.Level.Number,
					"actions":      newCodeEntryViews(model.Level.MixedActions),
				}, nil
			},
		},
		{
			name: toolViewImage,
			description: "Look at a picture from game content. Pass a URL reported in the images list of " +
				"enc_level or enc_game_state. The picture is returned to you directly, so use this " +
				"whenever a task refers to an image instead of guessing from the surrounding text.",
			parameters: schema(map[string]any{
				"url": stringProp("Image URL from the level's images list."),
			}, "url"),
			// An inlined image is megabytes of base64; keeping it in the read cache
			// would hold the whole picture in memory for the rest of the turn.
			noCache: true,
			gate:    g,
			run: func(ctx context.Context, args arguments) (any, error) {
				rawURL, err := args.requireString("url")
				if err != nil {
					return nil, err
				}
				resource, err := engine.FetchResource(ctx, rawURL)
				if err != nil {
					return nil, err
				}
				encoded, err := dataURL(resource)
				if err != nil {
					return nil, err
				}
				return toolOutput{
					value: map[string]any{
						"url":          resource.URL,
						"content_type": resource.ContentType,
						"bytes":        len(resource.Data),
						"note":         "The image is attached to this result.",
					},
					media: []string{encoded},
				}, nil
			},
		},
		{
			name:        toolStatistics,
			description: "Read game statistics: level breakdown, team rankings and per-level timings.",
			parameters: schema(map[string]any{
				"game_id": intProp("Game ID."),
			}, "game_id"),
			gate: g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				return engine.GetGameStatistics(ctx, gameID)
			},
		},
	}
}

func mutatingTools(engine Engine, g *gate) []*Tool {
	return []*Tool{
		{
			name: toolSendCode,
			description: "Submit a level or sector answer. A wrong code costs time and may trigger the level's " +
				"answer-block rule, so only submit codes the user asked for or that you are confident about.",
			parameters: schema(map[string]any{
				"game_id": intProp("Game ID."),
				"code":    stringProp("The answer to submit, exactly as it should reach the engine."),
			}, "game_id", "code"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				return submitAnswer(ctx, engine, args, false)
			},
		},
		{
			name:        toolSendBonusCode,
			description: "Submit a bonus answer on the active level.",
			parameters: schema(map[string]any{
				"game_id": intProp("Game ID."),
				"code":    stringProp("The bonus answer to submit."),
			}, "game_id", "code"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				return submitAnswer(ctx, engine, args, true)
			},
		},
		{
			name: toolPenaltyHint,
			description: "Take a penalty hint by its help ID. This adds the hint's penalty time to the team's " +
				"result and cannot be undone.",
			parameters: schema(map[string]any{
				"game_id": intProp("Game ID."),
				"hint_id": intProp("Penalty hint help_id, as reported by enc_level."),
			}, "game_id", "hint_id"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				hintID, err := args.requireInt("hint_id")
				if err != nil {
					return nil, err
				}
				model, err := engine.GetPenaltyHint(ctx, gameID, hintID)
				if err != nil {
					return nil, err
				}
				result := map[string]any{"game_id": gameID, "hint_id": hintID}
				if model != nil && model.Level != nil {
					result["penalty_hints"] = newHintViews(model.Level.PenaltyHelps)
				}
				return result, nil
			},
		},
		{
			name: toolEnterGame,
			description: "Submit an application to join a game as a player. This does not start a game; " +
				"only the organizer can do that.",
			parameters: schema(map[string]any{
				"game_id": intProp("Game ID."),
			}, "game_id"),
			mutating: true,
			gate:     g,
			run: func(ctx context.Context, args arguments) (any, error) {
				gameID, err := args.requireInt("game_id")
				if err != nil {
					return nil, err
				}
				// The engine answers with the full registration page; the agent only
				// needs to know the request went through.
				if _, err := engine.EnterGame(ctx, gameID); err != nil {
					return nil, err
				}
				return map[string]any{
					"game_id":   gameID,
					"submitted": true,
					"note":      "Application submitted. Read enc_game_state to confirm participation.",
				}, nil
			},
		},
	}
}

// encxGameList is the projected reply of enc_game_list.
type encxGameList struct {
	ComingGames []gameSummaryView `json:"coming_games,omitempty"`
	ActiveGames []gameSummaryView `json:"active_games,omitempty"`
	Message     string            `json:"message,omitempty"`
}

func fetchGameList(ctx context.Context, engine Engine, page ...int) (*encxGameList, error) {
	response, err := engine.GetGameList(ctx, page...)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return &encxGameList{}, nil
	}
	return &encxGameList{
		ComingGames: newGameSummaryViews(response.ComingGames),
		ActiveGames: newGameSummaryViews(response.ActiveGames),
		Message:     response.Message,
	}, nil
}

func loadLevelModel(ctx context.Context, engine Engine, args arguments) (*gameModel, error) {
	gameID, err := args.requireInt("game_id")
	if err != nil {
		return nil, err
	}
	if levelNumber, ok := args.optionalInt("level_number"); ok {
		return engine.GetGameModelLevel(ctx, gameID, levelNumber)
	}
	return engine.GetGameModel(ctx, gameID)
}

func submitAnswer(ctx context.Context, engine Engine, args arguments, bonus bool) (any, error) {
	gameID, err := args.requireInt("game_id")
	if err != nil {
		return nil, err
	}
	code, err := args.requireString("code")
	if err != nil {
		return nil, err
	}

	model, err := engine.GetGameModel(ctx, gameID)
	if err != nil {
		return nil, fmt.Errorf("read game state before submitting: %w", err)
	}
	if model == nil || model.Level == nil {
		return nil, errors.New("the game has no active level, so there is nothing to answer")
	}
	level := model.Level
	if !bonus && !level.CanSubmitLevelAnswer() {
		return nil, errors.New("the active level does not accept level answers right now")
	}

	var result *gameModel
	if bonus {
		result, err = engine.SendBonusCode(ctx, gameID, level.LevelId, level.Number, code)
	} else {
		result, err = engine.SendCode(ctx, gameID, level.LevelId, level.Number, code)
	}
	if err != nil {
		return nil, err
	}
	return newActionResultView(result, level.Number, bonus), nil
}

func schema(properties map[string]any, required ...string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	out := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func intProp(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func stringProp(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
