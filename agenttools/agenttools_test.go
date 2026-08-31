package agenttools

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/tools"
	"github.com/skrashevich/encx-cli/encx"
)

// stubEngine is a scripted Engine: every method returns whatever the test put in
// the matching field and records that it was called.
type stubEngine struct {
	profile     *encx.Profile
	team        *encx.TeamManagementInfo
	domainGames []encx.DomainGame
	gameList    *encx.GameListResponse
	statistics  *encx.GameStatisticsResponse
	timeout     *int
	model       *encx.GameModel
	levelModel  *encx.GameModel
	sendResult  *encx.GameModel
	hintResult  *encx.GameModel
	resource    *encx.Resource
	enterBody   string
	err         error

	// The pacing tests drive the stub from several goroutines at once, the way
	// an LLM runtime does.
	mu    sync.Mutex
	calls []string
}

func (s *stubEngine) record(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, name)
}

func (s *stubEngine) recorded() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func (s *stubEngine) called(name string) bool {
	for _, call := range s.recorded() {
		if call == name {
			return true
		}
	}
	return false
}

func (s *stubEngine) GetDomainGames(context.Context) ([]encx.DomainGame, error) {
	s.record("GetDomainGames")
	return s.domainGames, s.err
}

func (s *stubEngine) GetGameList(_ context.Context, page ...int) (*encx.GameListResponse, error) {
	if len(page) > 0 {
		s.record("GetGameList/page")
	} else {
		s.record("GetGameList")
	}
	return s.gameList, s.err
}

func (s *stubEngine) GetGameModel(context.Context, int, ...url.Values) (*encx.GameModel, error) {
	s.record("GetGameModel")
	return s.model, s.err
}

func (s *stubEngine) GetGameModelLevel(context.Context, int, int) (*encx.GameModel, error) {
	s.record("GetGameModelLevel")
	if s.levelModel != nil {
		return s.levelModel, s.err
	}
	return s.model, s.err
}

func (s *stubEngine) GetGameStatistics(context.Context, int) (*encx.GameStatisticsResponse, error) {
	s.record("GetGameStatistics")
	return s.statistics, s.err
}

func (s *stubEngine) GetTimeoutToGame(context.Context, int) (*int, error) {
	s.record("GetTimeoutToGame")
	return s.timeout, s.err
}

func (s *stubEngine) GetProfile(context.Context) (*encx.Profile, error) {
	s.record("GetProfile")
	return s.profile, s.err
}

func (s *stubEngine) GetTeamManagementInfo(context.Context, int) (*encx.TeamManagementInfo, error) {
	s.record("GetTeamManagementInfo")
	return s.team, s.err
}

func (s *stubEngine) EnterGame(context.Context, int) (string, error) {
	s.record("EnterGame")
	return s.enterBody, s.err
}

func (s *stubEngine) SendCode(_ context.Context, _, _, _ int, _ string) (*encx.GameModel, error) {
	s.record("SendCode")
	return s.sendResult, s.err
}

func (s *stubEngine) SendBonusCode(_ context.Context, _, _, _ int, _ string) (*encx.GameModel, error) {
	s.record("SendBonusCode")
	return s.sendResult, s.err
}

func (s *stubEngine) GetPenaltyHint(context.Context, int, int) (*encx.GameModel, error) {
	s.record("GetPenaltyHint")
	return s.hintResult, s.err
}

func (s *stubEngine) FetchResource(_ context.Context, rawURL string, _ ...encx.ResourceOptions) (*encx.Resource, error) {
	s.record("FetchResource")
	if s.resource != nil {
		return s.resource, s.err
	}
	return &encx.Resource{URL: rawURL, ContentType: "image/png", Data: []byte("png-bytes")}, s.err
}

