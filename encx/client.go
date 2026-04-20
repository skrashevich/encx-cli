package encx

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"time"
)

const defaultUserAgent = "EnApp by necto68"
const defaultTimeout = 15 * time.Second

// Client is an HTTP client for the Encounter (en.cx) game engine API.
type Client struct {
	domain     string
	scheme     string
	httpClient *http.Client
	userAgent  string
	lang       string
	debugLog   func(string, ...any)
}

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

// New creates a new Encounter API client for the given domain.
// By default it uses HTTPS, a 15-second timeout, and the standard User-Agent.
func New(domain string, opts ...Option) *Client {
	jar, _ := cookiejar.New(nil)

	c := &Client{
		domain:    domain,
		scheme:    "https",
		userAgent: defaultUserAgent,
		lang:      "ru",
		httpClient: &http.Client{
			Timeout: defaultTimeout,
			Jar:     jar,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					NextProtos: []string{"http/1.1"},
				},
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	for _, opt := range opts {
		opt(c)
	}
	if c.debugLog != nil {
		base := c.httpClient.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		c.httpClient.Transport = &debugTransport{
			base:   base,
			debugf: c.debugf,
		}
	}

	return c
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

	location := resp.Header.Get("Location")
	contentType := resp.Header.Get("Content-Type")
	if location != "" {
		t.debugf("encx http done: %s %s status=%d duration=%s redirect=%s content_type=%q",
			req.Method, urlStr, resp.StatusCode, duration, location, contentType)
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

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("encx: GET %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("encx: read response: %w", err)
	}

	return string(body), nil
}
