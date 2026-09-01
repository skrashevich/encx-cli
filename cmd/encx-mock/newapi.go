package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/enapi"
)

// The new-engine routes reuse the legacy handlers and translate their answers,
// rather than reimplementing the game. One source of truth for the mock's state
// keeps the two engines telling the same story, which is the point of running
// e2e against both.

// registerNewAPIRoutes adds the Encounter Go Backend routes to mux.
func (s *server) registerNewAPIRoutes(root *http.ServeMux, legacy http.Handler) {
	mux := tolerantMux{root}
	mux.HandleFunc("GET /version", s.handleAPIVersion)
	mux.HandleFunc("GET /sites/domain/{domain}", s.handleAPISiteByDomain)
	mux.HandleFunc("POST /login", s.handleAPILogin(legacy))
	mux.HandleFunc("GET /auth/session", s.handleAPISession(legacy))
	mux.HandleFunc("GET /games/home", s.handleAPIHome(legacy))
	mux.HandleFunc("GET /games/active", s.handleAPIGameList(legacy, "active"))
	mux.HandleFunc("GET /games/coming", s.handleAPIGameList(legacy, "coming"))
	mux.HandleFunc("GET /games/{id}/details", s.handleAPIGameDetails(legacy))
	mux.HandleFunc("GET /games/{id}/fee-box", s.handleAPIFeeBox(legacy))
	mux.HandleFunc("POST /games/{id}/make-fee", s.handleAPIMakeFee(legacy))
	mux.HandleFunc("GET /games/{id}/statistics", s.handleAPIStatistics(legacy))
	mux.HandleFunc("GET /gameengines/{controller}/play/{gameId}", s.handleAPIEngine(legacy))
	mux.HandleFunc("POST /gameengines/{controller}/play/{gameId}", s.handleAPIEngine(legacy))
}

// handleAPISiteByDomain answers the probe encx uses to decide whether a domain
// has moved to the new engine. The mock owns whatever domain it is addressed
// by, so it always reports a site.
func (s *server) handleAPISiteByDomain(w http.ResponseWriter, r *http.Request) {
	domain := r.PathValue("domain")
	writeJSON(w, http.StatusOK, map[string]any{
		"id":             1,
		"name":           "encx-mock",
		"primary_domain": domain,
		"domains":        []map[string]any{{"id": 1, "site_id": 1, "domain": domain, "is_primary": true}},
		"status_id":      1,
	})
}

func (s *server) handleAPIVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"site_version":        "encx-mock",
		"db_version":          "mock",
		"expected_db_version": "mock",
		"db_match":            true,
	})
}

