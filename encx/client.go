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
				TLSClientConfig: &tls.Config{},
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}

	for _, opt := range opts {
		opt(c)
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
