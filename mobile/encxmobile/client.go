package encxmobile

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

const defaultCodeSendTimeout = time.Second
const defaultGameRequestMinInterval = 350 * time.Millisecond

// EncClient wraps encx.Client for use from iOS via gomobile.
type EncClient struct {
	domain                 string
	client                 *encx.Client
	codeSendTimeout        time.Duration
	gameRequestMu          sync.Mutex
	lastGameRequest        time.Time
	gameRequestMinInterval time.Duration
}

func normalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimSpace(domain))
}

func isDemoDomain(domain string) bool {
	return normalizeDomain(domain) == "demo.en.cx"
}

func newEncClient(domain string, client *encx.Client) *EncClient {
	return &EncClient{
		domain:                 domain,
		client:                 client,
		codeSendTimeout:        defaultCodeSendTimeout,
		gameRequestMinInterval: defaultGameRequestMinInterval,
	}
}

// NewClient creates an Encounter API client for the given domain.
// Set insecureTLS to true to skip TLS certificate verification (e.g. for tech.en.cx).
func NewClient(domain string, insecureTLS bool) *EncClient {
	opts := []encx.Option{}
	if insecureTLS || isDemoDomain(domain) {
		opts = append(opts, encx.WithInsecureTLS())
	}
	return newEncClient(domain, encx.New(domain, opts...))
}

// NewClientWithOptions creates a client with extended configuration.
// timeoutSeconds: HTTP client timeout (0 = default 15s). lang: API language (empty = "ru").
func NewClientWithOptions(domain string, insecureTLS, useHTTP bool, timeoutSeconds int64, lang string) *EncClient {
	opts := []encx.Option{}
	if insecureTLS || isDemoDomain(domain) {
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
	return newEncClient(domain, encx.New(domain, opts...))
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

// SetGameRequestMinIntervalMillis sets the minimum interval between mobile game-engine requests.
// Zero or negative values disable pacing.
func (c *EncClient) SetGameRequestMinIntervalMillis(milliseconds int64) {
	c.gameRequestMu.Lock()
	defer c.gameRequestMu.Unlock()
	if milliseconds <= 0 {
		c.gameRequestMinInterval = 0
		return
	}
	c.gameRequestMinInterval = time.Duration(milliseconds) * time.Millisecond
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

func (c *EncClient) paceGameRequest(ctx context.Context) error {
	c.gameRequestMu.Lock()
	defer c.gameRequestMu.Unlock()

	d := c.gameRequestMinInterval
	if d <= 0 {
		return nil
	}

	if !c.lastGameRequest.IsZero() {
		wait := time.Until(c.lastGameRequest.Add(d))
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	c.lastGameRequest = time.Now()
	return nil
}

// Domain returns the configured Encounter domain.
func (c *EncClient) Domain() string {
	return c.domain
}
