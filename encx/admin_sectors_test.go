package encx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestParseSectorAnswerFields(t *testing.T) {
	body := `<input name="txtAnswer_0" value="">
	<input value="поехалистрадать" class="answer" name='txtAnswer_6588020'>
	<select name="ddlAnswerFor_6588020"><option value="0" selected></select>`
	fields := parseSectorAnswerFields(body)
	if len(fields) != 2 {
		t.Fatalf("got %d fields", len(fields))
	}
	if fields[1].AnswerName != "txtAnswer_6588020" || fields[1].Value != "поехалистрадать" {
		t.Fatalf("unexpected second field: %+v", fields[1])
	}
}

func TestParseSectorDeleteIDs(t *testing.T) {
	body := `<a href="/Administration/Games/LevelEditor.aspx?gid=1&level=2&swanswers=1&delsector=9">del</a>
	<a href="LevelEditor.aspx?delsector=3&swanswers=1">x</a>
	<a href="?delsector=9">dup</a>`
	got := parseSectorDeleteIDs(body)
	want := []int{9, 3}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestParseListPageSectorAnswersMap(t *testing.T) {
	body := `<div id='divAnswersView_3486043'><span class="nonLatinChar">answer</span>66</div>`
	got := parseListPageSectorAnswersMap(body)
	if len(got[3486043]) != 1 || got[3486043][0] != "answer66" {
		t.Fatalf("unexpected answers: %v", got)
	}
}

func TestAdminAddSectorAnswersFormTargetsExistingSector(t *testing.T) {
	body := `<form method="post" class="noPadMarg" action="/Administration/Games/LevelEditor.aspx?level=4&gid=82443">
		<div id='divSectorsAddAnswersRows_3495771'></div>
		<input type="image" name="AnswersTable_ctl00_ctl06_SectorsRepeater_ctl00_pnlNewAnswers_btnSave" />
		<input type="hidden" name="ddlSector" value="3495771"/>
		<input type="hidden" name="saveanswers" value="1"/>
	</form>`
	form := adminAddSectorAnswersForm(body, 3495771, []string{"ok", "  ", "ок"})
	if got := form.Get("ddlSector"); got != "3495771" {
		t.Fatalf("ddlSector = %q, want 3495771", got)
	}
	if got := form.Get("txtSectorName"); got != "" {
		t.Fatalf("txtSectorName = %q, want empty", got)
	}
	if got := form.Get("savesector"); got != "" {
		t.Fatalf("savesector = %q, want empty", got)
	}
	if got := form.Get("saveanswers"); got != "1" {
		t.Fatalf("saveanswers = %q, want 1", got)
	}
	if got := form.Get("AnswersTable_ctl00_ctl06_SectorsRepeater_ctl00_pnlNewAnswers_btnSave.x"); got != "1" {
		t.Fatalf("pnlNewAnswers submit x = %q, want 1", got)
	}
	if got := form.Get("txtAnswer_0"); got != "ok" {
		t.Fatalf("txtAnswer_0 = %q, want ok", got)
	}
	if got := form.Get("txtAnswer_1"); got != "ок" {
		t.Fatalf("txtAnswer_1 = %q, want ок", got)
	}
	if got := form.Get("ddlAnswerFor_1"); got != "0" {
		t.Fatalf("ddlAnswerFor_1 = %q, want 0", got)
	}
}

func TestAdminClearLevelSectorsRemovesEmptyShells(t *testing.T) {
	alive := map[int]bool{1: true, 2: true, 3: true}
	var deleted []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "ALoader/LevelInfo.aspx") && r.URL.Query().Get("object") == "3" && r.URL.Query().Get("sector") == "":
			var b strings.Builder
			for id := range alive {
				if alive[id] {
					fmt.Fprintf(&b, `<option value="%d">Сектор %d</option>`, id, id)
				}
			}
			_, _ = w.Write([]byte(b.String()))
		case strings.Contains(r.URL.Path, "ALoader/LevelInfo.aspx") && r.URL.Query().Get("sector") != "":
			_, _ = w.Write([]byte(``))
		case strings.Contains(r.URL.Path, "LevelEditor.aspx") && r.URL.Query().Get("swanswers") == "1" && r.URL.Query().Get("delsector") == "":
			var b strings.Builder
			for id := range alive {
				if alive[id] {
					fmt.Fprintf(&b, `<a href="?delsector=%d">del</a><div id='divAnswersView_%d'></div>`, id, id)
				}
			}
			_, _ = w.Write([]byte(b.String()))
		case strings.Contains(r.URL.Path, "LevelEditor.aspx") && r.URL.Query().Get("delsector") != "":
			id, _ := strconv.Atoi(r.URL.Query().Get("delsector"))
			alive[id] = false
			deleted = append(deleted, id)
			http.Redirect(w, r, "/Administration/Games/LevelEditor.aspx?level=1&gid=1&swanswers=1", http.StatusFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	client := New(host, WithHTTP(), WithAdminDelay(0))
	if err := client.AdminClearLevelSectors(context.Background(), 1, 1); err != nil {
		t.Fatalf("AdminClearLevelSectors: %v", err)
	}
	sort.Ints(deleted)
	if len(deleted) != 3 {
		t.Fatalf("expected all sector IDs deleted, got %v", deleted)
	}
}

func TestAdminGetSectorAnswersPacesGETReads(t *testing.T) {
	var sectorHits []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "ALoader/LevelInfo.aspx") && r.URL.Query().Get("object") == "3" && r.URL.Query().Get("sector") == "":
			_, _ = w.Write([]byte(`<option value="101">A</option><option value="102">B</option>`))
		case strings.Contains(r.URL.Path, "ALoader/LevelInfo.aspx") && r.URL.Query().Get("object") == "3" && r.URL.Query().Get("sector") != "":
			sectorHits = append(sectorHits, time.Now())
			_, _ = w.Write([]byte(`<input name="txtAnswer_1" value="ok">`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	client := New(host, WithHTTP())
	start := time.Now()
	sectors, err := client.AdminGetSectorAnswers(context.Background(), 1, 1)
	if err != nil {
		t.Fatalf("AdminGetSectorAnswers: %v", err)
	}
	if len(sectors) != 2 {
		t.Fatalf("sectors = %d, want 2", len(sectors))
	}

	if len(sectorHits) != 2 {
		t.Fatalf("sector hits = %d, want 2", len(sectorHits))
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond {
		t.Fatalf("GET sector reads were not paced, elapsed=%s", elapsed)
	}
}

func TestAdminClearLevelSectorsRemovesEditorOrphansWhenALoaderEmpty(t *testing.T) {
	alive := map[int]bool{99: true, 100: true}
	var deleted []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "ALoader/LevelInfo.aspx") && r.URL.Query().Get("object") == "3":
			_, _ = w.Write([]byte(``))
		case strings.Contains(r.URL.Path, "LevelEditor.aspx") && r.URL.Query().Get("swanswers") == "1" && r.URL.Query().Get("delsector") == "":
			var b strings.Builder
			for id := range alive {
				if alive[id] {
					fmt.Fprintf(&b, `<a href="?delsector=%d">del</a>`, id)
				}
			}
			_, _ = w.Write([]byte(b.String()))
		case strings.Contains(r.URL.Path, "LevelEditor.aspx") && r.URL.Query().Get("delsector") != "":
			id, _ := strconv.Atoi(r.URL.Query().Get("delsector"))
			alive[id] = false
			deleted = append(deleted, id)
			http.Redirect(w, r, "/Administration/Games/LevelEditor.aspx?gid=1&level=1&swanswers=1", http.StatusFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	client := New(host, WithHTTP(), WithAdminDelay(0))
	if err := client.AdminClearLevelSectors(context.Background(), 1, 1); err != nil {
		t.Fatalf("AdminClearLevelSectors: %v", err)
	}
	sort.Ints(deleted)
	if len(deleted) != 2 {
		t.Fatalf("expected editor orphan IDs deleted, got %v", deleted)
	}
}

func TestAdminClearLevelSectorsUsesDelsectorLinks(t *testing.T) {
	alive := map[int]bool{5: true, 7: true}
	var deleted []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "ALoader/LevelInfo.aspx") && r.URL.Query().Get("object") == "3" && r.URL.Query().Get("sector") == "":
			var b strings.Builder
			for id, name := range map[int]string{5: "Сектор A", 7: "Сектор B"} {
				if alive[id] {
					fmt.Fprintf(&b, `<option value="%d">%s</option>`, id, name)
				}
			}
			_, _ = w.Write([]byte(b.String()))
		case strings.Contains(r.URL.Path, "ALoader/LevelInfo.aspx") && r.URL.Query().Get("sector") != "":
			_, _ = w.Write([]byte(`<input name="txtAnswer_0" value="a">`))
		case strings.Contains(r.URL.Path, "LevelEditor.aspx") && r.URL.Query().Get("swanswers") == "1" && r.URL.Query().Get("delsector") == "":
			var b strings.Builder
			for id := range alive {
				if alive[id] {
					fmt.Fprintf(&b, `<a href="?delsector=%d">del</a>`, id)
				}
			}
			_, _ = w.Write([]byte(b.String()))
		case strings.Contains(r.URL.Path, "LevelEditor.aspx") && r.URL.Query().Get("delsector") != "":
			id, _ := strconv.Atoi(r.URL.Query().Get("delsector"))
			alive[id] = false
			deleted = append(deleted, id)
			http.Redirect(w, r, "/Administration/Games/LevelEditor.aspx?level=1&gid=1&swanswers=1", http.StatusFound)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	client := New(host, WithHTTP(), WithAdminDelay(0))
	if err := client.AdminClearLevelSectors(context.Background(), 1, 1); err != nil {
		t.Fatalf("AdminClearLevelSectors: %v", err)
	}
	sort.Ints(deleted)
	if len(deleted) != 2 || deleted[0] != 5 || deleted[1] != 7 {
		t.Fatalf("deleted IDs: %v", deleted)
	}
}

func TestAdminClearLevelSectorsReturnsStartedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("delsector") != "" {
			_, _ = w.Write([]byte(`Сектор не может быть удален, его начали проходить участники.`))
			return
		}
		_, _ = w.Write([]byte(`<a href="?delsector=5">del</a>`))
	}))
	defer srv.Close()

	client := New(strings.TrimPrefix(srv.URL, "http://"), WithHTTP(), WithAdminDelay(0))
	err := client.AdminClearLevelSectors(context.Background(), 1, 1)
	if !errors.Is(err, ErrSectorStarted) {
		t.Fatalf("got %v, want ErrSectorStarted", err)
	}
}

