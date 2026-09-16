package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestLLMSettings stores settings through the production writer, so the
// tests read back exactly what the -web panel would have produced.
func writeTestLLMSettings(t *testing.T, s llmSettings) {
	t.Helper()
	if err := saveLLMSettings(s); err != nil {
		t.Fatalf("saveLLMSettings: %v", err)
	}
}

func TestLLMSettingsRoundTrip(t *testing.T) {
	isolateLLMEnv(t)
	writeTestLLMSettings(t, llmSettings{
		AuthMethod: "  APIKEY ",
		BaseURL:    " https://api.example.com/v1 ",
		APIKey:     " sk-file-key ",
		Model:      " gpt-4o ",
	})

	got, err := loadLLMSettings()
	if err != nil {
		t.Fatalf("loadLLMSettings: %v", err)
	}
	want := llmSettings{
		AuthMethod: authMethodAPIKey,
		BaseURL:    "https://api.example.com/v1",
		APIKey:     "sk-file-key",
		Model:      "gpt-4o",
	}
	if got != want {
		t.Fatalf("settings = %+v, want %+v (the writer and the reader must both trim)", got, want)
	}

	if err := deleteLLMSettings(); err != nil {
		t.Fatalf("deleteLLMSettings: %v", err)
	}
	if got, err := loadLLMSettings(); err != nil || got != (llmSettings{}) {
		t.Fatalf("after delete: settings = %+v, err = %v", got, err)
	}
	// Deleting settings that are already gone is what the panel's reset does on
	// a machine that never configured anything.
	if err := deleteLLMSettings(); err != nil {
		t.Fatalf("deleteLLMSettings on a missing file: %v", err)
	}
}

