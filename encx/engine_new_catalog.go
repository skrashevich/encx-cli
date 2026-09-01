package encx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/skrashevich/encx-cli/encx/enapi"
)

func (e *newEngine) GetGameList(ctx context.Context, page ...int) (*GameListResponse, error) {
	pageNum := 1
	if len(page) > 0 && page[0] > 0 {
		pageNum = page[0]
	}

	// The home block is the direct counterpart of /home/?json=1, but it is not
	// paged; later pages come from the paged catalog endpoints instead.
	if pageNum == 1 {
		var home enapi.HomeGamesResponse
		if err := e.c.api().GetJSON(ctx, "/games/home", nil, &home); err != nil {
			return nil, err
		}
		return &GameListResponse{
			ComingGames: gameInfosFromAPI(home.ComingGames),
			ActiveGames: gameInfosFromAPI(home.ActiveGames),
		}, nil
	}

	active, err := e.listGames(ctx, "/games/active", pageNum)
	if err != nil {
		return nil, err
	}
	coming, err := e.listGames(ctx, "/games/coming", pageNum)
	if err != nil {
		return nil, err
	}
	return &GameListResponse{
		ActiveGames: gameInfosFromAPI(active),
		ComingGames: gameInfosFromAPI(coming),
	}, nil
}

func (e *newEngine) listGames(ctx context.Context, path string, page int) ([]enapi.Game, error) {
	q := url.Values{}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	var resp enapi.GamesResponse
	if err := e.c.api().GetJSON(ctx, path, q, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

func (e *newEngine) GetDomainGames(ctx context.Context) ([]DomainGame, error) {
	list, err := e.GetGameList(ctx)
	if err != nil {
		return nil, err
	}
	return domainGamesFromList(list), nil
}

func (e *newEngine) GetGameDetails(ctx context.Context, gameId int) (string, error) {
	var details enapi.GameDetails
	path := fmt.Sprintf("/games/%d/details", gameId)
	if err := e.c.api().GetJSON(ctx, path, nil, &details); err != nil {
		return "", err
	}
	// The legacy engine hands back the HTML page; the new one has no HTML to
	// give, so callers receive the structured document verbatim.
	encoded, err := json.Marshal(details)
	if err != nil {
		return "", fmt.Errorf("encx: encode game details: %w", err)
	}
	return string(encoded), nil
}

func (e *newEngine) GetTimeoutToGame(ctx context.Context, gameId int) (*int, error) {
	var box enapi.GameFeeBox
	path := fmt.Sprintf("/games/%d/fee-box", gameId)
	if err := e.c.api().GetJSON(ctx, path, nil, &box); err != nil {
		return nil, err
	}
	// nil means "this game shows no countdown", which is what the legacy engine
	// reported when the page carried no StartCounter. show_timer is that same
	// statement, so a game counting down to zero returns 0 rather than nil —
	// collapsing the two would tell a caller polling for the start that the
	// countdown had vanished at the moment it reached the start.
	if !box.ShowTimer && box.SecondsToStart <= 0 {
		return nil, nil
	}
	// The legacy counter was digits only, so it could never come back negative;
	// a game that has just started reports 0 seconds left rather than a count
	// running the wrong way.
	seconds := max(box.SecondsToStart, 0)
	return &seconds, nil
}

func (e *newEngine) EnterGame(ctx context.Context, gameId int) (string, error) {
	var resp enapi.GameJoinResponse
	path := fmt.Sprintf("/games/%d/make-fee", gameId)
	err := e.c.api().Do(ctx, enapi.Request{
		Method: "POST",
		Path:   path,
		Query:  url.Values{"confirm": {"yes"}},
		Out:    &resp,
	})
	if err != nil {
		return "", err
	}
	if !resp.Success {
		message := firstNonEmpty(resp.Message, resp.FeeAcceptedText)
		if message == "" {
			message = "заявка отклонена"
		}
		return "", fmt.Errorf("encx: enter game %d: %s", gameId, message)
	}
	encoded, err := json.Marshal(resp)
	if err != nil {
		return "", fmt.Errorf("encx: encode join response: %w", err)
	}
	return string(encoded), nil
}

func (e *newEngine) FetchResource(ctx context.Context, rawURL string, opts ...ResourceOptions) (*Resource, error) {
	// Media on the new engine is served by the API host, so site-relative
	// references resolve against it rather than against the site domain.
	base := e.c.APIBaseURL()
	if base == "" {
		return nil, &enapi.MissingHostError{Domain: e.c.domain}
	}
	return e.c.fetchResource(ctx, base, rawURL, opts...)
}

// gameInfosFromAPI maps catalog entries onto the GameInfo consumers already read.
func gameInfosFromAPI(games []enapi.Game) []GameInfo {
	if len(games) == 0 {
		return nil
	}
	out := make([]GameInfo, 0, len(games))
	for i := range games {
		out = append(out, gameInfoFromAPI(&games[i]))
	}
	return out
}

func gameInfoFromAPI(game *enapi.Game) GameInfo {
	info := GameInfo{
		GameID:                   game.ID,
		GameNum:                  game.GameNum,
		SiteID:                   game.SiteID,
		LangID:                   game.LangID,
		CompetitionID:            game.CompetitionID,
		OwnerID:                  game.OwnerID,
		LevelNumber:              game.LevelNumber,
		CreateDateTime:           dateTimeFromAPI(game.CreateDateTime),
		StartDateTime:            dateTimeFromAPI(game.StartDateTime),
		FinishDateTime:           dateTimeFromAPI(game.FinishDateTime),
		RequestLastDate:          dateTimeFromAPI(game.RequestLastDate),
		AcceptRateFromDateTime:   dateTimeFromAPI(game.AcceptRateFromDateTime),
		Title:                    game.Title,
		Descr:                    game.Descr,
		GameTypeID:               game.GameTypeID,
		ZoneId:                   game.ZoneID,
		LevelsSequence:           game.LevelsSequenceID,
		LevelsSequenceId:         game.LevelsSequenceID,
		ScenarioAvailability:     game.ScenarioAvailability,
		MaxPlayers:               game.MaxPlayers,
		MaxTeamMembers:           game.MaxTeamMembers,
		ShowInCalendar:           game.ShowInCalendar,
		FeeType:                  game.FeeTypeID,
		FeeCurrencyId:            game.FeeCurrencyID,
		FeeName:                  game.FeeName,
		ShowFee:                  game.ShowFee,
		Fee:                      moneyFromAPI(game.Fee),
		Prize:                    moneyFromAPI(game.Prize),
		PrizeType:                game.PrizeType,
		Started:                  game.Started,
		Finished:                 game.Finished,
		InProgress:               game.Started && !game.Finished,
		IsModerated:              game.IsModerated,
		QualityRate:              intFromNumber(game.QualityRate),
		QualityRateCalculated:    game.QualityRateCalculated,
		AuthorIndexCalculated:    game.AuthorIndexCalculated,
		AFC:                      game.AFC,
		TopicId:                  game.TopicID,
		HideLevelsNames:          game.HideLevelsNames,
		HidePlayersList:          game.HidePlayersList,
		HideGameDescr:            game.HideGameDescr,
		ReplaceNlToBr:            game.ReplaceNlToBr,
		PublicAccess:             game.PublicAccess,
		RateClosed:               game.RateClosed,
		AllowMakeStakes:          game.AllowMakeStakes,
		DisplayMonitoring:        game.DisplayMonitoring,
		DisplayAnnouncement:      game.DisplayAnnouncement,
		CertificatePlaces:        game.CertificatePlaces,
		CertificateAccessMode:    game.CertificateAccessMode,
		ShowFinishPlace:          game.ShowFinishPlace,
		StatusId:                 game.StatusID,
		IsAvailableAfterFinished: game.IsAvailableAfterFinished,
		StatAvailabilityTypeID:   game.StatAvailabilityTypeID,
		ForUserID:                game.ForUserID,
	}
	info.TSRemain = remainingUntil(info.StartDateTime)
	return info
}

// remainingUntil rebuilds the countdown the legacy engine computed server-side.
func remainingUntil(start *DateTime) *Duration {
	if start == nil {
		return nil
	}
	remain := time.Unix(start.Timestamp, 0).Sub(timeNow())
	if remain <= 0 {
		return nil
	}
	return &Duration{
		Days:         int(remain.Hours()) / 24,
		Hours:        int(remain.Hours()) % 24,
		Minutes:      int(remain.Minutes()) % 60,
		Seconds:      int(remain.Seconds()) % 60,
		TotalDays:    remain.Hours() / 24,
		TotalHours:   remain.Hours(),
		TotalMinutes: remain.Minutes(),
		TotalSeconds: remain.Seconds(),
	}
}

func moneyFromAPI(value int) *Money {
	if value == 0 {
		return nil
	}
	return &Money{Value: value, Cents: value * 100, Formated: strconv.Itoa(value)}
}

func intFromNumber(value json.Number) int {
	if value == "" {
		return 0
	}
	parsed, err := value.Float64()
	if err != nil {
		return 0
	}
	return int(parsed)
}

// apiTimeLayouts covers the formats the new backend emits for timestamps.
var apiTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func dateTimeFromAPI(value string) *DateTime {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	for _, layout := range apiTimeLayouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return dateTimeFromUnix(parsed.Unix())
		}
	}
	return nil
}

// timeNow is a seam so tests can pin the clock the catalog mapping reads.
var timeNow = time.Now
