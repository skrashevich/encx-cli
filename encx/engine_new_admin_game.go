package encx

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/skrashevich/encx-cli/encx/enapi"
)

// --- Game editor ---

func (e *newEngine) AdminGetGameInfo(ctx context.Context, gameId int) (*AdminGameInfo, error) {
	var editor enapi.AdminGameEditorResponse
	if err := e.c.api().GetJSON(ctx, fmt.Sprintf("/admin/games/%d", gameId), nil, &editor); err != nil {
		return nil, err
	}
	if editor.Game == nil {
		return nil, fmt.Errorf("encx: admin get game info: game %d is missing from the editor", gameId)
	}

	game := editor.Game
	logins := make([]string, 0, len(editor.Authors))
	for _, author := range editor.Authors {
		if author.Login != "" {
			logins = append(logins, author.Login)
		}
	}

	return &AdminGameInfo{
		Title:                    game.Title,
		Authors:                  strings.Join(logins, ","),
		Description:              game.Descr,
		Prize:                    itoaOrEmpty(game.Prize),
		FinishDateTime:           game.FinishDateTime,
		RequestLastDate:          game.RequestLastDate,
		IsModerated:              game.IsModerated,
		GameStatAvailability:     itoaOrEmpty(game.StatAvailabilityTypeID),
		GameScenarioAvailability: itoaOrEmpty(game.ScenarioAvailability),
		ShowFinishPlace:          game.ShowFinishPlace,
		MaxPlayers:               itoaOrEmpty(game.MaxPlayers),
		MaxTeamPlayers:           itoaOrEmpty(game.MaxTeamMembers),
		ShowFee:                  itoaOrEmpty(game.ShowFee),
		CertificateMode:          itoaOrEmpty(game.CertificateAccessMode),
		FirstPlaces:              itoaOrEmpty(game.CertificatePlaces),
		AcceptRateFrom:           game.AcceptRateFromDateTime,
		AuthorComplexity:         formatFloatOrEmpty(game.AFC),
	}, nil
}

// AdminUpdateGameInfo sends only the fields the caller filled in.
//
// AdminGameInfo carries strings because the legacy editor was an HTML form, so
// an empty field means "leave it alone"; the REST route is a partial update and
// honours exactly that.
func (e *newEngine) AdminUpdateGameInfo(ctx context.Context, gameId int, info AdminGameInfo) error {
	update := map[string]any{}
	setString := func(key, value string) {
		if strings.TrimSpace(value) != "" {
			update[key] = value
		}
	}
	setInt := func(key, value string) {
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
			update[key] = parsed
		}
	}

	setString("title", info.Title)
	setString("descr", info.Description)
	setString("finish_date_time", info.FinishDateTime)
	setString("request_last_date", info.RequestLastDate)
	setString("accept_rate_from_date_time", info.AcceptRateFrom)
	// models.Game.prize is in whole currency units, the update request takes
	// cents (its field is named prize_cents and documented as Fee.Cents), so the
	// value AdminGetGameInfo handed the caller has to be scaled back.
	if prize, err := strconv.Atoi(strings.TrimSpace(info.Prize)); err == nil {
		update["prize_cents"] = prize * 100
	}
	setInt("stat_availability_type_id", info.GameStatAvailability)
	setInt("scenario_availability", info.GameScenarioAvailability)
	setInt("max_players", info.MaxPlayers)
	setInt("max_team_members", info.MaxTeamPlayers)
	setInt("show_fee", info.ShowFee)
	setInt("certificate_access_mode", info.CertificateMode)
	setInt("certificate_places", info.FirstPlaces)
	if afc, err := strconv.ParseFloat(strings.TrimSpace(info.AuthorComplexity), 64); err == nil {
		update["afc"] = afc
	}
	if authors := splitLogins(info.Authors); len(authors) > 0 {
		update["authors"] = authors
	}
	// Booleans have no "unset" state in AdminGameInfo, so they always travel.
	update["is_moderated"] = info.IsModerated
	update["show_finish_place"] = info.ShowFinishPlace

	return e.c.api().PatchJSON(ctx, fmt.Sprintf("/admin/games/%d", gameId), update, nil)
}

