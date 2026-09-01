// Package rt provides the hand-written runtime shared by the generated PHP
// cgo bindings: a handle registry for live *encxmobile.EncClient values and a
// JSON response envelope. Generated cgo wrappers call into this package; it
// contains no cgo itself.
package rt

import (
	"errors"
	"fmt"
	"sync"

	"github.com/skrashevich/encx-cli/mobile/encxmobile"
)

// Registry stores live clients under integer handles suitable for passing
// across the C ABI (e.g. as an opaque int64 to PHP).
//
// Handles are monotonically increasing and are never reused after Free, so a
// handle freed on the PHP side can never be silently reassigned to a
// different, unrelated client. Handle 0 is reserved and never issued; it
// always means "invalid handle".
type Registry struct {
	mu      sync.RWMutex
	clients map[int64]*encxmobile.EncClient
	next    int64
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		clients: make(map[int64]*encxmobile.EncClient),
		next:    1,
	}
}

// Default is the global registry used by the generated wrappers.
var Default = NewRegistry()

// ErrNilClient is returned by Add when asked to register a nil client.
// Registering nil is refused rather than silently accepted, since a nil
// client stored under a valid handle would panic on first use instead of
// failing at registration time.
var ErrNilClient = errors.New("rt: cannot register nil client")

// Add registers c and returns a new, non-zero handle for it. It returns an
// error (and a zero handle) if c is nil; nil clients are never registered.
func (r *Registry) Add(c *encxmobile.EncClient) (int64, error) {
	if c == nil {
		return 0, ErrNilClient
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.next
	r.next++
	r.clients[h] = c
	return h, nil
}

// Get returns the client registered under h. It returns an error naming the
// handle if h is 0, unknown, or has already been freed.
func (r *Registry) Get(h int64) (*encxmobile.EncClient, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[h]
	if !ok {
		return nil, fmt.Errorf("rt: unknown or freed handle %d", h)
	}
	return c, nil
}

// Free removes the client registered under h. It reports whether h existed.
// The handle is never reused for a future Add.
func (r *Registry) Free(h int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.clients[h]; !ok {
		return false
	}
	delete(r.clients, h)
	return true
}

// Len returns the number of currently registered clients.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.clients)
}
