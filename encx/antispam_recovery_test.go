package encx_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

func TestLoginForAntiSpamRecoveryDoesNotRecurseHandler(t *testing.T) {
	var handlerCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login/signin" {
			http.Redirect(w, r, "/NotHumanRequest.aspx?return=%2f", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	host := srv.Listener.Addr().String()
	client := encx.New(host, encx.WithHTTP(), encx.WithAntiSpamHandler(func(string) error {
		handlerCalls.Add(1)
		return nil
	}))

	_, err := client.LoginForAntiSpamRecovery(t.Context(), "", "u", "p")
	if err == nil {
		t.Fatal("expected anti-spam error from login")
	}
	if !encx.IsAntiSpam(err) {
		t.Fatalf("expected anti-spam error, got %v", err)
	}
	if handlerCalls.Load() != 0 {
		t.Fatalf("handler called %d times during recovery login, want 0", handlerCalls.Load())
	}
}
