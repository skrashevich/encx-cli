package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

// isolateEngineSettings points the settings file at this test's temp directory.
// engineOptions consults that file, so without it an engine test would read —
// and a save would overwrite — the developer's real one. A test that already
// chose a path keeps it, so settings may be seeded before the flags are parsed.
func isolateEngineSettings(t *testing.T) {
	t.Helper()
	if strings.TrimSpace(os.Getenv(engineSettingsFileEnvVar)) != "" {
		return
	}
	t.Setenv(engineSettingsFileEnvVar, filepath.Join(t.TempDir(), "engine-settings.json"))
}

// silentEngineEnv isolates the settings file and empties both engine variables,
// so a test states its own precedence rather than inheriting the developer's.
func silentEngineEnv(t *testing.T) {
	t.Helper()
	isolateEngineSettings(t)
	t.Setenv(encx.EngineEnvVar, "")
	t.Setenv(apiBaseURLEnvVar, "")
}

func writeTestEngineSettings(t *testing.T, s engineSettings) {
	t.Helper()
	if err := saveEngineSettings(s); err != nil {
		t.Fatalf("saveEngineSettings(%+v): %v", s, err)
	}
}

// engineModeFor builds a client the way every command does, so the assertions
// cover the whole path from flag and file to the client's selection.
func engineModeFor(t *testing.T, cfg *config) encx.EngineMode {
	t.Helper()
	opts, err := engineOptions(cfg)
	if err != nil {
		t.Fatalf("engineOptions: %v", err)
	}
	return encx.New("tech.en.cx", opts...).EngineMode()
}

func apiBaseURLFor(t *testing.T, cfg *config) string {
	t.Helper()
	opts, err := engineOptions(cfg)
	if err != nil {
		t.Fatalf("engineOptions: %v", err)
	}
	return encx.New("tech.en.cx", opts...).APIBaseURL()
}

func TestEngineSettingsRoundTrip(t *testing.T) {
	silentEngineEnv(t)
	writeTestEngineSettings(t, engineSettings{Engine: "  NEW ", APIBaseURL: " https://api.example.com "})

	got, err := loadEngineSettings()
	if err != nil {
		t.Fatalf("loadEngineSettings: %v", err)
	}
	want := engineSettings{Engine: "new", APIBaseURL: "https://api.example.com"}
	if got != want {
		t.Fatalf("settings = %+v, want %+v (the writer and the reader must both trim)", got, want)
	}

	if err := deleteEngineSettings(); err != nil {
		t.Fatalf("deleteEngineSettings: %v", err)
	}
	// Deleting settings that are already gone is what a reset does on a machine
	// that never configured anything.
	if err := deleteEngineSettings(); err != nil {
		t.Fatalf("deleteEngineSettings on a missing file: %v", err)
	}
}

func TestLoadEngineSettingsMissingFileIsNotAnError(t *testing.T) {
	silentEngineEnv(t)
	got, err := loadEngineSettings()
	if err != nil {
		t.Fatalf("loadEngineSettings on a missing file: %v", err)
	}
	if got != (engineSettings{}) {
		t.Fatalf("settings = %+v, want the zero value", got)
	}
}

// The path belongs in the message because deleting that file is the operator's
// only way out of a settings file that no longer parses.
func TestLoadEngineSettingsReportsThePathOfGarbage(t *testing.T) {
	silentEngineEnv(t)
	path := engineSettingsFile()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, err := loadEngineSettings()
	if err == nil {
		t.Fatal("loadEngineSettings accepted garbage")
	}
	if !strings.Contains(err.Error(), path) {
		t.Fatalf("error %q does not name %q", err, path)
	}
}

func TestSaveEngineSettingsRejectsBadValues(t *testing.T) {
	silentEngineEnv(t)
	if err := saveEngineSettings(engineSettings{Engine: "legcy"}); err == nil {
		t.Fatal("saveEngineSettings accepted an unknown engine name")
	}
	if err := saveEngineSettings(engineSettings{APIBaseURL: "api.example.com"}); err == nil {
		t.Fatal("saveEngineSettings accepted a base URL without a scheme")
	}
	// A refused save must not leave a half-written file behind.
	if _, err := os.Stat(engineSettingsFile()); !os.IsNotExist(err) {
		t.Fatalf("stat after a refused save: %v", err)
	}
}

// The file names the host that credentials are sent to, so neither it nor the
// directory it lives in may be readable by another account on the machine.
func TestSaveEngineSettingsFilePermissions(t *testing.T) {
	silentEngineEnv(t)
	// Use a nested path so the directory mode is the one saveEngineSettings
	// creates rather than the temp directory's.
	path := filepath.Join(t.TempDir(), "engine", "settings.json")
	t.Setenv(engineSettingsFileEnvVar, path)

	writeTestEngineSettings(t, engineSettings{Engine: "new"})

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat settings: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("settings mode = %v, want 0600", perm)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat settings dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0700 {
		t.Fatalf("settings dir mode = %v, want 0700", perm)
	}
}