func playableEngine() *stubEngine {
	level := &encx.Level{
		LevelId:              77,
		Number:               3,
		Name:                 "Третий уровень",
		RequiredSectorsCount: 2,
		PassedSectorsCount:   1,
		SectorsLeftToClose:   1,
		Tasks:                []encx.LevelTask{{TaskText: "Найдите табличку"}},
		Sectors: []encx.Sector{
			{SectorId: 1, Order: 1, Name: "Сектор 1", IsAnswered: true, Answer: encx.FlexString("alpha")},
			{SectorId: 2, Order: 2, Name: "Сектор 2"},
		},
		PenaltyHelps: []encx.Help{{HelpId: 9, Number: 1, IsPenalty: true, Penalty: 300}},
		MixedActions: []encx.CodeAction{{ActionId: 5, LevelNumber: 3, Kind: 1, Answer: "alpha", IsCorrect: true}},
	}
	model := &encx.GameModel{
		GameId:    42,
		GameTitle: "Тестовая игра",
		TeamId:    11,
		TeamName:  "Команда",
		Levels:    []encx.LevelSummary{{LevelId: 77, LevelNumber: 3, LevelName: "Третий уровень"}},
		Level:     level,
	}
	correct := true
	answer := "bravo"
	return &stubEngine{
		profile: &encx.Profile{ID: 1, Login: "player", TeamID: 11, Team: "Команда"},
		team:    &encx.TeamManagementInfo{TeamID: 11, TeamName: "Команда"},
		model:   model,
		sendResult: &encx.GameModel{
			GameId: 42,
			Level:  level,
			EngineAction: &encx.EngineAction{
				LevelNumber: 3,
				LevelAction: &encx.ActionResult{Answer: &answer, IsCorrectAnswer: &correct},
				BonusAction: &encx.ActionResult{Answer: &answer, IsCorrectAnswer: &correct},
			},
		},
		hintResult: model,
	}
}

func newTestCatalog(t *testing.T, engine Engine, opts Options) *Catalog {
	t.Helper()
	catalog, err := NewCatalog(engine, opts)
	if err != nil {
		t.Fatalf("NewCatalog: %v", err)
	}
	return catalog
}

func allowAll() Confirmer {
	return ConfirmerFunc(func(context.Context, ConfirmRequest) (bool, error) { return true, nil })
}

