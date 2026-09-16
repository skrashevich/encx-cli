package encx

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func verifyingClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return New(strings.TrimPrefix(server.URL, "http://"),
		WithHTTP(), WithAdminDelay(0), WithEngine(EngineLegacy))
}

// levelEditorWithAutopass renders the one element the editor uses to report the
// setting: the link whose text is the current autopass.
func levelEditorWithAutopass(text string) string {
	return `<html><body><span class="white9">Автопереход:</span> ` +
		`<a ID="lnkAdjustAutopass" class="no_decoration Text4">` + text + `</a>` +
		`<div ID="AutoPassSettingsHolder"></div></body></html>`
}

// The engine freezes level settings once a game has participants. It does not
// say so: it answers 200 with the editor rendered from the old values, which is
// indistinguishable from success unless the answer is read. A live agent set the
// autopass of a running game three times, was told "success" three times, and
// the level kept its five minutes throughout.
func TestAdminUpdateAutopassReportsARefusedChange(t *testing.T) {
	t.Parallel()
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(levelEditorWithAutopass("через 5 минут")))
			return
		}
		_, _ = w.Write([]byte(levelEditorWithAutopass("через 5 минут")))
	})

	err := c.legacyAdminUpdateAutopass(context.Background(), 82856, 1, AdminLevelSettings{})
	if err == nil {
		t.Fatal("a refused change was reported as success")
	}
	for _, want := range []string{"0:05:00", "участник"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error must say what is there now and why: %v", err)
		}
	}
}

func TestAdminUpdateAutopassAcceptsAConfirmedChange(t *testing.T) {
	t.Parallel()
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(levelEditorWithAutopass("через 1 час 30 минут")))
	})

	settings := AdminLevelSettings{AutopassHours: 1, AutopassMinutes: 30}
	if err := c.legacyAdminUpdateAutopass(context.Background(), 82856, 1, settings); err != nil {
		t.Fatalf("a confirmed change must succeed: %v", err)
	}
}

// Not every answer is the level editor — a redirect, or a page this parser does
// not know, must not be read as "the engine refused".
func TestAdminUpdateAutopassStaysQuietWhenThereIsNothingToVerify(t *testing.T) {
	t.Parallel()
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	})

	settings := AdminLevelSettings{AutopassMinutes: 7}
	if err := c.legacyAdminUpdateAutopass(context.Background(), 82856, 1, settings); err != nil {
		t.Fatalf("an unreadable answer is not a refusal: %v", err)
	}
}

const taskAddForm = `<html><body><span id="lblPageName">Добавление задания</span>` +
	`<form method="post"><textarea name="inputTask"></textarea>` +
	`<input src="add.gif" name="btnAdd" type="image"/></form></body></html>`

// A level holds one general task. Asked for a second one the editor re-renders
// its own add form with status 200 and changes nothing, so "task created" was
// reported for a level whose text stayed as it was.
func TestAdminCreateTaskReportsARefusedAdd(t *testing.T) {
	t.Parallel()
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(taskAddForm))
	})

	err := c.legacyAdminCreateTask(context.Background(), 82856, 1, AdminTask{Text: "новый текст"})
	if err == nil {
		t.Fatal("a refused add was reported as success")
	}
	// The caller has to learn the way out, which is a different call.
	if !strings.Contains(err.Error(), "AdminUpdateTask") {
		t.Fatalf("error must name the tool that does replace a task: %v", err)
	}
}

// An accepted add redirects back to the level editor.
func TestAdminCreateTaskAcceptsTheRedirect(t *testing.T) {
	t.Parallel()
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Location", "/Administration/Games/LevelEditor.aspx?gid=82856&level=1")
			w.WriteHeader(http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(taskAddForm))
	})

	if err := c.legacyAdminCreateTask(context.Background(), 82856, 1, AdminTask{Text: "первое задание"}); err != nil {
		t.Fatalf("an accepted add must succeed: %v", err)
	}
}

// The form is posted whole either way; verification must not change what is
// sent, only what is believed about the answer.
func TestAdminCreateTaskStillPostsTheTask(t *testing.T) {
	t.Parallel()
	var got string
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			got = fmt.Sprintf("%s|%s", r.Form.Get("inputTask"), r.Form.Get("chkReplaceNlToBr"))
			w.WriteHeader(http.StatusFound)
			return
		}
	})

	task := AdminTask{Text: "текст задания", ReplaceNl: true}
	if err := c.legacyAdminCreateTask(context.Background(), 82856, 1, task); err != nil {
		t.Fatalf("legacyAdminCreateTask: %v", err)
	}
	if got != "текст задания|on" {
		t.Fatalf("posted form = %q", got)
	}
}

// Deleting a game answers with the games list either way. A game that still has
// applications or players is simply kept, and the only way to tell is to look
// for it in the answer: a probe game reported "deleted" three times and stayed.
func TestAdminDeleteGameReportsAGameTheEngineKept(t *testing.T) {
	t.Parallel()
	const stillListed = `<html><body><a href="/Administration/GamesManager.aspx?page=1">Игры</a>` +
		`<a href="/Administration/Games/GameEditor.aspx?gid=82860&page=1">probe</a></body></html>`
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(stillListed))
	})

	err := c.legacyAdminDeleteGame(context.Background(), 82860)
	if err == nil {
		t.Fatal("a game that survived deletion was reported as deleted")
	}
	if !strings.Contains(err.Error(), "заявки") {
		t.Fatalf("error must name the reason: %v", err)
	}
}

