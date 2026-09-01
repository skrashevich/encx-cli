package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/skrashevich/encx-cli/encx"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

const (
	defaultAddr = "0.0.0.0:18080"
	mockGameID  = 424242
	mockTeamID  = 5150
	// mockAPIUserID and mockTeamName mirror the identity the legacy handlers
	// report, so the new-engine routes describe the same account.
	mockAPIUserID = 101
	mockTeamName  = "MockTeam"
	// The engine is not consistent about the case of the sign-in path and IIS
	// does not care: the game engine route bounces to /login.aspx, the fee page
	// to /Login.aspx (both measured on svk.en.cx, 2026-09-01). Go's ServeMux is
	// case-sensitive, so the mock serves the page under both spellings.
	enginePlayLoginPath = "/login.aspx"
	makeFeeLoginPath    = "/Login.aspx"
	// mockAnonymousLogin names the player in the public statistics an anonymous
	// caller reads. The engine shows real player names there; the mock has one
	// player and no identity to attribute the run to.
	mockAnonymousLogin = "MockPlayer"

	networkDropCode     = "PZDC"
	networkDropDuration = time.Minute
	networkDropHangMax  = 2 * time.Second

	mockDomain = "mock.en.cx"
)

var version = "dev"

type server struct {
	mu                    sync.Mutex
	fixtures              *fixtureSet
	scenario              *scenario.Document
	sessions              map[string]*sessionState
	authStates            map[string]*sessionState
	silentUntil           map[string]time.Time
	antiBotAnswerAttempts map[int]bool
	nextSID               uint64
	admin                 *adminState
}

type sessionState struct {
	mu                sync.Mutex
	AuthKey           string
	Login             string
	CurrentIdx        int
	Completed         bool
	Passed            []bool
	SectorPassed      [][]bool
	SectorAnswers     [][]string
	LevelStartedAt    []time.Time
	AnsweredBonuses   map[int]bool
	BonusAnswers      map[int]string
	Actions           []encx.CodeAction
	LastAction        *encx.EngineAction
	UpdatedAt         time.Time
	PendingTransition int
	AnswerAttempts    int
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version", "version":
			fmt.Println("encx-mock", version)
			return
		}
	}

	var scenarioFlag, engineFlag, quirksFlag string
	fs := flag.NewFlagSet("encx-mock", flag.ExitOnError)
	fs.StringVar(&scenarioFlag, "scenario", "", "path to scenario file (overrides ENCX_MOCK_SCENARIO)")
	fs.StringVar(&engineFlag, "engine", os.Getenv("ENCX_MOCK_ENGINE"),
		"which engine to serve: legacy (default), new, or both (env: ENCX_MOCK_ENGINE)")
	fs.StringVar(&quirksFlag, "quirks", os.Getenv("ENCX_MOCK_QUIRKS"),
		"comma-separated deployed-backend quirks to reproduce, e.g. reorder-noop (env: ENCX_MOCK_QUIRKS)")
	_ = fs.Parse(os.Args[1:])

	fixtures, err := loadFixtures()
	if err != nil {
		log.Fatal(err)
	}

	scenarioPath := resolveScenarioPath(scenarioFlag)
	scenarioDoc, err := loadScenario(scenarioPath)
	if err != nil {
		log.Fatal(err)
	}

	setQuirks(quirksFlag)

	addr := strings.TrimSpace(os.Getenv("ENCX_MOCK_ADDR"))
	if addr == "" {
		addr = defaultAddr
	}

	s := &server{
		fixtures:              fixtures,
		scenario:              scenarioDoc,
		sessions:              make(map[string]*sessionState),
		authStates:            make(map[string]*sessionState),
		silentUntil:           make(map[string]time.Time),
		antiBotAnswerAttempts: antiBotAnswerAttemptsFromEnv(),
	}
	// The admin state describes the same game the player-facing routes serve, so
	// both halves of the mock tell one story.
	s.admin = newAdminState(mockGameID, s.levelCount(), s.gameTitle(), s.scenario, mockQuirkEnabled("reorder-noop"))

	mux := s.routes()
	handler := http.Handler(mux)
	switch strings.ToLower(strings.TrimSpace(engineFlag)) {
	case "new", "both":
		// The new-engine routes translate the same handlers, so both engines
		// answer from one game state and e2e can exercise either.
		apiMux := http.NewServeMux()
		s.registerNewAPIRoutes(apiMux, mux)
		s.registerAdminAPIRoutes(apiMux)
		if engineFlag == "new" {
			handler = apiMux
		} else {
			handler = newEngineFallbackMux(apiMux, mux)
		}
		log.Printf("serving the new engine API (-engine %s)", engineFlag)
	}

	log.Printf("encx-mock listening on http://%s", addr)
	log.Printf("test account: any login/password except fail:fail")
	log.Printf("mock game id=%d, team id=%d, levels=%d", mockGameID, mockTeamID, s.levelCount())
	if s.scenario != nil {
		log.Printf("scenario: %q (%d levels) from %s", s.gameTitle(), len(s.scenario.Levels), s.scenario.SourcePath)
		if len(s.scenario.MissingAssets) > 0 {
			log.Printf("scenario: warning: %d linked asset(s) missing on disk", len(s.scenario.MissingAssets))
		}
	} else {
		log.Printf("sectors/level=%d; level codes: %v; sector codes: 1-%d", mockSectorsPerLevel, legacyLevelCodes, mockLevelCount*mockSectorsPerLevel)
	}
	log.Printf("network drop test: send code %q to ignore all requests for that login:password for %s", networkDropCode, networkDropDuration)
	if path := strings.TrimSpace(os.Getenv("ENCX_MOCK_HAR")); path != "" {
		log.Printf("loaded fixtures from HAR: %s", path)
	}
	if scenarioPath != "" {
		log.Printf("loaded game content from scenario: %s", scenarioPath)
	}

	if err := http.ListenAndServe(addr, withCommonHeaders(handler)); err != nil {
		log.Fatal(err)
	}
}

func withCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", time.Unix(1, 0).UTC().Format(http.TimeFormat))
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"Error":   5,
			"Message": "Invalid form body",
		})
		return
	}
	login := r.Form.Get("Login")
	password := r.Form.Get("Password")

	authKey := credentialsKey(login, password)
	if s.dropIfSilent(r, authKey) {
		return
	}

	if login == "fail" && password == "fail" {
		writeJSON(w, http.StatusOK, map[string]any{
			"Error":                2,
			"Message":              "Неправильный логин или пароль.",
			"IpUnblockUrl":         nil,
			"BruteForceUnblockUrl": nil,
			"ConfirmEmailUrl":      nil,
			"CaptchaUrl":           nil,
			"AdminWhoCanActivate":  nil,
		})
		return
	}

	s.mu.Lock()
	state, exists := s.authStates[authKey]
	if !exists {
		state = &sessionState{
			AuthKey:         authKey,
			Login:           login,
			CurrentIdx:      0,
			Passed:          s.newSessionPassedState(),
			SectorPassed:    s.newSessionSectorState(),
			SectorAnswers:   s.newSessionSectorAnswerState(),
			LevelStartedAt:  make([]time.Time, s.levelCount()),
			AnsweredBonuses: make(map[int]bool),
			BonusAnswers:    make(map[int]string),
			Actions:         []encx.CodeAction{},
			UpdatedAt:       time.Now(),
		}
		s.ensureLevelStarted(state, 0, time.Now())
		s.authStates[authKey] = state
	}
	s.mu.Unlock()

	if exists {
		state.mu.Lock()
		state.AuthKey = authKey
		state.Login = login
		if len(state.LevelStartedAt) == 0 {
			state.LevelStartedAt = make([]time.Time, s.levelCount())
			s.ensureLevelStarted(state, state.CurrentIdx, time.Now())
		}
		state.mu.Unlock()
	}

	sid := s.newSession(state)
	setMockAuthCookies(w, sid, login)

	writeJSON(w, http.StatusOK, map[string]any{
		"Error":                0,
		"Message":              nil,
		"IpUnblockUrl":         nil,
		"BruteForceUnblockUrl": nil,
		"ConfirmEmailUrl":      nil,
		"CaptchaUrl":           nil,
		"AdminWhoCanActivate":  nil,
	})
}

func setMockAuthCookies(w http.ResponseWriter, sessionID, login string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "SESSION_ID",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	setProtocolCookies(w)
	http.SetCookie(w, &http.Cookie{
		Name:     "atoken",
		Value:    fmt.Sprintf("uid%%3d101%%26iss%%3d0%%26iscd%%3d1%%26tkn%%3dmock-%s", login),
		Path:     "/",
		HttpOnly: true,
	})
}

