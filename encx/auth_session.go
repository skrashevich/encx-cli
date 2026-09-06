package encx

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// legacyLoginComplete establishes a session that works for the ASP.NET
// Administration pages: Login.aspx form first, JSON /login/signin second.
func (c *Client) legacyLoginComplete(ctx context.Context, login, password string, opts ...LoginOptions) error {
	login = strings.TrimSpace(login)
	if login == "" || password == "" {
		return fmt.Errorf("encx: login and password required")
	}

	pageURL := c.baseURL() + "/Login.aspx?return=/"
	var pageErr error
	if err := c.LoginViaLoginPage(ctx, pageURL, login, password, opts...); err != nil {
		pageErr = err
	} else if verifyErr := c.legacyVerifyAdminSession(ctx); verifyErr == nil {
		return nil
	} else {
		pageErr = verifyErr
	}

	resp, err := c.legacyLogin(ctx, login, password, opts...)
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
	if err := c.legacyVerifyAdminSession(ctx); err != nil {
		return fmt.Errorf("%w: administration pages still require login: %v", ErrAdminAccessUnverified, err)
	}
	return nil
}

// legacyVerifyAdminSession reports whether the cookie jar can access the
// ASP.NET game administration URLs.
func (c *Client) legacyVerifyAdminSession(ctx context.Context) error {
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
		// Редирект в неизвестное место (например, на главную для
		// неавторизованного) — это не доступ к админке.
		return fmt.Errorf("encx: admin session check redirected to %q", headers.Get("Location"))
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
