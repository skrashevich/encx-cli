package encx

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/skrashevich/encx-cli/encx/enapi"
)

// loginRequest is models.LoginRequest.
type loginRequest struct {
	Login          string `json:"login"`
	Password       string `json:"password"`
	CaptchaCode    string `json:"captcha_code,omitempty"`
	CaptchaToken   string `json:"captcha_token,omitempty"`
	Client         string `json:"client,omitempty"`
	NoCookie       bool   `json:"no_cookie,omitempty"`
	SharedComputer bool   `json:"shared_computer,omitempty"`
}

// loginPayload covers both the success body (token, user, message) and the
// failure bodies, which the API documents only by prose. Reading them through
// one permissive struct keeps a new error field from breaking the decode.
type loginPayload struct {
	Token               string          `json:"token"`
	Message             string          `json:"message"`
	SessionClass        string          `json:"session_class"`
	User                json.RawMessage `json:"user"`
	Error               string          `json:"error"`
	Code                int             `json:"code"`
	CaptchaToken        string          `json:"captcha_token"`
	CaptchaURL          string          `json:"captcha_url"`
	IPUnblockURL        string          `json:"ip_unblock_url"`
	BruteForceURL       string          `json:"brute_force_unblock_url"`
	ConfirmEmailURL     string          `json:"confirm_email_url"`
	AdminWhoCanActivate []string        `json:"admin_who_can_activate"`
}

// clientClassHeader tells the backend which session class the caller belongs to.
const clientClassHeader = "X-En-Client"

// automationClientClass keeps encx sessions out of the browser's class.
//
// The new engine evicts earlier sessions of the same class on every sign-in, so
// a cookie session here would log the user out of their browser. Asking for a
// cookie-less session puts encx in its own class: the browser session survives
// and is merely reported back in other_class_sessions.
const automationClientClass = "automation"

func (e *newEngine) Login(ctx context.Context, login, password string, opts ...LoginOptions) (*LoginResponse, error) {
	body := loginRequest{
		Login:    login,
		Password: password,
		Client:   automationClientClass,
		NoCookie: true,
	}
	if len(opts) > 0 && opts[0].MagicNumbers != "" {
		body.CaptchaCode = opts[0].MagicNumbers
		body.CaptchaToken = e.c.captchaToken()
	}

	var payload loginPayload
	err := e.c.api().Do(ctx, enapi.Request{
		Method: http.MethodPost,
		Path:   "/login",
		Body:   body,
		Out:    &payload,
		Header: http.Header{clientClassHeader: []string{automationClientClass}},
	})
	if err == nil {
		e.c.api().SetToken(payload.Token)
		e.c.setCaptchaToken("")
		return &LoginResponse{Error: 0, Message: payload.Message}, nil
	}

	apiErr, ok := enapi.AsAPIError(err)
	if !ok {
		return nil, err
	}
	// Failure bodies are JSON too; decode what is there and translate the
	// result into the legacy LoginResponse contract callers already handle.
	_ = json.Unmarshal([]byte(apiErr.Body), &payload)
	resp := loginResponseFromAPIError(apiErr, payload)
	e.c.setCaptchaToken(payload.CaptchaToken)
	return resp, nil
}

// loginResponseFromAPIError maps a new-engine login failure onto the legacy
// error codes documented in errors.go, so LoginErrorText keeps working.
func loginResponseFromAPIError(apiErr *enapi.APIError, payload loginPayload) *LoginResponse {
	resp := &LoginResponse{Error: 5, Message: payload.Message}
	if resp.Message == "" {
		resp.Message = firstNonEmpty(payload.Error, apiErr.Message, apiErr.Err, apiErr.Body)
	}
	if payload.CaptchaURL != "" {
		resp.CaptchaUrl = &payload.CaptchaURL
	}
	if payload.IPUnblockURL != "" {
		resp.IpUnblockUrl = &payload.IPUnblockURL
	}
	if payload.BruteForceURL != "" {
		resp.BruteForceUnblockUrl = &payload.BruteForceURL
	}
	if payload.ConfirmEmailURL != "" {
		resp.ConfirmEmailUrl = &payload.ConfirmEmailURL
	}
	resp.AdminWhoCanActivate = payload.AdminWhoCanActivate

	// The marker is read only from the fields that name the reason. Matching
	// substrings against the whole body would let an unrelated word — "recipient"
	// contains "ip" — turn wrong credentials into an IP block.
	marker := strings.ToLower(firstNonEmpty(payload.Error, apiErr.Err, apiErr.SentenceKey))
	switch {
	case strings.Contains(marker, "captcha"):
		resp.Error = 1
	case strings.Contains(marker, "brute"):
		resp.Error = 9
	case strings.Contains(marker, "blacklist") || strings.Contains(marker, "black list"):
		resp.Error = 3
	case strings.Contains(marker, "ip block") || strings.Contains(marker, "ip_block") ||
		strings.Contains(marker, "ipblock") || strings.Contains(marker, "ip filter"):
		resp.Error = 4
	case strings.Contains(marker, "email not confirmed") ||
		strings.Contains(marker, "confirm_email") ||
		strings.Contains(marker, "unconfirmed email"):
		resp.Error = 10
	case strings.Contains(marker, "not activated") || strings.Contains(marker, "inactive"):
		resp.Error = 8
	case strings.Contains(marker, "blocked") || strings.Contains(marker, "banned"):
		resp.Error = 7
	case apiErr.Status == http.StatusUnauthorized:
		resp.Error = 2
	case apiErr.Status == http.StatusForbidden:
		resp.Error = 7
	}
	return resp
}

// LoginComplete on the new engine is a plain sign-in: one JWT grants both play
// and administration access, so there is no second form to submit.
func (e *newEngine) LoginComplete(ctx context.Context, login, password string, opts ...LoginOptions) error {
	login = strings.TrimSpace(login)
	if login == "" || password == "" {
		return fmt.Errorf("encx: login and password required")
	}
	resp, err := e.Login(ctx, login, password, opts...)
	if err != nil {
		return err
	}
	if resp.Error != 0 {
		return fmt.Errorf("encx: login error %d: %s", resp.Error, LoginErrorText(resp.Error))
	}
	if err := e.VerifyAdminSession(ctx); err != nil {
		return fmt.Errorf("encx: signed in but the session is not accepted: %w", err)
	}
	return nil
}

func (e *newEngine) VerifyAdminSession(ctx context.Context) error {
	var session map[string]any
	if err := e.c.api().GetJSON(ctx, "/auth/session", nil, &session); err != nil {
		if enapi.IsUnauthorized(err) {
			return fmt.Errorf("encx: session expired or access denied: %w", err)
		}
		return err
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