func setProtocolCookies(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: "Domain", Value: mockDomain, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "stoken", Value: "mock-stoken", Path: "/"})
}

func (s *server) handleUserDetails(w http.ResponseWriter, r *http.Request) {
	st, ok := s.requireSessionHTML(w, r)
	if !ok {
		return
	}
	st.mu.Lock()
	login := st.Login
	st.mu.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(s.renderUserDetails(login)))
}

// handleGameList serves the catalog. It is public: GET /home/?json=1 on
// svk.en.cx answers 200 with the full ActiveGames list to a caller with no
// cookies at all (2026-09-01). A signed-in caller sees their own progress
// reflected in the game entry, an anonymous one sees the game as running.
func (s *server) handleGameList(w http.ResponseWriter, r *http.Request) {
	completed := false
	if st, ok := s.optionalSession(w, r); ok && st != nil {
		st.mu.Lock()
		completed = st.Completed
		st.mu.Unlock()
	} else if !ok {
		return
	}
	now := time.Now()
	gameInfo, err := s.buildGameInfoResponse(completed, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"Error": 5, "Message": err.Error()})
		return
	}
	active := []any{}
	if !completed {
		active = append(active, gameInfo)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ComingGames":          []any{},
		"ActiveGames":          active,
		"Error":                0,
		"Message":              nil,
		"IpUnblockUrl":         nil,
		"BruteForceUnblockUrl": nil,
		"ConfirmEmailUrl":      nil,
		"CaptchaUrl":           nil,
		"AdminWhoCanActivate":  nil,
	})
}

func (s *server) handleDomainRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	title := html.EscapeString(s.gameTitle())
	_, _ = fmt.Fprintf(w, `<html><body>
<h1 class="gametitle"><a href="/details/%d/">%s</a></h1>
<a id="lnkGameTitle" href="/GameDetails.aspx?gid=%d">%s</a>
</body></html>`, mockGameID, title, mockGameID, title)
}

func (s *server) handleEnterGame(w http.ResponseWriter, r *http.Request) {
	_, ok := s.requireSessionHTML(w, r)
	if !ok {
		return
	}
	_ = r.ParseForm()
	gid, _ := strconv.Atoi(r.Form.Get("gid"))
	if gid != mockGameID {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<html><body>Team already accepted to the game.</body></html>`))
}

func (s *server) handleMakeGameFee(w http.ResponseWriter, r *http.Request) {
	_, ok := s.requireSessionRedirect(w, r, makeFeeLoginPath)
	if !ok {
		return
	}
	gid, _ := strconv.Atoi(r.URL.Query().Get("gid"))
	if gid != mockGameID {
		http.NotFound(w, r)
		return
	}
	if r.URL.Query().Get("confirm") != "yes" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><a href="?confirm=yes&gid=` + strconv.Itoa(mockGameID) + `">Confirm</a></body></html>`))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<html><body>Team already accepted to the game.</body></html>`))
}

// handleGameDetails serves the game card, which is public: GET
// /GameDetails.aspx?gid=82448 answers 200 to an anonymous caller with or
// without json=1 (svk.en.cx, 2026-09-01).
func (s *server) handleGameDetails(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.optionalSession(w, r); !ok {
		return
	}
	gid, _ := strconv.Atoi(r.URL.Query().Get("gid"))
	if gid != mockGameID {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<html><body><h1>%s</h1><p>Team accepted: yes</p><p>Levels: %d</p></body></html>`, html.EscapeString(s.gameTitle()), s.levelCount())
}

