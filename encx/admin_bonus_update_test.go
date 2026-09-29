package encx

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// The form contract was checked with a read-only GET of BonusEdit.aspx on
// tech.en.cx: add posts action=save; edit posts action=update and btnUpdate,
// with positive answer_<id> names for existing answers.
func TestLegacyBonusUpdateDoesNotCreateCopy(t *testing.T) {
	t.Parallel()
	bonuses, seconds := 1, 30
	var posted url.Values
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprintf(w, `<form id="BonusForm" method="post" action="BonusEdit.aspx?gid=7&amp;level=2&amp;bonus=42&amp;action=update">
<input name="txtBonusName" value="Б1"><input name="txtSeconds" value="%d">
<input name="answer_901" value="код1"><input name="answer_902" value="код2">
<input name="answer_-1" value=""><input type="image" name="btnUpdate">
</form>`, seconds)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		posted = r.PostForm
		switch r.URL.Query().Get("action") {
		case "save":
			bonuses++
		case "update":
			if r.URL.Query().Get("bonus") != "42" {
				t.Error("lost bonus ID")
			}
			if r.PostForm.Get("btnUpdate.x") == "" {
				t.Error("missing update submit")
			}
			if r.PostForm.Get("txtMinutes") == "3" {
				seconds = 180
			}
		default:
			t.Errorf("unexpected action: %s", r.URL)
		}
		w.WriteHeader(http.StatusFound)
	})
	if err := c.AdminUpdateBonus(t.Context(), 7, 2, 42, AdminBonus{Name: "Б1", Answers: []string{"код2", "код1"}, AwardMinutes: 3}); err != nil {
		t.Fatal(err)
	}
	if bonuses != 1 || seconds != 180 {
		t.Fatalf("bonuses=%d, seconds=%d; want one existing bonus with 180 seconds", bonuses, seconds)
	}
	if posted.Get("answer_901") != "код1" || posted.Get("answer_902") != "код2" {
		t.Fatalf("existing answer IDs lost: %v", posted)
	}
	if posted.Has("answer_-1") {
		t.Fatal("unchanged answers submitted as new")
	}
}

func TestLegacyBonusUpdateRequiresReadableExistingBonus(t *testing.T) {
	t.Parallel()
	posts := 0
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		fmt.Fprint(w, "<html>unavailable</html>")
	})
	for _, id := range []int{0, -1, 42} {
		if err := c.AdminUpdateBonus(t.Context(), 7, 2, id, AdminBonus{AwardMinutes: 3}); err == nil {
			t.Errorf("accepted bonus %d", id)
		}
	}
	if posts != 0 {
		t.Fatalf("sent %d writes without an existing bonus form", posts)
	}
}

func TestBonusUpdateAnswerReplacement(t *testing.T) {
	t.Parallel()
	form := adminBonusUpdateAnswers(`<input name="answer_901" value="old"><input name="answer_902" value="keep"><input name="answer_-1" value="">`, []string{"keep", "new", "new"})
	if form.Get("answer_901") != "" || !form.Has("answer_901") ||
		form.Get("answer_902") != "keep" || form.Get("answer_-1") != "new" ||
		form.Get("answer_-2") != "new" || len(form) != 4 {
		t.Fatalf("unexpected replacement fields: %v", form)
	}
}

// The editor renders rbAllLevels as two radio buttons, one always checked.
// Reading "a checked rbAllLevels exists" as game-wide made every time-only
// update move level bonuses into "all levels".
func TestLegacyBonusUpdateKeepsLevelBinding(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, form string
		wantLevel  int
		want       url.Values
	}{
		{
			name: "levels",
			form: `<input type="radio" name="rbAllLevels" value="1"><input type="radio" name="rbAllLevels" value="0" checked="checked">
<input type="checkbox" name="level_555" checked="checked"><input type="checkbox" name="level_556" checked="checked"><input type="checkbox" name="level_557">`,
			wantLevel: 555,
			want:      url.Values{"rbAllLevels": {"0"}, "level_555": {"on"}, "level_556": {"on"}},
		},
		{
			name:      "game",
			form:      `<input type="radio" name="rbAllLevels" value="1" checked="checked"><input type="radio" name="rbAllLevels" value="0"><input type="checkbox" name="level_555" disabled="disabled">`,
			wantLevel: -1,
			want:      url.Values{"rbAllLevels": {"1"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var posted url.Values
			c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					fmt.Fprint(w, `<input name="txtBonusName" value="Б&amp;1"><input name="txtMinutes" value="0"><input name="txtSeconds" value="30">
<input name="answer_901" value="x&amp;y">`+tc.form+`<textarea name="txtTask">a &lt;b&gt; c</textarea>`)
					return
				}
				if err := r.ParseForm(); err != nil {
					t.Error(err)
				}
				posted = r.PostForm
				w.WriteHeader(http.StatusFound)
			})
			b, err := c.AdminGetBonus(t.Context(), 7, 2, 42)
			if err != nil {
				t.Fatal(err)
			}
			if b.LevelID != tc.wantLevel || b.Name != "Б&1" || b.Task != "a <b> c" || len(b.Answers) != 1 || b.Answers[0] != "x&y" {
				t.Fatalf("read %+v", b)
			}
			b.AwardMinutes, b.AwardSeconds = 3, 0
			if err := c.AdminUpdateBonus(t.Context(), 7, 2, 42, *b); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"rbAllLevels", "level_555", "level_556", "level_557"} {
				if posted.Get(key) != tc.want.Get(key) {
					t.Errorf("%s = %q, want %q", key, posted.Get(key), tc.want.Get(key))
				}
			}
			if posted.Get("txtBonusName") != "Б&1" || posted.Get("txtTask") != "a <b> c" || posted.Get("answer_901") != "x&y" || posted.Has("answer_-1") {
				t.Errorf("text fields not round-tripped: %v", posted)
			}
		})
	}
}

func TestLegacyBonusUpdateMovesToRequestedLevel(t *testing.T) {
	t.Parallel()
	var posted url.Values
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<input name="txtBonusName" value="Б1"><input type="radio" name="rbAllLevels" value="1" checked="checked"><input type="radio" name="rbAllLevels" value="0">`)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		posted = r.PostForm
		w.WriteHeader(http.StatusFound)
	})
	if err := c.AdminUpdateBonus(t.Context(), 7, 2, 42, AdminBonus{Name: "Б1", LevelID: 555}); err != nil {
		t.Fatal(err)
	}
	if posted.Get("rbAllLevels") != "0" || posted.Get("level_555") != "on" {
		t.Fatalf("binding not changed: %v", posted)
	}
}

func TestLegacyBonusUpdateKeepsEmptyLevelSelection(t *testing.T) {
	t.Parallel()
	var posted url.Values
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			fmt.Fprint(w, `<input name="txtBonusName" value="Б1"><input type="radio" name="rbAllLevels" value="1"><input type="radio" name="rbAllLevels" value="0" checked="checked"><input type="checkbox" name="level_555">`)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		posted = r.PostForm
		w.WriteHeader(http.StatusFound)
	})
	b, err := c.AdminGetBonus(t.Context(), 7, 2, 42)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AdminUpdateBonus(t.Context(), 7, 2, 42, *b); err != nil {
		t.Fatal(err)
	}
	if posted.Get("rbAllLevels") != "0" {
		t.Fatalf("bonus made game-wide: %v", posted)
	}
}