func TestAdminDeleteGameAcceptsAGameThatIsGone(t *testing.T) {
	t.Parallel()
	const deleted = `<html><body><a href="/Administration/GamesManager.aspx?page=1">Игры</a>` +
		`<a href="/Administration/Games/GameEditor.aspx?gid=82448&page=1">другая игра</a></body></html>`
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(deleted))
	})

	if err := c.legacyAdminDeleteGame(context.Background(), 82860); err != nil {
		t.Fatalf("a deleted game must not be reported as kept: %v", err)
	}
}

// The id also appears in rating forms and other furniture of the same page, so
// a bare id match would call every deletion a failure.
func TestAdminDeleteGameIgnoresTheIdOutsideEditorLinks(t *testing.T) {
	t.Parallel()
	const rated = `<html><body><a href="/Administration/GamesManager.aspx?page=1">Игры</a>` +
		`<form action="https://svk.en.cx/Administration/GamesManager.aspx?gid=82860&page=1&action=Delete">` +
		`<select name="ddlRates"></select></form></body></html>`
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(rated))
	})

	if err := c.legacyAdminDeleteGame(context.Background(), 82860); err != nil {
		t.Fatalf("the id in a rating form is not the game still being listed: %v", err)
	}
}

// The editor freezes the start once the game has begun. It does not refuse the
// post: it saves every other field, drops the start and answers with the same
// redirect as a full success. A probe game was "moved" to March 2027 three
// times and kept its 13:51 start each time.
func TestAdminUpdateGameInfoReportsAStartTheEngineDropped(t *testing.T) {
	t.Parallel()
	var posted url.Values
	c := legacyGameEditorClient(t, true, &posted)

	info := AdminGameInfo{Title: "Игра", StartDateTime: "01.03.2027 10:00"}
	err := c.legacyAdminUpdateGameInfo(context.Background(), 82860, info)
	if err == nil {
		t.Fatal("a start the engine dropped was reported as saved")
	}
	for _, want := range []string{"начавшейся", "остальные поля сохранены"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error must say what happened and what did not: %v", err)
		}
	}
}

func TestAdminUpdateGameInfoAcceptsAStartThatStuck(t *testing.T) {
	t.Parallel()
	var posted url.Values
	c := legacyGameEditorClient(t, false, &posted)

	info := AdminGameInfo{Title: "Игра", StartDateTime: "07.06.2027 18:30"}
	if err := c.legacyAdminUpdateGameInfo(context.Background(), 82867, info); err != nil {
		t.Fatalf("a saved start must not be reported as dropped: %v", err)
	}
}

// The editor prints seconds for a value the caller typed without them, and a
// caller may write RFC3339. Neither is a changed start.
func TestLegacyDateEqualIgnoresSpelling(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2]string{
		{"07.06.2027 18:30:00", "07.06.2027 18:30"},
		{"07.06.2027 18:30:00", "2027-06-07T18:30:00"},
		{"07.06.2027 18:30:00", "07.06.2027 18:30:00"},
	} {
		if !legacyDateEqual(pair[0], pair[1]) {
			t.Errorf("%q and %q are the same moment", pair[0], pair[1])
		}
	}
	if legacyDateEqual("07.06.2027 18:30:00", "07.06.2027 18:31:00") {
		t.Error("a minute apart is not the same moment")
	}
}

// MessageEdit.aspx is the one admin page whose level= is a level ID rather than
// a level number. Posted a number it answers with a stub that closes the window
// and changes nothing, so the interface's level-number calls have to resolve the
// id first — the way the new engine already did.
func TestLegacyMessageCallsAddressLevelsById(t *testing.T) {
	t.Parallel()
	const levelManager = `<html><body>
		<input name="txtLevelName_1580899" value="Первый">
		<a href="LevelEditor.aspx?level=1&gid=82856">1</a>
		<input name="txtLevelName_1580900" value="Второй">
		<a href="LevelEditor.aspx?level=2&gid=82856">2</a>
		</body></html>`

	var messageURLs []string
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "MessageEdit.aspx") {
			messageURLs = append(messageURLs, r.URL.String())
		}
		_, _ = w.Write([]byte(levelManager))
	})

	if err := c.legacyAdminDeleteMessage(context.Background(), 82856, 2, 7001); err != nil {
		t.Fatalf("legacyAdminDeleteMessage: %v", err)
	}
	if len(messageURLs) != 1 {
		t.Fatalf("message requests = %v", messageURLs)
	}
	if !strings.Contains(messageURLs[0], "level=1580900") {
		t.Fatalf("level 2 must be addressed by its id: %s", messageURLs[0])
	}
}

