package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

// registerEngineFlag adds -engine, which selects the Encounter backend.
//
// Encounter is migrating from the ASP.NET engine to a REST one; both are
// implemented in encx, so the flag only chooses which of them answers.
func registerEngineFlag(fs *flag.FlagSet, cfg *config) {
	fs.DurationVar(&cfg.apiRequestInterval, "api-request-interval", 40*time.Millisecond, "Minimum interval between REST API requests (e.g. 200ms = 5 requests/s)")
	fs.StringVar(&cfg.engine, "engine", os.Getenv(encx.EngineEnvVar),
		"Encounter engine: auto (default) to probe the API host, legacy, or new (env: "+encx.EngineEnvVar+")")
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
	if cfg.apiRequestInterval < 0 {
		return nil, fmt.Errorf("api-request-interval must not be negative")
	}
	if cfg.apiRequestInterval > 0 {
		opts = append(opts, encx.WithAPIRequestInterval(cfg.apiRequestInterval))
	}
	// The stored selection only fills in for a silent flag and environment, both
	// of which reach us through cfg (registerEngineFlag defaults the flags from
	// the environment), so reading it here preserves flag > env > file.
	//
	// A settings file that does not parse is logged and ignored, where the LLM
	// one is propagated to the caller. The asymmetry is the cost of being wrong:
	// a broken LLM file would silently run the agent against a provider the
	// operator did not choose, while the engine default is auto, which probes
	// the API host and corrects itself.
	stored, err := loadEngineSettings()
	if err != nil {
		debugf("engine settings: %v", err)
		stored = engineSettings{}
	}
	value := strings.TrimSpace(cfg.engine)
	if value == "" && stored.Engine != "" {
		if _, ok := encx.ParseEngineMode(stored.Engine); ok {
			value = stored.Engine
		} else {
			// Hand-edited, since saveEngineSettings refuses such a name. Ignored
			// for the same reason as a corrupt file: it must not take down every
			// command that builds a client.
			debugf("engine settings: ignoring unknown engine %q", stored.Engine)
		}
	}
	if value != "" {
		mode, ok := encx.ParseEngineMode(value)
		if !ok {
			return nil, fmt.Errorf("unknown -engine %q: use legacy, new or auto", value)
		}
		opts = append(opts, encx.WithEngine(mode))
	}
	baseURL := cfg.apiBaseURL
	if baseURL == "" {
		baseURL = stored.APIBaseURL
	}
	if baseURL != "" {
		opts = append(opts, encx.WithAPIBaseURL(baseURL))
	}
	return opts, nil
}
