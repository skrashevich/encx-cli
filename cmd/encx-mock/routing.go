package main

import (
	"net/http"
	"strings"
)

// The live engine is served by ASP.NET behind IIS, which routes "/login/signin"
// and "/login/signin/" to the same action. Go's ServeMux does not: an exact
// pattern matches the exact path only, so the trailing-slash form falls through
// to the "GET /" catch-all and a POST there is answered with 405 instead of the
// login response. Real clients do send the trailing-slash form (enxbot posts to
// "/login/signin/"), so the mock has to accept it or it reports a failure the
// engine would never report.
//
// tolerantMux closes that gap at registration time rather than by rewriting
// request paths: every exact pattern is also registered with a trailing slash,
// which keeps ServeMux.Handler introspection honest for newEngineFallbackMux.
type tolerantMux struct{ *http.ServeMux }

// HandleFunc registers pattern, plus its trailing-slash twin when the pattern is
// an exact match. Subtree patterns ("GET /home/") already cover both forms:
// ServeMux redirects the slashless path to them on its own.
func (m tolerantMux) HandleFunc(pattern string, handler http.HandlerFunc) {
	m.ServeMux.HandleFunc(pattern, handler)
	if !strings.HasSuffix(pattern, "/") {
		m.ServeMux.HandleFunc(pattern+"/{$}", handler)
	}
}

// guardedMux wraps every handler it registers in one middleware. It exists so a
// whole route group can state its access rule once, at the top of the group,
// instead of repeating a guard call in forty handlers where a single omission
// would silently open a route.
type guardedMux struct {
	mux   tolerantMux
	guard func(http.HandlerFunc) http.HandlerFunc
}

func (g guardedMux) HandleFunc(pattern string, handler http.HandlerFunc) {
	g.mux.HandleFunc(pattern, g.guard(handler))
}
