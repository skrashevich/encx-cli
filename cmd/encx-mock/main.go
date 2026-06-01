package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

const (
	defaultAddr = "0.0.0.0:18080"
	mockGameID  = 424242
	mockTeamID  = 5150
)

var levelAnswers = []string{"CODE-1", "CODE-2", "CODE-3"}

type server struct {
	mu         sync.Mutex
	sessions   map[string]*sessionState
	authStates map[string]*sessionState
	nextSID    uint64
}

type sessionState struct {
	Login      string
	CurrentIdx int
	Completed  bool
	Passed     []bool
	Actions    []encx.CodeAction
	LastAction *encx.EngineAction
	UpdatedAt  time.Time
}

func main() {
	addr := strings.TrimSpace(os.Getenv("ENCX_MOCK_ADDR"))
	if addr == "" {
		addr = defaultAddr
	}

	s := &server{
		sessions:   make(map[string]*sessionState),
		authStates: make(map[string]*sessionState),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/signin", s.handleLogin)
	mux.HandleFunc("GET /home/", s.handleGameList)
	mux.HandleFunc("GET /", s.handleDomainRoot)
	mux.HandleFunc("POST /gameengines/encounter/makefee/Login.aspx", s.handleEnterGame)
	mux.HandleFunc("GET /GameDetails.aspx", s.handleGameDetails)
	mux.HandleFunc("GET /Teams/TeamDetails.aspx", s.handleTeamDetails)
	mux.HandleFunc("GET /gamestatistics/full/", s.handleGameStatistics)
	mux.HandleFunc("GET /gameengines/encounter/play/", s.handleGamePlayGET)
	mux.HandleFunc("POST /gameengines/encounter/play/", s.handleGamePlayPOST)
	mux.HandleFunc("GET /NotHumanRequest.aspx", s.handleNotHuman)

	log.Printf("encx-mock listening on http://%s", addr)
	log.Printf("test account: any login/password except fail:fail")
	log.Printf("mock game id=%d, team id=%d, levels=%d", mockGameID, mockTeamID, len(levelAnswers))

	if err := http.ListenAndServe(addr, withCommonHeaders(mux)); err != nil {
		log.Fatal(err)
	}
}

func withCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Login       string `json:"Login"`
		Password    string `json:"Password"`
		DdlNetwork  any    `json:"ddlNetwork"`
		MagicNumber string `json:"MagicNumbers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"Error":   5,
			"Message": "Invalid JSON body",
		})
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
			"AdminWhoCanActivate":  []string{},
		})
		return
	}

	authKey := credentialsKey(req.Login, req.Password)
	s.mu.Lock()
	state := s.authStates[authKey]
	if state == nil {
		state = &sessionState{
			Login:      req.Login,
			CurrentIdx: 0,
			Passed:     make([]bool, len(levelAnswers)),
			Actions:    []encx.CodeAction{},
			UpdatedAt:  time.Now(),
		}
		s.authStates[authKey] = state
	}
	s.mu.Unlock()
	sid := s.newSession(state)
	http.SetCookie(w, &http.Cookie{
		Name:     "SESSION_ID",
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"Error":                0,
		"Message":              "",
		"IpUnblockUrl":         nil,
		"BruteForceUnblockUrl": nil,
		"ConfirmEmailUrl":      nil,
		"CaptchaUrl":           nil,
		"AdminWhoCanActivate":  []string{},
	})
}

func (s *server) handleGameList(w http.ResponseWriter, r *http.Request) {
	st, ok := s.requireSessionJSON(w, r)
	if !ok {
		return
	}
	now := time.Now()
	game := buildGameInfoForState(st, now)
	resp := encx.GameListResponse{
		ComingGames: []encx.GameInfo{},
		ActiveGames: func() []encx.GameInfo {
			if st.Completed {
				return []encx.GameInfo{}
			}
			return []encx.GameInfo{game}
		}(),
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) handleDomainRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<html><body>
<h1 class="gametitle"><a href="/details/%d/">Mock Game 2026</a></h1>
<a id="lnkGameTitle" href="/GameDetails.aspx?gid=%d">Mock Game 2026</a>
</body></html>`, mockGameID, mockGameID)
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
	_, _ = fmt.Fprintf(w, `<html><body><h1>Mock Game %d</h1><p>Team accepted: yes</p><p>Levels: %d</p></body></html>`, mockGameID, len(levelAnswers))
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
	gameID, err := extractTailInt(r.URL.Path, "/gamestatistics/full/")
	if err != nil || gameID != mockGameID {
		http.NotFound(w, r)
		return
	}
	now := time.Now()

	stats := make([][]encx.StatItem, len(levelAnswers))
	for i := range levelAnswers {
		stats[i] = []encx.StatItem{
			{
				ActionTime:   dt(now.Add(time.Duration(i+1) * time.Minute)),
				UserId:       101,
				LevelId:      levelID(i),
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

	levels := make([]encx.LevelStatInfo, len(levelAnswers))
	levelPlayers := make([]encx.LevelPlayerCount, len(levelAnswers))
	for i := range levelAnswers {
		levels[i] = encx.LevelStatInfo{
			LevelId:       levelID(i),
			LevelNumber:   i + 1,
			LevelName:     fmt.Sprintf("Mock level %d", i+1),
			Dismissed:     false,
			PassedPlayers: 1,
		}
		levelPlayers[i] = encx.LevelPlayerCount{
			LevelNum: i + 1,
			Count:    1,
		}
	}

	curIdx := st.CurrentIdx
	if curIdx >= len(levelAnswers) {
		curIdx = len(levelAnswers) - 1
	}

	resp := encx.GameStatisticsResponse{
		Game:               ptr(buildGameInfoForState(st, now)),
		Level:              &levels[curIdx],
		StatItems:          stats,
		Levels:             levels,
		IsLevelNamesVisible: true,
		LevelPlayers:       levelPlayers,
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

func (s *server) handleGamePlayGET(w http.ResponseWriter, r *http.Request) {
	st, ok := s.requireSessionJSON(w, r)
	if !ok {
		return
	}
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
			GameId: mockGameID,
			LevelId: currentLevelID(st),
		}
	}
	s.touch(st)
	writeJSON(w, http.StatusOK, s.buildGameModel(st))
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
	if answer != "" {
		idx := st.CurrentIdx
		if idx >= len(levelAnswers) {
			idx = len(levelAnswers) - 1
		}
		expected := levelAnswers[idx]
		correct := strings.EqualFold(answer, expected)

		if correct && idx < len(st.Passed) {
			st.Passed[idx] = true
			if st.CurrentIdx < len(levelAnswers)-1 {
				st.CurrentIdx++
			} else {
				st.Completed = true
			}
		}

		st.Actions = append(st.Actions, encx.CodeAction{
			ActionId:    len(st.Actions) + 1,
			LevelId:     levelID(idx),
			LevelNumber: idx + 1,
			UserId:      101,
			Kind:        1,
			Login:       st.Login,
			Answer:      answer,
			LocDateTime: time.Now().Format("2006-01-02 15:04:05"),
			IsCorrect:   correct,
			Penalty:     0,
			Negative:    false,
		})

		st.LastAction = &encx.EngineAction{
			LevelNumber: idx + 1,
			LevelAction: &encx.ActionResult{
				Answer:          ptr(answer),
				IsCorrectAnswer: ptr(correct),
			},
			GameId:  mockGameID,
			LevelId: levelID(idx),
		}
	}

	s.touch(st)
	writeJSON(w, http.StatusOK, s.buildGameModel(st))
}

func (s *server) handleNotHuman(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<html><body><h1>Anti-spam check</h1><p>Mock page.</p></body></html>`))
}

func (s *server) buildGameModel(st *sessionState) encx.GameModel {
	now := time.Now()
	lvlSummaries := make([]encx.LevelSummary, len(levelAnswers))
	for i := range levelAnswers {
		lvlSummaries[i] = encx.LevelSummary{
			LevelId:     levelID(i),
			LevelNumber: i + 1,
			LevelName:   fmt.Sprintf("Mock level %d", i+1),
			Dismissed:   false,
			IsPassed:    st.Passed[i],
			Task: &encx.LevelTask{
				TaskText:          fmt.Sprintf("Find code for level %d", i+1),
				TaskTextFormatted: fmt.Sprintf("<p>Find code for level %d</p>", i+1),
				ReplaceNlToBr:     true,
			},
			LevelAction: nil,
		}
	}

	idx := st.CurrentIdx
	if idx >= len(levelAnswers) {
		idx = len(levelAnswers) - 1
	}
	curPassed := st.Passed[idx]
	curAnswer := ""
	if curPassed {
		curAnswer = levelAnswers[idx]
	}

	model := encx.GameModel{
		Event:             func() any { if st.Completed { return 100 } ; return 0 }(),
		GameId:            mockGameID,
		GameNumber:        1,
		GameTitle:         "Mock Game 2026",
		GameTypeId:        1,
		GameZoneId:        0,
		LevelSequence:     1,
		UserId:            101,
		TeamId:            mockTeamID,
		Login:             st.Login,
		TeamName:          "MockTeam",
		IsCaptain:         true,
		GameDateTimeStart: now.Format("2006-01-02 15:04:05"),
		Levels:            lvlSummaries,
		EngineAction:      st.LastAction,
		Level: &encx.Level{
			LevelId:              levelID(idx),
			Number:               idx + 1,
			Name:                 func() string { if st.Completed { return "Финиш — игра завершена" } ; return fmt.Sprintf("Mock level %d", idx+1) }(),
			Timeout:              1800,
			TimeoutSecondsRemain: 1200,
			TimeoutAward:         0,
			IsPassed:             curPassed,
			Dismissed:            false,
			StartTime:            dt(now.Add(-10 * time.Minute)),
			HasAnswerBlockRule:   false,
			BlockDuration:        0,
			BlockTargetId:        0,
			AttemtsNumber:        0,
			AttemtsPeriod:        0,
			RequiredSectorsCount: 1,
			PassedSectorsCount:   btoi(curPassed),
			PassedBonusesCount:   0,
			SectorsLeftToClose:   1 - btoi(curPassed),
			Tasks: []encx.LevelTask{
				{
					TaskText:          fmt.Sprintf("Answer is %s (for automated tests)", levelAnswers[idx]),
					TaskTextFormatted: fmt.Sprintf("<p>Answer is <b>%s</b> (for automated tests)</p>", levelAnswers[idx]),
					ReplaceNlToBr:     true,
				},
			},
			Task: &encx.LevelTask{
				TaskText:          fmt.Sprintf("Answer is %s (for automated tests)", levelAnswers[idx]),
				TaskTextFormatted: fmt.Sprintf("<p>Answer is <b>%s</b> (for automated tests)</p>", levelAnswers[idx]),
				ReplaceNlToBr:     true,
			},
			Messages: []encx.AdminMessage{
				{
					OwnerId:      1,
					OwnerLogin:   "mock-admin",
					MessageId:    1,
					MessageText:  func() string { if st.Completed { return "Игра завершена. Все коды приняты." } ; return "Test mode enabled" }(),
					WrappedText:  func() string { if st.Completed { return "Игра завершена. Все коды приняты." } ; return "Test mode enabled" }(),
					ReplaceNl2Br: true,
				},
			},
			Sectors: []encx.Sector{
				{
					SectorId:   levelID(idx)*10 + 1,
					Order:      1,
					Name:       "Main sector",
					IsAnswered: curPassed || st.Completed,
					Answer:     encx.FlexString(curAnswer),
				},
			},
			Helps: []encx.Help{
				{
					HelpId:           101,
					Number:           1,
					HelpText:         ptr("Try CODE-N format"),
					IsPenalty:        false,
					Penalty:          0,
					PenaltyComment:   nil,
					RequestConfirm:   false,
					PenaltyHelpState: 1,
					RemainSeconds:    0,
					PenaltyMessage:   nil,
				},
			},
			Bonuses: []encx.Bonus{},
			PenaltyHelps: []encx.Help{
				{
					HelpId:           201,
					Number:           1,
					HelpText:         ptr("Penalty hint content"),
					IsPenalty:        true,
					Penalty:          30,
					PenaltyComment:   ptr("Penalty 30 sec"),
					RequestConfirm:   true,
					PenaltyHelpState: 1,
					RemainSeconds:    0,
					PenaltyMessage:   ptr("Penalty accepted"),
				},
			},
			MixedActions: st.Actions,
		},
	}

	return model
}

func buildGameInfo(_ string, now time.Time) encx.GameInfo {
	return encx.GameInfo{
		GameID:                mockGameID,
		GameNum:               1,
		SiteID:                1,
		LangID:                1,
		CompetitionID:         0,
		OwnerID:               1,
		LevelNumber:           len(levelAnswers),
		CreateDateTime:        dt(now.Add(-24 * time.Hour)),
		StartDateTime:         dt(now.Add(-1 * time.Hour)),
		FinishDateTime:        dt(now.Add(3 * time.Hour)),
		Title:                 "Mock Game 2026",
		Descr:                 "Single always-available game for integration tests",
		DescrWrapped:          "<p>Single always-available game for integration tests</p>",
		GameTypeID:            1,
		ZoneId:                0,
		LevelsSequence:        1,
		ScenarioAvailability:  1,
		MaxPlayers:            100,
		MaxTeamMembers:        10,
		ShowInCalendar:        true,
		FeeType:               0,
		FeeCurrencyId:         1,
		FeeName:               "RUB",
		ShowFee:               1,
		Fee:                   &encx.Money{Cents: 0, Value: 0, Formated: "0"},
		Prize:                 &encx.Money{Cents: 0, Value: 0, Formated: "0"},
		PrizeType:             0,
		PrizeTypeSymbol:       "",
		TSRemain:              dur(3 * time.Hour),
		Started:               true,
		Finished:              false,
		InProgress:            true,
		IsSectorsSupported:    true,
		IsOnlineStatAvailable: true,
		IsComplexitySupported: false,
		IsModerated:           false,
		ComplexityFactor:      1,
		ComplexityMembersFactor: 1,
		QualityRate:           100,
		QualityRateFormatted:  "100",
		TopicId:               1,
		AcceptRateFromDateTime: dt(now.Add(-2 * time.Hour)),
		RequestLastDate:       dt(now.Add(2 * time.Hour)),
		HideLevelsNames:       false,
		AlwaysAvailable:       true,
		PublicAccess:          true,
		DisplayMonitoring:     0,
	}
}

func buildGameInfoForState(st *sessionState, now time.Time) encx.GameInfo {
	g := buildGameInfo(st.Login, now)
	if st.Completed {
		g.Finished = true
		g.InProgress = false
	}
	return g
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

func (s *server) touch(st *sessionState) {
	s.mu.Lock()
	st.UpdatedAt = time.Now()
	s.mu.Unlock()
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

func levelID(idx int) int {
	return 1000 + idx + 1
}

func currentLevelID(st *sessionState) int {
	idx := st.CurrentIdx
	if idx >= len(levelAnswers) {
		idx = len(levelAnswers) - 1
	}
	return levelID(idx)
}

func currentLevelNum(st *sessionState) int {
	idx := st.CurrentIdx
	if idx >= len(levelAnswers) {
		idx = len(levelAnswers) - 1
	}
	return idx + 1
}

func mustAtoi(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}

func dt(t time.Time) *encx.DateTime {
	return &encx.DateTime{
		Value:     float64(t.Unix()),
		Timestamp: t.Unix(),
	}
}

func dur(d time.Duration) *encx.Duration {
	totalSeconds := d.Seconds()
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return &encx.Duration{
		Days:         h / 24,
		Hours:        h % 24,
		Minutes:      m,
		Seconds:      s,
		TotalSeconds: totalSeconds,
	}
}

func ptr[T any](v T) *T {
	return &v
}

func credentialsKey(login, password string) string {
	return login + "\x00" + password
}
