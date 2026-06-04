package encx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHARRecorderCapturesRequestResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %q", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

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
					Status int `json:"status"`
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
