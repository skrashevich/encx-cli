package encx

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// GameNotFoundError means the ASP.NET engine does not host this game: the play
// URL answered with a redirect to the site's home page, which is how the
// engine reports a game id the domain does not know (a typo, or a game that
// lives on another domain). This is a permanent condition — retrying or
// re-logging in cannot make the game appear.
type GameNotFoundError struct {
	GameID int
	Domain string
}

func (e *GameNotFoundError) Error() string {
	return fmt.Sprintf("encx: game %d does not exist on %s (the engine redirected to the home page); check the game id and its domain",
		e.GameID, e.Domain)
}

// IsGameNotFound reports whether err says the domain does not host the game.
func IsGameNotFound(err error) bool {
	var gnf *GameNotFoundError
	return errors.As(err, &gnf)
}

// gameNotFoundFromRedirect classifies a redirect on a play request. A redirect
// to the sign-in page never reaches here (doRequestAndRead reports it as an
// expired session); a redirect to the home page is the engine disowning the
// game id. Any other target is left for the generic HTML classification, which
// does not claim more than it knows.
func (c *Client) gameNotFoundFromRedirect(gameId, status int, headers http.Header) error {
	if !isRedirectStatus(status) {
		return nil
	}
	location := strings.TrimSpace(headers.Get("Location"))
	if location == "" {
		return nil
	}
	resolved, err := resolveAgainstBase(c.baseURL()+"/", location)
	if err != nil {
		return nil
	}
	target, err := url.Parse(resolved)
	if err != nil {
		return nil
	}
	if target.Path == "" || target.Path == "/" {
		return &GameNotFoundError{GameID: gameId, Domain: c.domain}
	}
	return nil
}
