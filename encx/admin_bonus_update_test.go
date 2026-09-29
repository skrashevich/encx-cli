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
