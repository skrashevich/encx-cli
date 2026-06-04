package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"log"
	"net/http"
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

	networkDropCode     = "PZDC"
	networkDropDuration = time.Minute
	networkDropHangMax  = 2 * time.Second
)

var version = "dev"

type server struct {
	mu          sync.Mutex
	fixtures    *fixtureSet
	scenario    *scenario.Document
	sessions    map[string]*sessionState
	authStates  map[string]*sessionState
	silentUntil map[string]time.Time
	nextSID     uint64
}

type sessionState struct {
	mu              sync.Mutex
	AuthKey         string
	Login           string
	CurrentIdx      int
	Completed       bool
	Passed          []bool
	SectorPassed    [][]bool
	SectorAnswers   [][]string
	LevelStartedAt  []time.Time
	AnsweredBonuses map[int]bool
	BonusAnswers    map[int]string
	Actions         []encx.CodeAction
	LastAction      *encx.EngineAction
	UpdatedAt       time.Time
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-v", "--version", "version":
			fmt.Println("encx-mock", version)
			return
		}
	}

	var scenarioFlag string
	fs := flag.NewFlagSet("encx-mock", flag.ExitOnError)
	fs.StringVar(&scenarioFlag, "scenario", "", "path to scenario file (overrides ENCX_MOCK_SCENARIO)")
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

	addr := strings.TrimSpace(os.Getenv("ENCX_MOCK_ADDR"))
	if addr == "" {
		addr = defaultAddr
	}

	s := &server{
		fixtures:    fixtures,
		scenario:    scenarioDoc,
		sessions:    make(map[string]*sessionState),
		authStates:  make(map[string]*sessionState),
		silentUntil: make(map[string]time.Time),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/signin", s.handleLogin)
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

	if err := http.ListenAndServe(addr, withCommonHeaders(mux)); err != nil {
		log.Fatal(err)
	}
}

func withCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Pragma", "no-cache")
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Login    string `json:"Login"`
		Password string `json:"Password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"Error":   5,
			"Message": "Invalid JSON body",
		})
		return
	}

	authKey := credentialsKey(req.Login, req.Password)
	if s.dropIfSilent(r, authKey) {
		return
	}

	if req.Login == "fail" && req.Password == "fail" {
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
			Login:           req.Login,
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
		state.Login = req.Login
		if len(state.LevelStartedAt) == 0 {
			state.LevelStartedAt = make([]time.Time, s.levelCount())
			s.ensureLevelStarted(state, state.CurrentIdx, time.Now())
		}
		state.mu.Unlock()
	}

	sid := s.newSession(state)
	setMockAuthCookies(w, sid, req.Login)

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
	http.SetCookie(w, &http.Cookie{
		Name:  "stoken",
		Value: "mock-stoken",
		Path:  "/",
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "atoken",
		Value:    fmt.Sprintf("uid%%3d101%%26iss%%3d0%%26iscd%%3d1%%26tkn%%3dmock-%s", login),
		Path:     "/",
		HttpOnly: true,
	})
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

func (s *server) handleGameList(w http.ResponseWriter, r *http.Request) {
	st, ok := s.requireSessionJSON(w, r)
	if !ok {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	now := time.Now()
	gameInfo, err := s.buildGameInfoResponse(st, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"Error": 5, "Message": err.Error()})
		return
	}
	active := []any{}
	if !st.Completed {
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
	_, ok := s.requireSessionHTML(w, r)
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

func (s *server) handleGameDetails(w http.ResponseWriter, r *http.Request) {
	_, ok := s.requireSessionHTML(w, r)
	if !ok {
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

func (s *server) handleGameStatistics(w http.ResponseWriter, r *http.Request) {
	st, ok := s.requireSessionJSON(w, r)
	if !ok {
		return
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

	gameInfo, err := s.buildGameInfoResponse(st, now)
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
	st, ok := s.requireSessionJSON(w, r)
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
}

func (s *server) handleGamePlayPOST(w http.ResponseWriter, r *http.Request) {
	st, ok := s.requireSessionJSON(w, r)
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

	answer := strings.TrimSpace(r.Form.Get("LevelAction.Answer"))
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
	if answer != "" {
		s.processAnswer(st, answer)
	}

	st.UpdatedAt = time.Now()
	s.writeGameModel(w, st)
}

func (s *server) writeGameModel(w http.ResponseWriter, st *sessionState) {
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
	return st, true
}

func (s *server) requireSessionHTML(w http.ResponseWriter, r *http.Request) (*sessionState, bool) {
	st, err := s.sessionFromRequest(r)
	if err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("<html><body>Unauthorized</body></html>"))
		return nil, false
	}
	if s.dropIfSilent(r, sessionAuthKey(st)) {
		return nil, false
	}
	return st, true
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

func ptr[T any](v T) *T {
	return &v
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
