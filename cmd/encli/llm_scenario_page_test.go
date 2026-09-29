package main

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/encx-cli/encx/scenario"
)

// bigScenario is shaped like the game that ended a run: thirty levels of long
// HTML tasks, 560 KB in all.
func bigScenario(t *testing.T, levels, taskBytes int) string {
	t.Helper()
	doc := scenario.Document{SourcePath: "api:/games/32055/scenario", GameID: 32055, GameTitle: "Ярославские дали"}
	for n := 1; n <= levels; n++ {
		task := fmt.Sprintf("<p>Уровень %d</p>", n) + strings.Repeat("<b>текст</b> ", taskBytes/20) +
			`<div class="map">© OpenStreetMap contributors</div>`
		doc.Levels = append(doc.Levels, scenario.Level{Number: n, Name: fmt.Sprintf("L%d", n), Tasks: []string{task},
			SectorAnswers: [][]string{{fmt.Sprintf("CODE%d", n)}}})
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// A scenario larger than the room left in the window arrives page by page of
// whole levels, each page within the room, and walking next_from_level reads
// every level exactly once and records all of them as loaded.
func TestScenarioPagesWalkTheWholeGameWithinTheRoom(t *testing.T) {
	t.Parallel()
	const room = 64 * 1024
	full := bigScenario(t, 30, 18000)
	if len(full) < 450*1024 {
		t.Fatalf("fixture too small: %d", len(full))
	}
	var src scenario.Document
	if err := json.Unmarshal([]byte(full), &src); err != nil {
		t.Fatal(err)
	}

	session := &llmSession{}
	seen := map[int]int{}
	from := 0
	for pages := 0; ; pages++ {
		if pages > 30 {
			t.Fatal("paging does not terminate")
		}
		page := selectScenarioLevels(&src, from, 0)
		raw, err := json.Marshal(page)
		if err != nil {
			t.Fatal(err)
		}
		result := pageScenarioForLLM(string(raw), room)
		if got := wireBytes(result); got > room {
			t.Fatalf("page from %d is %d bytes, room %d", from, got, room)
		}
		var out struct {
			Levels          []scenario.Level `json:"levels"`
			Truncated       bool             `json:"truncated"`
			NextFromLevel   int              `json:"next_from_level"`
			AllLevelNumbers []int            `json:"all_level_numbers"`
			TruncatedLevels []int            `json:"truncated_levels"`
			Hint            string           `json:"hint"`
		}
		if err := json.Unmarshal([]byte(result), &out); err != nil {
			t.Fatalf("%v: %.300s", err, result)
		}
		if len(out.Levels) == 0 || len(out.AllLevelNumbers) != 30 || len(out.TruncatedLevels) != 0 {
			t.Fatalf("bad page from %d: levels=%d all=%v truncated=%v", from, len(out.Levels), out.AllLevelNumbers, out.TruncatedLevels)
		}
		for _, level := range out.Levels {
			seen[level.Number]++
			want := src.Levels[level.Number-1]
			if level.Tasks[0] != want.Tasks[0] || level.SectorAnswers[0][0] != want.SectorAnswers[0][0] {
				t.Fatalf("level %d content changed on the page", level.Number)
			}
		}
		markScenarioContentLoaded(session, result)
		if out.NextFromLevel == 0 {
			break
		}
		if !out.Truncated || !strings.Contains(out.Hint, fmt.Sprintf("from_level=%d", out.NextFromLevel)) {
			t.Fatalf("page does not say how to continue: %s", out.Hint)
		}
		from = out.NextFromLevel
	}
	for n := 1; n <= 30; n++ {
		if seen[n] != 1 {
			t.Fatalf("level %d delivered %d times", n, seen[n])
		}
	}
	if missing := missingLevelsForContentSummary(session, "покажи сценарий"); len(missing) != 0 {
		t.Fatalf("levels still counted as unread: %v", missing)
	}
}

// Before the last page, the levels not yet read stay unread for the
// completeness guard.
func TestScenarioFirstPageLeavesTheRestUnread(t *testing.T) {
	t.Parallel()
	result := pageScenarioForLLM(bigScenario(t, 30, 18000), 64*1024)
	session := &llmSession{}
	markScenarioContentLoaded(session, result)
	missing := missingLevelsForContentSummary(session, "покажи сценарий")
	if len(missing) == 0 || missing[len(missing)-1] != 30 {
		t.Fatalf("unread levels lost: %v", missing)
	}
}

// One level larger than the room on its own is shortened, never dropped, and
// the page says so.
func TestScenarioOversizedLevelIsShortenedAndMarked(t *testing.T) {
	t.Parallel()
	const room = 16 * 1024
	result := pageScenarioForLLM(bigScenario(t, 2, 200000), room)
	if got := wireBytes(result); got > room {
		t.Fatalf("page is %d bytes, room %d", got, room)
	}
	var out struct {
		Levels          []scenario.Level `json:"levels"`
		TruncatedLevels []int            `json:"truncated_levels"`
		NextFromLevel   int              `json:"next_from_level"`
	}
	if err := json.Unmarshal([]byte(result), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Levels) != 1 || out.Levels[0].Number != 1 || len(out.TruncatedLevels) != 1 || out.NextFromLevel != 2 {
		t.Fatalf("unexpected page: levels=%d truncated=%v next=%d", len(out.Levels), out.TruncatedLevels, out.NextFromLevel)
	}
	if !strings.Contains(out.Levels[0].Tasks[0], "bytes omitted]") || out.Levels[0].SectorAnswers[0][0] != "CODE1" {
		t.Fatal("shortening is not marked or cut the answers")
	}
}

// The regression itself: a model with a 1M-token window reads a 560 KB
// scenario whole. Only the fixed 128K assumption made it too large.
func TestScenarioFitsWholeInALargeWindow(t *testing.T) {
	t.Parallel()
	full := bigScenario(t, 30, 18000)
	p := &observedPicoProvider{budget: agentBudgetCalibrator{tokenBudget: agentTokenBudgetForWindow(1 << 20)}}
	p.seen = []providers.Message{
		{Role: "system", Content: strings.Repeat("s", 6000)},
		{Role: "user", Content: "убери плашку © OpenStreetMap contributors на каждом уровне"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "1", Name: "admin_game_scenario"}}},
	}
	if got := pageScenarioForLLM(full, p.resultRoom()); got != full {
		t.Fatalf("scenario paged in a 1M window: room %d, scenario %d", p.resultRoom(), len(full))
	}

	small := &observedPicoProvider{budget: agentBudgetCalibrator{}}
	small.seen = p.seen
	if got := pageScenarioForLLM(full, small.resultRoom()); got == full {
		t.Fatal("scenario delivered whole into a 128K window")
	}
	messages := append(p.seen, providers.Message{Role: "tool", ToolCallID: "1", Content: pageScenarioForLLM(full, small.resultRoom())})
	if _, err := boundedAgentMessages(messages, nil, small.budget.byteBudget()); err != nil {
		t.Fatalf("paged result still does not fit: %v", err)
	}
}