// A level the game does not have is named rather than posted to a page that
// would answer with a stub.
func TestLegacyMessageCallsRefuseAnUnknownLevel(t *testing.T) {
	t.Parallel()
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`<html><body><input name="txtLevelName_1580899" value="Первый">
			<a href="LevelEditor.aspx?level=1&gid=82856">1</a></body></html>`))
	})

	err := c.legacyAdminDeleteMessage(context.Background(), 82856, 9, 7001)
	if err == nil || !strings.Contains(err.Error(), "has no level 9") {
		t.Fatalf("error = %v, want it to name the missing level", err)
	}
}

// AdminSector.ForMemberID is honoured when a sector is created and used to be
// dropped when it is updated: every ddlAnswerFor field was reset to "0" and none
// was written back. The new engine applied it in both cases, so the same call
// addressed a different audience depending on which engine answered.
func TestLegacyUpdateSectorKeepsForMemberID(t *testing.T) {
	t.Parallel()
	const answersEditor = `<html><body><form>
		<input type="text" name="txtAnswer_0" value="старый"/>
		<select name="ddlAnswerFor_0"><option value="0">все</option>
			<option value="1516219">skrashevich</option></select>
		<input type="text" name="txtAnswer_1" value=""/>
		<select name="ddlAnswerFor_1"><option value="0">все</option></select>
		<input type="image" name="btnSaveSector"/>
		</form></body></html>`

	var posted url.Values
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			posted = r.Form
		}
		_, _ = w.Write([]byte(answersEditor))
	})

	sector := AdminSector{Name: "Ответ", ForMemberID: "1516219", Answers: []string{"ответ"}}
	if err := c.legacyAdminUpdateSector(context.Background(), 82856, 1, 3525903, sector); err != nil {
		t.Fatalf("legacyAdminUpdateSector: %v", err)
	}
	if got := posted.Get("ddlAnswerFor_0"); got != "1516219" {
		t.Fatalf("ddlAnswerFor_0 = %q, want the member the caller named", got)
	}
	// A field that carries no answer stays addressed to everyone.
	if got := posted.Get("ddlAnswerFor_1"); got != "0" {
		t.Fatalf("ddlAnswerFor_1 = %q, want the untouched default", got)
	}
}

// An empty ForMemberID keeps meaning "for everyone".
func TestLegacyUpdateSectorDefaultsToEveryone(t *testing.T) {
	t.Parallel()
	const answersEditor = `<html><body><form>
		<input type="text" name="txtAnswer_0" value="старый"/>
		<select name="ddlAnswerFor_0"><option value="0">все</option></select>
		<input type="image" name="btnSaveSector"/>
		</form></body></html>`

	var posted url.Values
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			posted = r.Form
		}
		_, _ = w.Write([]byte(answersEditor))
	})

	sector := AdminSector{Name: "Ответ", Answers: []string{"ответ"}}
	if err := c.legacyAdminUpdateSector(context.Background(), 82856, 1, 3525903, sector); err != nil {
		t.Fatalf("legacyAdminUpdateSector: %v", err)
	}
	if got := posted.Get("ddlAnswerFor_0"); got != "0" {
		t.Fatalf("ddlAnswerFor_0 = %q, want everyone", got)
	}
}

// The message editor spells its two operations differently. Posting the create
// pair at an existing message adds a copy instead of changing it: a live probe
// collected four messages out of one create and three "updates".
func TestLegacyMessageFormsUseTheRightSubmit(t *testing.T) {
	t.Parallel()
	const levelManager = `<html><body>
		<input name="txtLevelName_1581001" value="Первый">
		<a href="LevelEditor.aspx?level=1&gid=82868">1</a>
		<input name="txtLevelName_1581002" value="Второй">
		<a href="LevelEditor.aspx?level=2&gid=82868">2</a>
		</body></html>`

	var posted []url.Values
	var urls []string
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				t.Errorf("ParseForm: %v", err)
			}
			posted = append(posted, r.Form)
			urls = append(urls, r.URL.String())
		}
		_, _ = w.Write([]byte(levelManager))
	})

	msg := AdminGameMessage{Text: "текст"}
	if err := c.legacyAdminCreateMessage(context.Background(), 82868, 1581002, msg); err != nil {
		t.Fatalf("legacyAdminCreateMessage: %v", err)
	}
	if err := c.legacyAdminUpdateMessage(context.Background(), 82868, 2, 43967, msg); err != nil {
		t.Fatalf("legacyAdminUpdateMessage: %v", err)
	}
	if len(posted) != 2 {
		t.Fatalf("posts = %d, want create and update", len(posted))
	}

	if posted[0].Get("action") != "save" || posted[0].Get("btnSave.x") == "" {
		t.Errorf("create must submit btnSave/action=save: %v", posted[0])
	}
	if posted[1].Get("action") != "update" || posted[1].Get("btnUpdate.x") == "" {
		t.Errorf("update must submit btnUpdate/action=update: %v", posted[1])
	}
	// The update goes to the editing view of that message, addressed by level id.
	if !strings.Contains(urls[1], "action=edit") || !strings.Contains(urls[1], "level=1581002") ||
		!strings.Contains(urls[1], "mid=43967") {
		t.Errorf("update URL = %s", urls[1])
	}
}
