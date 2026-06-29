package encx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminCreateHintSendsReplaceNlCheckboxWhenRequested(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/Administration/Games/PromptEdit.aspx" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		seen = append(seen, r.Form.Get("chkReplaceNlToBr"))
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := New(strings.TrimPrefix(server.URL, "http://"), WithHTTP(), WithAdminDelay(0))
	err := client.AdminCreateHint(t.Context(), 1, 2, AdminHint{
		Text:      "line 1\nline 2",
		ReplaceNl: true,
		Minutes:   5,
	})
	if err != nil {
		t.Fatalf("AdminCreateHint with ReplaceNl: %v", err)
	}
	err = client.AdminCreateHint(t.Context(), 1, 2, AdminHint{
		Text:    "<b>line 1</b>\n<br/>line 2",
		Minutes: 5,
	})
	if err != nil {
		t.Fatalf("AdminCreateHint without ReplaceNl: %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("seen %d POSTs, want 2", len(seen))
	}
	if seen[0] != "on" {
		t.Fatalf("first chkReplaceNlToBr = %q, want on", seen[0])
	}
	if seen[1] != "" {
		t.Fatalf("second chkReplaceNlToBr = %q, want empty", seen[1])
	}
}
