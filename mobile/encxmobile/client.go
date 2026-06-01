package encxmobile

import (
	"context"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

const defaultCodeSendTimeout = time.Second

// EncClient wraps encx.Client for use from iOS via gomobile.
type EncClient struct {
	domain          string
	client          *encx.Client
	codeSendTimeout time.Duration
}

// NewClient creates an Encounter API client for the given domain.
// Set insecureTLS to true to skip TLS certificate verification (e.g. for tech.en.cx).
func NewClient(domain string, insecureTLS bool) *EncClient {
	opts := []encx.Option{}
	if insecureTLS {
		opts = append(opts, encx.WithInsecureTLS())
	}
	return &EncClient{
		domain:          domain,
		client:          encx.New(domain, opts...),
		codeSendTimeout: defaultCodeSendTimeout,
	}
}

// NewClientWithOptions creates a client with extended configuration.
// timeoutSeconds: HTTP client timeout (0 = default 15s). lang: API language (empty = "ru").
func NewClientWithOptions(domain string, insecureTLS, useHTTP bool, timeoutSeconds int64, lang string) *EncClient {
	opts := []encx.Option{}
	if insecureTLS {
		opts = append(opts, encx.WithInsecureTLS())
	}
	if useHTTP {
		opts = append(opts, encx.WithHTTP())
	}
	if timeoutSeconds > 0 {
		opts = append(opts, encx.WithTimeout(time.Duration(timeoutSeconds)*time.Second))
	}
	if lang != "" {
		opts = append(opts, encx.WithLang(lang))
	}
	return &EncClient{
		domain:          domain,
		client:          encx.New(domain, opts...),
		codeSendTimeout: defaultCodeSendTimeout,
	}
}

// SetCodeSendTimeoutSeconds sets the per-request timeout for code submissions and quick probes.
// Zero or negative values reset to the default (1 second).
func (c *EncClient) SetCodeSendTimeoutSeconds(seconds int64) {
	if seconds <= 0 {
		c.codeSendTimeout = defaultCodeSendTimeout
		return
	}
	c.codeSendTimeout = time.Duration(seconds) * time.Second
}

func (c *EncClient) bg() context.Context {
	return context.Background()
}

func (c *EncClient) codeSendCtx() (context.Context, context.CancelFunc) {
	d := c.codeSendTimeout
	if d <= 0 {
		d = defaultCodeSendTimeout
	}
	return context.WithTimeout(context.Background(), d)
}

// Domain returns the configured Encounter domain.
func (c *EncClient) Domain() string {
	return c.domain
}
