package main

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/sipeed/picoclaw/pkg/fileutil"
	"github.com/sipeed/picoclaw/pkg/providers"
	"golang.org/x/term"
)

const (
	authMethodClaude     = "claude"
	defaultClaudeModel   = "claude-sonnet-4-6"
	claudeAuthFileEnvVar = "ENCLI_CLAUDE_AUTH_FILE"
	claudeTokenEnvVar    = "CLAUDE_CODE_OAUTH_TOKEN"
)

var errNoClaudeCredential = errors.New("no Claude subscription token. Run claude setup-token, then encli claude-login")

type claudeCredential struct {
	AccessToken string `json:"access_token"`
}

func claudeAuthFile() string {
	if path := strings.TrimSpace(os.Getenv(claudeAuthFileEnvVar)); path != "" {
		return path
	}
	return filepath.Join(sessionDir(), "claude", "auth.json")
}

func validateClaudeToken(token string) error {
	if !strings.HasPrefix(token, "sk-ant-oat01-") || len(token) < 80 || len(token) > 16384 || strings.ContainsFunc(token, unicode.IsSpace) {
		return errors.New("invalid Claude subscription token; paste the token from claude setup-token (sk-ant-oat01-…), not an API key")
	}
	return nil
}

func loadClaudeCredential() (*claudeCredential, error) {
	if token := strings.TrimSpace(os.Getenv(claudeTokenEnvVar)); token != "" {
		if err := validateClaudeToken(token); err != nil {
			return nil, fmt.Errorf("%s: %w", claudeTokenEnvVar, err)
		}
		return &claudeCredential{AccessToken: token}, nil
	}
	data, err := os.ReadFile(claudeAuthFile())
	if errors.Is(err, os.ErrNotExist) {
		return nil, errNoClaudeCredential
	}
	if err != nil {
		return nil, fmt.Errorf("read Claude credential: %w", err)
	}
	var cred claudeCredential
	if err := json.Unmarshal(data, &cred); err != nil {
		// Parser errors can contain fragments of the secret-bearing input.
		return nil, errors.New("invalid Claude credential file; run encli claude-login again")
	}
	if err := validateClaudeToken(cred.AccessToken); err != nil {
		return nil, err
	}
	return &cred, nil
}

func saveClaudeCredential(token string) error {
	token = strings.TrimSpace(token)
	if err := validateClaudeToken(token); err != nil {
		return err
	}
	data, err := json.Marshal(claudeCredential{AccessToken: token})
	if err != nil {
		return err
	}
	path := claudeAuthFile()
	warnIfSessionGlobCollision(claudeAuthFileEnvVar, path, "Claude credential")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return fileutil.WriteFileAtomic(path, data, 0600)
}

func deleteClaudeCredential() error {
	if err := os.Remove(claudeAuthFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func hasClaudeCredential() bool {
	_, err := loadClaudeCredential()
	return err == nil
}

func claudeModel(configured string) string {
	name := strings.ToLower(strings.TrimSpace(configured))
	name = strings.TrimPrefix(name, "anthropic/")
	if strings.HasPrefix(name, "claude-") && !strings.ContainsAny(name, "/ \t\r\n") {
		// PicoClaw normalizes dotted version numbers before sending them.
		return strings.ReplaceAll(name, ".", "-")
	}
	return defaultClaudeModel
}

func newClaudeProvider() (providers.LLMProvider, error) {
	return newClaudeProviderAt("https://api.anthropic.com")
}

func validateClaudeEnvironment() error {
	if strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY")) != "" {
		// PicoClaw's SDK client inherits this header even with a token source.
		return errors.New("unset ANTHROPIC_API_KEY to use the Claude subscription transport without API-key billing")
	}
	return nil
}

// The endpoint is fixed in production: generic LLM_BASE_URL must never receive
// a subscription token. The parameter exists for local protocol tests only.
func newClaudeProviderAt(baseURL string) (providers.LLMProvider, error) {
	if err := validateClaudeEnvironment(); err != nil {
		return nil, err
	}
	cred, err := loadClaudeCredential()
	if err != nil {
		return nil, err
	}
	delegate := providers.NewClaudeProviderWithTokenSourceAndBaseURL(cred.AccessToken, func() (string, error) {
		// Re-read on every request so replacement/logout also affects an active
		// chat. Setup tokens have no refresh token; renew with claude setup-token.
		current, err := loadClaudeCredential()
		if err != nil {
			return "", err
		}
		return current.AccessToken, nil
	}, baseURL)
	return &claudeSubscriptionProvider{LLMProvider: delegate}, nil
}

// The OAuth endpoint requires this first system block, as sent by Claude Code
// in SDK mode. Without it, valid setup-tokens can receive a generic 429 even
// while the same token and model work in Claude Code.
const claudeSubscriptionSystem = "You are a Claude agent, built on Anthropic's Claude Agent SDK."

type claudeSubscriptionProvider struct {
	providers.LLMProvider
}

func (p *claudeSubscriptionProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]any) (*providers.LLMResponse, error) {
	// Allocate a separate slice: the caller owns its persisted history. A new
	// system message also preserves structured prompt parts and cache controls.
	request := make([]providers.Message, 0, len(messages)+1)
	request = append(request, providers.Message{Role: "system", Content: claudeSubscriptionSystem})
	request = append(request, messages...)
	return p.LLMProvider.Chat(ctx, request, tools, model, options)
}

