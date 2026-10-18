package encxmobile

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestResolveEngineJSONRetainsRetryAfterProbeFailure(t *testing.T) {
	var reachable atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !reachable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"id":135,"primary_domain":"demo.en.cx","is_site_active_by_rule":true}`))
	}))
	t.Cleanup(server.Close)
	client := NewClientWithAPIOptions("demo.en.cx", false, false, 3, "ru", server.URL)
	client.SetEngine("auto")
	for _, tt := range []struct {
		reachable  bool
		engine     string
		definitive bool
	}{
		{false, "legacy", false}, {true, "new", true},
	} {
		reachable.Store(tt.reachable)
		out, err := client.ResolveEngine()
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Engine     string `json:"engine"`
			Definitive bool   `json:"definitive"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatal(err)
		}
		if got.Engine != tt.engine || got.Definitive != tt.definitive {
			t.Fatalf("resolution = %s", out)
		}
	}
	if client.Engine() != "new" {
		t.Fatal("existing Engine API did not retain the resolution")
	}
}

func TestLevelSectorProgressJSON(t *testing.T) {
	for _, tt := range []struct{ input, want string }{
		{`null`, `{"total":0,"done":0,"required":0,"remaining":null}`},
		{`{"RequiredSectorsCount":1,"Sectors":[]}`, `{"total":1,"done":0,"required":1,"remaining":1}`},
		{`{"RequiredSectorsCount":1,"IsPassed":true}`, `{"total":1,"done":0,"required":1,"remaining":0}`},
	} {
		out, err := LevelSectorProgress(tt.input)
		if err != nil {
			t.Fatal(err)
		}
		if out != tt.want {
			t.Fatalf("progress = %s, want %s", out, tt.want)
		}
	}
	for _, input := range []string{`{`, `[]`, `"level"`} {
		if _, err := LevelSectorProgress(input); err == nil {
			t.Fatalf("accepted malformed level %q", input)
		}
	}
}
