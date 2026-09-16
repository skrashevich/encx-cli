package main

import (
	"fmt"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
)

// engineSettingsFileEnvVar relocates the settings file; tests use it to stay out
// of the developer's real home directory.
const engineSettingsFileEnvVar = "ENCLI_ENGINE_SETTINGS_FILE"

// engineSettings is the Encounter backend an operator picked and wants kept
// across runs. It is the persistent counterpart of ENCX_ENGINE and
// ENCX_API_BASE_URL, and it is read by exactly one function: engineOptions.
//
// A field left empty means "not configured here", so clearing it hands the
// choice back to the environment rather than pinning an empty value.
type engineSettings struct {
	Engine     string `json:"engine,omitempty"`
	APIBaseURL string `json:"api_base_url,omitempty"`
}

func (s engineSettings) trimmed() engineSettings {
	return engineSettings{
		Engine:     strings.ToLower(strings.TrimSpace(s.Engine)),
		APIBaseURL: strings.TrimSpace(s.APIBaseURL),
	}
}

// engineSettingsFile resolves the settings path. The "engine" subdirectory is
// the part that matters; stateFilePath explains why.
func engineSettingsFile() string {
	return stateFilePath(engineSettingsFileEnvVar, "engine", "settings.json")
}

// engineSettingsWhat is the operator-facing noun in this file's errors and in
// the warning about a relocated path.
const engineSettingsWhat = "engine settings"

// loadEngineSettings reads the stored engine selection, normalising it on the
// way out so callers never have to wonder whether a stored value was padded.
func loadEngineSettings() (engineSettings, error) {
	s, err := loadJSONState[engineSettings](engineSettingsFile(), engineSettingsWhat)
	if err != nil {
		return engineSettings{}, err
	}
	return s.trimmed(), nil
}

// saveEngineSettings writes the selection for the next process to read.
//
// It validates before writing so a typo is refused while the operator is still
// looking at the panel, rather than aborting their next command.
func saveEngineSettings(s engineSettings) error {
	s = s.trimmed()
	if err := validateEngineMode(s.Engine); err != nil {
		return err
	}
	if err := validateEngineAPIBaseURL(s.APIBaseURL); err != nil {
		return err
	}
	return saveJSONState(engineSettingsFile(), engineSettingsFileEnvVar, engineSettingsWhat, s)
}

func deleteEngineSettings() error {
	return deleteJSONState(engineSettingsFile())
}

// validateEngineMode rejects a name no engine answers to. An empty value is
// accepted: it means the file does not configure the engine.
func validateEngineMode(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if _, ok := encx.ParseEngineMode(raw); !ok {
		return fmt.Errorf("unknown engine %q: use legacy, new or auto", raw)
	}
	return nil
}

// validateEngineAPIBaseURL applies the same reachability rules as the LLM base
// URL — the two are the same question about a different host, so the check is
// borrowed rather than copied — and names the engine so the operator knows
// which of the two fields the panel refused.
func validateEngineAPIBaseURL(raw string) error {
	if err := validateLLMBaseURL(raw); err != nil {
		return fmt.Errorf("engine API %w", err)
	}
	return nil
}

// The environment variables consulted for each stored field. They are listed
// here rather than inline so the settings API can name the variable that is
// shadowing a stored value.
var (
	engineEnvVars           = []string{encx.EngineEnvVar}
	engineAPIBaseURLEnvVars = []string{apiBaseURLEnvVar}
)

// resolveEngineSettingField reports where one engine field's value came from,
// correcting for a quirk of the flag set: registerEngineFlag defaults -engine
// and -api-base-url from the environment, so by the time a value reaches here a
// real flag and an exported variable are indistinguishable. Handing cfg.engine
// to resolveLLMField as the explicit value would therefore label the
// environment's choice a flag. Matching it against the environment first keeps
// the report honest; a flag that repeats the environment verbatim is reported
// as env, which changes nothing the operator can act on because the value is
// the same either way.
func resolveEngineSettingField(cfgValue string, envNames []string, stored, def string) llmFieldSource {
	explicit := strings.TrimSpace(cfgValue)
	if env, _ := firstEnv(envNames); env != "" && env == explicit {
		explicit = ""
	}
	return resolveLLMField(explicit, envNames, stored, def)
}