func (s *server) handleTeamDetails(w http.ResponseWriter, r *http.Request) {
	_, ok := s.requireSessionHTML(w, r)
	if !ok {
		return
	}
	tid, _ := strconv.Atoi(r.URL.Query().Get("tid"))
	if tid == 0 {
		tid = mockTeamID
	}
	action := r.URL.Query().Get("action")
	status := "already accepted"
	if action == "accept_invitation" {
		status = "invitation accepted"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<html><body><h1>Team %d</h1><p>%s</p></body></html>`, tid, status)
}

// handleGameStatistics serves the full statistics, which Encounter publishes to
// anyone: GET /gamestatistics/full/82448?json=1 answers 200 with the whole
// document and no cookies (svk.en.cx, 2026-09-01).
//
// The mock keeps game progress per session, so an anonymous caller is shown the
// game from the start rather than another player's position. That is a limit of
// the mock's state model, not of the engine.
func (s *server) handleGameStatistics(w http.ResponseWriter, r *http.Request) {
	st, ok := s.optionalSession(w, r)
	if !ok {
		return
	}
	if st == nil {
		st = s.anonymousViewState()
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	gameID, err := extractTailInt(r.URL.Path, "/gamestatistics/full/")
	if err != nil || gameID != mockGameID {
		http.NotFound(w, r)
		return
	}
	now := time.Now()

	stats := make([][]encx.StatItem, s.levelCount())
	for i := 0; i < s.levelCount(); i++ {
		stats[i] = []encx.StatItem{
			{
				ActionTime:   dt(now.Add(time.Duration(i+1) * time.Minute)),
				UserId:       101,
				LevelId:      mockLevelBaseID + i + 1,
				TeamId:       mockTeamID,
				UserName:     st.Login,
				TeamName:     "MockTeam",
				LevelNum:     i + 1,
				SpentSeconds: 90 + i*20,
				LevelOrder:   i + 1,
				PassType:     0,
				Scores:       10,
			},
		}
	}

	levels := make([]encx.LevelStatInfo, s.levelCount())
	levelPlayers := make([]encx.LevelPlayerCount, s.levelCount())
	for i := 0; i < s.levelCount(); i++ {
		levels[i] = encx.LevelStatInfo{
			LevelId:       mockLevelBaseID + i + 1,
			LevelNumber:   i + 1,
			LevelName:     s.levelName(i),
			Dismissed:     false,
			PassedPlayers: 1,
		}
		levelPlayers[i] = encx.LevelPlayerCount{
			LevelNum: i + 1,
			Count:    1,
		}
	}

	curIdx := st.CurrentIdx
	if curIdx >= s.levelCount() {
		curIdx = s.levelCount() - 1
	}

	gameInfo, err := s.buildGameInfoResponse(st.Completed, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"Error": 5, "Message": err.Error()})
		return
	}
	gameInfoStruct := mapToGameInfo(gameInfo)

	resp := encx.GameStatisticsResponse{
		Game:                &gameInfoStruct,
		Level:               &levels[curIdx],
		StatItems:           stats,
		Levels:              levels,
		IsLevelNamesVisible: true,
		LevelPlayers:        levelPlayers,
		User: &encx.UserProfile{
			ID:             101,
			Login:          st.Login,
			FirstName:      "Mock",
			PatronymicName: "Tester",
			LastName:       "User",
			Email:          "mock@example.local",
			EmailChecked:   true,
			GenderID:       1,
			TeamID:         mockTeamID,
			ParentID:       0,
			SiteId:         1,
			IsActive:       true,
			Points:         1000,
			BonusPoints:    20,
			RankID:         1,
			StatusId:       1,
			Network:        1,
			CityId:         1,
			CountryId:      1,
			ProvinceId:     1,
			RegDateTime:    dt(now.Add(-365 * 24 * time.Hour)),
			LastVisitTime:  dt(now),
			IsSuperAdmin:   false,
			BlockByIP:      false,
		},
		PagerVisible:     false,
		ShowAdminWarning: false,
	}

	writeJSON(w, http.StatusOK, resp)
}

func mapToGameInfo(m map[string]any) encx.GameInfo {
	raw, _ := json.Marshal(m)
	var info encx.GameInfo
	_ = json.Unmarshal(raw, &info)
	if info.GameID == 0 {
		info.GameID = mockGameID
	}
	if info.Title == "" {
		info.Title = "Mock Game 2026"
	}
	return info
}

func (s *server) handleGamePlayGET(w http.ResponseWriter, r *http.Request) {
	st, ok := s.requireSessionRedirect(w, r, enginePlayLoginPath)
	if !ok {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	gameID, err := extractTailInt(r.URL.Path, "/gameengines/encounter/play/")
	if err != nil || gameID != mockGameID {
		http.NotFound(w, r)
		return
	}

	q := r.URL.Query()
	if q.Get("pact") == "1" && q.Get("pid") != "" {
		st.LastAction = &encx.EngineAction{
			LevelNumber: currentLevelNum(st),
			PenaltyAction: &encx.PenaltyActionResult{
				PenaltyId:  mustAtoi(q.Get("pid")),
				ActionType: 1,
			},
			LevelAction: &encx.ActionResult{},
			BonusAction: &encx.ActionResult{},
			GameId:      mockGameID,
			LevelId:     currentLevelID(st),
		}
	}
	st.UpdatedAt = time.Now()
	s.writeGameModel(w, st)
	st.LastAction = nil
}

func (s *server) handleGamePlayPOST(w http.ResponseWriter, r *http.Request) {
	st, ok := s.requireSessionRedirect(w, r, enginePlayLoginPath)
	if !ok {
		return
	}
	gameID, err := extractTailInt(r.URL.Path, "/gameengines/encounter/play/")
	if err != nil || gameID != mockGameID {
		http.NotFound(w, r)
		return
	}

	if err := r.ParseForm(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid form"})
		return
	}

	levelAnswer := r.Form.Get("LevelAction.Answer")
	bonusAnswer := r.Form.Get("BonusAction.Answer")
	_, hasLevelAnswer := r.Form["LevelAction.Answer"]
	_, hasBonusAnswer := r.Form["BonusAction.Answer"]
	if !hasLevelAnswer && !hasBonusAnswer {
		st.mu.Lock()
		defer st.mu.Unlock()
		st.UpdatedAt = time.Now()
		s.writeGameModel(w, st)
		st.LastAction = nil
		return
	}
	answer := strings.TrimSpace(levelAnswer)
	if answer == "" {
		answer = strings.TrimSpace(bonusAnswer)
	}
	if strings.EqualFold(answer, networkDropCode) {
		st.mu.Lock()
		authKey := st.AuthKey
		st.mu.Unlock()
		s.activateNetworkDrop(authKey)
		s.dropIfSilent(r, authKey)
		return
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if s.shouldInjectAntiBot(st) {
		w.Header().Set("Location", "/NotHumanRequest.aspx?return=redacted")
		w.WriteHeader(http.StatusFound)
		return
	}
	levelID, levelIDSet, err := formInt(r, "LevelId")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid level identity"})
		return
	}
	levelNumber, levelNumberSet, err := formInt(r, "LevelNumber")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid level identity"})
		return
	}
	if (levelIDSet && levelID != currentLevelID(st)) || (levelNumberSet && levelNumber != currentLevelNum(st)) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "contradictory level identity"})
		return
	}
	if strings.TrimSpace(levelAnswer) != "" && strings.TrimSpace(bonusAnswer) != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multiple action types"})
		return
	}
	previousLevel := st.CurrentIdx
	wasCompleted := st.Completed
	if levelAnswer != "" {
		s.processLevelAnswer(st, levelID, levelNumber, levelAnswer)
	} else if bonusAnswer != "" {
		s.processBonusAnswer(st, levelID, levelNumber, bonusAnswer)
	}
	currentLevel := st.CurrentIdx
	isCompleted := st.Completed
	stateTransitioned := currentLevel != previousLevel || (!wasCompleted && isCompleted)

	st.UpdatedAt = time.Now()
	actionKind := "level-action"
	if strings.TrimSpace(bonusAnswer) != "" {
		actionKind = "bonus-action"
	}
	s.writeGameModelWithTransition(w, st, s.profileTransitionEvent(actionKind, stateTransitioned))
	st.LastAction = nil
}

func formInt(r *http.Request, name string) (int, bool, error) {
	raw, ok := r.Form[name]
	if !ok {
		return 0, false, nil
	}
	if len(raw) != 1 || strings.TrimSpace(raw[0]) == "" {
		return 0, false, errors.New("invalid form value")
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw[0]))
	if err != nil {
		return 0, false, err
	}
	return value, true, nil
}

func (s *server) writeGameModel(w http.ResponseWriter, st *sessionState) {
	if st.PendingTransition != 0 {
		event := st.PendingTransition
		st.PendingTransition = 0
		writeJSON(w, http.StatusOK, map[string]any{
			"Level":        nil,
			"Levels":       []any{},
			"Event":        event,
			"EngineAction": engineActionToMap(st.LastAction, mockGameID),
		})
		return
	}
	s.writeGameModelWithTransition(w, st, 0)
}

func (s *server) writeGameModelWithTransition(w http.ResponseWriter, st *sessionState, event int) {
	if event != 0 {
		st.PendingTransition = event
		s.writeGameModel(w, st)
		return
	}
	now := time.Now()
	if s.scenario != nil {
		s.applyScenarioAutopass(st, now)
	}
	model, err := s.buildGameModelResponse(st, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"Error": 5, "Message": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model)
}

func (s *server) profileTransitionEvent(actionKind string, stateAdvanced bool) int {
	if !stateAdvanced || s.fixtures == nil || s.fixtures.profile == nil {
		return 0
	}
	for _, record := range s.fixtures.profile.Records {
		if record.Kind == actionKind && record.Variant == "transition" && record.Event != 0 {
			return record.Event
		}
	}
	return 0
}

func (s *server) handleNotHuman(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<html><body><h1>Anti-spam check</h1><p>Mock page.</p></body></html>`))
}

