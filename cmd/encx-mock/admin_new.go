package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// The new engine's admin surface. Every response shape here was taken from a
// live capture against demo.en.cx (see encx/zz_capture_test.go in the working
// tree of the audit that produced it), not from the specification: the swagger
// snapshot has been wrong about this API more than once, and a mock written from
// it would agree with the client's mistakes instead of catching them.

const (
	mockAdminLogin  = "svk"
	mockAdminUserID = mockAPIUserID
	mockAdminStart  = "2026-09-02T03:40:06Z"
	mockAdminFinish = "2026-09-03T03:40:06Z"
)

// registerAdminAPIRoutes adds the /admin/** surface plus the two game routes the
// admin client reads.
// The whole /admin/** surface needs a session. Anonymous calls to
// /admin/games, /admin/games/{id}, /admin/games/{id}/levels and
// /admin/games/{id}/lifecycle all answer
// 401 {"error":"unauthorized","message":"Authorization required","code":401}
// on api.en.cx (X-En-Domain: demo.en.cx, 2026-09-01). The guard is applied at
// registration so no handler can be added to the group without it.
//
// The two game routes below are not part of that group and are not symmetric:
// /games/{id}/monitoring is guarded like the admin surface, /games/{id}/
// corrections answers 200 to anyone. Both measured the same day.
func (s *server) registerAdminAPIRoutes(root *http.ServeMux) {
	public := tolerantMux{root}
	mux := guardedMux{mux: public, guard: s.requireAPISession}
	mux.HandleFunc("GET /admin/games", s.handleAdminGames)
	mux.HandleFunc("GET /admin/games/{id}", s.handleAdminGameEditor)
	mux.HandleFunc("PATCH /admin/games/{id}", s.handleAdminGamePatch)
	mux.HandleFunc("PUT /admin/games/{id}/status", s.handleAdminGameStatus)
	mux.HandleFunc("GET /admin/games/{id}/lifecycle", s.handleAdminLifecycle)
	mux.HandleFunc("POST /admin/games/{id}/points/calculate", s.handleAdminLifecycleAction("points"))
	mux.HandleFunc("POST /admin/games/{id}/rate/close", s.handleAdminLifecycleAction("rate"))
	mux.HandleFunc("POST /admin/games/{id}/quality-index", s.handleAdminLifecycleAction("qi"))

	mux.HandleFunc("GET /admin/games/{id}/levels", s.handleAdminLevels)
	mux.HandleFunc("POST /admin/games/{id}/levels", s.handleAdminCreateLevel)
	mux.HandleFunc("POST /admin/games/{id}/levels/exchange", s.handleAdminExchangeLevels)
	mux.HandleFunc("POST /admin/games/{id}/levels/put", s.handleAdminPutLevel)
	mux.HandleFunc("POST /admin/games/{id}/levels/copy", s.handleAdminCopyLevels)
	mux.HandleFunc("DELETE /admin/games/{id}/levels/{levelId}", s.handleAdminDeleteLevel)
	mux.HandleFunc("GET /admin/games/{id}/levels/{levelId}/editor", s.handleAdminLevelEditor)
	mux.HandleFunc("PUT /admin/games/{id}/levels/{levelId}/meta", s.handleAdminLevelMeta)
	mux.HandleFunc("PUT /admin/games/{id}/levels/{levelId}/autopass", s.handleAdminAutopass)
	mux.HandleFunc("PUT /admin/games/{id}/levels/{levelId}/settings", s.handleAdminLevelSettings)

	mux.HandleFunc("POST /admin/games/{id}/levels/{levelId}/tasks", s.handleAdminCreateTask)
	mux.HandleFunc("PUT /admin/games/{id}/levels/{levelId}/tasks/{taskId}", s.handleAdminUpdateTask)
	mux.HandleFunc("DELETE /admin/games/{id}/levels/{levelId}/tasks/{taskId}", s.handleAdminDeleteTask)

	mux.HandleFunc("POST /admin/games/{id}/levels/{levelId}/helps", s.handleAdminCreateHelp)
	mux.HandleFunc("PUT /admin/games/{id}/levels/{levelId}/helps/{helpId}", s.handleAdminUpdateHelp)
	mux.HandleFunc("DELETE /admin/games/{id}/levels/{levelId}/helps/{helpId}", s.handleAdminDeleteHelp)

	mux.HandleFunc("POST /admin/games/{id}/levels/{levelId}/bonuses", s.handleAdminCreateBonus)
	mux.HandleFunc("PUT /admin/games/{id}/levels/{levelId}/bonuses/{bonusId}", s.handleAdminUpdateBonus)
	mux.HandleFunc("DELETE /admin/games/{id}/levels/{levelId}/bonuses/{bonusId}", s.handleAdminDeleteBonus)

	mux.HandleFunc("POST /admin/games/{id}/levels/{levelId}/messages", s.handleAdminCreateMessage)
	mux.HandleFunc("PUT /admin/games/{id}/levels/{levelId}/messages/{messageId}", s.handleAdminUpdateMessage)
	mux.HandleFunc("DELETE /admin/games/{id}/levels/{levelId}/messages/{messageId}", s.handleAdminDeleteMessage)

	mux.HandleFunc("POST /admin/games/{id}/levels/{levelId}/sectors", s.handleAdminCreateSector)
	mux.HandleFunc("PUT /admin/games/{id}/levels/{levelId}/sectors/{sectorId}", s.handleAdminUpdateSector)
	mux.HandleFunc("DELETE /admin/games/{id}/levels/{levelId}/sectors/{sectorId}", s.handleAdminDeleteSector)

	mux.HandleFunc("POST /admin/games/{id}/levels/{levelId}/answers/batch", s.handleAdminAnswersBatch)
	mux.HandleFunc("DELETE /admin/games/{id}/levels/{levelId}/answers/{answerId}", s.handleAdminDeleteAnswer)

	mux.HandleFunc("GET /games/{id}/monitoring", s.handleAdminMonitoring)
	public.HandleFunc("GET /games/{id}/corrections", s.handleAdminCorrections)
}

