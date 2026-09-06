package encx

import (
	"context"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/skrashevich/encx-cli/encx/enapi"
)

// EngineMode selects which Encounter backend the client talks to.
type EngineMode string

const (
	// EngineLegacy is the ASP.NET engine: JSON endpoints plus HTML admin forms.
	EngineLegacy EngineMode = "legacy"
	// EngineNew is the Encounter Go Backend REST API (see docs/newengine).
	EngineNew EngineMode = "new"
	// EngineAuto probes the new engine once and falls back to legacy.
	EngineAuto EngineMode = "auto"
)

// EngineEnvVar names the environment variable that selects the default engine.
const EngineEnvVar = "ENCX_ENGINE"

// engineProbeTimeout bounds the EngineAuto probe so a silent API host cannot
// stall the first real request behind it.
const engineProbeTimeout = 5 * time.Second

// DefaultEngineMode is what a client uses when nothing selects an engine: it
// asks the API host whether this domain has moved, and stays on the legacy
// engine when it has not.
const DefaultEngineMode = EngineAuto

// ParseEngineMode maps a user-supplied value onto an EngineMode. Unknown and
// empty values report false and resolve to DefaultEngineMode, so a typo cannot
// silently pin a caller to one engine.
func ParseEngineMode(value string) (EngineMode, bool) {
	switch EngineMode(strings.ToLower(strings.TrimSpace(value))) {
	case EngineLegacy:
		return EngineLegacy, true
	case EngineNew:
		return EngineNew, true
	case EngineAuto:
		return EngineAuto, true
	default:
		return DefaultEngineMode, false
	}
}

// WithEngine selects the backend explicitly, overriding ENCX_ENGINE.
func WithEngine(mode EngineMode) Option {
	return func(c *Client) {
		if parsed, ok := ParseEngineMode(string(mode)); ok {
			c.engineMode = parsed
		}
	}
}

// WithAPIBaseURL overrides the host of the new engine (default: api.<zone>).
//
// Naming the host is also the caller vouching for it: a host given here is
// trusted even when the site it returns does not list its domains, which a
// derived host is not.
func WithAPIBaseURL(baseURL string) Option {
	return func(c *Client) {
		if trimmed := strings.TrimRight(strings.TrimSpace(baseURL), "/"); trimmed != "" {
			c.apiBaseURL = trimmed
			c.apiBaseURLExplicit = true
		}
	}
}

// engineModeFromEnv reads ENCX_ENGINE, falling back to DefaultEngineMode.
func engineModeFromEnv() EngineMode {
	mode, _ := ParseEngineMode(os.Getenv(EngineEnvVar))
	return mode
}

// encounterZones are the DNS zones Encounter itself serves. The API host is
// only ever derived for these: stripping a label off an arbitrary domain would
// point the probe — and after it the site header, the login and the password —
// at a host somebody else owns (quest.example.com would resolve to
// api.example.com). Anything outside the list needs an explicit WithAPIBaseURL,
// which is the caller vouching for the host.
var encounterZones = []string{
	"en.cx",
	"encounter.cx",
	"encounter.ru",
	"en-world.org",
	"quest.ua",
}

// normalizeDomainHost strips the port, a trailing dot and case, so two spellings
// of the same host compare equal.
func normalizeDomainHost(domain string) string {
	host := strings.ToLower(strings.TrimSpace(domain))
	if idx := strings.Index(host, ":"); idx >= 0 {
		host = host[:idx]
	}
	return strings.TrimSuffix(host, ".")
}

// encounterZoneOf returns the Encounter zone a domain belongs to, empty when it
// belongs to none.
func encounterZoneOf(domain string) string {
	host := normalizeDomainHost(domain)
	for _, zone := range encounterZones {
		if host == zone || strings.HasSuffix(host, "."+zone) {
			return zone
		}
	}
	return ""
}

