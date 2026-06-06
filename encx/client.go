package encx

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const defaultUserAgent = "encx-cli"
const defaultTimeout = 15 * time.Second

// Client is an HTTP client for the Encounter (en.cx) game engine API.
type Client struct {
	domain               string
	scheme               string
	httpClient           *http.Client
	rootTransport        http.RoundTripper
	userAgent            string
	lang                 string
	debugLog             func(string, ...any)
	har                  *HARRecorder
	harAutoEnabled       bool
	antiSpamHandler      func(string) error
	antiSpamRecovery     atomic.Bool // suppress handler during anti-spam recovery Login
	adminDelayDuration   time.Duration
	adminDelayConfigured bool
}

const defaultAdminDelay = 1200 * time.Millisecond

// Option configures the Client.
type Option func(*Client)

// WithInsecureTLS disables TLS certificate verification.
func WithInsecureTLS() Option {
	return func(c *Client) {
		transport := c.httpClient.Transport.(*http.Transport)
		transport.TLSClientConfig.InsecureSkipVerify = true
	}
}

// WithUserAgent sets a custom User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		c.userAgent = ua
	}
}

// WithHTTP forces plain HTTP instead of HTTPS.
func WithHTTP() Option {
	return func(c *Client) {
		c.scheme = "http"
	}
}

// WithTimeout sets the HTTP client timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = d
	}
}

// WithLang sets the language parameter for API requests.
func WithLang(lang string) Option {
	return func(c *Client) {
		c.lang = lang
	}
}

// WithDebugLogger enables verbose request/response logging.
func WithDebugLogger(logf func(string, ...any)) Option {
	return func(c *Client) {
		c.debugLog = logf
	}
}

// WithHARRecording enables HAR 1.2 capture for all HTTP requests made by the client.
func WithHARRecording(enabled bool) Option {
	return func(c *Client) {
		c.harAutoEnabled = enabled
		if c.har == nil {
			c.har = NewHARRecorder()
		}
		c.har.SetEnabled(enabled)
	}
}

// WithAdminDelay sets the pause between admin panel requests (default 1200ms).
// Pass 0 to disable throttling (e.g. httptest).
func WithAdminDelay(d time.Duration) Option {
	return func(c *Client) {
		c.adminDelayDuration = d
		c.adminDelayConfigured = true
	}
}

// SetAdminDelay overrides the pause between admin panel requests at runtime.
func (c *Client) SetAdminDelay(d time.Duration) {
	c.adminDelayDuration = d
	c.adminDelayConfigured = true
}

// AdminDelay returns the configured pause between admin panel requests.
func (c *Client) AdminDelay() time.Duration {
	if c.adminDelayConfigured {
		return c.adminDelayDuration
	}
	return defaultAdminDelay
}

// WithAntiSpamHandler sets a callback used when Encounter anti-spam challenge is detected.
// The callback should block until user passes verification (or return an error to abort).
func WithAntiSpamHandler(handler func(url string) error) Option {
	return func(c *Client) {
		c.antiSpamHandler = handler
	}
}