// --- helpers ---

// adminPathInt reads a path value and reports whether it was a number, which
// the game and level lookups need in order to answer 404 rather than treat a
// malformed id as zero.
func adminPathInt(r *http.Request, name string) (int, bool) {
	value, err := strconv.Atoi(r.PathValue(name))
	return value, err == nil
}

// adminGameFrom resolves the game in the path, which the mock serves for one id.
func (s *server) adminGameFrom(w http.ResponseWriter, r *http.Request) *adminState {
	id, ok := adminPathInt(r, "id")
	if !ok || id != s.admin.gameID {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "Forbidden"})
		return nil
	}
	return s.admin
}

// adminLevelFrom resolves the level in the path.
func (s *server) adminLevelFrom(w http.ResponseWriter, r *http.Request) (*adminState, *adminLevel) {
	st := s.adminGameFrom(w, r)
	if st == nil {
		return nil, nil
	}
	levelID, ok := adminPathInt(r, "levelId")
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "level not found"})
		return nil, nil
	}
	level, _ := st.levelByID(levelID)
	if level == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "level not found"})
		return nil, nil
	}
	return st, level
}

func decodeJSONBody(r *http.Request, out any) error {
	defer func() { _ = r.Body.Close() }()
	return json.NewDecoder(r.Body).Decode(out)
}

func noContent(w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) }

// --- games ---

func (s *server) handleAdminGames(w http.ResponseWriter, r *http.Request) {
	st := s.admin
	st.mu.Lock()
	defer st.mu.Unlock()

	// The listing is paged. One page is enough for the mock's single game, but
	// the pager fields are present because the client walks them.
	writeJSON(w, http.StatusOK, map[string]any{
		"items": []any{map[string]any{
			"game_id":          st.gameID,
			"game_num":         st.gameNum,
			"title":            st.title,
			"zone_id":          0,
			"game_type_id":     1,
			"status_id":        0,
			"start_date_time":  st.startDateTime,
			"finish_date_time": st.finishDateTime,
			"fee_type":         "Cash",
			"owner_id":         mockAdminUserID,
			"owner_login":      mockAdminLogin,
			"referee_end":      false,
		}},
		"total_count": 1,
		"total_pages": 1,
		"page":        1,
		"only_own":    true,
	})
}

func (s *server) adminGameObject(st *adminState) map[string]any {
	return map[string]any{
		"id":                          st.gameID,
		"game_num":                    st.gameNum,
		"site_id":                     135,
		"lang_id":                     21,
		"owner_id":                    mockAdminUserID,
		"game_type_id":                1,
		"zone_id":                     0,
		"status_id":                   0,
		"title":                       st.title,
		"descr":                       st.descr,
		"start_date_time":             st.startDateTime,
		"finish_date_time":            st.finishDateTime,
		"request_last_date":           st.requestLastDate,
		"accept_rate_from_date_time":  st.acceptRateFrom,
		"price":                       0,
		"prize":                       st.prize,
		"fee":                         0,
		"fee_type":                    "Cash",
		"fee_type_id":                 1,
		"fee_currency_id":             1,
		"fee_name":                    "",
		"prize_type":                  1,
		"show_fee":                    st.showFee,
		"max_players":                 st.maxPlayers,
		"max_team_members":            st.maxTeamMembers,
		"is_moderated":                st.isModerated,
		"show_finish_place":           st.showFinishPlace,
		"stat_availability_type_id":   st.statAvailability,
		"scenario_availability":       st.scenarioAvail,
		"certificate_access_mode":     st.certificateMode,
		"certificate_places":          st.certificatePlaces,
		"afc":                         st.afc,
		"levels_sequence_id":          0,
		"started":                     st.started,
		"finished":                    false,
		"rate_closed":                 false,
		"referee_end":                 false,
		"replace_nl_to_br":            false,
		"hide_game_descr":             false,
		"hide_levels_names":           false,
		"hide_players_list":           false,
		"public_access":               false,
		"show_in_calendar":            false,
		"allow_make_stakes":           true,
		"quality_rate_calculated":     false,
		"author_index_calculated":     false,
		"is_available_after_finished": false,
		"display_monitoring":          0,
		"display_announcement":        0,
		"competition_id":              -1,
		"topic_id":                    0,
	}
}