// defaultAPIBaseURL derives the API host from a site domain: the new backend
// serves every site of a zone from one host and takes the site from a header,
// so tech.en.cx and moscow.en.cx both resolve to api.en.cx.
// It returns an empty string for a domain outside those zones: there is no host
// we could name for it, and substituting the default Encounter host would send
// that domain's sign-in to a backend which never claimed to serve it.
func defaultAPIBaseURL(scheme, domain string) string {
	zone := encounterZoneOf(domain)
	if zone == "" {
		return ""
	}
	if scheme == "" {
		scheme = "https"
	}
	return scheme + "://api." + zone
}

// APIBaseURL returns the host used for new-engine requests, empty when the
// domain is outside the Encounter zones and no host was given explicitly.
func (c *Client) APIBaseURL() string {
	if c.apiBaseURL != "" {
		return c.apiBaseURL
	}
	return defaultAPIBaseURL(c.scheme, c.domain)
}

// api returns the lazily created new-engine transport. It shares the HTTP
// client, and therefore the cookie jar, HAR recorder and debug logger, with the
// legacy paths.
func (c *Client) api() *enapi.Client {
	c.apiMu.RLock()
	client := c.apiClient
	c.apiMu.RUnlock()
	if client != nil {
		return client
	}

	c.apiMu.Lock()
	defer c.apiMu.Unlock()
	if c.apiClient == nil {
		c.apiClient = enapi.New(
			c.httpClient,
			c.APIBaseURL(),
			c.domain,
			enapi.WithUserAgent(c.userAgent),
			enapi.WithLang(c.lang),
		)
	}
	return c.apiClient
}

// EngineMode returns the configured mode, which may still be EngineAuto.
func (c *Client) EngineMode() EngineMode {
	c.engineMu.RLock()
	defer c.engineMu.RUnlock()
	return c.engineMode
}

// Engine returns the backend actually in use, resolving EngineAuto by probing
// the API host once.
func (c *Client) Engine() EngineMode {
	return c.resolveEngine(context.Background())
}

// SetEngine switches the backend at runtime and discards a cached auto probe.
func (c *Client) SetEngine(mode EngineMode) {
	parsed, ok := ParseEngineMode(string(mode))
	if !ok {
		return
	}
	c.engineMu.Lock()
	c.engineMode = parsed
	c.engineResolved = ""
	c.engineMu.Unlock()
}

// useNewEngine reports whether the current request must go to the new backend.
func (c *Client) useNewEngine(ctx context.Context) bool {
	return c.resolveEngine(ctx) == EngineNew
}

func (c *Client) resolveEngine(ctx context.Context) EngineMode {
	c.engineMu.RLock()
	mode, resolved := c.engineMode, c.engineResolved
	c.engineMu.RUnlock()

	if mode != EngineAuto {
		return mode
	}
	if resolved != "" {
		return resolved
	}

	migrated, definitive := c.probeNewEngine(ctx)
	detected := EngineLegacy
	if migrated {
		detected = EngineNew
	}
	if !definitive {
		// The probe could not reach the API host. Serve this request from the
		// legacy engine but leave the question open, so a client that outlives a
		// network blip is not pinned to one engine for the rest of the process.
		return detected
	}

	c.engineMu.Lock()
	// A concurrent probe may have stored a verdict already; keep the first one
	// so the engine cannot flip between requests.
	if c.engineResolved == "" {
		c.engineResolved = detected
	}
	detected = c.engineResolved
	c.engineMu.Unlock()
	return detected
}

