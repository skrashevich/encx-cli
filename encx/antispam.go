package encx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// ErrAntiSpam indicates the server redirected to NotHumanRequest.aspx (rate-limit / bot check).
var ErrAntiSpam = errors.New("encx: anti-spam verification required")

// AntiSpamError carries the page URL the user must open to pass the check.
type AntiSpamError struct {
	URL string
}

func (e *AntiSpamError) Error() string {
	return fmt.Sprintf("encx: anti-spam verification required; open %s in a browser", e.URL)
}

func (e *AntiSpamError) Is(target error) bool {
	return target == ErrAntiSpam
}

// IsAntiSpam reports whether err is an anti-spam challenge (redirect to NotHumanRequest.aspx).
func IsAntiSpam(err error) bool {
	var ae *AntiSpamError
	return errors.As(err, &ae) || errors.Is(err, ErrAntiSpam)
}

// AntiSpamURLFromError returns the verification page URL when err is anti-spam, else "".
func AntiSpamURLFromError(err error) string {
	var ae *AntiSpamError
	if errors.As(err, &ae) {
		return ae.URL
	}
	return ""
}

// AntiSpamUserMessage returns a user-facing hint when err is anti-spam, else "".
func AntiSpamUserMessage(err error) string {
	if u := AntiSpamURLFromError(err); u != "" {
		return fmt.Sprintf("Сработала антиспам-защита Encounter. Пройдите проверку в браузере: %s", u)
	}
	if IsAntiSpam(err) {
		return "Сработала антиспам-защита Encounter. Пройдите проверку на сайте домена."
	}
	return ""
}

// AntiSpamPageURL builds the full NotHumanRequest.aspx URL for a domain.
// returnPath is optional (e.g. "/" or "/home/"); defaults to "/".
func AntiSpamPageURL(domain, scheme, returnPath string) string {
	if scheme == "" {
		scheme = "https"
	}
	if returnPath == "" {
		returnPath = "/"
	}
	if !strings.HasPrefix(returnPath, "/") {
		returnPath = "/" + returnPath
	}
	u := url.URL{
		Scheme: scheme,
		Host:   domain,
		Path:   "/NotHumanRequest.aspx",
	}
	q := u.Query()
	q.Set("return", returnPath)
	u.RawQuery = q.Encode()
	return u.String()
}

func newAntiSpamError(domain, scheme, location string) error {
	pageURL := resolveAntiSpamURL(domain, scheme, location)
	return &AntiSpamError{URL: pageURL}
}

func resolveAntiSpamURL(domain, scheme, location string) string {
	loc := strings.TrimSpace(location)
	if loc == "" {
		return AntiSpamPageURL(domain, scheme, "/")
	}
	if strings.HasPrefix(loc, "http://") || strings.HasPrefix(loc, "https://") {
		return loc
	}
	base := scheme + "://" + domain
	if !strings.HasPrefix(loc, "/") {
		loc = "/" + loc
	}
	return base + loc
}

func isNotHumanRequest(location string) bool {
	return strings.Contains(strings.ToLower(location), "nothumanrequest")
}

func isRedirectStatus(code int) bool {
	return code == http.StatusMovedPermanently ||
		code == http.StatusFound ||
		code == http.StatusSeeOther ||
		code == http.StatusTemporaryRedirect ||
		code == http.StatusPermanentRedirect
}

// guardAntiSpam returns an error when the server challenged with NotHumanRequest.aspx.
func guardAntiSpam(domain, scheme string, resp *http.Response, body []byte) error {
	if resp != nil {
		if isRedirectStatus(resp.StatusCode) {
			if isNotHumanRequest(resp.Header.Get("Location")) {
				return newAntiSpamError(domain, scheme, resp.Header.Get("Location"))
			}
		}
		if resp.Request != nil && resp.Request.URL != nil && isNotHumanRequest(resp.Request.URL.String()) {
			return newAntiSpamError(domain, scheme, resp.Request.URL.RequestURI())
		}
	}
	if len(body) > 0 && isNotHumanRequest(string(body)) {
		return newAntiSpamError(domain, scheme, "")
	}
	return nil
}

// readResponseBody reads the HTTP body and detects anti-spam redirects.
func (c *Client) readResponseBody(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := guardAntiSpam(c.domain, c.scheme, resp, body); err != nil {
		return nil, err
	}
	return body, nil
}

// ensureJSONBody rejects HTML responses before JSON decode (redirect bodies, legacy paths).
func (c *Client) ensureJSONBody(body []byte) error {
	if len(body) == 0 || body[0] != '<' {
		return nil
	}
	if isNotHumanRequest(string(body)) {
		return newAntiSpamError(c.domain, c.scheme, "")
	}
	return fmt.Errorf("encx: session expired or access denied (server returned HTML instead of JSON; try re-login)")
}

func (c *Client) decodeJSON(body []byte, v any, context string) error {
	if err := c.ensureJSONBody(body); err != nil {
		return err
	}
	if len(body) == 0 {
		return fmt.Errorf("encx: empty response (%s)", context)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("encx: decode %s: %w", context, err)
	}
	return nil
}
