package encx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// gameEditorPage renders the fields legacyAdminGetGameInfo reads. The start is
// an ordinary input on a game that has not begun and a disabled one afterwards,
// which is how the editor itself renders it.
func gameEditorPage(startDisabled bool) string {
	start := `<input name="StartDateTime" value="10.09.2026 18:00:00">`
	if startDisabled {
		start = `<input name="StartDateTime" value="10.09.2026 18:00:00" disabled="disabled">`
	}
	return `<form>` + start + `
	<input name="GameTitle" value="Игра">
	<input name="GameAuthors" value="svk">
	<input name="Prize" value="0">
	<input name="FinishDateTime" value="11.09.2026 18:00:00">
	<input name="RequestLastDate" value="09.09.2026 18:00:00">
	<input name="MaxPlayers" value="0">
	<input name="MaxTeamPlayers" value="0">
	<input name="FirstPlaces" value="3">
	<input name="NotFirstPlaces" value="3">
	<input name="txtAcceptRateFrom" value="11.09.2026 18:00:00">
	<input type="checkbox" name="IsModerated" checked>
	<textarea name="Descr">Описание</textarea>
	</form>`
}

// legacyGameEditorClient serves the editor page and records the form each
// update posts back to it.
func legacyGameEditorClient(t *testing.T, startDisabled bool, posted *url.Values) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			*posted = r.Form
			_, _ = w.Write([]byte("ok"))
			return
		}
		_, _ = w.Write([]byte(gameEditorPage(startDisabled)))
	}))
	t.Cleanup(server.Close)
	return New(strings.TrimPrefix(server.URL, "http://"),
		WithHTTP(), WithAdminDelay(0), WithEngine(EngineLegacy))
}

// The start and the request settings are editable on the legacy engine too, so
// the editor form has to carry them — the gap that left them settable only in
// the web admin panel.
func TestLegacyAdminGameInfoCarriesStartAndRequestSettings(t *testing.T) {
	var posted url.Values
	c := legacyGameEditorClient(t, false, &posted)
	ctx := t.Context()

	info, err := c.AdminGetGameInfo(ctx, 82448)
	if err != nil {
		t.Fatalf("AdminGetGameInfo: %v", err)
	}
	if info.StartDateTime != "10.09.2026 18:00:00" {
		t.Errorf("StartDateTime = %q", info.StartDateTime)
	}
	if !info.IsModerated {
		t.Errorf("IsModerated = false, want the checked box")
	}

	info.StartDateTime = "2026-09-12T18:00:00+03:00"
	info.IsModerated = false
	if err := c.AdminUpdateGameInfo(ctx, 82448, *info); err != nil {
		t.Fatalf("AdminUpdateGameInfo: %v", err)
	}
	// An RFC3339 start is rewritten in the spelling the ASP form expects.
	if got := posted.Get("StartDateTime"); got != "12.09.2026 18:00:00" {
		t.Errorf("StartDateTime = %q, want the form's own spelling", got)
	}
	if got := posted.Get("RequestLastDate"); got != "09.09.2026 18:00:00" {
		t.Errorf("RequestLastDate = %q, want the value that was read back", got)
	}
	if got := posted.Get("IsModerated"); got != "false" {
		t.Errorf("IsModerated = %q, want requests to be accepted automatically", got)
	}
}

// A started game renders the start disabled, so it is neither read nor written
// back: posting it empty would fail the page's own validation.
func TestLegacyAdminUpdateGameInfoOmitsDisabledStart(t *testing.T) {
	var posted url.Values
	c := legacyGameEditorClient(t, true, &posted)
	ctx := t.Context()

	info, err := c.AdminGetGameInfo(ctx, 82448)
	if err != nil {
		t.Fatalf("AdminGetGameInfo: %v", err)
	}
	if info.StartDateTime != "" {
		t.Errorf("StartDateTime = %q, want it withheld for a disabled field", info.StartDateTime)
	}
	if err := c.AdminUpdateGameInfo(ctx, 82448, *info); err != nil {
		t.Fatalf("AdminUpdateGameInfo: %v", err)
	}
	if _, ok := posted["StartDateTime"]; ok {
		t.Errorf("StartDateTime = %q was posted for a started game", posted.Get("StartDateTime"))
	}
}

// TestLegacyAdminDeleteGameHitsTheManagerAction checks the delete link the
// games manager renders, including the page number it carries.
func TestLegacyAdminDeleteGameHitsTheManagerAction(t *testing.T) {
	var requested *url.URL
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)
	c := New(strings.TrimPrefix(server.URL, "http://"),
		WithHTTP(), WithAdminDelay(0), WithEngine(EngineLegacy))

	if err := c.AdminDeleteGame(context.Background(), 82448); err != nil {
		t.Fatalf("AdminDeleteGame: %v", err)
	}
	if requested == nil {
		t.Fatal("no request was sent")
	}
	if requested.Path != "/Administration/GamesManager.aspx" {
		t.Errorf("path = %q", requested.Path)
	}
	query := requested.Query()
	if query.Get("gid") != "82448" || query.Get("action") != "Delete" || query.Get("page") != "1" {
		t.Errorf("query = %q, want gid=82448, action=Delete, page=1", requested.RawQuery)
	}
}