func toolNames(list []*Tool) []string {
	names := make([]string, 0, len(list))
	for _, tool := range list {
		names = append(names, tool.Name())
	}
	return names
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestParsePolicy(t *testing.T) {
	for input, want := range map[string]Policy{
		"":         DefaultPolicy,
		"readonly": PolicyReadonly,
		"approve":  PolicyApprove,
		"full":     PolicyFull,
	} {
		got, err := ParsePolicy(input)
		if err != nil {
			t.Fatalf("ParsePolicy(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("ParsePolicy(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := ParsePolicy("yolo"); err == nil {
		t.Fatal("ParsePolicy(\"yolo\") should fail")
	}
}

func TestNewCatalogValidatesInput(t *testing.T) {
	if _, err := NewCatalog(nil, Options{Policy: PolicyFull}); err == nil {
		t.Fatal("a nil engine should be rejected")
	}
	if _, err := NewCatalog(playableEngine(), Options{Policy: PolicyApprove}); err == nil {
		t.Fatal("approve policy without a confirmer should be rejected")
	}
	if _, err := NewCatalog(playableEngine(), Options{Policy: "nonsense"}); err == nil {
		t.Fatal("an unknown policy should be rejected")
	}
}

func TestCatalogClassifiesTools(t *testing.T) {
	catalog := newTestCatalog(t, playableEngine(), Options{Policy: PolicyFull})

	var read, mutating []string
	for _, tool := range catalog.All() {
		if tool.Mutating() {
			mutating = append(mutating, tool.Name())
		} else {
			read = append(read, tool.Name())
		}
	}

	if len(read) < 8 {
		t.Fatalf("want at least 8 read tools, got %d: %v", len(read), read)
	}
	wantMutating := []string{toolSendCode, toolSendBonusCode, toolPenaltyHint, toolEnterGame}
	if len(mutating) != len(wantMutating) {
		t.Fatalf("want exactly %d mutating tools, got %v", len(wantMutating), mutating)
	}
	for _, name := range wantMutating {
		if !contains(mutating, name) {
			t.Fatalf("%s should be classified as mutating; got %v", name, mutating)
		}
	}
	for _, name := range []string{toolGameState, toolLevel, toolStatistics, toolProfile, toolTeam, toolActionLog, toolGameList, toolDomainGames} {
		if !contains(read, name) {
			t.Fatalf("%s should be present as a read tool; got %v", name, read)
		}
	}
}

func TestReadonlyPolicyHidesMutatingTools(t *testing.T) {
	catalog := newTestCatalog(t, playableEngine(), Options{Policy: PolicyReadonly})

	exposed := toolNames(catalog.Tools())
	for _, name := range []string{toolSendCode, toolSendBonusCode, toolPenaltyHint, toolEnterGame} {
		if contains(exposed, name) {
			t.Fatalf("%s must not be exposed under the read-only policy: %v", name, exposed)
		}
	}
	if len(catalog.All()) <= len(exposed) {
		t.Fatal("All() should still report the hidden mutating tools")
	}
}

func TestReadonlyPolicyRefusesDirectMutatingCall(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})

	tool, ok := catalog.Lookup(toolSendCode)
	if !ok {
		t.Fatalf("%s should still be resolvable by name", toolSendCode)
	}
	result := tool.Execute(context.Background(), map[string]any{"game_id": 42, "code": "bravo"})
	if !result.IsError {
		t.Fatal("a mutating call under read-only should return an error result")
	}
	if !strings.Contains(result.ForLLM, "read-only") {
		t.Fatalf("the refusal should explain the policy, got %q", result.ForLLM)
	}
	if engine.called("SendCode") {
		t.Fatal("the engine must not be reached under the read-only policy")
	}
}

func TestApprovePolicyGrantsAndSubmits(t *testing.T) {
	engine := playableEngine()
	var seen ConfirmRequest
	catalog := newTestCatalog(t, engine, Options{
		Policy: PolicyApprove,
		Confirmer: ConfirmerFunc(func(_ context.Context, req ConfirmRequest) (bool, error) {
			seen = req
			return true, nil
		}),
	})

	tool, _ := catalog.Lookup(toolSendCode)
	result := tool.Execute(context.Background(), map[string]any{"game_id": 42, "code": "bravo"})
	if result.IsError {
		t.Fatalf("an approved submission should succeed, got %q", result.ForLLM)
	}
	if seen.Tool != toolSendCode || seen.Args["code"] != "bravo" {
		t.Fatalf("the confirmer should receive the call and its arguments, got %+v", seen)
	}
	if !engine.called("SendCode") {
		t.Fatal("an approved submission should reach the engine")
	}

	var payload actionResultView
	if err := json.Unmarshal([]byte(result.ForLLM), &payload); err != nil {
		t.Fatalf("the result should be JSON: %v (%q)", err, result.ForLLM)
	}
	if payload.IsCorrect == nil || !*payload.IsCorrect {
		t.Fatalf("the engine verdict should be carried through, got %+v", payload)
	}
}

func TestApprovePolicyDenialSkipsEngine(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{
		Policy:    PolicyApprove,
		Confirmer: ConfirmerFunc(func(context.Context, ConfirmRequest) (bool, error) { return false, nil }),
	})

	tool, _ := catalog.Lookup(toolSendBonusCode)
	result := tool.Execute(context.Background(), map[string]any{"game_id": 42, "code": "bravo"})
	if !result.IsError {
		t.Fatal("a declined submission should return an error result")
	}
	if !strings.Contains(result.ForLLM, "declined") {
		t.Fatalf("the refusal should say the user declined, got %q", result.ForLLM)
	}
	if engine.called("SendBonusCode") {
		t.Fatal("a declined submission must not reach the engine")
	}
}

func TestApprovePolicyPropagatesConfirmerFailure(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{
		Policy: PolicyApprove,
		Confirmer: ConfirmerFunc(func(context.Context, ConfirmRequest) (bool, error) {
			return false, errors.New("the chat went away")
		}),
	})

	tool, _ := catalog.Lookup(toolPenaltyHint)
	result := tool.Execute(context.Background(), map[string]any{"game_id": 42, "hint_id": 9})
	if !result.IsError || !strings.Contains(result.ForLLM, "the chat went away") {
		t.Fatalf("a confirmer failure should surface to the model, got %q", result.ForLLM)
	}
	if engine.called("GetPenaltyHint") {
		t.Fatal("an unconfirmed hint must not reach the engine")
	}
}

func TestFullPolicySkipsConfirmation(t *testing.T) {
	engine := playableEngine()
	confirmed := false
	catalog := newTestCatalog(t, engine, Options{
		Policy: PolicyFull,
		Confirmer: ConfirmerFunc(func(context.Context, ConfirmRequest) (bool, error) {
			confirmed = true
			return true, nil
		}),
	})

	tool, _ := catalog.Lookup(toolEnterGame)
	if result := tool.Execute(context.Background(), map[string]any{"game_id": 42}); result.IsError {
		t.Fatalf("joining a game under the full policy should succeed, got %q", result.ForLLM)
	}
	if confirmed {
		t.Fatal("the full policy should not ask for confirmation")
	}
	if !engine.called("EnterGame") {
		t.Fatal("the engine should be reached under the full policy")
	}
}

func TestReadToolsReturnProjectedJSON(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})

	stateTool, _ := catalog.Lookup(toolGameState)
	result := stateTool.Execute(context.Background(), map[string]any{"game_id": 42})
	if result.IsError {
		t.Fatalf("reading game state should succeed, got %q", result.ForLLM)
	}
	var state gameStateView
	if err := json.Unmarshal([]byte(result.ForLLM), &state); err != nil {
		t.Fatalf("game state should be JSON: %v", err)
	}
	if state.GameID != 42 || state.CurrentLevel == nil || state.CurrentLevel.Number != 3 {
		t.Fatalf("game state projection is wrong: %+v", state)
	}
	if len(state.CurrentLevel.Sectors) != 2 || state.CurrentLevel.Sectors[0].Answer != "alpha" {
		t.Fatalf("sectors should be projected with answers: %+v", state.CurrentLevel.Sectors)
	}

	logTool, _ := catalog.Lookup(toolActionLog)
	logResult := logTool.Execute(context.Background(), map[string]any{"game_id": 42})
	if logResult.IsError || !strings.Contains(logResult.ForLLM, `"answer":"alpha"`) {
		t.Fatalf("the action log should carry submitted codes, got %q", logResult.ForLLM)
	}
}