func TestAdminUpdateSectorGrowsAnswerFields(t *testing.T) {
	var saved []string
	var postCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm: %v", err)
			}
			postCount++
			saved = saved[:0]
			for i := 0; ; i++ {
				name := fmt.Sprintf("txtAnswer_%d", i)
				if _, ok := r.Form[name]; !ok {
					break
				}
				if answer := strings.TrimSpace(r.Form.Get(name)); answer != "" {
					saved = append(saved, answer)
				}
			}
			_, _ = w.Write([]byte(`ok`))
			return
		}

		fieldCount := len(saved) + 1
		if fieldCount < 1 {
			fieldCount = 1
		}
		var b strings.Builder
		for i := 0; i < fieldCount; i++ {
			value := ""
			if i < len(saved) {
				value = saved[i]
			}
			fmt.Fprintf(&b, `<input name="txtAnswer_%d" value="%s">`, i, value)
			fmt.Fprintf(&b, `<select name="ddlAnswerFor_%d"><option selected value="0">all</option></select>`, i)
		}
		_, _ = w.Write([]byte(b.String()))
	}))
	defer srv.Close()

	client := New(strings.TrimPrefix(srv.URL, "http://"), WithHTTP(), WithAdminDelay(0))
	err := client.AdminUpdateSector(context.Background(), 1, 1, 10, AdminSector{
		Name:    "Сектор 1",
		Answers: []string{"one", "two", "three"},
	})
	if err != nil {
		t.Fatalf("AdminUpdateSector: %v", err)
	}
	if postCount != 3 {
		t.Fatalf("postCount = %d, want 3", postCount)
	}
	if strings.Join(saved, ",") != "one,two,three" {
		t.Fatalf("saved answers = %v", saved)
	}
}
