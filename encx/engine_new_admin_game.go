package encx

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/skrashevich/encx-cli/encx/enapi"
)

// --- Game editor ---

// AdminCreateGame creates a game and returns its id. Title and StartDateTime
// are checked here, the same fields legacyAdminCreateGame refuses before
// sending the request, so both engines fail the same way instead of one
// spending a round trip on the server's generic "Invalid request body".
func (e *newEngine) AdminCreateGame(ctx context.Context, params AdminCreateGameParams) (int, error) {
	if strings.TrimSpace(params.Title) == "" {
		return 0, fmt.Errorf("encx: admin create game: title is required")
	}
	if strings.TrimSpace(params.StartDateTime) == "" {
		return 0, fmt.Errorf("encx: admin create game: start date is required")
	}

	req := enapi.AdminCreateGameRequest{
		Title:           params.Title,
		Descr:           params.Description,
		GameTypeID:      params.GameType,
		StartDateTime:   params.StartDateTime,
		FinishDateTime:  params.FinishDateTime,
		RequestLastDate: params.RequestLastDate,
		ZoneID:          params.ZoneID,
		IsModerated:     params.IsModerated,
	}
	for _, login := range authorLogins(params.Authors) {
		req.Authors = append(req.Authors, enapi.AdminCreateGameAuthor{Login: login})
	}

	// The route's answer is untyped in the API document, so the id is dug out of
	// whatever shape comes back rather than decoded into a struct.
	var created map[string]any
	if err := e.c.api().PostJSON(ctx, "/admin/games", req, &created); err != nil {
		return 0, err
	}
	id := gameIDFromCreationPayload(created)
	if id == 0 {
		return 0, fmt.Errorf("encx: создание игры: ответ движка не содержит идентификатора")
	}
	return id, nil
}

// gameIDFromCreationPayload digs the new game's id out of the untyped answer.
//
// The API document declares the answer of POST /admin/games an open object.
// Measured on a live domain it is flat and keyed game_id, alongside game_num,
// status_id and zone_id; id is accepted too in case the route ever spells it the
// way the rest of the API does.
func gameIDFromCreationPayload(payload map[string]any) int {
	for _, key := range []string{"game_id", "id"} {
		if value, ok := payload[key].(float64); ok && value != 0 {
			return int(value)
		}
	}
	return 0
}

// AdminDeleteGame removes the game through the same route the editor deletes
// with. The engine answers with an empty body, so nothing is decoded.
func (e *newEngine) AdminDeleteGame(ctx context.Context, gameId int) error {
	return e.c.api().Delete(ctx, fmt.Sprintf("/admin/games/%d", gameId), nil, nil)
}

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
		StartDateTime:            startDateTimeOrEmpty(game),
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
		AuthorComplexity:         formatFloatOrEmpty(game.AFC * afcToLegacyScale),
	}, nil
}

// startDateTimeOrEmpty hides the start of a game that has already begun, the
// way the legacy editor disables the field there. Both engines refuse to move a
// start that has passed, so handing it back would only make a read-modify-write
// fail on a value the caller never touched.
func startDateTimeOrEmpty(game *enapi.Game) string {
	if game.Started {
		return ""
	}
	return game.StartDateTime
}

// afcToLegacyScale converts between the two spellings of the author complexity.
//
// Measured on a live domain: the route accepts afc in 0..1 inclusive and stores
// it in steps of a tenth, truncating the rest (0.25 becomes 0.2, 0.99 becomes
// 0.9). The field therefore has exactly eleven states — the same eleven the
// legacy editor's ddlAuthorsCompexity offers as whole numbers 0..10, with 10 for
// its default. The specification agrees, noting "SP stores *10". Passing the
// value through unchanged would make a legacy-shaped 10 a validation error and a
// REST-shaped 1 mean a tenth of what it does on the other engine.
const afcToLegacyScale = 10

// afcLegacyMax is the largest author complexity the legacy spelling can carry;
// it is the scale only because the field happens to run from zero to one.
const afcLegacyMax = 10

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
	// A date in the legacy editor's spelling is a wall-clock time with no zone,
	// while this route stores an instant: converting one into the other here
	// would move the game by the domain's offset. The caller is told what to
	// write instead of reading the route's bare "Validation failed".
	var dateErr error
	setDateTime := func(key, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, err := time.Parse(legacyCreateDateLayout, value); err == nil {
			if dateErr == nil {
				dateErr = fmt.Errorf(
					"encx: дата %q записана в формате старой админки; новый движок "+
						"принимает RFC3339, например 2026-09-10T18:00:00+03:00", value)
			}
			return
		}
		update[key] = value
	}

	setString("title", info.Title)
	setString("descr", info.Description)
	setDateTime("start_date_time", info.StartDateTime)
	setDateTime("finish_date_time", info.FinishDateTime)
	setDateTime("request_last_date", info.RequestLastDate)
	setDateTime("accept_rate_from_date_time", info.AcceptRateFrom)
	// prize_cents is named for the column it feeds, not for a different unit:
	// writing prize_cents=1234 makes models.Game.prize read back as 1234. The
	// value therefore travels exactly as AdminGetGameInfo handed it over, the way
	// the legacy form passed its Prize field through. Scaling it here used to turn
	// a plain read-modify-write into a hundredfold raise, and past the server's
	// limit into an outright HTTP 400.
	setInt("prize_cents", info.Prize)
	setInt("stat_availability_type_id", info.GameStatAvailability)
	setInt("scenario_availability", info.GameScenarioAvailability)
	setInt("max_players", info.MaxPlayers)
	setInt("max_team_members", info.MaxTeamPlayers)
	setInt("show_fee", info.ShowFee)
	setInt("certificate_access_mode", info.CertificateMode)
	setInt("certificate_places", info.FirstPlaces)
	if afc, err := strconv.ParseFloat(strings.TrimSpace(info.AuthorComplexity), 64); err == nil {
		if afc < 0 || afc > afcLegacyMax {
			return fmt.Errorf(
				"encx: авторская сложность %q вне диапазона: новый движок принимает 0..%d",
				info.AuthorComplexity, afcLegacyMax)
		}
		update["afc"] = afc / afcToLegacyScale
	}
	if authors := splitLogins(info.Authors); len(authors) > 0 {
		update["authors"] = authors
	}
	// Booleans have no "unset" state in AdminGameInfo, so they always travel.
	update["is_moderated"] = info.IsModerated
	update["show_finish_place"] = info.ShowFinishPlace

	if dateErr != nil {
		return dateErr
	}
	return e.c.api().PatchJSON(ctx, fmt.Sprintf("/admin/games/%d", gameId), update, nil)
}

