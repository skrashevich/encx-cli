package encx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func legacySequencePage(disabled bool) string {
	attribute := ""
	if disabled {
		attribute = ` disabled="disabled"`
	}
	return fmt.Sprintf(`<form name="SequecesChangeForm" action="./LevelManager.aspx">`+
		`<input name="sequences" value="change">`+
		`<select name="ddlLevelsSequence" id="ddlLevelsSequence"%s>`+
		`<option value="0">Линейная</option><option value="1">Указанная</option>`+
		`<option value="2">Случайная</option><option value="3" selected="selected">Штурмовая</option>`+
		`<option value="4">Динамически случайная</option></select></form>`, attribute)
}

func TestLegacyLevelSequenceMatchesAssaultManager(t *testing.T) {
	sequence, err := parseLegacyLevelSequence(legacySequencePage(false))
	if err != nil {
		t.Fatal(err)
	}
	if sequence.ID != SequenceAssault || !sequence.CanChange {
		t.Fatalf("sequence = %+v", sequence)
	}
	sequence, err = parseLegacyLevelSequence(legacySequencePage(true))
	if err != nil || sequence.CanChange {
		t.Fatalf("disabled sequence = %+v, %v", sequence, err)
	}
}

func TestLegacySetLevelSequenceUsesManagerQueryAndVerifies(t *testing.T) {
	current := SequenceLinear
	var changeQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("sequences") == "change" {
			changeQuery = r.URL.RawQuery
			if r.URL.Query().Get("ddlLevelsSequence") == "3" {
				current = SequenceAssault
			}
		}
		page := strings.Replace(legacySequencePage(false), `value="3" selected="selected"`, `value="3"`, 1)
		page = strings.Replace(page, fmt.Sprintf(`value="%d">`, current), fmt.Sprintf(`value="%d" selected="selected">`, current), 1)
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(server.Close)
	c := New(strings.TrimPrefix(server.URL, "http://"), WithHTTP(), WithEngine(EngineLegacy))
	if err := c.AdminSetLevelSequence(t.Context(), 82913, SequenceAssault); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(changeQuery, "sequences=change") || !strings.Contains(changeQuery, "ddlLevelsSequence=3") || !strings.Contains(changeQuery, "gid=82913") {
		t.Fatalf("change query = %q", changeQuery)
	}
}

func TestNewEngineSetLevelSequenceUsesSequenceRoute(t *testing.T) {
	current := SequenceLinear
	var wrote int
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/games/82913/levels":
			_, _ = fmt.Fprintf(w, `{"game_id":82913,"levels_sequence_id":%d,"can_change_levels_sequence":true}`, current)
		case r.Method == http.MethodPut && r.URL.Path == "/admin/games/82913/levels/sequence":
			var body struct {
				ID int `json:"levels_sequence_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			wrote = body.ID
			current = body.ID
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	if err := c.AdminSetLevelSequence(t.Context(), 82913, SequenceAssault); err != nil {
		t.Fatal(err)
	}
	if wrote != SequenceAssault {
		t.Fatalf("wrote = %d", wrote)
	}
}
