package encx

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Resource is a file fetched from the Encounter site with the player's session.
type Resource struct {
	URL         string `json:"url"`
	ContentType string `json:"content_type"`
	Data        []byte `json:"-"`
}

// ResourceOptions bounds a resource fetch.
type ResourceOptions struct {
	// MaxBytes rejects anything larger. Zero means DefaultResourceMaxBytes.
	MaxBytes int64
	// RestrictToDomain limits fetches to the client's own domain. Off by
	// default: game authors routinely host task images on image services, and a
	// viewer that refused them would be useless.
	RestrictToDomain bool
}

// DefaultResourceMaxBytes caps a fetched resource. Task images are photographs,
// not archives, and the caller usually has to base64 them into an LLM request.
const DefaultResourceMaxBytes = 8 << 20

// FetchResource downloads a file referenced by game content, reusing the
// authenticated session — many Encounter attachments are not public.
//
// rawURL may be absolute or site-relative, and may point at any public host:
// authors host task images wherever they like. Addresses on the local network
// are still refused, because a URL taken from game content is untrusted input
// and the device running this may sit inside a private network.
func (c *Client) FetchResource(ctx context.Context, rawURL string, opts ...ResourceOptions) (*Resource, error) {
	return c.engine(ctx).FetchResource(ctx, rawURL, opts...)
}

// fetchResource performs the download, resolving site-relative references
// against base — the site host on the legacy engine, the API host on the new
// one, which is where the latter serves media from.
func (c *Client) fetchResource(ctx context.Context, base, rawURL string, opts ...ResourceOptions) (*Resource, error) {
	options := ResourceOptions{}
	if len(opts) > 0 {
		options = opts[0]
	}
	limit := options.MaxBytes
	if limit <= 0 {
		limit = DefaultResourceMaxBytes
	}

	resolved, err := c.resolveResourceURL(base, rawURL, options.RestrictToDomain)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resolved, nil)
	if err != nil {
		return nil, fmt.Errorf("encx: create resource request: %w", err)
	}
	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("encx: fetch %s: %w", resolved, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("encx: fetch %s: HTTP %d", resolved, resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return nil, fmt.Errorf(
			"encx: %s is %d bytes, over the %d byte limit", resolved, resp.ContentLength, limit)
	}

	// Read one byte past the limit so a missing or lying Content-Length is still
	// caught.
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("encx: read %s: %w", resolved, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("encx: %s is larger than the %d byte limit", resolved, limit)
	}

	contentType := resp.Header.Get("Content-Type")
	if idx := strings.IndexByte(contentType, ';'); idx >= 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}

	return &Resource{URL: resolved, ContentType: contentType, Data: data}, nil
}

func (c *Client) resolveResourceURL(base, rawURL string, restrictToDomain bool) (string, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return "", fmt.Errorf("encx: resource URL is empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("encx: parse resource URL %q: %w", trimmed, err)
	}

	if !parsed.IsAbs() {
		return absURLAgainst(base, trimmed), nil
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return "", fmt.Errorf("encx: unsupported resource scheme %q", parsed.Scheme)
	}

	host := parsed.Hostname()
	if isPrivateHost(host) {
		return "", fmt.Errorf("encx: %s is a local address", host)
	}
	if restrictToDomain && !hostBelongsToDomain(host, c.domain) && !hostBelongsToBase(host, base) {
		return "", fmt.Errorf("encx: %s is outside the %s domain", host, c.domain)
	}
	return parsed.String(), nil
}

// hostBelongsToBase keeps the API host inside the restricted set: on the new
// engine the site's own media lives there, not on the site domain.
func hostBelongsToBase(host, base string) bool {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSuffix(host, "."), strings.TrimSuffix(parsed.Hostname(), "."))
}

// isPrivateHost reports addresses that only exist on the local network. A URL
// lifted from game content must not be able to reach the device's own network.
func isPrivateHost(host string) bool {
	lower := strings.ToLower(strings.TrimSuffix(host, "."))
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return true
	}
	ip := net.ParseIP(lower)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

// hostBelongsToDomain accepts the domain itself and its subdomains, which is
// where Encounter serves images from (img.<domain>, m.<domain>, …).
func hostBelongsToDomain(host, domain string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	if host == "" || domain == "" {
		return false
	}
	if host == domain {
		return true
	}
	if strings.HasSuffix(host, "."+domain) {
		return true
	}
	// en.cx games live on per-city subdomains of a shared root, and attachments
	// are commonly served from another one.
	root := registrableRoot(domain)
	return root != "" && (host == root || strings.HasSuffix(host, "."+root))
}

func registrableRoot(domain string) string {
	parts := strings.Split(domain, ".")
	if len(parts) < 2 {
		return ""
	}
	return strings.Join(parts[len(parts)-2:], ".")
}