// Results of the same turn share the room: the second one sees what the first
// already took.
func TestResultRoomCountsResultsOfTheSameTurn(t *testing.T) {
	t.Parallel()
	p := &observedPicoProvider{}
	p.seen = []providers.Message{{Role: "system", Content: "s"}, {Role: "user", Content: "u"}}
	before := p.resultRoom()
	p.pendingResultBytes = 40000
	if got := p.resultRoom(); got != before-40000 {
		t.Fatalf("room %d, want %d", got, before-40000)
	}
}

func TestAgentTokenBudgetForWindow(t *testing.T) {
	t.Parallel()
	for window, want := range map[int]int{
		0:          agentRequestTokenBudget,
		128 * 1024: agentRequestTokenBudget,
		1 << 20:    (1 << 20) - (1<<20)/8,
		32 * 1024:  16 * 1024,
	} {
		if got := agentTokenBudgetForWindow(window); got != want {
			t.Errorf("window %d: budget %d, want %d", window, got, want)
		}
	}
	c := agentBudgetCalibrator{tokenBudget: agentTokenBudgetForWindow(1 << 20)}
	if got, want := c.byteBudget(), (1<<20)-(1<<20)/8; got != want {
		t.Fatalf("byte budget %d, want %d", got, want)
	}
}

// The window comes from the catalog in either shape: OpenRouter's top-level
// context_length next to its first provider's, and polza.ai's provider-only one.
func TestCatalogContextWindow(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[
			{"id":"deepseek/deepseek-v4-flash-0731","context_length":null,"top_provider":{"context_length":1048576}},
			{"id":"openrouter/model","context_length":1310720,"top_provider":{"context_length":1048576}},
			{"id":"plain/model","context_length":131072},
			{"id":"bare/model"}]}`)
	}))
	defer server.Close()
	models := fetchLLMCatalog(t.Context(), server.URL, "")
	for model, want := range map[string]int{
		"deepseek/deepseek-v4-flash-0731": 1048576,
		"openrouter/model:free":           1048576,
		"plain/model":                     131072,
		"bare/model":                      0,
	} {
		m, ok := findLLMCatalogModel(models, model)
		if !ok || m.contextTokens() != want {
			t.Errorf("%s: window %d (found %v), want %d", model, m.contextTokens(), ok, want)
		}
	}
	if llmCatalogPublishes(server.URL) {
		t.Fatal("a generic endpoint must not be asked for a catalog")
	}
}

// Levels read in one game do not count for the next: the first page of a game
// starts the record over, and a level shortened to fit is not counted as read.
func TestScenarioPageCoverageIsPerGameAndSkipsShortenedLevels(t *testing.T) {
	t.Parallel()
	session := &llmSession{}
	markScenarioContentLoaded(session, bigScenario(t, 30, 100)) // game A read whole
	page := pageScenarioForLLM(bigScenario(t, 40, 18000), 64*1024)
	markScenarioContentLoaded(session, page)
	missing := missingLevelsForContentSummary(session, "покажи сценарий")
	if len(missing) < 30 || missing[0] > 5 {
		t.Fatalf("game A's levels counted for game B: missing %v", missing)
	}

	session = &llmSession{}
	markScenarioContentLoaded(session, pageScenarioForLLM(bigScenario(t, 2, 200000), 16*1024))
	if missing := missingLevelsForContentSummary(session, "покажи сценарий"); len(missing) != 2 || missing[0] != 1 {
		t.Fatalf("shortened level counted as read: missing %v", missing)
	}
}

// Scenario HTML is remote input: a stray invalid byte must not cost the whole
// read, and zero timers are not printed for every level.
func TestScenarioOutputSurvivesInvalidUTF8(t *testing.T) {
	doc := &scenario.Document{GameID: 1, Levels: []scenario.Level{{Number: 1, Tasks: []string{"<b>ok\xff</b>"}}}}
	out := captureStdout(t, func() { outputCompactJSON(selectScenarioLevels(doc, 0, 0)) })
	if !strings.Contains(out, "<b>ok�</b>") || strings.Contains(out, "autopass_seconds") {
		t.Fatalf("unexpected output: %s", out)
	}
}

// A catalog is fetched once, and a failed fetch is remembered too.
func TestCatalogIsCachedIncludingFailures(t *testing.T) {
	t.Parallel()
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if strings.HasPrefix(r.URL.Path, "/down") {
			http.Error(w, "down", http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"m","context_length":1000}]}`)
	}))
	defer server.Close()
	for range 3 {
		if models := fetchLLMCatalog(t.Context(), server.URL+"/up", ""); len(models) != 1 {
			t.Fatalf("catalog: %v", models)
		}
		if models := fetchLLMCatalog(t.Context(), server.URL+"/down", ""); models != nil {
			t.Fatalf("failed catalog: %v", models)
		}
	}
	if hits != 2 {
		t.Fatalf("%d fetches, want 2", hits)
	}
}