func (s *server) requireSessionJSON(w http.ResponseWriter, r *http.Request) (*sessionState, bool) {
	st, err := s.sessionFromRequest(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"Error":   3,
			"Message": "Unauthorized, call /login/signin first",
		})
		return nil, false
	}
	if s.dropIfSilent(r, sessionAuthKey(st)) {
		return nil, false
	}
	setProtocolCookies(w)
	return st, true
}

// optionalSession serves the pages Encounter leaves open to anyone: the
// catalog, the game card and the full statistics all answer 200 without a
// session (measured on svk.en.cx, 2026-09-01). It returns a nil state for an
// anonymous caller, and ok=false only when the request must be abandoned
// because the network-drop emulation swallowed it.
func (s *server) optionalSession(w http.ResponseWriter, r *http.Request) (*sessionState, bool) {
	st, err := s.sessionFromRequest(r)
	if err != nil {
		return nil, true
	}
	if s.dropIfSilent(r, sessionAuthKey(st)) {
		return nil, false
	}
	setProtocolCookies(w)
	return st, true
}

// anonymousViewState is the throwaway state the public statistics are rendered
// from. It is never stored, so an anonymous read cannot alter or leak any
// player's progress.
func (s *server) anonymousViewState() *sessionState {
	now := time.Now()
	st := &sessionState{
		Login:           mockAnonymousLogin,
		Passed:          s.newSessionPassedState(),
		SectorPassed:    s.newSessionSectorState(),
		SectorAnswers:   s.newSessionSectorAnswerState(),
		LevelStartedAt:  make([]time.Time, s.levelCount()),
		AnsweredBonuses: make(map[int]bool),
		BonusAnswers:    make(map[int]string),
		Actions:         []encx.CodeAction{},
		UpdatedAt:       now,
	}
	s.ensureLevelStarted(st, 0, now)
	return st
}