func (s *server) handleAdminGameEditor(w http.ResponseWriter, r *http.Request) {
	st := s.adminGameFrom(w, r)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()

	authors := make([]any, 0, len(st.authors))
	for _, login := range st.authors {
		authors = append(authors, map[string]any{"user_id": mockAdminUserID, "login": login})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"game":    s.adminGameObject(st),
		"authors": authors,
		"fee_currencies": []any{
			map[string]any{"currency_id": 1, "currency_name": "en usd"},
		},
	})
}

// handleAdminGamePatch applies a partial update, the way PATCH /admin/games/{id}
// does: a field that is absent is left alone.
func (s *server) handleAdminGamePatch(w http.ResponseWriter, r *http.Request) {
	st := s.adminGameFrom(w, r)
	if st == nil {
		return
	}
	var patch map[string]any
	if err := decodeJSONBody(r, &patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Validation failed"})
		return
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	// afc is accepted in 0..1 and stored to a tenth, truncating the rest — the
	// behaviour measured on the deployed engine, and the reason the client
	// scales the legacy 0..10 spelling by ten.
	if raw, ok := patch["afc"]; ok {
		value, isNumber := raw.(float64)
		if !isNumber || value < 0 || value > 1 {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Validation failed"})
			return
		}
		st.afc = float64(int(value*10)) / 10
	}
	setString := func(key string, dst *string) {
		if raw, ok := patch[key].(string); ok {
			*dst = raw
		}
	}
	setInt := func(key string, dst *int) {
		if raw, ok := patch[key].(float64); ok {
			*dst = int(raw)
		}
	}
	setBool := func(key string, dst *bool) {
		if raw, ok := patch[key].(bool); ok {
			*dst = raw
		}
	}
	setString("title", &st.title)
	setString("descr", &st.descr)
	setString("start_date_time", &st.startDateTime)
	setString("finish_date_time", &st.finishDateTime)
	setString("request_last_date", &st.requestLastDate)
	setString("accept_rate_from_date_time", &st.acceptRateFrom)
	// prize_cents is named for the column it feeds, not a different unit: what
	// goes in comes back out of models.Game.prize unchanged.
	setInt("prize_cents", &st.prize)
	setInt("max_players", &st.maxPlayers)
	setInt("max_team_members", &st.maxTeamMembers)
	setInt("show_fee", &st.showFee)
	setInt("certificate_access_mode", &st.certificateMode)
	setInt("certificate_places", &st.certificatePlaces)
	setInt("stat_availability_type_id", &st.statAvailability)
	setInt("scenario_availability", &st.scenarioAvail)
	setBool("is_moderated", &st.isModerated)
	setBool("show_finish_place", &st.showFinishPlace)
	if raw, ok := patch["authors"].([]any); ok {
		logins := make([]string, 0, len(raw))
		for _, item := range raw {
			if entry, ok := item.(map[string]any); ok {
				if login, ok := entry["login"].(string); ok && login != "" {
					logins = append(logins, login)
				}
			}
		}
		if len(logins) > 0 {
			st.authors = logins
		}
	}
	noContent(w)
}

func (s *server) handleAdminGameStatus(w http.ResponseWriter, r *http.Request) {
	if st := s.adminGameFrom(w, r); st == nil {
		return
	}
	var body struct {
		StatusID int `json:"status_id"`
	}
	if err := decodeJSONBody(r, &body); err != nil || body.StatusID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Validation failed"})
		return
	}
	noContent(w)
}

func (s *server) handleAdminLifecycle(w http.ResponseWriter, r *http.Request) {
	st := s.adminGameFrom(w, r)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()

	// A game that has not started has nothing to award or rate. The show_* flags
	// are true for anyone the route answers, which is why they are no use as an
	// ownership test — the client learned that the hard way.
	writeJSON(w, http.StatusOK, map[string]any{
		"game_id": st.gameID, "game_num": st.gameNum, "title": st.title,
		"zone_id": 0, "game_type_id": 1, "status_id": 0,
		"started": st.started, "finished": false, "rate_closed": false,
		"points_calculated": false, "quality_rate_calculated": false,
		"can_deliver": false, "can_cancel": false, "can_correct_results": st.started,
		"can_calculate_points": st.started, "can_cancel_points": false,
		"can_close_rate": st.started, "can_open_rate": false,
		"can_calculate_qi": st.started, "can_cancel_qi": false,
		"show_status_row": true, "show_manage_row": true, "show_rate_actions": true,
		"show_referee_toggle": false, "can_toggle_referee": false, "referee_end": false,
		"electronic_fee": true, "electronic_fee_funds": false,
	})
}

