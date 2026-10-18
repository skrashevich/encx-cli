package encx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestResolveEngineDistinguishesFallbackFromConfirmedSelection(t *testing.T) {
	var reachable atomic.Bool
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		if !reachable.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"id":135,"primary_domain":"demo.en.cx","is_site_active_by_rule":true}`))
	}))
	t.Cleanup(server.Close)
	client := New("demo.en.cx", WithEngine(EngineAuto), WithAPIBaseURL(server.URL))
	if got := client.ResolveEngine(t.Context()); got.Engine != EngineLegacy || got.Definitive {
		t.Fatalf("failed probe must be a temporary legacy fallback: %+v", got)
	}
	reachable.Store(true)
	if got := client.ResolveEngine(t.Context()); got.Engine != EngineNew || !got.Definitive {
		t.Fatalf("recovered probe must confirm the new engine: %+v", got)
	}
	before := probes.Load()
	if client.Engine() != EngineNew || !client.ResolveEngine(t.Context()).Definitive {
		t.Fatal("old and new APIs must share the confirmed selection")
	}
	if probes.Load() != before {
		t.Fatal("confirmed selection was probed again")
	}
	client.SetEngine(EngineLegacy)
	if got := client.ResolveEngine(t.Context()); got.Engine != EngineLegacy || !got.Definitive {
		t.Fatalf("explicit selection must override the cached probe: %+v", got)
	}
	client.SetEngine(EngineAuto)
	if got := client.ResolveEngine(t.Context()); got.Engine != EngineNew || !got.Definitive || probes.Load() == before {
		t.Fatalf("returning to auto must discard the previous verdict: %+v", got)
	}
}

func TestResolveEngineConfirmsLegacyWithoutRepeatedProbes(t *testing.T) {
	var probes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		http.Error(w, `{"error":"domain_unregistered"}`, http.StatusNotFound)
	}))
	t.Cleanup(server.Close)
	client := New("demo.en.cx", WithEngine(EngineAuto), WithAPIBaseURL(server.URL))
	for range 2 {
		if got := client.ResolveEngine(t.Context()); got.Engine != EngineLegacy || !got.Definitive {
			t.Fatalf("404 must confirm legacy: %+v", got)
		}
	}
	if probes.Load() != 1 {
		t.Fatalf("probes = %d, want 1", probes.Load())
	}
}

func TestResolveEngineCancellationDoesNotPinFallback(t *testing.T) {
	client := New("demo.en.cx", WithEngine(EngineAuto))
	ctx := t.Context()
	// An already cancelled context must never turn a failed probe into a verdict.
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if got := client.ResolveEngine(ctx); got.Engine != EngineLegacy || got.Definitive {
		t.Fatalf("cancelled probe was confirmed: %+v", got)
	}
}
