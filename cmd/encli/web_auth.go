package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

// AuthRegistry maps Encounter domains to lazily constructed encx clients.
// Session cookies are loaded via loadSession using the same on-disk layout as the CLI.
type AuthRegistry struct {
	mu       sync.RWMutex
	clients  map[string]*encx.Client
	domainMu map[string]*sync.Mutex
}

// AuthDomainStatus reports whether a persisted session file exists for a domain.
type AuthDomainStatus struct {
	Domain      string `json:"domain"`
	Login       string `json:"login,omitempty"`
	SessionPath string `json:"session_path"`
	HasSession  bool   `json:"has_session"`
	KnownClient bool   `json:"known_client"`
}

// NewAuthRegistry constructs an empty registry.
func NewAuthRegistry() *AuthRegistry {
	return &AuthRegistry{
		clients:  make(map[string]*encx.Client),
		domainMu: make(map[string]*sync.Mutex),
	}
}

func (r *AuthRegistry) domainMutex(domain string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.domainMu[domain]
	if !ok {
		m = &sync.Mutex{}
		r.domainMu[domain] = m
	}
	return m
}

// WithDomainLock serializes API calls that share one cookie jar (same Encounter domain).
func (r *AuthRegistry) WithDomainLock(domain string, fn func()) {
	r.domainMutex(domain).Lock()
	defer r.domainMutex(domain).Unlock()
	fn()
}

// Get returns the client for domain, creating it with encx.New(domain, opts...) if needed
// and attempting loadSession against a minimal config that preserves domain only.
func (r *AuthRegistry) Get(domain string, opts []encx.Option) *encx.Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.clients[domain]; ok {
		return c
	}
	c := encx.New(domain, opts...)
	cfg := &config{domain: domain}
	loadSession(cfg, c)
	r.clients[domain] = c
	return c
}

// ForEachClient invokes fn for every cached client under read lock.
func (r *AuthRegistry) ForEachClient(fn func(domain string, client *encx.Client)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for domain, client := range r.clients {
		fn(domain, client)
	}
}

// Login authenticates and persists cookies via saveSession (same path as CLI sessionFile).
func (r *AuthRegistry) Login(ctx context.Context, domain, login, password string, opts []encx.Option) error {
	c := encx.New(domain, opts...)
	resp, err := c.Login(ctx, login, password)
	if err != nil {
		return err
	}
	if resp.Error != 0 {
		return EncxLoginError{Code: resp.Error}
	}
	cfg := &config{domain: domain, login: login}
	saveSession(cfg, c)

	r.mu.Lock()
	r.clients[domain] = c
	r.mu.Unlock()
	return nil
}

// EncxLoginError wraps a non-zero encx login response code.
type EncxLoginError struct {
	Code int
}

func (e EncxLoginError) Error() string {
	return encx.LoginErrorText(e.Code)
}

// Logout removes the persisted session file for domain and drops any cached client.
func (r *AuthRegistry) Logout(domain string) {
	r.mu.Lock()
	delete(r.clients, domain)
	r.mu.Unlock()

	cfg := &config{domain: domain}
	path := sessionFile(cfg)
	_ = os.Remove(path)
	removeSessionMeta(cfg)
}

// ListStatus returns auth-related status for every domain that has a session file on disk
// or an entry in the in-memory client map.
func (r *AuthRegistry) ListStatus() []AuthDomainStatus {
	domains := map[string]struct{}{}

	r.mu.RLock()
	for d := range r.clients {
		domains[d] = struct{}{}
	}
	r.mu.RUnlock()

	entries, err := filepath.Glob(filepath.Join(sessionDir(), "*.json"))
	if err == nil {
		for _, p := range entries {
			base := filepath.Base(p)
			if strings.HasSuffix(base, ".meta.json") {
				continue
			}
			if len(base) < len(".json")+1 {
				continue
			}
			d := base[:len(base)-len(".json")]
			domains[d] = struct{}{}
		}
	}

	out := make([]AuthDomainStatus, 0, len(domains))
	for d := range domains {
		path := sessionFile(&config{domain: d})
		st, statErr := os.Stat(path)
		r.mu.RLock()
		_, known := r.clients[d]
		r.mu.RUnlock()
		login := ""
		if statErr == nil && !st.IsDir() {
			login = loadSessionLogin(d)
		}
		out = append(out, AuthDomainStatus{
			Domain:      d,
			Login:       login,
			SessionPath: path,
			HasSession:  statErr == nil && !st.IsDir(),
			KnownClient: known,
		})
	}
	slices.SortFunc(out, func(a, b AuthDomainStatus) int {
		if a.Domain < b.Domain {
			return -1
		}
		if a.Domain > b.Domain {
			return 1
		}
		return 0
	})
	return out
}

// ResolveLogin returns the username for a saved session, loading meta from disk or
// fetching the profile when cookies exist but login was never persisted.
func (r *AuthRegistry) ResolveLogin(ctx context.Context, domain string, opts []encx.Option) string {
	if login := loadSessionLogin(domain); login != "" {
		return login
	}
	var login string
	r.WithDomainLock(domain, func() {
		c := r.Get(domain, opts)
		reqCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		profile, err := c.GetProfile(reqCtx)
		if err != nil || profile == nil {
			return
		}
		login = strings.TrimSpace(profile.Login)
		if encx.IsPlausibleEncounterLogin(login) {
			saveSessionMeta(&config{domain: domain, login: login})
		} else {
			login = ""
		}
	})
	return login
}
