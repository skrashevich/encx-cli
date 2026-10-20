package encx

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func legacyPlayersPage(total int, rows string) string {
	return fmt.Sprintf(`<a id="lnkPlayersAll">Все</a><span id="spanTotal">%d</span><div id="divPlayers"><table>%s</table></div>`, total, rows)
}
func legacyPlayerRow(id int, action string) string {
	return fmt.Sprintf(`<tr ID="trPlayer"><td><a ID="lnkPlayer" href="UserDetails.aspx?uid=999">Test &amp; User</a></td><td>Принят к участию</td><td><a ID="lnkAction1" href="/GamePlayers.aspx?action=%s&amp;page=1&amp;sort=1&amp;status=-1&amp;player=%d&amp;gid=42">action</a></td></tr>`, action, id)
}

func TestAdminPlayersLegacyPagination(t *testing.T) {
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") != "-1" {
			t.Errorf("not requesting all statuses: %s", r.URL)
		}
		row := legacyPlayerRow(101, "delete")
		if r.URL.Query().Get("page") == "2" {
			row = legacyPlayerRow(102, "disqualify")
		}
		fmt.Fprint(w, legacyPlayersPage(2, row)+`<a href="/GamePlayers.aspx?gid=42&amp;status=-1&amp;sort=1&amp;page=2">2</a>`)
	})
	ps, err := c.AdminGetPlayers(t.Context(), 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 || ps[0].PlayerID != 101 || ps[0].UserID != 999 || ps[0].Name != "Test & User" || !ps[0].CanDelete || ps[1].CanDelete {
		t.Fatalf("players=%+v", ps)
	}
}

func TestAdminDeletePlayerLegacyVerifies(t *testing.T) {
	for _, refuse := range []bool{false, true} {
		t.Run(fmt.Sprint(refuse), func(t *testing.T) {
			deleted := false
			c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("action") == "delete" {
					if r.URL.Query().Get("player") != "101" || r.URL.Query().Get("gid") != "42" {
						t.Errorf("wrong target: %s", r.URL)
					}
					deleted = !refuse
				}
				if deleted {
					fmt.Fprint(w, legacyPlayersPage(0, ""))
				} else {
					fmt.Fprint(w, legacyPlayersPage(1, legacyPlayerRow(101, "delete")))
				}
			})
			err := c.AdminDeletePlayer(t.Context(), 42, 101)
			if (err != nil) != refuse {
				t.Fatalf("refuse=%v err=%v", refuse, err)
			}
		})
	}
}

func TestAdminPlayersLegacyRejectsIncompleteOrPublicView(t *testing.T) {
	for _, body := range []string{`<html>login</html>`, `<div id="divPlayers">Игроки отсутствуют</div>`, legacyPlayersPage(2, legacyPlayerRow(101, "delete")), strings.ReplaceAll(legacyPlayersPage(1, legacyPlayerRow(101, "delete")), `id="lnkPlayersAll"`, `id="lnkPlayersAll" href="?status=-1"`)} {
		t.Run(fmt.Sprint(len(body)), func(t *testing.T) {
			c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			if _, err := c.AdminGetPlayers(t.Context(), 42); err == nil {
				t.Fatal("accepted incomplete view")
			}
		})
	}
}

func TestAdminDeletePlayerLegacyRefusesWrongTargets(t *testing.T) {
	for _, tc := range []struct {
		name, row string
		id        int
	}{
		{"not found", legacyPlayerRow(101, "delete"), 999},
		{"started", legacyPlayerRow(101, "disqualify"), 101},
		{"external link", strings.ReplaceAll(legacyPlayerRow(101, "delete"), `href="/GamePlayers`, `href="https://evil.example/GamePlayers`), 101},
		{"other game", strings.ReplaceAll(legacyPlayerRow(101, "delete"), "gid=42", "gid=43"), 101},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("action") != "" {
					t.Error("must not mutate")
				}
				fmt.Fprint(w, legacyPlayersPage(1, tc.row))
			})
			if err := c.AdminDeletePlayer(t.Context(), 42, tc.id); err == nil {
				t.Fatal("unsafe delete accepted")
			}
		})
	}
}

func newPlayersJSON(page, pages, total int, rows string) string {
	return fmt.Sprintf(`{"game_id":42,"can_view":true,"has_game_management_permission":true,"status_id":-1,"page":%d,"total_pages":%d,"total_rows":%d,"players":[%s]}`, page, pages, total, rows)
}

const newPlayerJSON = `{"player_id":101,"player_name":"Test","user_id":999,"status_id":0,"can_delete":true}`

func TestAdminPlayersNewPagination(t *testing.T) {
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/games/42/players" || r.URL.Query().Get("status") != "-1" {
			t.Errorf("unexpected URL %s", r.URL)
		}
		if r.URL.Query().Get("page") == "1" {
			fmt.Fprint(w, newPlayersJSON(1, 2, 2, newPlayerJSON))
		} else {
			fmt.Fprint(w, newPlayersJSON(2, 2, 2, strings.ReplaceAll(newPlayerJSON, "101", "102")))
		}
	})
	ps, err := c.AdminGetPlayers(t.Context(), 42)
	if err != nil || len(ps) != 2 || ps[1].PlayerID != 102 {
		t.Fatalf("players=%+v err=%v", ps, err)
	}
}

func TestAdminDeletePlayerNewVerifies(t *testing.T) {
	for _, mode := range []string{"success", "refused", "lost permission", "http error"} {
		t.Run(mode, func(t *testing.T) {
			sent := false
			c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					if r.URL.Path != "/games/42/players/101/delete" {
						t.Errorf("wrong mutation %s", r.URL)
					}
					b, _ := io.ReadAll(r.Body)
					if string(b) != `{"confirm":true}` {
						t.Errorf("body=%s", b)
					}
					sent = true
					if mode == "http error" {
						http.Error(w, "forbidden", 403)
						return
					}
					fmt.Fprint(w, `{"success":true}`)
					return
				}
				body := newPlayersJSON(1, 1, 1, newPlayerJSON)
				if sent && mode == "success" {
					body = newPlayersJSON(1, 0, 0, "")
				}
				if sent && mode == "lost permission" {
					body = strings.ReplaceAll(body, `"has_game_management_permission":true`, `"has_game_management_permission":false`)
				}
				fmt.Fprint(w, body)
			})
			err := c.AdminDeletePlayer(t.Context(), 42, 101)
			if (err == nil) != (mode == "success") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
		})
	}
}

func TestAdminPlayersRejectInvalidIDs(t *testing.T) {
	c := verifyingClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
	if _, err := c.AdminGetPlayers(t.Context(), 0); err == nil {
		t.Fatal("zero game ID")
	}
	for _, ids := range [][2]int{{0, 1}, {1, 0}, {1, -1}} {
		if err := c.AdminDeletePlayer(t.Context(), ids[0], ids[1]); err == nil {
			t.Fatalf("accepted %v", ids)
		}
	}
}