// probeNewEngine reports whether this domain is served by the new backend.
//
// The question is per domain, not per host: one API host fronts the whole zone
// and answers for every request, so a liveness check would declare every
// en.cx site migrated. GET /sites/domain/{domain} is the discriminator — it
// returns the site for a domain the new backend owns and 404
// "domain_unregistered" for one still on the ASP.NET engine.
func (c *Client) probeNewEngine(ctx context.Context) (migrated, definitive bool) {
	if !c.canProbeNewEngine() {
		return false, true
	}
	probeCtx, cancel := context.WithTimeout(ctx, engineProbeTimeout)
	defer cancel()

	var site enapi.Site
	path := "/sites/domain/" + url.PathEscape(c.domain)
	if err := c.api().GetJSON(probeCtx, path, url.Values(nil), &site); err != nil {
		// A 404 is the backend saying the domain is still on the ASP.NET engine
		// and will not change on retry. Anything else — DNS, a timeout, a
		// cancelled context — is a failure to ask, so the verdict is not cached.
		if enapi.IsNotFound(err) {
			c.debugf("encx engine probe: %s is still on the legacy engine", c.domain)
			return false, true
		}
		c.debugf("encx engine probe for %s failed, using legacy for now: %v", c.domain, err)
		return false, false
	}
	if site.ID == 0 {
		c.debugf("encx engine probe: %s returned no site, using legacy", c.domain)
		return false, true
	}
	if !c.siteOwnsDomain(&site) {
		c.debugf("encx engine probe: %s answered for site %d %q, which does not claim this domain",
			c.APIBaseURL(), site.ID, site.Name)
		return false, true
	}
	// Owning the domain is no longer proof of migration: the registry mirrors
	// legacy sites too (moscow.en.cx answers as site 51 with an empty game
	// catalog). Only a site the backend marks active is actually served by it.
	if !site.IsSiteActiveByRule {
		c.debugf("encx engine probe: %s is registered as site %d %q but not active on the new backend, using legacy",
			c.domain, site.ID, site.Name)
		return false, true
	}
	c.debugf("encx engine probe: %s is on the new backend (site %d %q)", c.domain, site.ID, site.Name)
	return true, true
}

// siteOwnsDomain checks that the site the API host returned actually claims the
// domain we asked about. Without it any host that answers with a non-zero id
// would be accepted as this domain's backend.
func (c *Client) siteOwnsDomain(site *enapi.Site) bool {
	wanted := normalizeDomainHost(c.domain)
	if wanted != "" && normalizeDomainHost(site.PrimaryDomain) == wanted {
		return true
	}
	for _, domain := range site.Domains {
		if wanted != "" && normalizeDomainHost(domain.Domain) == wanted {
			return true
		}
	}
	// A site that names no domain cannot be verified. That is only trusted when
	// the caller named the API host themselves.
	if site.PrimaryDomain == "" && len(site.Domains) == 0 {
		return c.apiBaseURLExplicit
	}
	return false
}

// canProbeNewEngine reports whether there is a host worth asking.
//
// Either the caller named the API host, or the domain belongs to an Encounter
// zone whose API host is known. Everything else — an IP, a local hostname, a
// domain outside those zones — stays on the legacy engine, because the only
// host we could derive would be one we have no reason to trust.
func (c *Client) canProbeNewEngine() bool {
	if strings.TrimSpace(c.domain) == "" {
		return false
	}
	if c.apiBaseURLExplicit {
		return true
	}
	return encounterZoneOf(c.domain) != ""
}

// engineState holds the mutable engine selection shared by Client methods.
type engineState struct {
	engineMu       sync.RWMutex
	engineMode     EngineMode
	engineResolved EngineMode

	apiBaseURL         string
	apiBaseURLExplicit bool
	apiMu              sync.RWMutex
	apiClient          *enapi.Client

	// legacy and modern are the two backend implementations; both are created
	// upfront so dispatch is a pointer read rather than an allocation.
	legacy *legacyEngine
	modern *newEngine

	// captchaTok carries the token issued with a CAPTCHA challenge so the
	// retry can pair the digits the user typed with the right image.
	captchaMu  sync.RWMutex
	captchaTok string
}

func (c *Client) captchaToken() string {
	c.captchaMu.RLock()
	defer c.captchaMu.RUnlock()
	return c.captchaTok
}

func (c *Client) setCaptchaToken(token string) {
	c.captchaMu.Lock()
	c.captchaTok = token
	c.captchaMu.Unlock()
}