// callLegacy replays a request against the legacy handler and returns its answer.
func callLegacy(legacy http.Handler, r *http.Request, method, target string, form url.Values) (*httptest.ResponseRecorder, error) {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req, err := http.NewRequestWithContext(r.Context(), method, target, body)
	if err != nil {
		return nil, err
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	// The legacy handlers identify a session by these headers and cookies.
	req.Header = mergeRequestHeaders(req.Header, r.Header)
	for _, cookie := range r.Cookies() {
		req.AddCookie(cookie)
	}
	// A mobile session authenticates with the bearer token alone. The legacy
	// half knows nothing about bearer tokens, so the token — which is the
	// session cookie's value — is handed to it as that cookie.
	if _, err := r.Cookie("SESSION_ID"); err != nil {
		if token := bearerToken(r); token != "" {
			req.AddCookie(&http.Cookie{Name: "SESSION_ID", Value: token})
		}
	}
	rec := httptest.NewRecorder()
	legacy.ServeHTTP(rec, req)
	return rec, nil
}

func mergeRequestHeaders(dst, src http.Header) http.Header {
	for key, values := range src {
		if strings.EqualFold(key, "Content-Type") || strings.EqualFold(key, "Content-Length") {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
	return dst
}

func (s *server) handleAPILogin(legacy http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Login    string `json:"login"`
			Password string `json:"password"`
			NoCookie bool   `json:"no_cookie"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad json"})
			return
		}

		form := url.Values{"Login": {body.Login}, "Password": {body.Password}}
		rec, err := callLegacy(legacy, r, http.MethodPost, "/login/signin?json=1", form)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}

		var legacyResp encx.LoginResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &legacyResp); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "bad legacy login answer"})
			return
		}
		if legacyResp.Error != 0 {
			writeJSON(w, http.StatusUnauthorized, map[string]any{
				"error":   "invalid credentials",
				"message": legacyResp.Message,
				"code":    http.StatusUnauthorized,
			})
			return
		}

		// The mock's session token is the legacy cookie, so both engines share
		// one session store.
		token := sessionTokenFromCookies(rec.Result().Cookies())
		for _, cookie := range rec.Result().Cookies() {
			http.SetCookie(w, cookie)
		}
		sessionClass := "web"
		if body.NoCookie {
			sessionClass = "mobile"
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"token":         token,
			"message":       "Login successful",
			"session_class": sessionClass,
			"user":          mockAPIUser(body.Login),
		})
	}
}

// apiSessionFromRequest resolves the caller's session for a new-engine route.
// The new backend authenticates with a bearer token, and a mobile session
// ("no_cookie") carries nothing else, so the cookie alone is not enough. The
// mock's token is the session cookie's value, which keeps one session store
// behind both engines.
func (s *server) apiSessionFromRequest(r *http.Request) (*sessionState, error) {
	if st, err := s.sessionFromRequest(r); err == nil {
		return st, nil
	}
	token := bearerToken(r)
	if token == "" {
		return nil, errors.New("no session")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.sessions[token]
	if st == nil {
		return nil, errors.New("unknown session")
	}
	return st, nil
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if len(header) < len("Bearer ") || !strings.EqualFold(header[:len("Bearer ")], "bearer ") {
		return ""
	}
	return strings.TrimSpace(header[len("Bearer "):])
}

// requireAPISession is the guard the protected new-engine groups are registered
// with. Anonymous callers get the backend's own envelope, not a legacy body.
func (s *server) requireAPISession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.apiSessionFromRequest(r); err != nil {
			writeAPIUnauthorized(w, "Authorization required")
			return
		}
		next(w, r)
	}
}

// writeAPIUnauthorized writes the new backend's unauthorized envelope. The live
// API keeps error and code fixed and varies only the message: /auth/session and
// /admin/** say "Authorization required", the engine route says "unauthorized"
// (measured against api.en.cx on 2026-09-01).
func writeAPIUnauthorized(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusUnauthorized, map[string]any{
		"error":   "unauthorized",
		"message": message,
		"code":    http.StatusUnauthorized,
	})
}

func sessionTokenFromCookies(cookies []*http.Cookie) string {
	for _, cookie := range cookies {
		if cookie.Value != "" {
			return cookie.Value
		}
	}
	return "mock-token"
}

func (s *server) handleAPISession(legacy http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rec, err := callLegacy(legacy, r, http.MethodGet, "/UserDetails.aspx", nil)
		if err != nil || rec.Code != http.StatusOK {
			writeAPIUnauthorized(w, "Authorization required")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"user": mockAPIUser(s.sessionLogin(r))})
	}
}

func (s *server) handleAPIHome(legacy http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := s.legacyGameList(legacy, r)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"active_games": apiGamesFromInfos(list.ActiveGames),
			"coming_games": apiGamesFromInfos(list.ComingGames),
		})
	}
}

func (s *server) handleAPIGameList(legacy http.Handler, block string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := s.legacyGameList(legacy, r)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		games := list.ActiveGames
		if block == "coming" {
			games = list.ComingGames
		}
		items := apiGamesFromInfos(games)
		writeJSON(w, http.StatusOK, map[string]any{"items": items, "total_count": len(items)})
	}
}

func (s *server) legacyGameList(legacy http.Handler, r *http.Request) (*encx.GameListResponse, error) {
	rec, err := callLegacy(legacy, r, http.MethodGet, "/home/?json=1", nil)
	if err != nil {
		return nil, err
	}
	var list encx.GameListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		return nil, err
	}
	return &list, nil
}

// apiGamesFromInfos renders GameInfo in the new engine's snake_case shape.
func apiGamesFromInfos(games []encx.GameInfo) []map[string]any {
	items := make([]map[string]any, 0, len(games))
	for _, game := range games {
		item := map[string]any{
			"id":                    game.GameID,
			"game_num":              game.GameNum,
			"site_id":               game.SiteID,
			"lang_id":               game.LangID,
			"owner_id":              game.OwnerID,
			"title":                 game.Title,
			"descr":                 game.Descr,
			"game_type_id":          game.GameTypeID,
			"zone_id":               game.ZoneId,
			"status_id":             game.StatusId,
			"levels_sequence_id":    game.LevelsSequence,
			"max_players":           game.MaxPlayers,
			"max_team_members":      game.MaxTeamMembers,
			"scenario_availability": game.ScenarioAvailability,
			"show_in_calendar":      game.ShowInCalendar,
			"started":               game.Started,
			"finished":              game.Finished,
			"is_moderated":          game.IsModerated,
			"fee_name":              game.FeeName,
			"show_fee":              game.ShowFee,
		}
		item["start_date_time"] = apiTimeFromDateTime(game.StartDateTime)
		item["finish_date_time"] = apiTimeFromDateTime(game.FinishDateTime)
		item["create_date_time"] = apiTimeFromDateTime(game.CreateDateTime)
		if game.Fee != nil {
			item["fee"] = game.Fee.Value
		}
		if game.Prize != nil {
			item["prize"] = game.Prize.Value
		}
		items = append(items, item)
	}
	return items
}

func apiTimeFromDateTime(value *encx.DateTime) string {
	if value == nil || value.Timestamp <= 0 {
		return ""
	}
	return timeUnix(value.Timestamp).UTC().Format("2006-01-02T15:04:05Z")
}

func (s *server) handleAPIGameDetails(legacy http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gameID := pathInt(r, "id")
		list, err := s.legacyGameList(legacy, r)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		game := findGameInfo(list, gameID)
		if game == nil {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "game not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"game":            apiGamesFromInfos([]encx.GameInfo{*game})[0],
			"can_manage_game": false,
			"total_winners":   0,
		})
	}
}

func (s *server) handleAPIFeeBox(legacy http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gameID := pathInt(r, "id")
		list, err := s.legacyGameList(legacy, r)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		game := findGameInfo(list, gameID)
		seconds := 0
		if game != nil && game.TSRemain != nil {
			seconds = int(game.TSRemain.TotalSeconds)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"seconds_to_start": seconds,
			"show_timer":       seconds > 0,
			"can_make_fee":     true,
		})
	}
}

func (s *server) handleAPIMakeFee(legacy http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gameID := pathInt(r, "id")
		target := "/MakeGameFee.aspx?gid=" + strconv.Itoa(gameID) + "&confirm=yes"
		rec, err := callLegacy(legacy, r, http.MethodGet, target, nil)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if rec.Code >= 400 {
			writeJSON(w, http.StatusOK, map[string]any{
				"success": false,
				"message": "заявка отклонена",
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success":      true,
			"fee_accepted": true,
			"message":      "ok",
		})
	}
}

func (s *server) handleAPIStatistics(legacy http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gameID := pathInt(r, "id")
		target := "/gamestatistics/full/" + strconv.Itoa(gameID) + "?json=1"
		rec, err := callLegacy(legacy, r, http.MethodGet, target, nil)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		var stats encx.GameStatisticsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "bad legacy statistics"})
			return
		}

		levels := make([]map[string]any, 0, len(stats.Levels))
		for _, level := range stats.Levels {
			levels = append(levels, map[string]any{
				"level_id":     level.LevelId,
				"level_number": level.LevelNumber,
				"level_name":   level.LevelName,
				"dismissed":    level.Dismissed,
			})
		}
		levelStats := map[string][]map[string]any{}
		for _, group := range stats.StatItems {
			for _, item := range group {
				key := strconv.Itoa(item.LevelNum)
				levelStats[key] = append(levelStats[key], map[string]any{
					"level_id":        item.LevelId,
					"level_num":       item.LevelNum,
					"level_order":     item.LevelOrder,
					"user_id":         item.UserId,
					"user_login":      item.UserName,
					"user_name":       item.UserName,
					"team_id":         item.TeamId,
					"team_name":       item.TeamName,
					"spent_seconds":   item.SpentSeconds,
					"scores":          item.Scores,
					"pass_type_id":    item.PassType,
					"enter_date_time": apiTimeFromDateTime(item.ActionTime),
				})
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"game_id":           gameID,
			"hide_levels_names": !stats.IsLevelNamesVisible,
			"total_levels":      len(levels),
			"current_page":      1,
			"total_pages":       1,
			"levels":            levels,
			"level_stats":       levelStats,
		})
	}
}

func (s *server) handleAPIEngine(legacy http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The session is checked before json=1, not after: measured against
		// api.en.cx (X-En-Domain: demo.en.cx) on 2026-09-01, an anonymous call
		// answers 401 with the flag, without it, and for a game id that does
		// not exist. The 404-without-json=1 rule below only ever shows up to a
		// caller who is already signed in, so the order is not interchangeable.
		if _, err := s.apiSessionFromRequest(r); err != nil {
			writeAPIUnauthorized(w, "unauthorized")
			return
		}
		if r.URL.Query().Get("json") != "1" {
			// The real backend answers 404 without json=1.
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "not found"})
			return
		}
		gameID := pathInt(r, "gameId")

		target := "/gameengines/encounter/play/" + strconv.Itoa(gameID) + "?json=1"
		if level := r.URL.Query().Get("level"); level != "" {
			target += "&level=" + url.QueryEscape(level)
		}

		method := http.MethodGet
		var form url.Values
		if r.Method == http.MethodPost {
			var msg enapi.EngineClientMessage
			if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad json"})
				return
			}
			method, form, target = legacyEngineRequest(msg, target)
		}

		rec, err := callLegacy(legacy, r, method, target, form)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
			return
		}
		if rec.Code == http.StatusFound {
			// The legacy half bounces an anonymous caller to its sign-in page.
			// The new backend has no such page: it answers 401 JSON, so the
			// redirect must not be forwarded to a new-engine client.
			writeAPIUnauthorized(w, "unauthorized")
			return
		}
		if rec.Code >= 400 {
			w.WriteHeader(rec.Code)
			_, _ = w.Write(rec.Body.Bytes())
			return
		}

		var model encx.GameModel
		if err := json.Unmarshal(rec.Body.Bytes(), &model); err != nil {
			// Anti-spam and other HTML answers pass through untouched so the
			// client's error handling still sees them.
			w.WriteHeader(rec.Code)
			_, _ = w.Write(rec.Body.Bytes())
			return
		}
		writeJSON(w, http.StatusOK, engineStateFromGameModel(&model))
	}
}

// legacyEngineRequest translates an engine message back into the legacy form
// the mock's handlers understand.
func legacyEngineRequest(msg enapi.EngineClientMessage, target string) (string, url.Values, string) {
	switch msg.Type {
	case enapi.EngineMessageAnswer, enapi.EngineMessageBonus:
		form := url.Values{
			"LevelId":     {strconv.Itoa(msg.LevelID)},
			"LevelNumber": {strconv.Itoa(msg.LevelNumber)},
		}
		field := "LevelAction.Answer"
		if msg.Type == enapi.EngineMessageBonus {
			field = "BonusAction.Answer"
		}
		form.Set(field, msg.Answer)
		return http.MethodPost, form, target
	case enapi.EngineMessagePenalty:
		return http.MethodGet, nil,
			target + "&pid=" + strconv.Itoa(msg.PenaltyID) + "&pact=1"
	default:
		return http.MethodGet, nil, target
	}
}

func findGameInfo(list *encx.GameListResponse, gameID int) *encx.GameInfo {
	for i := range list.ActiveGames {
		if list.ActiveGames[i].GameID == gameID {
			return &list.ActiveGames[i]
		}
	}
	for i := range list.ComingGames {
		if list.ComingGames[i].GameID == gameID {
			return &list.ComingGames[i]
		}
	}
	return nil
}

func pathInt(r *http.Request, name string) int {
	value, _ := strconv.Atoi(r.PathValue(name))
	return value
}

// newEngineFallbackMux serves the new API where it has a route and the legacy
// handlers everywhere else, so one process can answer both engines at once.
func newEngineFallbackMux(api *http.ServeMux, legacy http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := api.Handler(r); pattern != "" {
			api.ServeHTTP(w, r)
			return
		}
		legacy.ServeHTTP(w, r)
	})
}

// mockAPIUser renders the mock account in the new engine's user shape. The IDs
// match the ones the legacy handlers report, so a session works on both.
func mockAPIUser(login string) map[string]any {
	return map[string]any{
		"id":                mockAPIUserID,
		"login":             login,
		"team_id":           mockTeamID,
		"team":              map[string]any{"id": mockTeamID, "name": mockTeamName},
		"rank_sentence_key": "Private soldier",
		"points":            1000,
		"email":             "mock@example.local",
		"email_checked":     true,
	}
}

// sessionLogin returns the login of the caller's session, empty when anonymous.
func (s *server) sessionLogin(r *http.Request) string {
	st, err := s.sessionFromRequest(r)
	if err != nil || st == nil {
		return ""
	}
	return st.Login
}