// handleAdminLifecycleAction refuses what the lifecycle says is unavailable,
// which is what the deployed engine does ("calculate points not allowed").
func (s *server) handleAdminLifecycleAction(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := s.adminGameFrom(w, r)
		if st == nil {
			return
		}
		st.mu.Lock()
		started := st.started
		st.mu.Unlock()
		if !started {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": kind + " not allowed"})
			return
		}
		noContent(w)
	}
}

// --- levels ---

func (s *server) levelItems(st *adminState) []any {
	items := make([]any, 0, len(st.levels))
	for i, level := range st.levels {
		items = append(items, map[string]any{
			"level_id":     level.id,
			"level_number": i + 1,
			"level_name":   level.name,
			"comment":      level.comment,
			"dismissed":    false,
			"owner_id":     mockAdminUserID,
		})
	}
	return items
}

func (s *server) handleAdminLevels(w http.ResponseWriter, r *http.Request) {
	st := s.adminGameFrom(w, r)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"game_id": st.gameID, "game_num": st.gameNum, "title": st.title,
		"zone_id": 0, "game_type_id": 1, "status_id": 0, "started": st.started,
		"levels_sequence_id": 0, "show_passing_sequence": true,
		"can_change_levels_sequence": !st.started,
		// Measured on the deployed engine: this stays true for a started game.
		"can_manipulate_levels": true,
		"levels":                s.levelItems(st),
	})
}

func (s *server) handleAdminCreateLevel(w http.ResponseWriter, r *http.Request) {
	st := s.adminGameFrom(w, r)
	if st == nil {
		return
	}
	var body struct {
		LevelName string `json:"level_name"`
		Comment   string `json:"comment"`
	}
	_ = decodeJSONBody(r, &body)

	st.mu.Lock()
	defer st.mu.Unlock()
	level := st.newLevel()
	level.name = body.LevelName
	level.comment = body.Comment
	st.levels = append(st.levels, level)
	writeJSON(w, http.StatusOK, map[string]any{"level_id": level.id})
}

func (s *server) handleAdminDeleteLevel(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	st.deleteLevel(level.id)
	noContent(w)
}

func (s *server) handleAdminExchangeLevels(w http.ResponseWriter, r *http.Request) {
	st := s.adminGameFrom(w, r)
	if st == nil {
		return
	}
	var body struct {
		Level1ID int `json:"level1_id"`
		Level2ID int `json:"level2_id"`
	}
	_ = decodeJSONBody(r, &body)

	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.exchange(body.Level1ID, body.Level2ID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "level not found"})
		return
	}
	noContent(w)
}

func (s *server) handleAdminPutLevel(w http.ResponseWriter, r *http.Request) {
	st := s.adminGameFrom(w, r)
	if st == nil {
		return
	}
	var body struct {
		LevelID      int `json:"level_id"`
		AfterLevelID int `json:"after_level_id"`
	}
	_ = decodeJSONBody(r, &body)

	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.putAfter(body.LevelID, body.AfterLevelID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "level not found"})
		return
	}
	noContent(w)
}

func (s *server) handleAdminCopyLevels(w http.ResponseWriter, r *http.Request) {
	st := s.adminGameFrom(w, r)
	if st == nil {
		return
	}
	var body struct {
		FromLevelID int `json:"from_level_id"`
		Count       int `json:"count"`
	}
	_ = decodeJSONBody(r, &body)

	st.mu.Lock()
	defer st.mu.Unlock()
	source, _ := st.levelByID(body.FromLevelID)
	if source == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "level not found"})
		return
	}
	for i := 0; i < body.Count; i++ {
		clone := st.newLevel()
		clone.name = source.name
		clone.comment = source.comment
		clone.timeoutSec = source.timeoutSec
		clone.timeoutAwardSec = source.timeoutAwardSec
		clone.attemptsNumber = source.attemptsNumber
		clone.attemptsPeriodSec = source.attemptsPeriodSec
		clone.blockTypeID = source.blockTypeID
		clone.passingConditionID = source.passingConditionID
		clone.requiredSectorsCount = source.requiredSectorsCount
		st.levels = append(st.levels, clone)
	}
	noContent(w)
}