func splitLogins(value string) []map[string]any {
	logins := authorLogins(value)
	authors := make([]map[string]any, 0, len(logins))
	for _, login := range logins {
		authors = append(authors, map[string]any{"login": login})
	}
	return authors
}

func authorLogins(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' })
	logins := make([]string, 0, len(parts))
	for _, part := range parts {
		if login := strings.TrimSpace(part); login != "" {
			logins = append(logins, login)
		}
	}
	return logins
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
	// value_text is rendered server-side ("3 minutes" / "3 минуты"); the route
	// takes no lang parameter, so the language travels in Accept-Language, which
	// enapi.Client sets from the configured lang.
	err := e.c.api().GetJSON(ctx, path, nil, &resp)
	if err == nil {
		return &resp, nil
	}
	// The route answers 403 for a game that has not started — there are no
	// results to correct yet — where the legacy Corrections.aspx rendered an
	// empty table. Measured on demo.en.cx: corrections are readable for every
	// started game, including games this account does not own, and refused only
	// for an own game that has not begun.
	//
	// So the 403 is turned into an empty list only after the game editor
	// confirms this session administers the game. That route, unlike the
	// lifecycle one, really does refuse a game somebody else owns: lifecycle
	// answers 200 for any game and would have waved through a stranger's 403.
	// Both halves of the measured precondition are required: the game is ours
	// AND it has not started. A started game whose corrections suddenly 403 —
	// a withdrawn co-author role, a policy change, a bug — is a real refusal and
	// must not be flattened into an empty list.
	if !enapi.IsForbidden(err) {
		return nil, err
	}
	administers, started := e.administersGame(ctx, gameID)
	if !administers || started {
		return nil, err
	}
	return &enapi.GameCorrectionsResponse{GameID: gameID}, nil
}

// administersGame reports whether this session may open the game's admin editor,
// which is the API's own answer to "is this your game", and whether that game
// has started. The lifecycle route cannot answer the first question: it replies
// 200 for games the account does not own.
func (e *newEngine) administersGame(ctx context.Context, gameID int) (administers, started bool) {
	var editor enapi.AdminGameEditorResponse
	if err := e.c.api().GetJSON(ctx, fmt.Sprintf("/admin/games/%d", gameID), nil, &editor); err != nil {
		return false, false
	}
	if editor.Game == nil {
		return false, false
	}
	return true, editor.Game.Started
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

// monitorPageLimit bounds the walk over the action monitor.
const monitorPageLimit = 100

func (e *newEngine) AdminGetActionMonitor(ctx context.Context, gameId int) ([]AdminActionMonitorEntry, error) {
	path := fmt.Sprintf("/games/%d/monitoring", gameId)
	entries := make([]AdminActionMonitorEntry, 0)
	seen := make(map[int]bool)

	// The monitor is paged at 50 rows by default and AdminGetActionMonitor takes
	// no page argument, so every page is read: the legacy ActionMonitor page
	// handed over every row it rendered, and a silent 50-row prefix of a busy
	// game's log is a different answer, not a shorter one.
	for page := 1; page <= monitorPageLimit; page++ {
		var resp enapi.GameMonitoringResponse
		// tab is documented as a number: 0 is the answer monitor the legacy
		// ActionMonitor.aspx showed, 1 is the bonus tab.
		q := url.Values{"tab": {"0"}}
		if page > 1 {
			q.Set("page", strconv.Itoa(page))
		}
		if err := e.c.api().GetJSON(ctx, path, q, &resp); err != nil {
			return nil, err
		}
		if page == 1 && !resp.CanView && len(resp.Actions) == 0 {
			return nil, fmt.Errorf("encx: action monitor %d: просмотр монитора недоступен", gameId)
		}
		for _, action := range resp.Actions {
			// A shifting log can repeat a row across pages; the action id is
			// what makes the walk stable.
			if action.ActionID != 0 {
				if seen[action.ActionID] {
					continue
				}
				seen[action.ActionID] = true
			}
			entries = append(entries, AdminActionMonitorEntry{
				Number:      strconv.Itoa(action.LevelNumber),
				Participant: firstNonEmpty(action.TeamName, action.UserLogin),
				Direction:   monitorDirection(action),
				Answer:      action.Answer,
				DateTime:    action.AnswerDateTime,
				Sectors:     action.SectorsInfo,
			})
		}
		if len(resp.Actions) == 0 || page >= resp.TotalPages {
			return entries, nil
		}
	}
	return nil, fmt.Errorf(
		"encx: action monitor %d: движок отдаёт больше %d страниц — результат был бы неполным",
		gameId, monitorPageLimit)
}

// monitorDirection renders the verdict column the legacy monitor showed.
func monitorDirection(action enapi.MonitoringAction) string {
	if action.IsCorrect {
		return "+"
	}
	return "-"
}
