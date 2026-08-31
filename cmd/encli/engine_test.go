package main

import (
	"flag"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

// parseEngineFlag runs the flag registration the same way main does.
func parseEngineFlag(t *testing.T, args ...string) *config {
	t.Helper()
	fs := flag.NewFlagSet("encli", flag.ContinueOnError)
	cfg := &config{}
	registerEngineFlag(fs, cfg)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parse %v: %v", args, err)
	}
	return cfg
}

func TestEngineFlagSelectsTheBackend(t *testing.T) {
	cases := []struct {
		args []string
		want encx.EngineMode
	}{
		{[]string{"-engine", "new"}, encx.EngineNew},
		{[]string{"-engine", "auto"}, encx.EngineAuto},
		{[]string{"-engine", "legacy"}, encx.EngineLegacy},
	}
	for _, tc := range cases {
		t.Setenv(encx.EngineEnvVar, "")
		cfg := parseEngineFlag(t, tc.args...)
		client := encx.New("tech.en.cx", appendEncOpts(cfg)...)
		if got := client.EngineMode(); got != tc.want {
			t.Errorf("%v: EngineMode = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestEngineFlagDefaultsToAuto(t *testing.T) {
	t.Setenv(encx.EngineEnvVar, "")
	cfg := parseEngineFlag(t)
	if cfg.engine != "" {
		t.Errorf("engine = %q, want empty without the flag", cfg.engine)
	}
	client := encx.New("tech.en.cx", appendEncOpts(cfg)...)
	if got := client.EngineMode(); got != encx.EngineAuto {
		t.Errorf("EngineMode = %q, want auto", got)
	}
}

func TestEngineFlagReadsEnvVar(t *testing.T) {
	t.Setenv(encx.EngineEnvVar, "new")
	cfg := parseEngineFlag(t)
	if cfg.engine != "new" {
		t.Errorf("engine = %q, want the environment value", cfg.engine)
	}
	client := encx.New("tech.en.cx", appendEncOpts(cfg)...)
	if got := client.EngineMode(); got != encx.EngineNew {
		t.Errorf("EngineMode = %q, want new", got)
	}
}

func TestEngineFlagOverridesEnvVar(t *testing.T) {
	t.Setenv(encx.EngineEnvVar, "new")
	cfg := parseEngineFlag(t, "-engine", "legacy")
	client := encx.New("tech.en.cx", appendEncOpts(cfg)...)
	if got := client.EngineMode(); got != encx.EngineLegacy {
		t.Errorf("EngineMode = %q, want legacy", got)
	}
}

// TestEngineOptionsRejectsUnknownValues pins that a typo is reported rather
// than silently downgraded: since the default became auto, "-engine legcy" would
// otherwise hand the caller a different engine than the one they asked for.
func TestEngineOptionsRejectsUnknownValues(t *testing.T) {
	t.Setenv(encx.EngineEnvVar, "")
	cfg := parseEngineFlag(t, "-engine", "nonsense")
	if _, err := engineOptions(cfg); err == nil {
		t.Fatal("engineOptions accepted an unknown engine name")
	}
}

func TestEngineOptionsAcceptsKnownValues(t *testing.T) {
	t.Setenv(encx.EngineEnvVar, "")
	for _, value := range []string{"legacy", "new", "auto", ""} {
		cfg := parseEngineFlag(t, "-engine", value)
		if _, err := engineOptions(cfg); err != nil {
			t.Errorf("engineOptions(%q): %v", value, err)
		}
	}
}

func TestAPIBaseURLFlagOverridesTheDerivedHost(t *testing.T) {
	t.Setenv(encx.EngineEnvVar, "")
	t.Setenv(apiBaseURLEnvVar, "")
	cfg := parseEngineFlag(t, "-api-base-url", "http://127.0.0.1:9999")
	client := encx.New("tech.en.cx", appendEncOpts(cfg)...)
	if got := client.APIBaseURL(); got != "http://127.0.0.1:9999" {
		t.Errorf("APIBaseURL = %q, want the override", got)
	}
}
