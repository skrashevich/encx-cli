package encx

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	loginFormActionRE      = regexp.MustCompile(`(?is)<form[^>]*\bid="formMain"[^>]*action="([^"]*)"`)
	loginFormActionAltRE   = regexp.MustCompile(`(?is)<form[^>]*action="([^"]*)"[^>]*>[\s\S]*?name="Login"`)
	loginNetworkSelectedRE = regexp.MustCompile(`(?is)<option[^>]*value="(\d+)"[^>]*selected`)
	loginPageErrorRE       = regexp.MustCompile(`(?is)<span[^>]*id="lblDBMessage"[^>]*>([^<]*)</span>`)
	loginPageFieldsRE      = regexp.MustCompile(`(?is)name="Login"`)
	inputTagRE             = regexp.MustCompile(`(?is)<input\b[^>]*>`)
	inputTypeRE            = regexp.MustCompile(`(?is)\btype\s*=\s*["']?([^"'\s>]+)`)
	inputNameRE            = regexp.MustCompile(`(?is)\bname\s*=\s*["']([^"']+)["']`)
	inputValueRE           = regexp.MustCompile(`(?is)\bvalue\s*=\s*["']([^"']*)["']`)
)

type loginPageForm struct {
	PostURL string
	Network string
	Hidden  url.Values
}

func parseLoginPageForm(pageURL string, pageHTML []byte) (loginPageForm, error) {
	base, err := url.Parse(pageURL)
	if err != nil {
		return loginPageForm{}, fmt.Errorf("encx: parse login page URL: %w", err)
	}
	if !loginPageFieldsRE.Match(pageHTML) {
		return loginPageForm{}, fmt.Errorf("encx: login form not found on page")
	}

	action := ""
	if m := loginFormActionRE.FindSubmatch(pageHTML); len(m) >= 2 {
		action = string(m[1])
	} else if m := loginFormActionAltRE.FindSubmatch(pageHTML); len(m) >= 2 {
		action = string(m[1])
	}
	postURL := pageURL
	if action != "" {
		if resolved := resolveRefURL(base, strings.TrimSpace(decodeURLAttr(action))); resolved != "" {
			postURL = resolved
		}
	}

	network := "1"
	if m := loginNetworkSelectedRE.FindSubmatch(pageHTML); len(m) >= 2 {
		network = string(m[1])
	}

	hidden := url.Values{}
	for _, input := range inputTagRE.FindAll(pageHTML, -1) {
		inputType := ""
		if m := inputTypeRE.FindSubmatch(input); len(m) >= 2 {
			inputType = strings.ToLower(strings.TrimSpace(string(m[1])))
		}
		if inputType != "hidden" {
			continue
		}
		name := ""
		if m := inputNameRE.FindSubmatch(input); len(m) >= 2 {
			name = strings.TrimSpace(html.UnescapeString(string(m[1])))
		}
		if name == "" {
			continue
		}
		value := ""
		if m := inputValueRE.FindSubmatch(input); len(m) >= 2 {
			value = html.UnescapeString(string(m[1]))
		}
		hidden.Add(name, value)
	}
	hidden.Set("socialAssign", "0")
	return loginPageForm{PostURL: postURL, Network: network, Hidden: hidden}, nil
}

func loginPageFailureMessage(pageHTML []byte) string {
	if m := loginPageErrorRE.FindSubmatch(pageHTML); len(m) >= 2 {
		return strings.TrimSpace(html.UnescapeString(string(m[1])))
	}
	if strings.Contains(strings.ToLower(string(pageHTML)), "incorrect login or password") {
		return "Incorrect login or password"
	}
	return ""
}

// isLoginCheckCookieRedirect is the post-credentials hop on many Encounter domains.
func isLoginCheckCookieRedirect(location string) bool {
	loc := strings.ToLower(strings.TrimSpace(location))
	return strings.Contains(loc, "login.aspx") && strings.Contains(loc, "checkcookie=1")
}

func isLoginFailureRedirect(location string) bool {
	loc := strings.ToLower(strings.TrimSpace(location))
	if loc == "" {
		return false
	}
	if isLoginCheckCookieRedirect(loc) {
		return false
	}
	return strings.Contains(loc, "login.aspx")
}

