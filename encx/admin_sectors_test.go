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
)

func TestParseSectorAnswerFields(t *testing.T) {
	body := `<input name="txtAnswer_0" value="">
	<input name="txtAnswer_6588020" value="поехалистрадать">
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