// The file holds an API key, so neither it nor the directory it lives in may be
// readable by another account on the machine.
func TestLLMSettingsFilePermissions(t *testing.T) {
	isolateLLMEnv(t)
	// isolateLLMEnv points the override at a file directly in its temp dir; use a
	// nested path so the directory mode is the one saveLLMSettings creates.
	path := filepath.Join(t.TempDir(), "llm", "settings.json")
	t.Setenv(llmSettingsFileEnvVar, path)

	writeTestLLMSettings(t, llmSettings{APIKey: "sk-file-key"})

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

	// os.WriteFile applies its mode only when it creates the file, so a save over
	// a world-readable file has to tighten it rather than inherit it.
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	writeTestLLMSettings(t, llmSettings{APIKey: "sk-file-key-2"})
	info, err = os.Stat(path)
	if err != nil {
		t.Fatalf("stat settings: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("settings mode = %v, want 0600 after overwriting a 0644 file", perm)
	}
}

// Nothing configured is the normal state for an operator who runs on the
// environment alone, so a missing file must not be reported as a failure.
func TestLLMSettingsMissingFileIsEmpty(t *testing.T) {
	isolateLLMEnv(t)
	t.Setenv(llmSettingsFileEnvVar, filepath.Join(t.TempDir(), "absent", "settings.json"))

	got, err := loadLLMSettings()
	if err != nil {
		t.Fatalf("a missing settings file must not be an error: %v", err)
	}
	if got != (llmSettings{}) {
		t.Fatalf("settings = %+v, want the zero value", got)
	}
}

func TestLLMSettingsEmptyFileIsEmpty(t *testing.T) {
	isolateLLMEnv(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv(llmSettingsFileEnvVar, path)
	if err := os.WriteFile(path, []byte("   \n"), 0600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	got, err := loadLLMSettings()
	if err != nil {
		t.Fatalf("a blank settings file must not be an error: %v", err)
	}
	if got != (llmSettings{}) {
		t.Fatalf("settings = %+v, want the zero value", got)
	}
}

// A file that does not parse must be loud: ignoring it would silently run the
// agent against a provider the operator did not choose.
func TestLLMSettingsRejectsBrokenJSON(t *testing.T) {
	isolateLLMEnv(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv(llmSettingsFileEnvVar, path)
	if err := os.WriteFile(path, []byte(`{"auth_method": `), 0600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	_, err := loadLLMSettings()
	if err == nil {
		t.Fatal("loadLLMSettings accepted a file that does not parse")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %v, want it to name the file to delete", err)
	}
}

// AuthRegistry.ListStatus reads every sessionDir()/*.json as an Encounter domain
// session, and its Logout deletes the file it names. Settings stored there would
// appear in the -web auth panel as a bogus domain whose Logout wipes them.
func TestLLMSettingsIsNotMistakenForADomainSession(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(llmSettingsFileEnvVar, "")

	path := llmSettingsFile()
	if credentialPathCollides(path) {
		t.Fatalf("the default settings path %q lands in the sessionDir()/*.json glob", path)
	}

	writeTestLLMSettings(t, llmSettings{APIKey: "sk-file-key"})
	for _, status := range NewAuthRegistry().ListStatus() {
		t.Errorf("settings surfaced as domain %q (session %s)", status.Domain, status.SessionPath)
	}
	NewAuthRegistry().Logout("settings")
	NewAuthRegistry().Logout("llm")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a web logout deleted the LLM settings: %v", err)
	}
}

// ENCLI_LLM_SETTINGS_FILE can undo the default path's safety, so a collision is
// worth saying out loud.
func TestLLMSettingsWarnsAboutACollidingOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	capture := func(path string) string {
		t.Helper()
		t.Setenv(llmSettingsFileEnvVar, path)
		read, write, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		original := os.Stderr
		os.Stderr = write
		saveErr := saveLLMSettings(llmSettings{APIKey: "sk-file-key"})
		os.Stderr = original
		write.Close()
		var buf strings.Builder
		if _, err := io.Copy(&buf, read); err != nil {
			t.Fatalf("read stderr: %v", err)
		}
		read.Close()
		if saveErr != nil {
			t.Fatalf("saveLLMSettings: %v", saveErr)
		}
		return buf.String()
	}

	if out := capture(filepath.Join(sessionDir(), "settings.json")); !strings.Contains(out, llmSettingsFileEnvVar) {
		t.Errorf("a colliding override produced no warning, got %q", out)
	}
	if out := capture(filepath.Join(sessionDir(), "llm", "settings.json")); out != "" {
		t.Errorf("the default path must not warn, got %q", out)
	}
}

func TestLLMSettingsValidateBaseURL(t *testing.T) {
	for _, tc := range []struct {
		raw     string
		wantErr bool
		why     string
	}{
		{"", false, "unset hands the choice back to the environment"},
		{"   ", false, "blank is unset"},
		{"https://api.example.com/v1", false, "the ordinary case"},
		{"http://127.0.0.1:8317/v1", false, "a local proxy"},
		{"ftp://x", true, "the transport only speaks HTTP"},
		{"не-url", true, "no scheme and no host"},
		{"api.example.com/v1", true, "a bare host is not a URL the client can dial"},
		{"https://", true, "no host"},
		{"://nope", true, "unparseable"},
	} {
		err := validateLLMBaseURL(tc.raw)
		if tc.wantErr && err == nil {
			t.Errorf("validateLLMBaseURL(%q) = nil, want an error (%s)", tc.raw, tc.why)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("validateLLMBaseURL(%q) = %v, want nil (%s)", tc.raw, err, tc.why)
		}
	}
}

// The mask exists so the key can be shown without being disclosed: it must be
// enough to tell two keys apart and useless to anyone reading the response.
func TestLLMSettingsMaskAPIKey(t *testing.T) {
	if got := maskAPIKey(""); got != "" {
		t.Errorf("maskAPIKey(%q) = %q, want the empty string", "", got)
	}
	if got := maskAPIKey("   "); got != "" {
		t.Errorf("maskAPIKey on blank = %q, want the empty string", got)
	}

	const key = "sk-secret-value-1234"
	masked := maskAPIKey(key)
	if strings.Contains(masked, "sk-secret") {
		t.Fatalf("maskAPIKey(%q) = %q, which discloses the head of the key", key, masked)
	}
	if strings.Contains(masked, key) {
		t.Fatalf("maskAPIKey(%q) = %q, which discloses the key", key, masked)
	}
	if !strings.HasSuffix(masked, "1234") {
		t.Errorf("maskAPIKey(%q) = %q, want the tail kept so two keys can be told apart", key, masked)
	}
	// A key short enough that a tail would be most of it discloses nothing at all.
	if got := maskAPIKey("abcd"); strings.Contains(got, "abcd") {
		t.Errorf("maskAPIKey(%q) = %q, which discloses the whole key", "abcd", got)
	}
}

func TestLLMSettingsFirstEnvNamesTheWinner(t *testing.T) {
	isolateLLMEnv(t)

	if value, from := firstEnv(llmAPIKeyEnvVars); value != "" || from != "" {
		t.Fatalf("firstEnv on a clean environment = (%q, %q)", value, from)
	}

	t.Setenv("OPENROUTER_API_KEY", "sk-fallback")
	if value, from := firstEnv(llmAPIKeyEnvVars); value != "sk-fallback" || from != "OPENROUTER_API_KEY" {
		t.Fatalf("firstEnv = (%q, %q), want the only variable set", value, from)
	}

	// The list order is the precedence, and the name matters: the panel reports
	// which variable is shadowing a stored value.
	t.Setenv("LLM_API_KEY", "  sk-primary  ")
	if value, from := firstEnv(llmAPIKeyEnvVars); value != "sk-primary" || from != "LLM_API_KEY" {
		t.Fatalf("firstEnv = (%q, %q), want the first listed variable, trimmed", value, from)
	}
}

func TestLLMSettingsResolveLLMFieldPrecedence(t *testing.T) {
	isolateLLMEnv(t)

	if got := resolveLLMField("", llmModelEnvVars, "", ""); got.Source != llmSourceUnset || got.Value != "" {
		t.Fatalf("nothing configured = %+v, want unset", got)
	}
	if got := resolveLLMField("", llmModelEnvVars, "", "def"); got.Source != llmSourceDefault || got.Value != "def" {
		t.Fatalf("default only = %+v", got)
	}
	if got := resolveLLMField("", llmModelEnvVars, "stored", "def"); got.Source != llmSourceSettings || got.Value != "stored" {
		t.Fatalf("stored over default = %+v", got)
	}

	t.Setenv("LLM_MODEL", "from-env")
	got := resolveLLMField("", llmModelEnvVars, "stored", "def")
	if got.Source != llmSourceEnv || got.Value != "from-env" || got.EnvVar != "LLM_MODEL" {
		t.Fatalf("env over stored = %+v", got)
	}
	if got := resolveLLMField("  from-flag  ", llmModelEnvVars, "stored", "def"); got.Source != llmSourceFlag || got.Value != "from-flag" {
		t.Fatalf("flag over env = %+v", got)
	}
}

// The panel can only offer to change what it stores, so a flag or a variable is
// something it has to explain rather than edit.
func TestLLMSettingsSourceIsAmbient(t *testing.T) {
	for _, source := range []string{llmSourceFlag, llmSourceEnv} {
		if !llmSourceIsAmbient(source) {
			t.Errorf("llmSourceIsAmbient(%q) = false, want true", source)
		}
	}
	for _, source := range []string{llmSourceSettings, llmSourceDefault, llmSourceUnset, ""} {
		if llmSourceIsAmbient(source) {
			t.Errorf("llmSourceIsAmbient(%q) = true, want false", source)
		}
	}
}

// --- resolveAgentConfig precedence: flag > environment > settings file > default ---

func TestResolveAgentConfigUsesStoredSettings(t *testing.T) {
	isolateLLMEnv(t)
	writeTestLLMSettings(t, llmSettings{
		BaseURL: "https://api.example.com/v1",
		APIKey:  "sk-file-key",
		Model:   "file/model",
	})

	got, err := resolveAgentConfig(&config{})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if got.APIKey != "sk-file-key" || got.BaseURL != "https://api.example.com/v1" || got.Model != "file/model" {
		t.Fatalf("config = %+v, want the stored transport", got)
	}
	if got.AuthMethod != "" {
		t.Fatalf("auth method = %q, want the API key path", got.AuthMethod)
	}
}

// A stored base URL alone is a deliberate statement of intent, so it must reach
// the agent even though no key accompanies it.
func TestResolveAgentConfigUsesAStoredLocalEndpointWithoutAKey(t *testing.T) {
	isolateLLMEnv(t)
	writeTestLLMSettings(t, llmSettings{BaseURL: "http://127.0.0.1:8317/v1"})

	got, err := resolveAgentConfig(&config{})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if got.BaseURL != "http://127.0.0.1:8317/v1" || got.Model != defaultLLMModel {
		t.Fatalf("config = %+v", got)
	}
}

func TestResolveAgentConfigEnvironmentOutranksStoredSettings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		envVar string
		env    string
		stored llmSettings
		check  func(t *testing.T, got AgentConfig)
	}{
		{
			name:   "api key",
			envVar: "LLM_API_KEY",
			env:    "sk-env-key",
			stored: llmSettings{APIKey: "sk-file-key"},
			check: func(t *testing.T, got AgentConfig) {
				if got.APIKey != "sk-env-key" {
					t.Fatalf("api key = %q, want the environment's", got.APIKey)
				}
			},
		},
		{
			name:   "base url",
			envVar: "LLM_BASE_URL",
			env:    "https://env.example.com/v1",
			stored: llmSettings{APIKey: "sk-file-key", BaseURL: "https://file.example.com/v1"},
			check: func(t *testing.T, got AgentConfig) {
				if got.BaseURL != "https://env.example.com/v1" {
					t.Fatalf("base URL = %q, want the environment's", got.BaseURL)
				}
			},
		},
		{
			name:   "model",
			envVar: "LLM_MODEL",
			env:    "env/model",
			stored: llmSettings{APIKey: "sk-file-key", Model: "file/model"},
			check: func(t *testing.T, got AgentConfig) {
				if got.Model != "env/model" {
					t.Fatalf("model = %q, want the environment's", got.Model)
				}
			},
		},
		{
			name:   "auth method",
			envVar: "LLM_AUTH",
			env:    authMethodCodex,
			stored: llmSettings{AuthMethod: authMethodAPIKey, APIKey: "sk-file-key"},
			check: func(t *testing.T, got AgentConfig) {
				if got.AuthMethod != authMethodCodex {
					t.Fatalf("auth method = %q, want the environment's", got.AuthMethod)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateLLMEnv(t)
			// The subscription is needed only by the auth-method case, but storing
			// it everywhere proves the other cases stay on the API key path.
			writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1"})
			writeTestLLMSettings(t, tc.stored)
			t.Setenv(tc.envVar, tc.env)

			got, err := resolveAgentConfig(&config{})
			if err != nil {
				t.Fatalf("resolveAgentConfig: %v", err)
			}
			tc.check(t, got)
		})
	}
}

// The command line is the operator speaking right now, so it outranks both the
// deployment's variables and whatever was typed into the panel weeks ago.
func TestResolveAgentConfigFlagOutranksEnvironmentAndSettings(t *testing.T) {
	isolateLLMEnv(t)
	writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1"})
	writeTestLLMSettings(t, llmSettings{AuthMethod: authMethodCodex, APIKey: "sk-file-key"})
	t.Setenv("LLM_AUTH", authMethodCodex)

	got, err := resolveAgentConfig(&config{llmAuth: authMethodAPIKey})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if got.AuthMethod != "" {
		t.Fatalf("auth method = %q, want the flag's API key path", got.AuthMethod)
	}
	if got.APIKey != "sk-file-key" {
		t.Fatalf("api key = %q, want the stored one to remain in use", got.APIKey)
	}
}

func TestResolveAgentConfigStoredAuthMethodSelectsCodex(t *testing.T) {
	isolateLLMEnv(t)
	writeTestCodexCredential(t, &codexCredential{AccessToken: "access-1"})
	// A stored base URL must not divert the transport once codex was chosen.
	writeTestLLMSettings(t, llmSettings{AuthMethod: authMethodCodex, BaseURL: "https://file.example.com/v1"})

	got, err := resolveAgentConfig(&config{})
	if err != nil {
		t.Fatalf("resolveAgentConfig: %v", err)
	}
	if got.AuthMethod != authMethodCodex {
		t.Fatalf("auth method = %q, want the stored subscription", got.AuthMethod)
	}
	if got.Model != defaultCodexModel {
		t.Fatalf("model = %q, want %q", got.Model, defaultCodexModel)
	}
	if got.APIKey != "" || got.BaseURL != "" {
		t.Fatalf("subscription config must carry no API key or base URL: %+v", got)
	}
}

func TestResolveAgentConfigStoredCodexNeedsASignIn(t *testing.T) {
	isolateLLMEnv(t)
	writeTestLLMSettings(t, llmSettings{AuthMethod: authMethodCodex})

	_, err := resolveAgentConfig(&config{})
	if err == nil || !strings.Contains(err.Error(), "codex-login") {
		t.Fatalf("error = %v, want a codex-login hint", err)
	}
}

func TestResolveAgentConfigRejectsAnUnknownStoredAuthMethod(t *testing.T) {
	isolateLLMEnv(t)
	writeTestLLMSettings(t, llmSettings{AuthMethod: "chatgpt"})

	_, err := resolveAgentConfig(&config{})
	if err == nil || !strings.Contains(err.Error(), "apikey, codex or gigachat") {
		t.Fatalf("error = %v, want the accepted values", err)
	}
}

// A settings file that does not parse must stop the run rather than quietly
// resolve the transport from the environment alone.
func TestResolveAgentConfigReportsABrokenSettingsFile(t *testing.T) {
	isolateLLMEnv(t)
	path := filepath.Join(t.TempDir(), "settings.json")
	t.Setenv(llmSettingsFileEnvVar, path)
	if err := os.WriteFile(path, []byte("not json at all"), 0600); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	_, err := resolveAgentConfig(&config{})
	if err == nil || !strings.Contains(err.Error(), "LLM settings") {
		t.Fatalf("error = %v, want the settings parse failure", err)
	}
}