// requireSessionRedirect is the game engine route's answer to an anonymous
// caller. The live ASP.NET engine does not report 401 there — it bounces to the
// sign-in page:
//
//	GET /gameengines/encounter/play/82448?json=1&lang=ru
//	302, Location: /login.aspx?return=%2fgameengines%2fencounter%2fplay%2f82448%3fjson%3d1%26lang%3dru
//	GET /MakeGameFee.aspx?gid=82448&json=1
//	302, Location: /Login.aspx?return=%2fMakeGameFee.aspx%3fgid%3d82448%26json%3d1
//
// measured against svk.en.cx on 2026-09-01, with or without json=1 and for a
// game id that does not exist: the session is checked before anything else.
//
// The difference is not cosmetic. The mock used to answer 401 with a JSON body,
// and encx decodes the engine's body without gating on the status (see
// decodeGameModelJSON), so that body parsed into an empty GameModel and the
// call reported success. A mock that hides a lost session is worse than no mock
// at all; the redirect makes encx classify it as an expired session, which is
// what a player sees against the real engine.
func (s *server) requireSessionRedirect(w http.ResponseWriter, r *http.Request, loginPath string) (*sessionState, bool) {
	st, err := s.sessionFromRequest(r)
	if err != nil {
		redirectToLoginPage(w, r, loginPath)
		return nil, false
	}
	if s.dropIfSilent(r, sessionAuthKey(st)) {
		return nil, false
	}
	setProtocolCookies(w)
	return st, true
}

// redirectToLoginPage writes the engine's "Object moved" bounce.
func redirectToLoginPage(w http.ResponseWriter, r *http.Request, loginPath string) {
	target := loginPageURL(loginPath, r.URL.RequestURI())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Location", target)
	w.WriteHeader(http.StatusFound)
	fmt.Fprintf(w, "<html><head><title>Object moved</title></head><body>\n"+
		"<h2>Object moved to <a href=%q>here</a>.</h2>\n</body></html>\n", target)
}

