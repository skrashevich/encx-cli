package encx

import (
	"testing"
)

func TestDomainGamesFromList(t *testing.T) {
	list := &GameListResponse{
		ActiveGames: []GameInfo{
			{GameID: 77734, Title: "Active game"},
			{GameID: 82000, Title: "Another active"},
		},
		ComingGames: []GameInfo{
			{GameID: 82228, Title: "Coming game"},
			{GameID: 77734, Title: "Duplicate active"},
		},
	}

	games := domainGamesFromList(list)
	if len(games) != 3 {
		t.Fatalf("expected 3 games, got %d", len(games))
	}
	if games[0].GameId != 77734 || games[0].Title != "Active game" {
		t.Fatalf("unexpected first game: %+v", games[0])
	}
	if games[2].GameId != 82228 {
		t.Fatalf("expected coming game last, got %+v", games[2])
	}
}

func TestGameTitleDesktopReIgnoresActionLinks(t *testing.T) {
	html := []byte(`
<a id="lnkGameTitle" href="/GameDetails.aspx?gid=79677">Игра 3</a>
<a href="/GameDetails.aspx?gid=81812" target="_blank">ссылке</a>
<a class="btn_reg_game" href="/MakeGameFee.aspx?gid=76339">Зарегистрировать свою команду</a>
<a href="/GameDetails.aspx?gid=79677">подать заявку на участие</a>
<a href="/GameDetails.aspx?gid=76339">Зарегистрировать свою команду</a>
`)

	matches := gameTitleDesktopRe.FindAllSubmatch(html, -1)
	if len(matches) != 1 {
		t.Fatalf("expected 1 game title match, got %d", len(matches))
	}
	if string(matches[0][1]) != "79677" || string(matches[0][2]) != "Игра 3" {
		t.Fatalf("unexpected match: id=%q title=%q", matches[0][1], matches[0][2])
	}
}