// New creates a new Encounter API client for the given domain.
// By default it uses HTTPS, a 15-second timeout, and the standard User-Agent.
func New(domain string, opts ...Option) *Client {
	jar, _ := cookiejar.New(nil)

	rootTransport := &http.Transport{
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"http/1.1"},
		},
	}

	c := &Client{
		domain:        domain,
		scheme:        "https",
		userAgent:     defaultUserAgent,
		lang:          "ru",
		rootTransport: rootTransport,
		httpClient: &http.Client{
			Timeout:   defaultTimeout,
			Jar:       jar,
			Transport: rootTransport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	for _, opt := range opts {
		opt(c)
	}
	c.rebuildTransport()

	return c
}

func (c *Client) ensureHAR() *HARRecorder {
	if c.har == nil {
		c.har = NewHARRecorder()
		if c.harAutoEnabled {
			c.har.SetEnabled(true)
		}
	}
	return c.har
}

// SetHARRecordingEnabled toggles HAR capture for subsequent HTTP requests.
func (c *Client) SetHARRecordingEnabled(enabled bool) {
	c.ensureHAR().SetEnabled(enabled)
	c.rebuildTransport()
}

// ClearHAR removes all captured HAR entries.
func (c *Client) ClearHAR() {
	c.ensureHAR().Clear()
}

// HAREntryCount returns the number of captured HAR entries.
func (c *Client) HAREntryCount() int {
	return c.ensureHAR().EntryCount()
}

// ExportHARJSON returns captured traffic as a HAR 1.2 JSON document.
func (c *Client) ExportHARJSON() (string, error) {
	return c.ensureHAR().ExportJSON()
}

func (c *Client) rebuildTransport() {
	transport := c.rootTransport
	if c.debugLog != nil {
		transport = &debugTransport{
			base:   transport,
			debugf: c.debugf,
		}
	}
	if c.har != nil && c.har.Enabled() {
		transport = c.har.wrap(transport)
	}
	c.httpClient.Transport = transport
}

func (c *Client) baseURL() string {
	return c.scheme + "://" + c.domain
}

func (c *Client) mobileBaseURL() string {
	return c.scheme + "://m." + c.domain
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", c.userAgent)
}

func (c *Client) debugf(format string, args ...any) {
	if c.debugLog == nil {
		return
	}
	c.debugLog(format, args...)
}

type debugTransport struct {
	base   http.RoundTripper
	debugf func(string, ...any)
}

// httpRedirectDebugTarget returns an absolute redirect URL for debug logging.
func httpRedirectDebugTarget(req *http.Request, resp *http.Response) string {
	loc := strings.TrimSpace(resp.Header.Get("Location"))
	if loc == "" {
		if refresh := strings.TrimSpace(resp.Header.Get("Refresh")); refresh != "" {
			return "Refresh: " + refresh
		}
		return ""
	}
	ref, err := url.Parse(loc)
	if err != nil {
		return loc
	}
	base := req.URL
	if resp.Request != nil && resp.Request.URL != nil {
		base = resp.Request.URL
	}
	if base == nil {
		return loc
	}
	return base.ResolveReference(ref).String()
}

func (t *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	urlStr := req.URL.String()
	if req.ContentLength > 0 {
		t.debugf("encx http start: %s %s body_bytes=%d", req.Method, urlStr, req.ContentLength)
	} else {
		t.debugf("encx http start: %s %s", req.Method, urlStr)
	}

	resp, err := t.base.RoundTrip(req)
	duration := time.Since(start).Round(time.Millisecond)
	if err != nil {
		t.debugf("encx http error: %s %s duration=%s err=%v", req.Method, urlStr, duration, err)
		return nil, err
	}

	contentType := resp.Header.Get("Content-Type")
	if isRedirectStatus(resp.StatusCode) {
		target := httpRedirectDebugTarget(req, resp)
		if target == "" {
			target = "(no Location header)"
		}
		t.debugf("encx http done: %s %s status=%d duration=%s redirect=%s content_type=%q",
			req.Method, urlStr, resp.StatusCode, duration, target, contentType)
	} else {
		t.debugf("encx http done: %s %s status=%d duration=%s content_type=%q",
			req.Method, urlStr, resp.StatusCode, duration, contentType)
	}

	return resp, nil
}

// doGet performs a GET request and returns the response body as a string.
func (c *Client) doGet(ctx context.Context, rawURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("encx: create request: %w", err)
	}
	c.setHeaders(req)

	_, _, body, err := c.doRequestAndRead(req)
	if err != nil {
		return "", fmt.Errorf("encx: GET %s: %w", rawURL, err)
	}

	return string(body), nil
}

func cloneRequestForRetry(req *http.Request) (*http.Request, error) {
	cloned := req.Clone(req.Context())
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err != nil {
			return nil, err
		}
		cloned.Body = body
	}
	return cloned, nil
}

func (c *Client) doRequestAndRead(req *http.Request) (int, http.Header, []byte, error) {
	for {
		attemptReq, err := cloneRequestForRetry(req)
		if err != nil {
			return 0, nil, nil, err
		}

		resp, err := c.httpClient.Do(attemptReq)
		if err != nil {
			return 0, nil, nil, err
		}

		statusCode := resp.StatusCode
		headers := resp.Header.Clone()
		body, readErr := c.readResponseBody(resp)
		// readResponseBody consumes body but does not close it.
		_ = resp.Body.Close()

		if readErr == nil {
			if err := guardHTTPLoginRedirect(statusCode, headers, body); err != nil {
				return statusCode, headers, body, err
			}
			return statusCode, headers, body, nil
		}
		if IsAntiSpam(readErr) && c.antiSpamHandler != nil && !c.antiSpamRecovery.Load() {
			if err := c.antiSpamHandler(AntiSpamURLFromError(readErr)); err != nil {
				return statusCode, headers, nil, err
			}
			continue
		}
		return statusCode, headers, nil, readErr
	}
}