// loginPageURL builds the sign-in URL the engine redirects to, with the return
// address escaped the way ASP.NET escapes it: lowercase hex digits, where Go
// emits uppercase. The mock is compared against captures byte for byte, so the
// difference is worth normalizing.
func loginPageURL(loginPath, returnTo string) string {
	return loginPath + "?return=" + lowerHexEscapes(url.QueryEscape(returnTo))
}

func lowerHexEscapes(escaped string) string {
	out := []byte(escaped)
	for i := 0; i+2 < len(out); i++ {
		if out[i] != '%' {
			continue
		}
		out[i+1] = asciiLower(out[i+1])
		out[i+2] = asciiLower(out[i+2])
	}
	return string(out)
}

func asciiLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}

// writeSignInRequiredPage reproduces Encounter's members-only interstitial. The
// wording is trimmed to the parts that carry meaning: the heading, and the
// /Login.aspx link every client and every human uses to get out of it.
func writeSignInRequiredPage(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusForbidden)
	_, _ = w.Write([]byte(`<!DOCTYPE html>
<html lang="ru">
<head><meta charset="utf-8"><meta name="robots" content="noindex,nofollow">
<title>Требуется вход — Encounter</title></head>
<body>
<h1>Для просмотра этой страницы нужно войти</h1>
<p>Профили игроков, команды и форум доступны участникам Encounter.</p>
<a class="btn" href="` + makeFeeLoginPath + `">Войти</a>
<p class="en">This page is available to signed-in members.<br>
<a class="home" href="` + makeFeeLoginPath + `">Sign in</a> &middot; <a class="home" href="/">Encounter</a></p>
</body>
</html>
`))
}

