package enapi

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The configured interval is a floor. Server response budgets can only slow it.
const apiRequestInterval = 40 * time.Millisecond

// Without a reset header, use a conservative minute window. This is a client
// fallback, not a server-confirmed reset timestamp.
const apiBudgetWindow = time.Minute

var apiHostPacers sync.Map

type requestPacer struct {
	gate         chan struct{}
	last         time.Time
	interval     time.Duration
	limit        int
	remaining    int
	budgetUntil  time.Time
	blockedUntil time.Time
}

func newRequestPacer() *requestPacer { return &requestPacer{gate: make(chan struct{}, 1)} }

// acquire holds the host gate until the response headers have been accounted
// for, so concurrent callers cannot spend an unobserved last permit.
func (p *requestPacer) acquire(ctx context.Context, interval time.Duration) error {
	select {
	case p.gate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := p.waitLocked(ctx, interval); err != nil {
		p.release()
		return err
	}
	return nil
}
func (p *requestPacer) release() { <-p.gate }
func (p *requestPacer) wait(ctx context.Context, interval time.Duration) error {
	if err := p.acquire(ctx, interval); err != nil {
		return err
	}
	p.release()
	return nil
}
func (p *requestPacer) waitLocked(ctx context.Context, interval time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now()
	p.interval = max(p.interval, interval)
	spacing := p.interval
	deadline := p.last.Add(spacing)
	if p.limit > 0 {
		reserve := max(1, p.limit/20)
		spacing = max(spacing, apiBudgetWindow/time.Duration(max(1, p.limit-reserve)))
		if now.Before(p.budgetUntil) {
			available := p.remaining - reserve
			if available <= 0 {
				deadline = maxTime(deadline, p.budgetUntil)
			} else {
				spacing = max(spacing, p.budgetUntil.Sub(now)/time.Duration(available))
			}
		}
		deadline = maxTime(deadline, p.last.Add(spacing))
	}
	deadline = maxTime(deadline, p.blockedUntil)
	if delay := time.Until(deadline); delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	p.last = time.Now()
	if p.limit > 0 {
		if !p.last.Before(p.budgetUntil) {
			p.budgetUntil = p.last.Add(apiBudgetWindow)
			p.remaining = p.limit
		}
		p.remaining = max(0, p.remaining-1)
	}
	return nil
}
func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func (p *requestPacer) observe(h http.Header, status int) {
	now := time.Now()
	limit, le := strconv.Atoi(h.Get("X-Ratelimit-Limit"))
	remaining, re := strconv.Atoi(h.Get("X-Ratelimit-Remaining"))
	if le == nil && re == nil && limit > 0 && remaining >= 0 && remaining <= limit {
		if p.limit == 0 || !now.Before(p.budgetUntil) || remaining > p.remaining || limit != p.limit {
			p.budgetUntil = now.Add(apiBudgetWindow)
		}
		p.limit = limit
		p.remaining = remaining
		if remaining <= max(1, limit/20) {
			p.blockedUntil = maxTime(p.blockedUntil, now.Add(apiBudgetWindow))
		}
	}
	if status == http.StatusTooManyRequests {
		p.blockedUntil = maxTime(p.blockedUntil, now.Add(apiBudgetWindow))
	}
	if raw := h.Get("Retry-After"); raw != "" {
		if seconds, err := strconv.ParseInt(raw, 10, 32); err == nil && seconds >= 0 {
			p.blockedUntil = maxTime(p.blockedUntil, now.Add(time.Duration(seconds)*time.Second))
		} else if at, err := http.ParseTime(raw); err == nil {
			p.blockedUntil = maxTime(p.blockedUntil, at)
		}
	}
}

type pacedAPITransport struct {
	base           http.RoundTripper
	interval       time.Duration
	attemptTimeout time.Duration
}

func (p pacedAPITransport) RoundTrip(req *http.Request) (*http.Response, error) {
	host := strings.ToLower(req.URL.Host)
	value, ok := apiHostPacers.Load(host)
	if !ok {
		value, _ = apiHostPacers.LoadOrStore(host, newRequestPacer())
	}
	pacer := value.(*requestPacer)
	if err := pacer.acquire(req.Context(), p.interval); err != nil {
		return nil, err
	}
	defer pacer.release()
	// Budget waits must not consume the client's network timeout. The caller's
	// explicit context deadline still covers both waiting and the actual request.
	cancel := func() {}
	if p.attemptTimeout > 0 {
		var ctx context.Context
		ctx, cancel = context.WithTimeout(req.Context(), p.attemptTimeout)
		req = req.Clone(ctx)
	}
	resp, err := p.base.RoundTrip(req)
	if err != nil {
		cancel()
		return resp, err
	}
	pacer.observe(resp.Header, resp.StatusCode)
	if resp.Body != nil {
		resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
	} else {
		cancel()
	}
	return resp, nil
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error { defer b.cancel(); return b.ReadCloser.Close() }