func (c *Client) completeLoginPageFlow(ctx context.Context, status int, headers http.Header, respBody []byte, loginPageURL string) error {
	if err := guardAntiSpam(c.domain, c.scheme, &http.Response{StatusCode: status, Header: headers}, respBody); err != nil {
		return err
	}
	if msg := loginPageFailureMessage(respBody); msg != "" {
		return fmt.Errorf("encx: login page: %s", msg)
	}

	currentURL := ""
	if isRedirectStatus(status) {
		loc := strings.TrimSpace(headers.Get("Location"))
		if loc == "" {
			return fmt.Errorf("encx: login page redirect without Location (HTTP %d)", status)
		}
		if isLoginFailureRedirect(loc) {
			return fmt.Errorf("encx: login page rejected credentials")
		}
		u, err := resolveAgainstBase(c.baseURL(), loc)
		if err != nil {
			return fmt.Errorf("encx: resolve login redirect: %w", err)
		}
		currentURL = u
	} else if loginPageFieldsRE.Match(respBody) {
		return fmt.Errorf("encx: login page rejected credentials (still on login form)")
	}

	const maxHops = 6
	for hop := 0; hop < maxHops && currentURL != ""; hop++ {
		st, hdr, body, err := c.doRequestRaw(ctx, http.MethodGet, currentURL, nil)
		if err != nil {
			return fmt.Errorf("encx: follow login redirect: %w", err)
		}
		if err := guardAntiSpam(c.domain, c.scheme, &http.Response{StatusCode: st, Header: hdr}, body); err != nil {
			return err
		}
		if msg := loginPageFailureMessage(body); msg != "" {
			return fmt.Errorf("encx: login page: %s", msg)
		}
		if st == http.StatusOK && !loginPageFieldsRE.Match(body) {
			currentURL = ""
			break
		}
		if isRedirectStatus(st) {
			loc := strings.TrimSpace(hdr.Get("Location"))
			if loc == "" {
				break
			}
			if isLoginFailureRedirect(loc) {
				return fmt.Errorf("encx: login page rejected credentials")
			}
			u, err := resolveAgainstBase(c.baseURL(), loc)
			if err != nil {
				return fmt.Errorf("encx: resolve login redirect: %w", err)
			}
			currentURL = u
			continue
		}
		if loginPageFieldsRE.Match(body) {
			return fmt.Errorf("encx: login page rejected credentials (still on login form)")
		}
		break
	}

	retPath := returnPathFromLoginURL(loginPageURL)
	st, hdr, body, err := c.doRequestRaw(ctx, http.MethodGet, c.baseURL()+retPath, nil)
	if err != nil {
		return fmt.Errorf("encx: follow return after login: %w", err)
	}
	if err := guardAntiSpam(c.domain, c.scheme, &http.Response{StatusCode: st, Header: hdr}, body); err != nil {
		return err
	}
	if st >= 400 {
		return fmt.Errorf("encx: follow return after login: HTTP %d", st)
	}
	return nil
}

func returnPathFromLoginURL(loginPageURL string) string {
	u, err := url.Parse(loginPageURL)
	if err != nil {
		return "/"
	}
	ret := strings.TrimSpace(u.Query().Get("return"))
	if ret == "" {
		return "/"
	}
	if !strings.HasPrefix(ret, "/") {
		ret = "/" + ret
	}
	return ret
}

// LoginViaLoginPage signs in through the HTML Login.aspx form (used during anti-spam recovery).
func (c *Client) LoginViaLoginPage(ctx context.Context, loginPageURL, login, password string, opts ...LoginOptions) error {
	loginPageURL = strings.TrimSpace(loginPageURL)
	if loginPageURL == "" {
		loginPageURL = c.baseURL() + "/Login.aspx?return=/"
	}

	pageHTML, err := c.doGetRaw(ctx, loginPageURL)
	if err != nil {
		return fmt.Errorf("encx: load login page: %w", err)
	}
	form, err := parseLoginPageForm(loginPageURL, pageHTML)
	if err != nil {
		return err
	}
	if len(opts) > 0 && opts[0].Network > 0 {
		form.Network = strconv.Itoa(opts[0].Network)
	}

	payload := url.Values{}
	for k, vals := range form.Hidden {
		for _, v := range vals {
			payload.Add(k, v)
		}
	}
	payload.Set("Login", login)
	payload.Set("Password", password)
	payload.Set("EnButton1", "Sign In")
	payload.Set("ddlNetwork", form.Network)

	status, headers, respBody, err := c.doPostRaw(ctx, form.PostURL, payload)
	if err != nil {
		return fmt.Errorf("encx: submit login form: %w", err)
	}
	return c.completeLoginPageFlow(ctx, status, headers, respBody, loginPageURL)
}