func TestLevelToolPicksRequestedLevel(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})
	tool, _ := catalog.Lookup(toolLevel)

	if result := tool.Execute(context.Background(), map[string]any{"game_id": 42}); result.IsError {
		t.Fatalf("the active level should be readable, got %q", result.ForLLM)
	}
	if engine.called("GetGameModelLevel") {
		t.Fatal("without level_number the active level endpoint should be used")
	}

	if result := tool.Execute(context.Background(), map[string]any{"game_id": 42, "level_number": 2}); result.IsError {
		t.Fatalf("a specific level should be readable, got %q", result.ForLLM)
	}
	if !engine.called("GetGameModelLevel") {
		t.Fatal("level_number should route to the per-level endpoint")
	}
}

func TestTeamToolFallsBackToProfileTeam(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})
	tool, _ := catalog.Lookup(toolTeam)

	if result := tool.Execute(context.Background(), map[string]any{}); result.IsError {
		t.Fatalf("the team should be resolvable from the profile, got %q", result.ForLLM)
	}
	if !engine.called("GetProfile") || !engine.called("GetTeamManagementInfo") {
		t.Fatalf("the profile should supply the team id, calls: %v", engine.calls)
	}
}

func TestMissingArgumentsAreReportedNotPanicked(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyFull})

	tool, _ := catalog.Lookup(toolSendCode)
	result := tool.Execute(context.Background(), map[string]any{"game_id": 42})
	if !result.IsError || !strings.Contains(result.ForLLM, `"code"`) {
		t.Fatalf("a missing code should be reported as an error, got %q", result.ForLLM)
	}
	if engine.called("SendCode") {
		t.Fatal("an invalid call must not reach the engine")
	}
}

func TestArgumentsAcceptStringifiedNumbers(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})
	tool, _ := catalog.Lookup(toolGameState)

	if result := tool.Execute(context.Background(), map[string]any{"game_id": "42"}); result.IsError {
		t.Fatalf("providers that stringify numbers should still work, got %q", result.ForLLM)
	}
}

func TestSendCodeRequiresAnAnsweringLevel(t *testing.T) {
	engine := playableEngine()
	engine.model.Level.IsPassed = true
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyFull})

	tool, _ := catalog.Lookup(toolSendCode)
	result := tool.Execute(context.Background(), map[string]any{"game_id": 42, "code": "bravo"})
	if !result.IsError {
		t.Fatal("a passed level should refuse level answers")
	}
	if engine.called("SendCode") {
		t.Fatal("a blocked level must not receive a submission")
	}
}

func TestParametersSchemaIsCopiedAndDescribesRequiredArguments(t *testing.T) {
	catalog := newTestCatalog(t, playableEngine(), Options{Policy: PolicyFull})
	tool, _ := catalog.Lookup(toolSendCode)

	params := tool.Parameters()
	if params["type"] != "object" {
		t.Fatalf("the schema should be an object, got %v", params["type"])
	}
	required, _ := params["required"].([]string)
	if !contains(required, "game_id") || !contains(required, "code") {
		t.Fatalf("game_id and code should be required, got %v", params["required"])
	}
	properties, _ := params["properties"].(map[string]any)
	if len(properties) != 2 {
		t.Fatalf("send_code should declare two properties, got %v", properties)
	}

	// Mutating the returned schema must not corrupt the catalog.
	properties["injected"] = "boom"
	if fresh, _ := tool.Parameters()["properties"].(map[string]any); len(fresh) != 2 {
		t.Fatalf("Parameters() should return a copy, got %v", fresh)
	}
}