// handleLoginPage serves the page the redirect points at. The txtLogin and
// txtPassword field names and the login.aspx form action are the markers encx
// keys on (looksLikeLoginPage) to tell an expired session apart from the
// anti-spam wall, so a stub page without them would change the diagnosis.
func (s *server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// The form posts back to the spelling the caller arrived at, the way the
	// engine's own page does.
	action := r.URL.Path
	if returnTo := r.URL.Query().Get("return"); returnTo != "" {
		action = loginPageURL(r.URL.Path, returnTo)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<html><head><title>Encounter</title></head><body>
<form method="post" action=%q>
<input type="text" name="txtLogin" />
<input type="password" name="txtPassword" />
<input type="submit" value="Sign in" />
</form>
</body></html>
`, action)
}

// requireSessionHTML guards the member-only HTML pages. Encounter answers those
// with 403 and a "sign in required" interstitial, not with 401:
//
//	GET /UserDetails.aspx?uid=1&json=1        403, text/html
//	GET /Teams/TeamDetails.aspx?tid=1&json=1  403, text/html
//
// measured on svk.en.cx and confirmed on kharkov.en.cx (2026-09-01), so it is
// the platform's behaviour and not one domain's own front-end. The page links
// to /Login.aspx, which is what makes encx classify it as an expired session
// rather than as an unreadable page.
func (s *server) requireSessionHTML(w http.ResponseWriter, r *http.Request) (*sessionState, bool) {
	st, err := s.sessionFromRequest(r)
	if err != nil {
		writeSignInRequiredPage(w)
		return nil, false
	}
	if s.dropIfSilent(r, sessionAuthKey(st)) {
		return nil, false
	}
	setProtocolCookies(w)
	return st, true
}

func antiBotAnswerAttemptsFromEnv() map[int]bool {
	attempts := make(map[int]bool)
	for _, raw := range strings.Split(os.Getenv("ENCX_MOCK_ANTIBOT_ATTEMPTS"), ",") {
		attempt, err := strconv.Atoi(strings.TrimSpace(raw))
		if err == nil && attempt > 0 {
			attempts[attempt] = true
		}
	}
	return attempts
}

func (s *server) shouldInjectAntiBot(st *sessionState) bool {
	st.AnswerAttempts++
	return s.antiBotAnswerAttempts[st.AnswerAttempts]
}

func (s *server) sessionFromRequest(r *http.Request) (*sessionState, error) {
	c, err := r.Cookie("SESSION_ID")
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.sessions[c.Value]
	if st == nil {
		return nil, errors.New("unknown session")
	}
	return st, nil
}

func (s *server) newSession(st *sessionState) string {
	id := atomic.AddUint64(&s.nextSID, 1)
	sid := fmt.Sprintf("mock-session-%d", id)
	s.mu.Lock()
	s.sessions[sid] = st
	s.mu.Unlock()
	return sid
}

func sessionAuthKey(st *sessionState) string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.AuthKey
}

func extractTailInt(path, prefix string) (int, error) {
	if !strings.HasPrefix(path, prefix) {
		return 0, errors.New("wrong prefix")
	}
	tail := strings.TrimPrefix(path, prefix)
	tail = strings.Trim(tail, "/")
	return strconv.Atoi(tail)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func currentLevelID(st *sessionState) int {
	idx := st.CurrentIdx
	if idx >= len(st.Passed) {
		idx = len(st.Passed) - 1
	}
	if idx < 0 {
		idx = 0
	}
	return mockLevelBaseID + idx + 1
}

func currentLevelNum(st *sessionState) int {
	idx := st.CurrentIdx
	if idx >= len(st.Passed) {
		idx = len(st.Passed) - 1
	}
	if idx < 0 {
		idx = 0
	}
	return idx + 1
}

func mustAtoi(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

func dt(t time.Time) *encx.DateTime {
	return &encx.DateTime{
		Value:     float64(t.Unix()),
		Timestamp: t.Unix(),
	}
}

func credentialsKey(login, password string) string {
	return login + "\x00" + password
}

func (s *server) activateNetworkDrop(authKey string) {
	if authKey == "" {
		return
	}
	until := time.Now().Add(networkDropDuration)
	s.mu.Lock()
	s.silentUntil[authKey] = until
	s.mu.Unlock()
	log.Printf("encx-mock: network drop for %s until %s (code %q)", credentialsKeyForLog(authKey), until.Format(time.RFC3339), networkDropCode)
}

func (s *server) dropIfSilent(r *http.Request, authKey string) bool {
	if authKey == "" {
		return false
	}
	now := time.Now()
	s.mu.Lock()
	until, ok := s.silentUntil[authKey]
	silent := ok && now.Before(until)
	if ok && !silent {
		delete(s.silentUntil, authKey)
	}
	s.mu.Unlock()
	if !silent {
		return false
	}
	log.Printf("encx-mock: dropping %s %s for %s (silent until %s)", r.Method, r.URL.Path, credentialsKeyForLog(authKey), until.Format(time.RFC3339))
	timer := time.NewTimer(networkDropHangMax)
	defer timer.Stop()
	select {
	case <-r.Context().Done():
	case <-timer.C:
	}
	return true
}

func credentialsKeyForLog(authKey string) string {
	login, _, ok := strings.Cut(authKey, "\x00")
	if !ok {
		return "<?>"
	}
	return login + ":***"
}

// routes builds the legacy ASP.NET surface the mock emulates.
func (s *server) routes() *http.ServeMux {
	root := http.NewServeMux()
	mux := tolerantMux{root}
	mux.HandleFunc("POST /login/signin", s.handleLogin)
	mux.HandleFunc("GET /login.aspx", s.handleLoginPage)
	mux.HandleFunc("GET /Login.aspx", s.handleLoginPage)
	mux.HandleFunc("GET /UserDetails.aspx", s.handleUserDetails)
	mux.HandleFunc("GET /home/", s.handleGameList)
	mux.HandleFunc("GET /", s.handleDomainRoot)
	mux.HandleFunc("POST /gameengines/encounter/makefee/Login.aspx", s.handleEnterGame)
	mux.HandleFunc("GET /MakeGameFee.aspx", s.handleMakeGameFee)
	mux.HandleFunc("GET /GameDetails.aspx", s.handleGameDetails)
	mux.HandleFunc("GET /Teams/TeamDetails.aspx", s.handleTeamDetails)
	mux.HandleFunc("GET /gamestatistics/full/", s.handleGameStatistics)
	mux.HandleFunc("GET /gameengines/encounter/play/", s.handleGamePlayGET)
	mux.HandleFunc("POST /gameengines/encounter/play/", s.handleGamePlayPOST)
	mux.HandleFunc("GET /NotHumanRequest.aspx", s.handleNotHuman)
	return root
}