func TestEngineSettingsPrecedenceForTheEngine(t *testing.T) {
	t.Run("stored beats the built-in default", func(t *testing.T) {
		silentEngineEnv(t)
		writeTestEngineSettings(t, engineSettings{Engine: "new"})
		cfg := parseEngineFlag(t)
		if got := engineModeFor(t, cfg); got != encx.EngineNew {
			t.Fatalf("EngineMode = %q, want the stored engine instead of %q", got, encx.DefaultEngineMode)
		}
	})

	t.Run("env beats stored", func(t *testing.T) {
		silentEngineEnv(t)
		writeTestEngineSettings(t, engineSettings{Engine: "new"})
		t.Setenv(encx.EngineEnvVar, "legacy")
		cfg := parseEngineFlag(t)
		if got := engineModeFor(t, cfg); got != encx.EngineLegacy {
			t.Fatalf("EngineMode = %q, want the environment to shadow the file", got)
		}
	})

	t.Run("flag beats env", func(t *testing.T) {
		silentEngineEnv(t)
		writeTestEngineSettings(t, engineSettings{Engine: "auto"})
		t.Setenv(encx.EngineEnvVar, "legacy")
		cfg := parseEngineFlag(t, "-engine", "new")
		if got := engineModeFor(t, cfg); got != encx.EngineNew {
			t.Fatalf("EngineMode = %q, want the flag to win", got)
		}
	})
}

func TestEngineSettingsPrecedenceForTheAPIBaseURL(t *testing.T) {
	const derived = "https://api.en.cx" // what tech.en.cx resolves to unaided

	t.Run("stored beats the derived host", func(t *testing.T) {
		silentEngineEnv(t)
		writeTestEngineSettings(t, engineSettings{APIBaseURL: "https://stored.example.com"})
		cfg := parseEngineFlag(t)
		if got := apiBaseURLFor(t, cfg); got != "https://stored.example.com" {
			t.Fatalf("APIBaseURL = %q, want the stored host instead of %q", got, derived)
		}
	})

	t.Run("env beats stored", func(t *testing.T) {
		silentEngineEnv(t)
		writeTestEngineSettings(t, engineSettings{APIBaseURL: "https://stored.example.com"})
		t.Setenv(apiBaseURLEnvVar, "https://env.example.com")
		cfg := parseEngineFlag(t)
		if got := apiBaseURLFor(t, cfg); got != "https://env.example.com" {
			t.Fatalf("APIBaseURL = %q, want the environment to shadow the file", got)
		}
	})

	t.Run("flag beats env", func(t *testing.T) {
		silentEngineEnv(t)
		writeTestEngineSettings(t, engineSettings{APIBaseURL: "https://stored.example.com"})
		t.Setenv(apiBaseURLEnvVar, "https://env.example.com")
		cfg := parseEngineFlag(t, "-api-base-url", "https://flag.example.com")
		if got := apiBaseURLFor(t, cfg); got != "https://flag.example.com" {
			t.Fatalf("APIBaseURL = %q, want the flag to win", got)
		}
	})

	t.Run("nothing configured leaves the derived host", func(t *testing.T) {
		silentEngineEnv(t)
		cfg := parseEngineFlag(t)
		if got := apiBaseURLFor(t, cfg); got != derived {
			t.Fatalf("APIBaseURL = %q, want the derived %q", got, derived)
		}
	})
}

// A settings file nobody can parse must not abort every command: appendEncOpts
// turns an engineOptions error into fatal(), and the engine default is auto,
// which probes the API host and corrects itself.
func TestEngineOptionsSurvivesAGarbageSettingsFile(t *testing.T) {
	silentEngineEnv(t)
	path := engineSettingsFile()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg := parseEngineFlag(t)
	if _, err := engineOptions(cfg); err != nil {
		t.Fatalf("engineOptions: %v, want a corrupt settings file to be ignored", err)
	}
	if got := engineModeFor(t, cfg); got != encx.DefaultEngineMode {
		t.Fatalf("EngineMode = %q, want the default %q", got, encx.DefaultEngineMode)
	}
}

// resolveEngineSettingField must not call an inherited environment value a
// flag, which is exactly what it looks like after registerEngineFlag defaults
// the flag from the environment.
func TestResolveEngineSettingFieldNamesTheRealSource(t *testing.T) {
	silentEngineEnv(t)
	t.Setenv(encx.EngineEnvVar, "legacy")

	// cfg.engine carries the environment value; the source is env, not flag.
	got := resolveEngineSettingField("legacy", engineEnvVars, "new", string(encx.DefaultEngineMode))
	if got.Source != llmSourceEnv || got.Value != "legacy" || got.EnvVar != encx.EngineEnvVar {
		t.Fatalf("resolved %+v, want the environment as the source", got)
	}

	// A flag that differs from the environment is a real flag.
	got = resolveEngineSettingField("new", engineEnvVars, "auto", string(encx.DefaultEngineMode))
	if got.Source != llmSourceFlag || got.Value != "new" {
		t.Fatalf("resolved %+v, want the flag as the source", got)
	}

	t.Setenv(encx.EngineEnvVar, "")
	got = resolveEngineSettingField("", engineEnvVars, "new", string(encx.DefaultEngineMode))
	if got.Source != llmSourceSettings || got.Value != "new" {
		t.Fatalf("resolved %+v, want the settings file as the source", got)
	}
	got = resolveEngineSettingField("", engineAPIBaseURLEnvVars, "", string(encx.DefaultEngineMode))
	if got.Source != llmSourceDefault {
		t.Fatalf("resolved %+v, want the built-in default as the source", got)
	}
}