func (s *server) handleAdminLevelMeta(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	var body struct {
		LevelName string `json:"level_name"`
		Comment   string `json:"comment"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Validation failed"})
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	level.name = body.LevelName
	level.comment = body.Comment
	noContent(w)
}

func (s *server) handleAdminAutopass(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	var body struct {
		TimeoutHours   int  `json:"timeout_hours"`
		TimeoutMinutes int  `json:"timeout_minutes"`
		TimeoutSeconds int  `json:"timeout_seconds"`
		PenaltyEnabled bool `json:"penalty_enabled"`
		PenaltyHours   int  `json:"penalty_hours"`
		PenaltyMinutes int  `json:"penalty_minutes"`
		PenaltySeconds int  `json:"penalty_seconds"`
		AwardIsPenalty bool `json:"award_is_penalty"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Validation failed"})
		return
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	level.timeoutSec = (body.TimeoutHours*60+body.TimeoutMinutes)*60 + body.TimeoutSeconds
	award := (body.PenaltyHours*60+body.PenaltyMinutes)*60 + body.PenaltySeconds
	switch {
	case !body.PenaltyEnabled || award == 0:
		level.timeoutAwardSec = 0
	case body.AwardIsPenalty:
		// The award is stored signed: a penalty takes time away.
		level.timeoutAwardSec = -award
	default:
		level.timeoutAwardSec = award
	}
	noContent(w)
}

func (s *server) handleAdminLevelSettings(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	var body struct {
		Section               string `json:"section"`
		AttemptsNumber        int    `json:"attempts_number"`
		AttemptsPeriodHours   int    `json:"attempts_period_hours"`
		AttemptsPeriodMinutes int    `json:"attempts_period_minutes"`
		AttemptsPeriodSeconds int    `json:"attempts_period_seconds"`
		BlockTypeID           int    `json:"block_type_id"`
		PassingConditionID    int    `json:"passing_condition_id"`
		RequiredSectorsCount  int    `json:"required_sectors_count"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Validation failed"})
		return
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	switch body.Section {
	case "blocking":
		level.attemptsNumber = body.AttemptsNumber
		level.attemptsPeriodSec = (body.AttemptsPeriodHours*60+body.AttemptsPeriodMinutes)*60 +
			body.AttemptsPeriodSeconds
		if body.BlockTypeID != 0 {
			level.blockTypeID = body.BlockTypeID
		}
	case "sectors":
		level.passingConditionID = body.PassingConditionID
		level.requiredSectorsCount = body.RequiredSectorsCount
	default:
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "unknown section"})
		return
	}
	noContent(w)
}

// handleAdminLevelEditor serves the whole level in one document, which is what
// the new engine does where the legacy editor needed a page per collection.
func (s *server) handleAdminLevelEditor(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()

	tasks := make([]any, 0, len(level.tasks))
	for _, task := range level.tasks {
		tasks = append(tasks, map[string]any{
			"task_id": task.id, "level_id": level.id, "task_text": task.text,
			"replace_nl_to_br": task.replaceNl,
			"for_user_id":      task.forMemberID, "for_team_id": 0,
		})
	}
	helps, penaltyHelps := []any{}, []any{}
	for _, help := range level.helps {
		item := map[string]any{
			"help_id": help.id, "level_id": level.id, "help_number": help.number,
			"help_text": help.text, "timeout": help.timeout, "is_penalty": help.isPenalty,
			"penalty_time": help.penaltyTime, "penalty_score": 0,
			"request_penalty_confirm": help.requestConfirm,
			"for_user_id":             help.forMemberID, "for_team_id": 0,
		}
		// The engine omits the comment rather than sending an empty one, so a
		// plain help carries no penalty_comment at all.
		if help.penaltyComment != "" {
			item["penalty_comment"] = help.penaltyComment
		}
		if help.isPenalty {
			penaltyHelps = append(penaltyHelps, item)
		} else {
			helps = append(helps, item)
		}
	}
	bonuses := make([]any, 0, len(level.bonuses))
	for _, bonus := range level.bonuses {
		item := map[string]any{
			"bonus_id": bonus.id, "game_id": st.gameID,
			"bonus_name": bonus.name, "task": bonus.task, "bonus_help": bonus.help,
			"answers": bonus.answers, "bonus_time": bonus.bonusTime, "negative": bonus.negative,
			"all_levels":         bonus.allLevels,
			"has_absolute_limit": bonus.hasAbsoluteLimit,
			"has_delay":          bonus.hasDelay,
			"has_relative_limit": bonus.hasRelativeLimit,
			"user_id":            bonus.forMemberID, "team_id": 0,
		}
		// A game-wide bonus carries no level_ids at all, which is how the client
		// tells it apart from one pinned to a level.
		if !bonus.allLevels {
			item["level_ids"] = bonus.levelIDs
		}
		if bonus.hasAbsoluteLimit {
			item["valid_from"], item["valid_to"] = bonus.validFrom, bonus.validTo
		}
		if bonus.hasDelay {
			item["delay_sec"] = bonus.delaySec
		}
		if bonus.hasRelativeLimit {
			item["life_time_sec"] = bonus.lifeTimeSec
		}
		bonuses = append(bonuses, item)
	}
	sectors := make([]any, 0, len(level.sectors))
	for _, sector := range level.sectors {
		sectors = append(sectors, map[string]any{
			"sector_id": sector.id, "level_id": level.id, "sector_name": sector.name,
		})
	}
	answers := make([]any, 0, len(level.answers))
	for _, answer := range level.answers {
		item := map[string]any{
			"answer_id": answer.id, "level_id": level.id, "sector_id": answer.sectorID,
			"answer_text": answer.text,
		}
		// An answer addressed to everybody carries no for_* keys at all.
		if answer.forMemberID != 0 {
			item["for_user_id"] = answer.forMemberID
			item["for_team_id"] = 0
		}
		answers = append(answers, item)
	}
	messages := make([]any, 0, len(level.messages))
	for _, message := range level.messages {
		item := map[string]any{
			"message_id": message.id, "message_text": message.text,
			"replace_nl_to_br": message.replaceNlToBr, "all_levels": message.allLevels,
		}
		if message.requiredPoints != 0 {
			item["required_points"] = message.requiredPoints
		}
		if !message.allLevels {
			item["level_ids"] = message.levelIDs
		}
		messages = append(messages, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"game_id": st.gameID, "game_num": st.gameNum, "title": st.title,
		"zone_id": 0, "game_type_id": 1, "version": "1-mock",
		"level":  s.levelItem(st, level),
		"levels": s.levelItems(st),

		"tasks": tasks, "helps": helps, "penalty_helps": penaltyHelps,
		"bonuses": bonuses, "sectors": sectors, "answers": answers, "messages": messages,

		"timeout_sec":            level.timeoutSec,
		"timeout_time_award_sec": level.timeoutAwardSec,
		"timeout_points_award":   0,
		"attempts_number":        level.attemptsNumber,
		"attempts_period_sec":    level.attemptsPeriodSec,
		"block_type_id":          level.blockTypeID,
		"passing_condition_id":   level.passingConditionID,
		"required_sectors_count": level.requiredSectorsCount,

		"supports_tasks": true, "supports_helps": true, "supports_penalty_helps": true,
		"supports_bonuses": true, "supports_answers": true, "supports_messages": true,
		"supports_auto_pass": true, "supports_answer_blocking": true,
		"supports_sectors_requirement": true, "is_classic_game": true,
		"players_started": st.started,
	})
}

func (s *server) levelItem(st *adminState, level *adminLevel) map[string]any {
	return map[string]any{
		"level_id": level.id, "level_number": st.numberOf(level),
		"level_name": level.name, "comment": level.comment,
		"dismissed": false, "owner_id": mockAdminUserID,
	}
}

// --- level content ---

func (s *server) handleAdminCreateTask(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	var body struct {
		TaskText      string `json:"task_text"`
		ReplaceNlToBr bool   `json:"replace_nl_to_br"`
		ForMemberID   int    `json:"for_member_id"`
	}
	_ = decodeJSONBody(r, &body)
	st.mu.Lock()
	defer st.mu.Unlock()
	level.tasks = append(level.tasks, &adminTask{
		id: st.takeTaskID(), text: body.TaskText,
		replaceNl: body.ReplaceNlToBr, forMemberID: body.ForMemberID,
	})
	noContent(w)
}

func (s *server) handleAdminUpdateTask(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	taskID, _ := adminPathInt(r, "taskId")
	var body struct {
		TaskText      string `json:"task_text"`
		ReplaceNlToBr bool   `json:"replace_nl_to_br"`
		ForMemberID   int    `json:"for_member_id"`
	}
	_ = decodeJSONBody(r, &body)
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, task := range level.tasks {
		if task.id == taskID {
			task.text, task.replaceNl, task.forMemberID = body.TaskText, body.ReplaceNlToBr, body.ForMemberID
			noContent(w)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "task not found"})
}

func (s *server) handleAdminDeleteTask(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	taskID, _ := adminPathInt(r, "taskId")
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, task := range level.tasks {
		if task.id == taskID {
			level.tasks = append(level.tasks[:i], level.tasks[i+1:]...)
			noContent(w)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "task not found"})
}

type helpRequestBody struct {
	HelpText              string `json:"help_text"`
	Timeout               int    `json:"timeout"`
	ForMemberID           int    `json:"for_member_id"`
	IsPenalty             bool   `json:"is_penalty"`
	PenaltyTime           int    `json:"penalty_time"`
	PenaltyComment        string `json:"penalty_comment"`
	RequestPenaltyConfirm bool   `json:"request_penalty_confirm"`
}

func (s *server) handleAdminCreateHelp(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	var body helpRequestBody
	_ = decodeJSONBody(r, &body)
	st.mu.Lock()
	defer st.mu.Unlock()
	level.helps = append(level.helps, &adminHelp{
		id: st.takeHelpID(), number: len(level.helps) + 1,
		text: body.HelpText, timeout: body.Timeout, isPenalty: body.IsPenalty,
		penaltyTime: body.PenaltyTime, penaltyComment: body.PenaltyComment,
		requestConfirm: body.RequestPenaltyConfirm, forMemberID: body.ForMemberID,
	})
	noContent(w)
}

func (s *server) handleAdminUpdateHelp(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	helpID, _ := adminPathInt(r, "helpId")
	var body helpRequestBody
	_ = decodeJSONBody(r, &body)
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, help := range level.helps {
		if help.id != helpID {
			continue
		}
		help.text, help.timeout, help.isPenalty = body.HelpText, body.Timeout, body.IsPenalty
		help.penaltyTime, help.penaltyComment = body.PenaltyTime, body.PenaltyComment
		help.requestConfirm, help.forMemberID = body.RequestPenaltyConfirm, body.ForMemberID
		noContent(w)
		return
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "help not found"})
}

func (s *server) handleAdminDeleteHelp(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	helpID, _ := adminPathInt(r, "helpId")
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, help := range level.helps {
		if help.id == helpID {
			level.helps = append(level.helps[:i], level.helps[i+1:]...)
			noContent(w)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "help not found"})
}

type bonusRequestBody struct {
	BonusName        string   `json:"bonus_name"`
	Task             string   `json:"task"`
	BonusHelp        string   `json:"bonus_help"`
	Answers          []string `json:"answers"`
	BonusTime        int      `json:"bonus_time"`
	Negative         bool     `json:"negative"`
	ForMemberID      int      `json:"for_member_id"`
	AllLevels        bool     `json:"all_levels"`
	LevelIDs         []int    `json:"level_ids"`
	HasAbsoluteLimit bool     `json:"has_absolute_limit"`
	ValidFrom        string   `json:"valid_from"`
	ValidTo          string   `json:"valid_to"`
	HasDelay         bool     `json:"has_delay"`
	DelaySec         int      `json:"delay_sec"`
	HasRelativeLimit bool     `json:"has_relative_limit"`
	LifeTimeSec      int      `json:"life_time_sec"`
}

func (body bonusRequestBody) apply(bonus *adminBonus) {
	bonus.name, bonus.task, bonus.help = body.BonusName, body.Task, body.BonusHelp
	bonus.answers = append([]string(nil), body.Answers...)
	bonus.bonusTime, bonus.negative = body.BonusTime, body.Negative
	bonus.allLevels, bonus.levelIDs = body.AllLevels, append([]int(nil), body.LevelIDs...)
	bonus.hasAbsoluteLimit, bonus.validFrom, bonus.validTo = body.HasAbsoluteLimit, body.ValidFrom, body.ValidTo
	bonus.hasDelay, bonus.delaySec = body.HasDelay, body.DelaySec
	bonus.hasRelativeLimit, bonus.lifeTimeSec = body.HasRelativeLimit, body.LifeTimeSec
	bonus.forMemberID = body.ForMemberID
}

func (s *server) handleAdminCreateBonus(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	var body bonusRequestBody
	_ = decodeJSONBody(r, &body)
	st.mu.Lock()
	defer st.mu.Unlock()
	bonus := &adminBonus{id: st.takeBonusID()}
	body.apply(bonus)
	level.bonuses = append(level.bonuses, bonus)
	noContent(w)
}

func (s *server) handleAdminUpdateBonus(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	bonusID, _ := adminPathInt(r, "bonusId")
	var body bonusRequestBody
	_ = decodeJSONBody(r, &body)
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, bonus := range level.bonuses {
		if bonus.id == bonusID {
			body.apply(bonus)
			noContent(w)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "bonus not found"})
}

func (s *server) handleAdminDeleteBonus(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	bonusID, _ := adminPathInt(r, "bonusId")
	st.mu.Lock()
	defer st.mu.Unlock()
	for i, bonus := range level.bonuses {
		if bonus.id == bonusID {
			level.bonuses = append(level.bonuses[:i], level.bonuses[i+1:]...)
			noContent(w)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "bonus not found"})
}

type messageRequestBody struct {
	MessageText    string `json:"message_text"`
	ReplaceNlToBr  bool   `json:"replace_nl_to_br"`
	AllLevels      bool   `json:"all_levels"`
	LevelIDs       []int  `json:"level_ids"`
	RequiredPoints int    `json:"required_points"`
}

func (body messageRequestBody) apply(message *adminMessage) {
	message.text, message.replaceNlToBr = body.MessageText, body.ReplaceNlToBr
	message.allLevels, message.levelIDs = body.AllLevels, append([]int(nil), body.LevelIDs...)
	message.requiredPoints = body.RequiredPoints
}

// messagesFor returns the levels a message belongs on: a game-wide message
// appears on every level's editor, a scoped one only on the levels it names.
func (st *adminState) placeMessage(message *adminMessage, created *adminLevel) {
	if message.allLevels {
		for _, level := range st.levels {
			level.messages = append(level.messages, message)
		}
		return
	}
	targets := message.levelIDs
	if len(targets) == 0 {
		targets = []int{created.id}
	}
	for _, id := range targets {
		if level, _ := st.levelByID(id); level != nil {
			level.messages = append(level.messages, message)
		}
	}
}

func (s *server) handleAdminCreateMessage(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	var body messageRequestBody
	_ = decodeJSONBody(r, &body)
	st.mu.Lock()
	defer st.mu.Unlock()
	message := &adminMessage{id: st.takeMessageID()}
	body.apply(message)
	st.placeMessage(message, level)
	noContent(w)
}

func (s *server) handleAdminUpdateMessage(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	messageID, _ := adminPathInt(r, "messageId")
	var body messageRequestBody
	_ = decodeJSONBody(r, &body)
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, message := range level.messages {
		if message.id == messageID {
			body.apply(message)
			noContent(w)
			return
		}
	}
	writeJSON(w, http.StatusNotFound, map[string]any{"error": "message not found"})
}

func (s *server) handleAdminDeleteMessage(w http.ResponseWriter, r *http.Request) {
	st, _ := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	messageID, _ := adminPathInt(r, "messageId")
	st.mu.Lock()
	defer st.mu.Unlock()
	removed := false
	for _, level := range st.levels {
		kept := level.messages[:0]
		for _, message := range level.messages {
			if message.id == messageID {
				removed = true
				continue
			}
			kept = append(kept, message)
		}
		level.messages = kept
	}
	if !removed {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "message not found"})
		return
	}
	noContent(w)
}

// --- sectors and answers ---

func (s *server) handleAdminCreateSector(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	var body struct {
		SectorName string `json:"sector_name"`
	}
	_ = decodeJSONBody(r, &body)
	st.mu.Lock()
	defer st.mu.Unlock()
	sector := &adminSector{id: st.takeSectorID(), name: body.SectorName}
	level.sectors = append(level.sectors, sector)
	// The client reads the new id out of this answer to attach the answers.
	writeJSON(w, http.StatusOK, map[string]any{
		"sector_id": sector.id, "level_id": level.id, "sector_name": sector.name,
	})
}

func (s *server) handleAdminUpdateSector(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	sectorID, _ := adminPathInt(r, "sectorId")
	var body struct {
		SectorName string `json:"sector_name"`
	}
	_ = decodeJSONBody(r, &body)
	st.mu.Lock()
	defer st.mu.Unlock()
	sector := level.sectorByID(sectorID)
	if sector == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "sector not found"})
		return
	}
	sector.name = body.SectorName
	noContent(w)
}

func (s *server) handleAdminDeleteSector(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	sectorID, _ := adminPathInt(r, "sectorId")
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.refuseSectorDeletes {
		// Participants have started the sector: the engine refuses and the
		// sector is still on the level afterwards, which is the condition the
		// client reads as ErrSectorStarted.
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "sector is started"})
		return
	}
	if !level.deleteSector(sectorID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "sector not found"})
		return
	}
	noContent(w)
}

func (s *server) handleAdminAnswersBatch(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	var body struct {
		SectorID int `json:"sector_id"`
		Answers  []struct {
			AnswerText  string `json:"answer_text"`
			ForMemberID int    `json:"for_member_id"`
		} `json:"answers"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Validation failed"})
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if body.SectorID != 0 && level.sectorByID(body.SectorID) == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "sector not found"})
		return
	}
	for _, item := range body.Answers {
		if strings.TrimSpace(item.AnswerText) == "" {
			continue
		}
		level.answers = append(level.answers, &adminAnswer{
			id: st.takeAnswerID(), sectorID: body.SectorID,
			text: item.AnswerText, forMemberID: item.ForMemberID,
		})
	}
	noContent(w)
}