func splitLogins(value string) []map[string]any {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' })
	authors := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		if login := strings.TrimSpace(part); login != "" {
			authors = append(authors, map[string]any{"login": login})
		}
	}
	return authors
}

func itoaOrEmpty(value int) string {
	if value == 0 {
		return ""
	}
	return strconv.Itoa(value)
}

func formatFloatOrEmpty(value float64) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatFloat(value, 'f', -1, 64)
}

// --- Lifecycle ---

// The new engine changes a game's lifecycle through PUT /admin/games/{id}/status
// with a numeric status_id. The API document does not publish the values and the
// operation is irreversible ("признание необратимо"), so the two calls that need
// them refuse rather than guess. Set these to the codes the backend expects to
// enable them.
var (
	// GameStatusDelivered marks a game as состоявшаяся. Zero means unknown.
	GameStatusDelivered = 0
	// GameStatusCancelled marks a game as несостоявшаяся. Zero means unknown.
	GameStatusCancelled = 0
)

func (e *newEngine) AdminDeliverGame(ctx context.Context, gameId int) error {
	return e.setGameStatus(ctx, gameId, GameStatusDelivered, "признание игры состоявшейся")
}

func (e *newEngine) AdminNotDeliverGame(ctx context.Context, gameId int) error {
	return e.setGameStatus(ctx, gameId, GameStatusCancelled, "признание игры несостоявшейся")
}

func (e *newEngine) setGameStatus(ctx context.Context, gameID, statusID int, operation string) error {
	if statusID == 0 {
		return fmt.Errorf(
			"encx: %s на новом движке не выполнено: код status_id не задан "+
				"(значения не описаны в API; задайте encx.GameStatusDelivered/GameStatusCancelled)",
			operation)
	}
	body := enapi.AdminGameStatusRequest{StatusID: statusID}
	return e.c.api().PutJSON(ctx, fmt.Sprintf("/admin/games/%d/status", gameID), body, nil)
}

func (e *newEngine) AdminAwardPoints(ctx context.Context, gameId int) error {
	return e.c.api().PostJSON(ctx, fmt.Sprintf("/admin/games/%d/points/calculate", gameId), nil, nil)
}

func (e *newEngine) AdminEndRatings(ctx context.Context, gameId int) error {
	return e.c.api().PostJSON(ctx, fmt.Sprintf("/admin/games/%d/rate/close", gameId), nil, nil)
}

func (e *newEngine) AdminCalculateIK(ctx context.Context, gameId int) error {
	return e.c.api().PostJSON(ctx, fmt.Sprintf("/admin/games/%d/quality-index", gameId), nil, nil)
}

// --- Corrections ---

func (e *newEngine) AdminGetCorrections(ctx context.Context, gameId int) ([]AdminCorrection, error) {
	resp, err := e.corrections(ctx, gameId)
	if err != nil {
		return nil, err
	}
	corrections := make([]AdminCorrection, 0, len(resp.Items))
	for _, item := range resp.Items {
		corrections = append(corrections, AdminCorrection{
			ID:       strconv.Itoa(item.CorrectID),
			DateTime: item.CorrectDateTime,
			Team:     firstNonEmpty(item.TeamName, item.Login),
			Level:    correctionLevelLabel(item),
			Reason:   item.CorrectText,
			Time:     item.ValueText,
			Comment:  item.Comment,
		})
	}
	return corrections, nil
}

// correctionLevelLabel renders the level column: level 0 is the whole game,
// which the legacy page showed as an empty cell.
func correctionLevelLabel(item enapi.GameCorrection) string {
	if item.LevelNum <= 0 {
		return ""
	}
	return strconv.Itoa(item.LevelNum)
}