func readClaudeToken(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(io.LimitReader(r, 16386))
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", errNoClaudeCredential
	}
	token := strings.TrimSpace(scanner.Text())
	return token, validateClaudeToken(token)
}

func cmdClaudeLogin(cfg *config) {
	fmt.Fprintln(os.Stderr, "Paste the token from claude setup-token:")
	var token string
	var err error
	if term.IsTerminal(int(os.Stdin.Fd())) {
		var data []byte
		data, err = term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		token = strings.TrimSpace(string(data))
	} else {
		token, err = readClaudeToken(os.Stdin)
	}
	if err == nil {
		err = saveClaudeCredential(token)
	}
	if err != nil {
		fatal("Claude sign-in failed: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true, "path": claudeAuthFile()})
		return
	}
	fmt.Println("Claude token saved. Run: encli -llm-auth claude --llm \"<prompt>\"")
}

func cmdClaudeLogout(cfg *config) {
	if err := deleteClaudeCredential(); err != nil {
		fatal("%v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{"success": true})
		return
	}
	fmt.Println("Stored Claude token cleared. If set, unset CLAUDE_CODE_OAUTH_TOKEN separately.")
}

type llmClaudeStatus struct {
	Configured bool   `json:"configured"`
	Source     string `json:"source"`
	Path       string `json:"path"`
	Error      string `json:"error,omitempty"`
}

func claudeStatus() llmClaudeStatus {
	status := llmClaudeStatus{Path: claudeAuthFile(), Source: "file"}
	if strings.TrimSpace(os.Getenv(claudeTokenEnvVar)) != "" {
		status.Source = "env"
	}
	_, err := loadClaudeCredential()
	status.Configured = err == nil
	if err != nil && !errors.Is(err, errNoClaudeCredential) {
		status.Error = err.Error()
	}
	return status
}

func cmdClaudeStatus(cfg *config) {
	status := claudeStatus()
	if cfg.jsonOutput {
		outputJSON(status)
	} else if status.Configured {
		fmt.Printf("Claude subscription token configured (%s). Server validity and expiry are checked on use.\n", status.Source)
	} else {
		fmt.Fprintln(os.Stderr, "Claude token not configured.", status.Error)
	}
	if !status.Configured {
		os.Exit(1)
	}
}

func (h *webHub) httpClaudeLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	defer r.Body.Close()
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32768))
	if err != nil || json.Unmarshal(data, &req) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid Claude token request"})
		return
	}
	if err := validateClaudeToken(strings.TrimSpace(req.Token)); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := saveClaudeCredential(req.Token); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, claudeStatus())
}

func (h *webHub) httpClaudeLogout(w http.ResponseWriter, r *http.Request) {
	if err := deleteClaudeCredential(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, claudeStatus())
}
