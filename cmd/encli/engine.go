package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
)

// registerEngineFlag adds -engine, which selects the Encounter backend.
//
// Encounter is migrating from the ASP.NET engine to a REST one; both are
// implemented in encx, so the flag only chooses which of them answers.
func registerEngineFlag(fs *flag.FlagSet, cfg *config) {
	fs.StringVar(&cfg.engine, "engine", os.Getenv(encx.EngineEnvVar),
		"Encounter engine: legacy (default), new, or auto to probe the API host (env: "+encx.EngineEnvVar+")")
	// Without an override the API host is derived from the domain
	// (tech.en.cx -> api.en.cx), which cannot address a local mock or a
	// self-hosted instance.
	fs.StringVar(&cfg.apiBaseURL, "api-base-url", os.Getenv(apiBaseURLEnvVar),
		"Base URL of the new engine API (env: "+apiBaseURLEnvVar+")")
}

// apiBaseURLEnvVar overrides the derived host of the new engine.
const apiBaseURLEnvVar = "ENCX_API_BASE_URL"

// engineOptions turns the parsed flags into client options, rejecting an engine
// name it does not know. Silently ignoring a typo would hand the caller the
// default engine while they believe they asked for another one.
func engineOptions(cfg *config) ([]encx.Option, error) {
	var opts []encx.Option
	if value := strings.TrimSpace(cfg.engine); value != "" {
		mode, ok := encx.ParseEngineMode(value)
		if !ok {
			return nil, fmt.Errorf("unknown -engine %q: use legacy, new or auto", value)
		}
		opts = append(opts, encx.WithEngine(mode))
	}
	if cfg.apiBaseURL != "" {
		opts = append(opts, encx.WithAPIBaseURL(cfg.apiBaseURL))
	}
	return opts, nil
}