func TestRegisterExposesToolsToPicoclaw(t *testing.T) {
	registry := tools.NewToolRegistry()
	catalog := newTestCatalog(t, playableEngine(), Options{Policy: PolicyReadonly})
	catalog.Register(registry)

	defs := registry.ToProviderDefs()
	if len(defs) != len(catalog.Tools()) {
		t.Fatalf("registry should hold %d tools, got %d", len(catalog.Tools()), len(defs))
	}
	for _, def := range defs {
		if !strings.HasPrefix(def.Function.Name, "enc_") {
			t.Fatalf("unexpected tool in the registry: %q", def.Function.Name)
		}
	}
}

func TestReadCacheServesRepeatedReadsWithinATurn(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly, ReadCacheTTL: time.Minute})
	tool, _ := catalog.Lookup(toolGameState)

	for range 3 {
		if result := tool.Execute(context.Background(), map[string]any{"game_id": 42}); result.IsError {
			t.Fatalf("reading game state should succeed, got %q", result.ForLLM)
		}
	}

	// A model commonly re-reads the same facts several times inside one answer;
	// each of those is a paced round trip to a slow engine.
	if got := countCalls(engine, "GetGameModel"); got != 1 {
		t.Fatalf("repeated identical reads should hit the engine once, got %d", got)
	}
}

func TestReadCacheKeysOnArguments(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly, ReadCacheTTL: time.Minute})
	tool, _ := catalog.Lookup(toolLevel)

	tool.Execute(context.Background(), map[string]any{"game_id": 42, "level_number": 2})
	tool.Execute(context.Background(), map[string]any{"game_id": 42, "level_number": 3})

	// Different levels are different reads and must not share an entry.
	if got := countCalls(engine, "GetGameModelLevel"); got != 2 {
		t.Fatalf("different arguments should each reach the engine, got %d", got)
	}
}

func TestMutationInvalidatesTheReadCache(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyFull, ReadCacheTTL: time.Minute})
	state, _ := catalog.Lookup(toolGameState)
	send, _ := catalog.Lookup(toolSendCode)

	state.Execute(context.Background(), map[string]any{"game_id": 42})
	send.Execute(context.Background(), map[string]any{"game_id": 42, "code": "bravo"})
	state.Execute(context.Background(), map[string]any{"game_id": 42})

	// Submitting a code can pass the level, so every cached read is suspect.
	// GetGameModel is called by the read twice plus once inside send_code.
	if got := countCalls(engine, "GetGameModel"); got != 3 {
		t.Fatalf("a mutation should invalidate cached reads, got %d calls", got)
	}
}

func TestInvalidateCacheForcesAFreshRead(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly, ReadCacheTTL: time.Minute})
	tool, _ := catalog.Lookup(toolGameState)

	tool.Execute(context.Background(), map[string]any{"game_id": 42})
	catalog.InvalidateCache()
	tool.Execute(context.Background(), map[string]any{"game_id": 42})

	if got := countCalls(engine, "GetGameModel"); got != 2 {
		t.Fatalf("an invalidated cache should re-read, got %d calls", got)
	}
}

func TestReadCacheExpires(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly, ReadCacheTTL: time.Minute})
	tool, _ := catalog.Lookup(toolGameState)

	tool.Execute(context.Background(), map[string]any{"game_id": 42})

	// The game keeps moving, so an entry must not outlive its TTL.
	catalog.cache.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	tool.Execute(context.Background(), map[string]any{"game_id": 42})

	if got := countCalls(engine, "GetGameModel"); got != 2 {
		t.Fatalf("a stale entry should be re-read, got %d calls", got)
	}
}

func TestCachingIsOffByDefault(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})
	tool, _ := catalog.Lookup(toolGameState)

	tool.Execute(context.Background(), map[string]any{"game_id": 42})
	tool.Execute(context.Background(), map[string]any{"game_id": 42})

	if got := countCalls(engine, "GetGameModel"); got != 2 {
		t.Fatalf("without a TTL every read should reach the engine, got %d", got)
	}
	catalog.InvalidateCache() // must not panic on a catalog with no cache
}

