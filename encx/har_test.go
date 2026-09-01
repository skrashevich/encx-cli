package encx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHARRecorderCapturesRequestResponse(t *testing.T) {
	server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %q", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	server.Start()

	client := New("example.test", WithHTTP(), WithHARRecording(true))

	_, err := client.doGet(t.Context(), server.URL+"/GameEngine.aspx?json=1")
	if err != nil {
		t.Fatalf("doGet: %v", err)
	}

	if got := client.HAREntryCount(); got != 1 {
		t.Fatalf("HAREntryCount = %d, want 1", got)
	}

	raw, err := client.ExportHARJSON()
	if err != nil {
		t.Fatalf("ExportHARJSON: %v", err)
	}

	var doc struct {
		Log struct {
			Version string `json:"version"`
			Entries []struct {
				Request struct {
					Method string `json:"method"`
					URL    string `json:"url"`
				} `json:"request"`
				Response struct {
					Status  int `json:"status"`
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"response"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if doc.Log.Version != "1.2" {
		t.Fatalf("version = %q", doc.Log.Version)
	}
	if len(doc.Log.Entries) != 1 {
		t.Fatalf("entries = %d", len(doc.Log.Entries))
	}
	entry := doc.Log.Entries[0]
	if entry.Request.Method != http.MethodGet {
		t.Fatalf("request method = %q", entry.Request.Method)
	}
	if entry.Response.Status != http.StatusOK {
		t.Fatalf("status = %d", entry.Response.Status)
	}
	if entry.Response.Content.Text != `{"ok":true}` {
		t.Fatalf("body = %q", entry.Response.Content.Text)
	}
}

func TestHARRecorderClear(t *testing.T) {
	client := New("example.test", WithHARRecording(true))
	client.ensureHAR().append(harEntry{})
	if client.HAREntryCount() != 1 {
		t.Fatalf("expected seeded entry")
	}
	client.ClearHAR()
	if client.HAREntryCount() != 0 {
		t.Fatalf("ClearHAR did not reset entries")
	}
}

func TestHARRedactsLoginPassword(t *testing.T) {
	server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Error":0}`))
	}))
	server.Start()

	host := strings.TrimPrefix(server.URL, "http://")
	client := New(host, WithHTTP(), WithHARRecording(true))
	_, err := client.Login(t.Context(), "player", "super-secret")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	raw, err := client.ExportHARJSON()
	if err != nil {
		t.Fatalf("ExportHARJSON: %v", err)
	}
	if strings.Contains(raw, "super-secret") {
		t.Fatalf("HAR must not contain plaintext password: %s", raw)
	}
	if !strings.Contains(raw, url.QueryEscape(harRedactedSecret)) && !strings.Contains(raw, harRedactedSecret) {
		t.Fatalf("HAR should contain redacted placeholder: %s", raw)
	}

	var doc struct {
		Log struct {
			Entries []struct {
				Request struct {
					PostData struct {
						Text string `json:"text"`
					} `json:"postData"`
				} `json:"request"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(doc.Log.Entries) == 0 {
		t.Fatal("expected HAR entries")
	}
	payload, err := url.ParseQuery(doc.Log.Entries[0].Request.PostData.Text)
	if err != nil {
		t.Fatalf("login payload: %v", err)
	}
	if payload.Get("Login") != "player" {
		t.Fatalf("login = %q", payload.Get("Login"))
	}
	if payload.Get("Password") != harRedactedSecret {
		t.Fatalf("password = %q", payload.Get("Password"))
	}
}

func TestHARRecorderSnapshotClearFirstKeepsNewEntries(t *testing.T) {
	rec := NewHARRecorder()
	rec.append(harEntry{Comment: "exported-1"})
	rec.append(harEntry{Comment: "exported-2"})

	doc, count, err := rec.ExportSnapshot()
	if err != nil {
		t.Fatalf("ExportSnapshot: %v", err)
	}
	if count != 2 {
		t.Fatalf("snapshot count = %d, want 2", count)
	}
	if !strings.Contains(doc, "exported-1") || !strings.Contains(doc, "exported-2") {
		t.Fatalf("snapshot does not contain exported entries: %q", doc)
	}

	// Запись, добавленная между экспортом и очисткой, должна пережить ClearFirst.
	rec.append(harEntry{Comment: "captured-during-upload"})
	rec.ClearFirst(count)

	if got := rec.EntryCount(); got != 1 {
		t.Fatalf("EntryCount after ClearFirst = %d, want 1", got)
	}
	after, _, err := rec.ExportSnapshot()
	if err != nil {
		t.Fatalf("ExportSnapshot after clear: %v", err)
	}
	if strings.Contains(after, "exported-1") {
		t.Fatalf("cleared entry still exported: %q", after)
	}
	if !strings.Contains(after, "captured-during-upload") {
		t.Fatalf("entry captured during upload was lost: %q", after)
	}

	rec.ClearFirst(100)
	if got := rec.EntryCount(); got != 0 {
		t.Fatalf("EntryCount after over-clear = %d, want 0", got)
	}
}