func (s *server) handleAdminDeleteAnswer(w http.ResponseWriter, r *http.Request) {
	st, level := s.adminLevelFrom(w, r)
	if st == nil {
		return
	}
	answerID, _ := adminPathInt(r, "answerId")
	st.mu.Lock()
	defer st.mu.Unlock()
	if !level.deleteAnswer(answerID) {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": "answer not found"})
		return
	}
	noContent(w)
}

// --- corrections and monitoring ---

// handleAdminCorrections reproduces the measured rule: the route refuses a game
// that has not started rather than answering with an empty table.
func (s *server) handleAdminCorrections(w http.ResponseWriter, r *http.Request) {
	st := s.adminGameFrom(w, r)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.started {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "forbidden"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"game_id": st.gameID, "game_num": st.gameNum, "game_title": st.title,
		"can_add": true, "is_admin": true,
		"items":   []any{},
		"levels":  s.correctionLevels(st),
		"players": []any{},
	})
}

func (s *server) correctionLevels(st *adminState) []any {
	levels := make([]any, 0, len(st.levels))
	for i, level := range st.levels {
		levels = append(levels, map[string]any{
			"level_id": level.id, "level_num": i + 1, "name": level.name,
		})
	}
	return levels
}

func (s *server) handleAdminMonitoring(w http.ResponseWriter, r *http.Request) {
	st := s.adminGameFrom(w, r)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"game_id": st.gameID, "game_num": st.gameNum, "game_title": st.title,
		"can_view": true, "mode": "main",
		"page": 1, "page_size": 50, "total_pages": 1, "total_rows": 0,
		"actions": []any{}, "players": []any{},
		"levels":  s.correctionLevels(st),
		"started": st.started, "finished": false,
	})
}
