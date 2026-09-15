package enapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type rateTestTransport func(*http.Request) (*http.Response, error)

func (f rateTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestRequestPacerConcurrentAndIdle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newRequestPacer()
		var mu sync.Mutex
		var times []time.Time
		var wg sync.WaitGroup
		for range 3100 {
			wg.Go(func() {
				if err := p.wait(t.Context(), apiRequestInterval); err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				times = append(times, time.Now())
				mu.Unlock()
			})
		}
		wg.Wait()
		for i := 1; i < len(times); i++ {
			if times[i].Sub(times[i-1]) < apiRequestInterval {
				t.Fatalf("burst at %d: %v", i, times[i].Sub(times[i-1]))
			}
		}
		left := 0
		for right, stamp := range times {
			for stamp.Sub(times[left]) >= time.Minute {
				left++
			}
			if count := right - left + 1; count > 1500 {
				t.Fatalf("rolling minute contained %d requests", count)
			}
		}
		time.Sleep(time.Minute)
		start := time.Now()
		if err := p.wait(t.Context(), apiRequestInterval); err != nil {
			t.Fatal(err)
		}
		if err := p.wait(t.Context(), apiRequestInterval); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) != apiRequestInterval {
			t.Fatalf("idle accumulated burst credit: %v", time.Since(start))
		}
	})
}

func TestRequestPacerCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newRequestPacer()
		if err := p.wait(t.Context(), apiRequestInterval); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		start := time.Now()
		if err := p.wait(ctx, apiRequestInterval); err != context.Canceled {
			t.Fatalf("got %v", err)
		}
		if time.Since(start) != 0 {
			t.Fatal("cancellation waited for permit")
		}
		if err := p.wait(t.Context(), apiRequestInterval); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) != apiRequestInterval {
			t.Fatal("canceled call consumed permit")
		}
	})
}

func TestAPIClientsShareHostRequestLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var times []time.Time
		transport := rateTestTransport(func(r *http.Request) (*http.Response, error) {
			times = append(times, time.Now())
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
		})
		hc := &http.Client{Transport: transport}
		a := New(hc, "https://shared-rate-test.invalid", "first.en.cx")
		b := New(hc, "https://shared-rate-test.invalid", "second.en.cx")
		for _, c := range []*Client{a, b, a} {
			if err := c.GetJSON(t.Context(), "/levels", nil, nil); err != nil {
				t.Fatal(err)
			}
		}
		if times[2].Sub(times[0]) != 2*apiRequestInterval {
			t.Fatalf("clients bypassed shared limit: %v", times)
		}
	})
}

func TestRequestPacerCancelWhileWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newRequestPacer()
		if err := p.wait(t.Context(), apiRequestInterval); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), apiRequestInterval/2)
		defer cancel()
		start := time.Now()
		if err := p.wait(ctx, apiRequestInterval); err != context.DeadlineExceeded {
			t.Fatalf("got %v", err)
		}
		if time.Since(start) != apiRequestInterval/2 {
			t.Fatal("did not cancel promptly")
		}
		if err := p.wait(t.Context(), apiRequestInterval); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) != apiRequestInterval {
			t.Fatal("canceled waiter consumed a permit")
		}
	})
}

func TestAPIRedirectUsesRequestLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var times []time.Time
		transport := rateTestTransport(func(r *http.Request) (*http.Response, error) {
			times = append(times, time.Now())
			response := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}
			if r.URL.Path == "/redirect" {
				response.StatusCode = http.StatusFound
				response.Header.Set("Location", "/target")
			}
			return response, nil
		})
		hc := &http.Client{Transport: transport}
		c := New(hc, "https://redirect-rate-test.invalid", "demo.en.cx")
		if err := c.GetJSON(t.Context(), "/redirect", nil, nil); err != nil {
			t.Fatal(err)
		}
		if len(times) != 2 || times[1].Sub(times[0]) != apiRequestInterval {
			t.Fatalf("redirect bypassed pacing: %v", times)
		}
		// The caller's transport must remain unpaced for legacy traffic.
		start := time.Now()
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://redirect-rate-test.invalid/target", nil)
		response, err := hc.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if time.Since(start) != 0 {
			t.Fatal("modified caller HTTP client")
		}
	})
}