func countCalls(engine *stubEngine, name string) int {
	count := 0
	for _, call := range engine.recorded() {
		if call == name {
			count++
		}
	}
	return count
}

func TestLevelReportsItsImages(t *testing.T) {
	engine := playableEngine()
	engine.model.Level.Tasks = []encx.LevelTask{{
		TaskText: `Опознайте здание: <img src="/upload/task.jpg" width="600">`,
	}}
	engine.model.Level.Bonuses = []encx.Bonus{{
		BonusId: 1,
		Name:    "Бонус",
		Task:    `<a href="https://img.en.cx/photo.png">фото</a>`,
	}}
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})
	tool, _ := catalog.Lookup(toolLevel)

	result := tool.Execute(context.Background(), map[string]any{"game_id": 42})
	if result.IsError {
		t.Fatalf("reading the level should succeed, got %q", result.ForLLM)
	}

	var level levelView
	if err := json.Unmarshal([]byte(result.ForLLM), &level); err != nil {
		t.Fatalf("the level should be JSON: %v", err)
	}
	// Without this the agent cannot know a picture exists, and on many levels the
	// picture is the whole task.
	if len(level.Images) != 2 {
		t.Fatalf("both the task and bonus images should be reported, got %+v", level.Images)
	}
	var wheres []string
	for _, image := range level.Images {
		wheres = append(wheres, image.Where)
	}
	if !contains(wheres, "task") {
		t.Fatalf("an image should say where it came from, got %v", wheres)
	}
}

func TestViewImageReturnsThePictureToTheModel(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})
	tool, ok := catalog.Lookup(toolViewImage)
	if !ok {
		t.Fatal("the catalog should expose an image viewer")
	}

	result := tool.Execute(context.Background(), map[string]any{"url": "/upload/task.jpg"})
	if result.IsError {
		t.Fatalf("viewing an image should succeed, got %q", result.ForLLM)
	}
	if !engine.called("FetchResource") {
		t.Fatal("the image should be fetched through the authenticated session")
	}

	// A link the model cannot open is useless; the picture has to travel as media.
	if len(result.Media) != 1 {
		t.Fatalf("the image should be attached as media, got %v", result.Media)
	}
	if !strings.HasPrefix(result.Media[0], "data:image/png;base64,") {
		t.Fatalf("media should be an inline image, got %.40q", result.Media[0])
	}
}

func TestViewImageRejectsNonImages(t *testing.T) {
	engine := playableEngine()
	engine.resource = &encx.Resource{
		URL:         "/upload/task.html",
		ContentType: "text/html",
		Data:        []byte("<html></html>"),
	}
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly})
	tool, _ := catalog.Lookup(toolViewImage)

	result := tool.Execute(context.Background(), map[string]any{"url": "/upload/task.html"})
	if !result.IsError {
		t.Fatalf("a non-image should be refused, got %q", result.ForLLM)
	}
	if len(result.Media) != 0 {
		t.Fatal("a refused fetch must not attach media")
	}
}

func TestViewImageIsNotCached(t *testing.T) {
	engine := playableEngine()
	catalog := newTestCatalog(t, engine, Options{Policy: PolicyReadonly, ReadCacheTTL: time.Minute})
	tool, _ := catalog.Lookup(toolViewImage)

	tool.Execute(context.Background(), map[string]any{"url": "/upload/task.jpg"})
	tool.Execute(context.Background(), map[string]any{"url": "/upload/task.jpg"})

	// Caching would pin megabytes of base64 for the rest of the turn.
	if got := countCalls(engine, "FetchResource"); got != 2 {
		t.Fatalf("image fetches should not be memoized, got %d calls", got)
	}
}

func TestSystemPromptAddendumDescribesPolicy(t *testing.T) {
	for policy, want := range map[Policy]string{
		PolicyReadonly: "READ-ONLY",
		PolicyApprove:  "APPROVAL REQUIRED",
		PolicyFull:     "FULL",
	} {
		opts := Options{Policy: policy}
		if policy == PolicyApprove {
			opts.Confirmer = allowAll()
		}
		catalog := newTestCatalog(t, playableEngine(), opts)
		if !strings.Contains(catalog.SystemPromptAddendum(), want) {
			t.Fatalf("the %q addendum should mention %q", policy, want)
		}
	}
}
