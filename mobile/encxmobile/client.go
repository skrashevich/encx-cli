package encxmobile

import (
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

// EncClient wraps encx.Client for use from iOS via gomobile.
type EncClient struct {
	domain string
	client *encx.Client
}

// NewClient creates an Encounter API client for the given domain.
// Set insecureTLS to true to skip TLS certificate verification (e.g. for tech.en.cx).
func NewClient(domain string, insecureTLS bool) *EncClient {
	opts := []encx.Option{}
	if insecureTLS {
		opts = append(opts, encx.WithInsecureTLS())
	}
	return &EncClient{domain: domain, client: encx.New(domain, opts...)}
}

// NewClientWithOptions creates a client with extended configuration.
// timeoutSeconds: HTTP timeout (0 = default 15s). lang: API language (empty = "ru").
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
	return &EncClient{domain: domain, client: encx.New(domain, opts...)}
}

// Domain returns the configured Encounter domain.
func (c *EncClient) Domain() string {
	return c.domain
}