func TestAPIConfiguredIntervalSharedAcrossClients(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var times []time.Time
		transport := rateTestTransport(func(r *http.Request) (*http.Response, error) {
			times = append(times, time.Now())
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
		})
		hc := &http.Client{Transport: transport}
		fast := New(hc, "https://configured-rate-test.invalid", "demo.en.cx", WithRequestInterval(20*time.Millisecond))
		slow := New(hc, "https://configured-rate-test.invalid", "other.en.cx", WithRequestInterval(200*time.Millisecond))
		for _, c := range []*Client{fast, slow, fast} {
			if err := c.GetJSON(t.Context(), "/levels", nil, nil); err != nil {
				t.Fatal(err)
			}
		}
		for i := 1; i < len(times); i++ {
			if times[i].Sub(times[i-1]) != 200*time.Millisecond {
				t.Fatalf("host budget bypassed: %v", times)
			}
		}
	})
}

func TestServerBudgetWaitExcludesNetworkTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var times []time.Time
		hc := &http.Client{Timeout: 5 * time.Second, Transport: rateTestTransport(func(r *http.Request) (*http.Response, error) {
			times = append(times, time.Now())
			h := make(http.Header)
			h.Set("X-Ratelimit-Limit", "1000")
			h.Set("X-Ratelimit-Remaining", "0")
			return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
		})}
		c := New(hc, "https://empty-budget-test.invalid", "demo.en.cx")
		for range 2 {
			if err := c.GetJSON(t.Context(), "/levels", nil, nil); err != nil {
				t.Fatal(err)
			}
		}
		if times[1].Sub(times[0]) < time.Minute {
			t.Fatalf("empty server budget ignored: %v", times)
		}
	})
}

func TestServerRemainingSlowsRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var times []time.Time
		hc := &http.Client{Transport: rateTestTransport(func(r *http.Request) (*http.Response, error) {
			times = append(times, time.Now())
			h := make(http.Header)
			h.Set("X-Ratelimit-Limit", "100")
			h.Set("X-Ratelimit-Remaining", "10")
			return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
		})}
		c := New(hc, "https://low-budget-test.invalid", "demo.en.cx")
		for range 2 {
			if err := c.GetJSON(t.Context(), "/levels", nil, nil); err != nil {
				t.Fatal(err)
			}
		}
		if times[1].Sub(times[0]) < 6*time.Second {
			t.Fatalf("server remaining ignored: %v", times)
		}
	})
}

func TestServerBudgetIsSharedAndWaitCancelable(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls := 0
		hc := &http.Client{Transport: rateTestTransport(func(r *http.Request) (*http.Response, error) {
			calls++
			h := make(http.Header)
			h.Set("X-Ratelimit-Limit", "1000")
			h.Set("X-Ratelimit-Remaining", "1")
			return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(`{}`)), Request: r}, nil
		})}
		a := New(hc, "https://shared-budget-test.invalid", "first.en.cx")
		b := New(hc, "https://shared-budget-test.invalid", "second.en.cx")
		if err := a.GetJSON(t.Context(), "/levels", nil, nil); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()
		if err := b.GetJSON(ctx, "/levels", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v", err)
		}
		if calls != 1 {
			t.Fatalf("spent last budget from other client: calls=%d", calls)
		}
	})
}

func TestInvalidBudgetHeadersDoNotClearCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newRequestPacer()
		h := make(http.Header)
		h.Set("X-Ratelimit-Limit", "1000")
		h.Set("X-Ratelimit-Remaining", "0")
		p.observe(h, 200)
		for _, value := range []string{"", "broken", "-1", "1001"} {
			h.Set("X-Ratelimit-Remaining", value)
			p.observe(h, 200)
		}
		start := time.Now()
		if err := p.wait(t.Context(), apiRequestInterval); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) < time.Minute {
			t.Fatal("malformed headers cleared budget")
		}
	})
}

func TestRetryAfterAndNetworkTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		p := newRequestPacer()
		h := make(http.Header)
		h.Set("Retry-After", "90")
		p.observe(h, 429)
		start := time.Now()
		if err := p.wait(t.Context(), apiRequestInterval); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) != 90*time.Second {
			t.Fatalf("Retry-After ignored: %v", time.Since(start))
		}
		hc := &http.Client{Timeout: 2 * time.Second, Transport: rateTestTransport(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() })}
		c := New(hc, "https://network-timeout-test.invalid", "demo.en.cx")
		start = time.Now()
		if err := c.GetJSON(t.Context(), "/levels", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("lost network timeout: %v", err)
		}
		if time.Since(start) != 2*time.Second {
			t.Fatalf("network timeout changed: %v", time.Since(start))
		}
	})
}