func (e *newEngine) corrections(ctx context.Context, gameID int) (*enapi.GameCorrectionsResponse, error) {
	var resp enapi.GameCorrectionsResponse
	path := fmt.Sprintf("/games/%d/corrections", gameID)
	if err := e.c.api().GetJSON(ctx, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (e *newEngine) AdminAddCorrection(ctx context.Context, gameId int, corr AdminCorrectionAdd) error {
	// The legacy form named the team and level; the REST route wants IDs, so
	// the same dropdown options the page rendered are used to resolve them.
	resp, err := e.corrections(ctx, gameId)
	if err != nil {
		return err
	}
	player, err := findCorrectionPlayer(resp.Players, corr.TeamName)
	if err != nil {
		return err
	}
	levelID, err := findCorrectionLevel(resp.Levels, corr.LevelName)
	if err != nil {
		return err
	}

	seconds := correctionSeconds(corr)
	body := enapi.GameCorrectionWriteRequest{
		PlayerID:     player.ID,
		GamePlayerID: player.GamePlayerID,
		LevelID:      levelID,
		IsBonus:      strings.TrimSpace(corr.CorrectionType) != "2",
		Seconds:      seconds,
		Comment:      corr.Comment,
	}
	return e.c.api().PostJSON(ctx, fmt.Sprintf("/games/%d/corrections", gameId), body, nil)
}

func correctionSeconds(corr AdminCorrectionAdd) int {
	value := atoiOrZero(corr.Days)*86400 +
		atoiOrZero(corr.Hours)*3600 +
		atoiOrZero(corr.Minutes)*60 +
		atoiOrZero(corr.Seconds)
	return value
}

func findCorrectionPlayer(players []enapi.CorrectionPlayerOption, name string) (enapi.CorrectionPlayerOption, error) {
	wanted := strings.TrimSpace(name)
	for _, player := range players {
		if strings.EqualFold(strings.TrimSpace(player.Label), wanted) {
			return player, nil
		}
	}
	return enapi.CorrectionPlayerOption{}, fmt.Errorf("encx: correction: participant %q is not in the game", name)
}

func findCorrectionLevel(levels []enapi.CorrectionLevelOption, name string) (int, error) {
	wanted := strings.TrimSpace(name)
	// "0" is the legacy marker for "all levels".
	if wanted == "" || wanted == "0" {
		return 0, nil
	}
	for _, level := range levels {
		if strings.EqualFold(strings.TrimSpace(level.Name), wanted) ||
			strconv.Itoa(level.LevelNum) == wanted {
			return level.LevelID, nil
		}
	}
	return 0, fmt.Errorf("encx: correction: level %q is not in the game", name)
}

func (e *newEngine) AdminDeleteCorrection(ctx context.Context, gameId int, correctionId string) error {
	id := strings.TrimSpace(correctionId)
	if id == "" {
		return fmt.Errorf("encx: correction id is empty")
	}
	path := fmt.Sprintf("/games/%d/corrections/%s", gameId, url.PathEscape(id))
	return e.c.api().Delete(ctx, path, nil, nil)
}

// --- Teams and monitoring ---

// AdminGetTeams returns the ForMember options of a level, which is what the
// legacy TaskEdit dropdown listed.
func (e *newEngine) AdminGetTeams(ctx context.Context, gameId, levelNum int) ([]AdminTeam, error) {
	editor, err := e.levelEditor(ctx, gameId, levelNum)
	if err != nil {
		return nil, err
	}
	teams := make([]AdminTeam, 0, len(editor.Members))
	for _, member := range editor.Members {
		teams = append(teams, AdminTeam{ID: strconv.Itoa(member.ID), Name: member.Label})
	}
	return teams, nil
}

func (e *newEngine) AdminGetActionMonitor(ctx context.Context, gameId int) ([]AdminActionMonitorEntry, error) {
	var resp enapi.GameMonitoringResponse
	q := url.Values{"tab": {"main"}}
	if err := e.c.api().GetJSON(ctx, fmt.Sprintf("/games/%d/monitoring", gameId), q, &resp); err != nil {
		return nil, err
	}
	entries := make([]AdminActionMonitorEntry, 0, len(resp.Actions))
	for _, action := range resp.Actions {
		entries = append(entries, AdminActionMonitorEntry{
			Number:      strconv.Itoa(action.LevelNumber),
			Participant: firstNonEmpty(action.TeamName, action.UserLogin),
			Direction:   monitorDirection(action),
			Answer:      action.Answer,
			DateTime:    action.AnswerDateTime,
			Sectors:     action.SectorsInfo,
		})
	}
	return entries, nil
}

// monitorDirection renders the verdict column the legacy monitor showed.
func monitorDirection(action enapi.MonitoringAction) string {
	if action.IsCorrect {
		return "+"
	}
	return "-"
}
