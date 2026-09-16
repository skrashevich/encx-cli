package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

// llmSettingsFileEnvVar relocates the settings file; tests use it to stay out of
// the developer's real home directory.
const llmSettingsFileEnvVar = "ENCLI_LLM_SETTINGS_FILE"

// llmSettings is the transport an operator configured in the -web settings
// panel. It is the persistent counterpart of the LLM_* environment variables,
// and it is read by exactly one function: resolveAgentConfig.
//
// A field left empty means "not configured here", so clearing a field in the UI
// hands the choice back to the environment rather than forcing an empty value.
type llmSettings struct {
	AuthMethod string `json:"auth_method,omitempty"`
	BaseURL    string `json:"base_url,omitempty"`
	APIKey     string `json:"api_key,omitempty"`
	Model      string `json:"model,omitempty"`
}

// llmSettingsFile resolves the settings path.
//
// Like the ChatGPT credential it lives in a subdirectory rather than directly in
// sessionDir(), because AuthRegistry.ListStatus globs sessionDir()/*.json and
// reads every match as an Encounter domain session: a settings file sitting
// there would surface in the -web auth panel as a bogus domain whose Logout
// button deletes it.
func llmSettingsFile() string {
	if path := strings.TrimSpace(os.Getenv(llmSettingsFileEnvVar)); path != "" {
		return path
	}
	return filepath.Join(sessionDir(), "llm", "settings.json")
}

// loadLLMSettings reads the stored transport. A missing file is not an error:
// nothing configured is the normal state for an operator who runs on the
// environment alone.
//
// A file that does not parse IS an error, and it is deliberately propagated all
// the way to resolveAgentConfig. Ignoring it would silently run the agent
// against a provider the operator did not choose.
func loadLLMSettings() (llmSettings, error) {
	path := llmSettingsFile()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return llmSettings{}, nil
		}
		return llmSettings{}, fmt.Errorf("read LLM settings %s: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return llmSettings{}, nil
	}
	var s llmSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return llmSettings{}, fmt.Errorf("parse LLM settings %s: %w (delete the file to start over)", path, err)
	}
	return s.trimmed(), nil
}

func (s llmSettings) trimmed() llmSettings {
	return llmSettings{
		AuthMethod: strings.ToLower(strings.TrimSpace(s.AuthMethod)),
		BaseURL:    strings.TrimSpace(s.BaseURL),
		APIKey:     strings.TrimSpace(s.APIKey),
		Model:      strings.TrimSpace(s.Model),
	}
}

// saveLLMSettings writes the settings for the next process to read.
//
// The file holds an API key, so it is created 0600 in a 0700 directory and
// written through a temp file: os.WriteFile applies its mode only when creating
// and would leave a pre-existing file world-readable.
func saveLLMSettings(s llmSettings) error {
	data, err := json.MarshalIndent(s.trimmed(), "", "  ")
	if err != nil {
		return fmt.Errorf("encode LLM settings: %w", err)
	}
	path := llmSettingsFile()
	warnIfSessionGlobCollision(llmSettingsFileEnvVar, path, "LLM settings")
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := fileutil.WriteFileAtomic(path, data, 0600); err != nil {
		return fmt.Errorf("write LLM settings %s: %w", path, err)
	}
	return nil
}

func deleteLLMSettings() error {
	if err := os.Remove(llmSettingsFile()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// --- resolution ---

// The environment variables consulted for each configurable field, in the order
// the first non-empty one wins. They are listed here rather than inline in
// resolveAgentConfig so the settings API can name the variable that is shadowing
// a stored value.
var (
	llmAuthEnvVars    = []string{"LLM_AUTH"}
	llmAPIKeyEnvVars  = []string{"LLM_API_KEY", "OPENROUTER_API_KEY"}
	llmBaseURLEnvVars = []string{"LLM_BASE_URL", "OPENROUTER_BASE_URL"}
	llmModelEnvVars   = []string{"LLM_MODEL", "OPENROUTER_MODEL"}
)

// firstEnv returns the first non-empty variable among names and the name that
// carried it.
func firstEnv(names []string) (value, from string) {
	for _, name := range names {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v, name
		}
	}
	return "", ""
}

// llmFieldSource is where one resolved field came from.
type llmFieldSource struct {
	Value  string `json:"value"`
	Source string `json:"source"`            // env | settings | default | unset
	EnvVar string `json:"env_var,omitempty"` // the variable that won, when source is env
}

const (
	llmSourceFlag     = "flag"
	llmSourceEnv      = "env"
	llmSourceSettings = "settings"
	llmSourceDefault  = "default"
	llmSourceUnset    = "unset"
)

// llmSourceIsAmbient reports whether a value came from outside the settings
// file. The panel can only offer to change what it stores, so these are the
// sources it has to explain rather than edit.
func llmSourceIsAmbient(source string) bool {
	return source == llmSourceFlag || source == llmSourceEnv
}

// resolveLLMField applies the precedence an operator was promised: an explicit
// value from the command line, then the environment, then what the settings
// panel stored, then the built-in default.
//
// The environment deliberately outranks the settings file. A key exported into
// a shell or a container is the deployment speaking, and it must not be
// overridden by a value someone typed into the UI weeks ago — so the panel
// reports the shadowing instead of losing to it silently.
func resolveLLMField(explicit string, envNames []string, stored, def string) llmFieldSource {
	if v := strings.TrimSpace(explicit); v != "" {
		return llmFieldSource{Value: v, Source: llmSourceFlag}
	}
	if v, from := firstEnv(envNames); v != "" {
		return llmFieldSource{Value: v, Source: llmSourceEnv, EnvVar: from}
	}
	if v := strings.TrimSpace(stored); v != "" {
		return llmFieldSource{Value: v, Source: llmSourceSettings}
	}
	if def != "" {
		return llmFieldSource{Value: def, Source: llmSourceDefault}
	}
	return llmFieldSource{Source: llmSourceUnset}
}

// validateLLMBaseURL rejects an endpoint the transport could never reach, so the
// settings panel fails at save time rather than on the operator's next message.
func validateLLMBaseURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("base URL is not a URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("base URL must start with http:// or https://, got %q", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("base URL has no host: %q", raw)
	}
	return nil
}

// maskAPIKey renders a stored key for display. Only the tail is shown: it is
// enough to tell two keys apart and useless to anyone reading the response.
func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	const tail = 4
	if len(key) <= tail {
		return strings.Repeat("•", len(key))
	}
	return "••••" + key[len(key)-tail:]
}
