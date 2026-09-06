package encx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPlayRedirectToHomeMeansTheGameIsNotHere pins the engine behaviour behind
// a real support case: the play URL for a game this domain does not host (a
// wrong id, or a game living on another domain) answers 302 to the site's home
// page. Decoding that page reported "retry shortly", sending the player to
// retry a request that can never succeed.
func TestPlayRedirectToHomeMeansTheGameIsNotHere(t *testing.T) {
	t.Setenv(EngineEnvVar, "legacy")
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/gameengines/encounter/play/") {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	srv.Start()
	host := strings.TrimPrefix(srv.URL, "http://")

	c := New(host, WithHTTP())

	_, err := c.GetGameModel(context.Background(), 82758)
	if err == nil {
		t.Fatal("a redirect to the home page must be an error")
	}
	if !IsGameNotFound(err) {
		t.Fatalf("error = %v, want a GameNotFoundError", err)
	}
	for _, marker := range []string{"82758", host} {
		if !strings.Contains(err.Error(), marker) {
			t.Errorf("error %q does not name %q", err.Error(), marker)
		}
	}
	if strings.Contains(strings.ToLower(err.Error()), "retry") {
		t.Errorf("error %q suggests retrying a permanent condition", err.Error())
	}

	// The POST path (answer submission) hits the same engine behaviour.
	_, err = c.SendCode(context.Background(), 82758, 1, 1, "code")
	if !IsGameNotFound(err) {
		t.Fatalf("SendCode error = %v, want a GameNotFoundError", err)
	}
}

// TestPlayRedirectElsewhereStaysGeneric keeps the new classification narrow: a
// redirect to anything but the home page is not proof the game does not exist,
// so it must not claim that.
func TestPlayRedirectElsewhereStaysGeneric(t *testing.T) {
	t.Setenv(EngineEnvVar, "legacy")
	srv := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/GameDetails.aspx?gid=82758", http.StatusFound)
	}))
	srv.Start()
	host := strings.TrimPrefix(srv.URL, "http://")

	c := New(host, WithHTTP())

	_, err := c.GetGameModel(context.Background(), 82758)
	if err == nil {
		t.Fatal("a redirect where JSON was expected must be an error")
	}
	if IsGameNotFound(err) {
		t.Fatalf("error = %v: a redirect to the game page must not claim the game is missing", err)
	}
}
