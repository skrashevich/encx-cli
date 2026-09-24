package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/skrashevich/encx-cli/encx"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

const (
	// fetchBodyHardLimit caps what is read off the wire regardless of max_bytes:
	// max_bytes limits the text handed to the model, but HTML extraction happens
	// before that trim, so without a separate ceiling a multi-gigabyte response
	// would be parsed into memory first and the trim would never be reached.
	fetchBodyHardLimit = 8 * 1024 * 1024
	fetchMaxRedirects  = 10
)

// fetchAllowPrivateHosts disarms the SSRF guard below so tests can point
// fetch_url at an httptest server, which always listens on 127.0.0.1.
//
// It is paired with testing.Testing() at the check itself, so in a shipped
// binary the escape hatch is dead code rather than one assignment away from
// turning the guard off. "Nothing outside tests sets it" is a convention; this
// is the guarantee.
var fetchAllowPrivateHosts bool

// fetchDialControl rejects addresses that belong to the machine or its network
// rather than the public internet.
//
// The check lives here, in the dialer, because it is the only place where the
// IP is already known: a hostname such as metadata.internal or an attacker's
// domain with an A record of 127.0.0.1 looks perfectly public until DNS
// resolves it. Validating the URL's host would pass both; validating the
// dialled address refuses them, and does so again on every redirect hop.
func fetchDialControl(_, address string, _ syscall.RawConn) error {
	if fetchAllowPrivateHosts && testing.Testing() {
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("refusing to connect to %q: %v", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("refusing to connect to %q: not an IP address", address)
	}
	// IsPrivate covers both RFC 1918 and the IPv6 unique-local range fc00::/7.
	// IsLoopback catches ::ffff:127.0.0.1 too, because ParseIP keeps the
	// v4-mapped form and IsLoopback consults To4() first.
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || isReservedIP(ip) {
		return fmt.Errorf("refusing to fetch from non-public address %s", ip)
	}
	return nil
}

// fetchReservedNets are the ranges the standard library's predicates leave out.
// None of them route on the public internet, so a page that resolves into one
// is either a misconfiguration or someone steering the agent at infrastructure.
var fetchReservedNets = func() []*net.IPNet {
	var nets []*net.IPNet
	for _, cidr := range []string{
		"0.0.0.0/8",     // "this network" — some stacks route 0.x.x.x to the host
		"100.64.0.0/10", // carrier-grade NAT, and Alibaba's metadata at 100.100.100.200
		"192.0.0.0/24",  // IETF protocol assignments
		"198.18.0.0/15", // benchmarking
		"192.0.2.0/24",  // documentation
		"64:ff9b::/96",  // NAT64 — an IPv6 wrapper around any IPv4 address, private ones included
		// IPv4-compatible IPv6 (::a.b.c.d). Deprecated, and Go's To4() does not
		// unwrap it the way it unwraps the ::ffff: form, so ::127.0.0.1 reads as
		// an ordinary global address to every predicate above.
		"::/96",
	} {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}()

func isReservedIP(ip net.IP) bool {
	for _, n := range fetchReservedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

var fetchHTTPClient = &http.Client{
	Timeout: 20 * time.Second,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			Control:   fetchDialControl,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		// One fetch is one request, so a pooled connection would only hold a
		// socket open to an arbitrary host for nothing. Not a security control:
		// the pool is keyed by host and port, so a reused connection goes to an
		// address Control already approved.
		DisableKeepAlives: true,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= fetchMaxRedirects {
			return fmt.Errorf("stopped after %d redirects", fetchMaxRedirects)
		}
		if !isFetchableScheme(req.URL.Scheme) {
			return fmt.Errorf("refusing to follow redirect to %s://", req.URL.Scheme)
		}
		return nil
	},
}

func isFetchableScheme(scheme string) bool {
	switch strings.ToLower(scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}

// toolFetchURL downloads a web page and returns it as plain text, so the
// agent can read a link the user pasted instead of claiming it cannot.
func toolFetchURL(ctx context.Context, encounter *encx.Client, rawURL string, maxBytes, offset int) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		fatal("url is required")
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		fatal("%q is not a valid URL: %v", rawURL, err)
	}
	if !isFetchableScheme(parsed.Scheme) {
		fatal("cannot fetch %q: only http and https URLs are supported", rawURL)
	}
	if parsed.Host == "" {
		fatal("cannot fetch %q: URL has no host", rawURL)
	}

	if maxBytes <= 0 {
		maxBytes = defaultLocalReadMaxBytes
	}
	// Anything past this is thrown away downstream by truncateToolContent, so a
	// model asking for half a megabyte pays for bytes it can never receive — and
	// then, seeing a short answer, asks again. Cap it here and say so honestly.
	if maxBytes > maxToolContentForLLM {
		maxBytes = maxToolContentForLLM
	}
	if offset < 0 {
		offset = 0
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		fatal("%v", err)
	}
	req.Header.Set("User-Agent", agentUserAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/json;q=0.9,*/*;q=0.1")

	client := fetchHTTPClient
	if encounter != nil {
		client = encounter.SessionHTTPClient(client)
	}
	resp, err := client.Do(req)
	if err != nil {
		fatal("failed to fetch %s: %v", rawURL, err)
	}
	defer resp.Body.Close()

	// One byte past the cap, so a document cut off at the wire can be told apart
	// from one that merely ended there.
	body, err := io.ReadAll(io.LimitReader(resp.Body, fetchBodyHardLimit+1))
	if err != nil {
		fatal("failed to read %s: %v", rawURL, err)
	}
	cutAtWire := len(body) > fetchBodyHardLimit
	if cutAtWire {
		body = body[:fetchBodyHardLimit]
	}

	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		fatal("HTTP %d from %s: %s", resp.StatusCode, finalURL, summarizeDebugText(string(body), 200))
	}

	contentType := resp.Header.Get("Content-Type")
	mediaType := parseFetchMediaType(contentType, body)

	var text, title string
	switch {
	case mediaType == "text/html" || mediaType == "application/xhtml+xml":
		text, title = extractHTMLText(decodeToUTF8(body, contentType))
	case mediaType == "application/json" || strings.HasPrefix(mediaType, "text/"):
		text = string(decodeToUTF8(body, contentType))
	default:
		fatal("cannot read %s as text: content type %q; only HTML, text and JSON are supported", finalURL, mediaType)
	}

	if offset > 0 {
		if offset >= len(text) {
			text = ""
		} else {
			text = text[alignToRuneStart(text, offset):]
		}
	}
	truncated := len(text) > maxBytes || cutAtWire
	content := truncateUTF8(text, maxBytes)

	result := map[string]any{
		"url":          finalURL,
		"status":       resp.StatusCode,
		"content_type": mediaType,
		"read":         len(content),
		"truncated":    truncated,
		"content":      content,
	}
	if title != "" {
		result["title"] = title
	}
	if offset > 0 {
		result["offset"] = offset
	}
	// Say how to continue, in the result itself. A model handed a bare
	// "truncated": true tends to call the same URL again with a different
	// max_bytes and get another prefix; naming the next offset is what turns a
	// second call into progress.
	if len(text) > len(content) {
		result["next_offset"] = offset + len(content)
		result["hint"] = fmt.Sprintf(
			"Showing bytes %d..%d. Call fetch_url again with offset=%d for the next part; "+
				"a larger max_bytes will NOT return more.",
			offset, offset+len(content), offset+len(content))
	} else if cutAtWire {
		result["hint"] = "The document was cut off at the transfer limit; the tail could not be read."
	}
	outputJSON(result)
}

// decodeToUTF8 re-encodes the body from whatever the server declared — or, for
// HTML, whatever the document declares in its own <meta> — into UTF-8.
//
// Half the Russian question-pack mirrors are still windows-1251. Handing those
// bytes to the model as if they were UTF-8 turns a whole pack into replacement
// characters, and the model has no way to tell that from a badly written page.
// A charset that cannot be resolved leaves the bytes alone rather than failing
// the fetch: an unreadable page is still better than no page.
func decodeToUTF8(body []byte, contentType string) []byte {
	reader, err := charset.NewReader(bytes.NewReader(body), contentType)
	if err != nil {
		return body
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return body
	}
	return decoded
}

// parseFetchMediaType strips the charset and other parameters. A server that
// sends no Content-Type at all is common enough on static hosts that guessing
// from the bytes beats refusing the page outright.
func parseFetchMediaType(header string, body []byte) string {
	if strings.TrimSpace(header) == "" {
		header = http.DetectContentType(body)
	}
	mediaType, _, err := mime.ParseMediaType(header)
	if err != nil {
		mediaType = strings.TrimSpace(strings.ToLower(header))
		if idx := strings.IndexByte(mediaType, ';'); idx >= 0 {
			mediaType = strings.TrimSpace(mediaType[:idx])
		}
	}
	return mediaType
}

// alignToRuneStart moves i forward to the next rune boundary, so paging with
// offset never starts in the middle of a Cyrillic character.
func alignToRuneStart(s string, i int) int {
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return i
}

// fetchBlockTags are the elements whose boundaries become line breaks. Without
// them a page arrives as one unbroken run of words: the model can still read it,
// but headings, list items and table rows stop being distinguishable.
var fetchBlockTags = map[string]bool{
	"address": true, "article": true, "aside": true, "blockquote": true, "br": true,
	"dd": true, "div": true, "dl": true, "dt": true, "fieldset": true, "figure": true,
	"footer": true, "form": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "header": true, "hr": true, "li": true, "main": true,
	"nav": true, "ol": true, "p": true, "pre": true, "section": true, "table": true,
	"tbody": true, "tfoot": true, "thead": true, "tr": true, "ul": true,
}

// extractHTMLText renders a document the way a reader sees it: script and style
// bodies are dropped rather than pasted in as text (which is what the naive tag
// stripper used elsewhere does), and block boundaries become newlines.
func extractHTMLText(body []byte) (text, title string) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		fatal("failed to parse HTML: %v", err)
	}

	var buf strings.Builder
	preDepth := 0
	pendingSpace := false
	atLineStart := true

	// writeCollapsed folds whitespace the way a browser lays out text — but
	// across text nodes, not within each one. In `<b>Дата:</b> 11.09.2020` the
	// space belongs to the node AFTER the tag, so collapsing every node on its
	// own dropped it and the label arrived glued to its value: "Дата:11.09.2020".
	writeCollapsed := func(s string) {
		for _, r := range s {
			if isHTMLSpace(r) {
				pendingSpace = true
				continue
			}
			if pendingSpace {
				pendingSpace = false
				if !atLineStart {
					buf.WriteByte(' ')
				}
			}
			buf.WriteRune(r)
			atLineStart = false
		}
	}
	// A break swallows the space around it: a line never starts with one, and a
	// trailing one would only be trimmed later anyway.
	//
	// Block tags break both on entry and on exit, so nesting them — and every
	// <li>, <tr> and <br> — used to emit two newlines where one was meant, and
	// the document reached the model double-spaced. Breaking only when the line
	// has something on it collapses that at the source, where a 64 KiB budget
	// makes the difference worth having.
	newline := func() {
		if atLineStart {
			return
		}
		buf.WriteByte('\n')
		pendingSpace = false
		atLineStart = true
	}

	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			if preDepth > 0 {
				buf.WriteString(n.Data)
				pendingSpace = false
				atLineStart = strings.HasSuffix(n.Data, "\n")
			} else {
				writeCollapsed(n.Data)
			}
			return
		case html.ElementNode:
			switch n.Data {
			case "script", "style", "noscript", "template", "svg":
				return
			// Not redundant with the head case below, though it looks it: once
			// html.Parse has opened <body>, a <title> written there STAYS there
			// rather than being moved into the head. Deleting this case loses the
			// title of every page that spells it that way.
			case "title":
				if title == "" {
					title = strings.TrimSpace(collapseHTMLSpaces(nodeText(n)))
				}
				return
			case "head":
				// Nothing in the head is readable content, but the title lives
				// there and is the best one-line label for the page.
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && c.Data == "title" && title == "" {
						title = strings.TrimSpace(collapseHTMLSpaces(nodeText(c)))
					}
				}
				return
			case "pre":
				preDepth++
				defer func() { preDepth-- }()
			}
			if fetchBlockTags[n.Data] {
				newline()
			}
		}

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}

		if n.Type == html.ElementNode {
			if fetchBlockTags[n.Data] {
				newline()
			} else if n.Data == "td" || n.Data == "th" {
				buf.WriteByte('\t')
				pendingSpace = false
				atLineStart = false
			}
		}
	}
	walk(doc)

	return normalizeExtractedText(buf.String()), title
}

func nodeText(n *html.Node) string {
	var buf strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.TextNode {
			buf.WriteString(node.Data)
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return buf.String()
}

func isHTMLSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\f', '\v', '\u00a0':
		return true
	default:
		return false
	}
}

// collapseHTMLSpaces folds every run of whitespace into a single space, the way
// a browser lays out text: source indentation must not reach the model as
// structure it might read meaning into. It is for standalone strings such as a
// <title>; running text goes through writeCollapsed, which carries the
// whitespace state across node boundaries.
func collapseHTMLSpaces(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	space := false
	for _, r := range s {
		if isHTMLSpace(r) {
			space = true
			continue
		}
		if space && buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		space = false
		buf.WriteRune(r)
	}
	if space && buf.Len() > 0 {
		buf.WriteByte(' ')
	}
	return buf.String()
}

// normalizeExtractedText drops trailing spaces and collapses the runs of blank
// lines that nested block elements inevitably produce.
func normalizeExtractedText(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.TrimRight(strings.TrimLeft(line, " "), " \t")
		if line == "" {
			blank++
			if blank > 1 || len(out) == 0 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
