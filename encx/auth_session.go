package encx

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// LoginComplete establishes a session that works for Administration pages.
// It signs in via Login.aspx first, then falls back to JSON /login/signin if needed.
func (c *Client) LoginComplete(ctx context.Context, login, password string, opts ...LoginOptions) error {
	login = strings.TrimSpace(login)
	if login == "" || password == "" {
		return fmt.Errorf("encx: login and password required")
	}

	pageURL := c.baseURL() + "/Login.aspx?return=/"
	var pageErr error
	if err := c.LoginViaLoginPage(ctx, pageURL, login, password, opts...); err == nil {
		if verifyErr := c.VerifyAdminSession(ctx); verifyErr == nil {
			return nil
		} else {
			pageErr = verifyErr
		}
	} else {
		pageErr = err
	}

	resp, err := c.Login(ctx, login, password, opts...)
	if err != nil {
		if pageErr != nil {
			return fmt.Errorf("encx: login page failed (%v); json login failed: %w", pageErr, err)
		}
		return err
	}
	if resp.Error != 0 {
		if pageErr != nil {
			return fmt.Errorf("encx: login page failed (%v); json login error %d: %s", pageErr, resp.Error, LoginErrorText(resp.Error))
		}
		return fmt.Errorf("encx: login error %d: %s", resp.Error, LoginErrorText(resp.Error))
	}
	if err := c.VerifyAdminSession(ctx); err != nil {
		return fmt.Errorf("encx: signed in but administration pages still require login: %w", err)
	}
	return nil
}

// VerifyAdminSession reports whether the cookie jar can access game administration URLs.
func (c *Client) VerifyAdminSession(ctx context.Context) error {
	u := c.baseURL() + "/Administration/Games/LevelManager.aspx"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("encx: create admin session check: %w", err)
	}
	c.setHeaders(req)

	statusCode, headers, body, err := c.doRequestAndRead(req)
	if err != nil {
		return err
	}
	if isRedirectStatus(statusCode) {
		loc := strings.ToLower(strings.TrimSpace(headers.Get("Location")))
		if isLoginRedirect(loc) {
			return fmt.Errorf("encx: session expired or access denied (redirect to login)")
		}
		if strings.Contains(loc, "/administration/") {
			return nil
		}
	}
	if err := guardAdminHTMLRequiresLogin(body); err != nil {
		return err
	}
	return nil
}

func guardAdminHTMLRequiresLogin(body []byte) error {
	if len(body) == 0 {
		return nil
	}
	lower := strings.ToLower(string(body))
	if strings.Contains(lower, "id=\"formmain\"") && strings.Contains(lower, "name=\"login\"") {
		return fmt.Errorf("encx: session expired or access denied (login page returned)")
	}
	return nil
}
