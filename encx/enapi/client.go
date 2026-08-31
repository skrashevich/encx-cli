// Package enapi is a low-level HTTP client for the Encounter Go Backend API —
// the REST engine that replaces the ASP.NET one.
//
// Unlike the legacy engine, every site lives behind a single API host and the
// site context travels in the X-En-Domain header, so one client can serve any
// domain. The package deliberately stops at transport and error decoding: the
// mapping of REST payloads onto the public encx types lives in encx itself.
package enapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

// DefaultBaseURL is the production host of the new engine.
const DefaultBaseURL = "https://api.en.cx"

// DomainHeader carries the site context (legacy multi-tenant domain).
const DomainHeader = "X-En-Domain"

// maxErrorBodyBytes bounds how much of a failing response is kept for the
// error message; API errors are small JSON objects, HTML gateway pages are not.
const maxErrorBodyBytes = 4096

// Client performs authenticated JSON calls against the new engine.
//
// The zero value is not usable — construct it with New.
type Client struct {
	baseURL    string
	domain     string
	httpClient *http.Client
	userAgent  string
	lang       string

	mu    sync.RWMutex
	token string
}

// Option configures the Client.
type Option func(*Client)

// WithUserAgent sets the User-Agent header sent with every request.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// WithLang sets the language passed to endpoints that localize their answer.
func WithLang(lang string) Option {
	return func(c *Client) {
		if lang != "" {
			c.lang = lang
		}
	}
}

// WithToken presets the bearer token, e.g. after restoring a saved session.
func WithToken(token string) Option {
	return func(c *Client) {
		c.token = token
	}
}

// New creates a client for the given API host and site domain.
//
// httpClient is supplied by the caller so that the cookie jar, HAR recording
// and debug logging configured on encx.Client apply to the new engine too.
//
// An empty baseURL is kept as-is rather than defaulted: the caller could not
// name a host, and quietly substituting one would send this domain's requests —
// including its sign-in — to a backend that never claimed to serve it. Requests
// then fail with an actionable error instead.
func New(httpClient *http.Client, baseURL, domain string, opts ...Option) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		domain:     domain,
		httpClient: httpClient,
		userAgent:  "encx-cli",
		lang:       "ru",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// BaseURL returns the API host without a trailing slash.
func (c *Client) BaseURL() string { return c.baseURL }

// Domain returns the site domain sent in X-En-Domain.
func (c *Client) Domain() string { return c.domain }

// Lang returns the configured language code.
func (c *Client) Lang() string { return c.lang }

// Token returns the current bearer token, empty when not signed in.
func (c *Client) Token() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// SetToken stores the bearer token used by subsequent requests.
func (c *Client) SetToken(token string) {
	c.mu.Lock()
	c.token = token
	c.mu.Unlock()
}

// URL builds an absolute request URL for an API path and optional query.
func (c *Client) URL(path string, query url.Values) string {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// Request describes a single API call.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	// Body is marshalled as JSON when non-nil.
	Body any
	// Out receives the decoded JSON response when non-nil.
	Out any
	// Header carries extra headers (rarely needed).
	Header http.Header
}

// Do performs the request, decoding a JSON body into req.Out on success.
func (c *Client) Do(ctx context.Context, req Request) error {
	_, err := c.doRaw(ctx, req)
	return err
}

// GetJSON performs a GET request and decodes the response into out.
func (c *Client) GetJSON(ctx context.Context, path string, query url.Values, out any) error {
	return c.Do(ctx, Request{Method: http.MethodGet, Path: path, Query: query, Out: out})
}

// PostJSON performs a POST request with a JSON body.
func (c *Client) PostJSON(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, Request{Method: http.MethodPost, Path: path, Body: body, Out: out})
}

// PutJSON performs a PUT request with a JSON body.
func (c *Client) PutJSON(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, Request{Method: http.MethodPut, Path: path, Body: body, Out: out})
}

// PatchJSON performs a PATCH request with a JSON body.
func (c *Client) PatchJSON(ctx context.Context, path string, body, out any) error {
	return c.Do(ctx, Request{Method: http.MethodPatch, Path: path, Body: body, Out: out})
}

// Delete performs a DELETE request, decoding the response into out when given.
func (c *Client) Delete(ctx context.Context, path string, query url.Values, out any) error {
	return c.Do(ctx, Request{Method: http.MethodDelete, Path: path, Query: query, Out: out})
}

// GetBytes performs a GET request and returns the raw response body. It is used
// for endpoints that answer with media or HTML rather than JSON.
func (c *Client) GetBytes(ctx context.Context, path string, query url.Values) ([]byte, http.Header, error) {
	resp, err := c.doRaw(ctx, Request{Method: http.MethodGet, Path: path, Query: query})
	if err != nil {
		return nil, nil, err
	}
	return resp.body, resp.header, nil
}

type rawResponse struct {
	status int
	header http.Header
	body   []byte
}

func (c *Client) doRaw(ctx context.Context, req Request) (*rawResponse, error) {
	if c.baseURL == "" {
		return nil, &MissingHostError{Domain: c.domain}
	}
	var bodyReader io.Reader
	var encoded []byte
	if req.Body != nil {
		var err error
		encoded, err = json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("enapi: encode request body: %w", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}

	method := req.Method
	if method == "" {
		method = http.MethodGet
	}
	rawURL := c.URL(req.Path, req.Query)

	httpReq, err := http.NewRequestWithContext(ctx, method, rawURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("enapi: create request: %w", err)
	}
	if encoded != nil {
		httpReq.ContentLength = int64(len(encoded))
		httpReq.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(encoded)), nil
		}
		httpReq.Header.Set("Content-Type", "application/json")
	}
	c.setHeaders(httpReq)
	for key, values := range req.Header {
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("enapi: %s %s: %w", method, req.Path, err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("enapi: %s %s: read response: %w", method, req.Path, err)
	}

	resp := &rawResponse{status: httpResp.StatusCode, header: httpResp.Header.Clone(), body: body}
	if httpResp.StatusCode >= 400 {
		return resp, parseAPIError(method, req.Path, httpResp.StatusCode, body)
	}

	if req.Out != nil {
		if len(bytes.TrimSpace(body)) == 0 {
			return resp, nil
		}
		if err := json.Unmarshal(body, req.Out); err != nil {
			return resp, fmt.Errorf("enapi: %s %s: decode response: %w", method, req.Path, err)
		}
	}
	return resp, nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if c.domain != "" {
		req.Header.Set(DomainHeader, c.domain)
	}
	if token := c.Token(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}
