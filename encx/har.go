package encx

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

const harCreatorName = "encx-cli"
const harCreatorVersion = "1.0"
const harRedactedSecret = "[REDACTED]"

type harNameValue struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
}

type harRequest struct {
	Method      string         `json:"method"`
	URL         string         `json:"url"`
	HTTPVersion string         `json:"httpVersion"`
	Headers     []harNameValue `json:"headers"`
	QueryString []harNameValue `json:"queryString"`
	HeadersSize int            `json:"headersSize"`
	BodySize    int            `json:"bodySize"`
	PostData    *harPostData   `json:"postData,omitempty"`
}

type harContent struct {
	Size     int    `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
}

type harResponse struct {
	Status      int            `json:"status"`
	StatusText  string         `json:"statusText"`
	HTTPVersion string         `json:"httpVersion"`
	Headers     []harNameValue `json:"headers"`
	Cookies     []any          `json:"cookies"`
	Content     harContent     `json:"content"`
	RedirectURL string         `json:"redirectURL"`
	HeadersSize int            `json:"headersSize"`
	BodySize    int            `json:"bodySize"`
}

type harTimings struct {
	Blocked int `json:"blocked"`
	DNS     int `json:"dns"`
	Connect int `json:"connect"`
	Send    int `json:"send"`
	Wait    int `json:"wait"`
	Receive int `json:"receive"`
	SSL     int `json:"ssl"`
}

type harEntry struct {
	StartedDateTime time.Time  `json:"startedDateTime"`
	Time            float64    `json:"time"`
	Request         harRequest `json:"request"`
	Response        harResponse `json:"response"`
	Cache           struct{}   `json:"cache"`
	Timings         harTimings `json:"timings"`
	Comment         string     `json:"comment,omitempty"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harDocument struct {
	Log struct {
		Version string     `json:"version"`
		Creator harCreator `json:"creator"`
		Comment string     `json:"comment,omitempty"`
		Entries []harEntry `json:"entries"`
	} `json:"log"`
}

// HARRecorder captures HTTP traffic in HAR 1.2 format for debugging and mock servers.
type HARRecorder struct {
	mu      sync.Mutex
	enabled bool
	entries []harEntry
}

func NewHARRecorder() *HARRecorder {
	return &HARRecorder{}
}

func (r *HARRecorder) Enabled() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enabled
}

func (r *HARRecorder) SetEnabled(enabled bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.enabled = enabled
	r.mu.Unlock()
}

func (r *HARRecorder) Clear() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.entries = nil
	r.mu.Unlock()
}

func (r *HARRecorder) EntryCount() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

func (r *HARRecorder) ExportJSON() (string, error) {
	if r == nil {
		return emptyHARJSON(), nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	doc := harDocument{}
	doc.Log.Version = "1.2"
	doc.Log.Creator = harCreator{Name: harCreatorName, Version: harCreatorVersion}
	doc.Log.Comment = "Encounter API traffic captured by encx-cli"
	doc.Log.Entries = append([]harEntry(nil), r.entries...)
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func emptyHARJSON() string {
	doc := harDocument{}
	doc.Log.Version = "1.2"
	doc.Log.Creator = harCreator{Name: harCreatorName, Version: harCreatorVersion}
	doc.Log.Entries = []harEntry{}
	data, _ := json.MarshalIndent(doc, "", "  ")
	return string(data)
}

func (r *HARRecorder) wrap(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &harTransport{base: base, recorder: r}
}

type harTransport struct {
	base     http.RoundTripper
	recorder *HARRecorder
}

func (t *harTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.recorder == nil || !t.recorder.Enabled() {
		return t.base.RoundTrip(req)
	}

	started := time.Now().UTC()
	entry := buildHAREntry(req, started)

	resp, err := t.base.RoundTrip(req)
	durationMs := float64(time.Since(started).Microseconds()) / 1000.0
	entry.Time = durationMs
	entry.Timings.Wait = int(durationMs)

	if err != nil {
		entry.Comment = err.Error()
		t.recorder.append(entry)
		return nil, err
	}

	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		entry.Comment = readErr.Error()
		t.recorder.append(entry)
		return nil, readErr
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	fillHARResponse(&entry, resp, body)
	t.recorder.append(entry)
	return resp, nil
}

func (r *HARRecorder) append(entry harEntry) {
	r.mu.Lock()
	r.entries = append(r.entries, entry)
	r.mu.Unlock()
}

func buildHAREntry(req *http.Request, started time.Time) harEntry {
	entry := harEntry{StartedDateTime: started}

	entry.Request.Method = req.Method
	entry.Request.URL = req.URL.String()
	entry.Request.HTTPVersion = "HTTP/1.1"
	entry.Request.Headers = headerPairs(req.Header)
	entry.Request.QueryString = queryPairs(req.URL.RawQuery)
	entry.Request.HeadersSize = estimateHeaderSize(req)

	bodyBytes := readRequestBody(req)
	entry.Request.BodySize = len(bodyBytes)
	if len(bodyBytes) > 0 {
		mimeType := req.Header.Get("Content-Type")
		entry.Request.PostData = &harPostData{
			MimeType: mimeType,
			Text:     redactSensitiveHARBody(mimeType, bodyBytes),
		}
		if entry.Request.PostData.MimeType == "" {
			entry.Request.PostData.MimeType = "application/octet-stream"
		}
	}

	return entry
}

func fillHARResponse(entry *harEntry, resp *http.Response, body []byte) {
	entry.Response.Status = resp.StatusCode
	entry.Response.StatusText = http.StatusText(resp.StatusCode)
	entry.Response.HTTPVersion = resp.Proto
	if entry.Response.HTTPVersion == "" {
		entry.Response.HTTPVersion = "HTTP/1.1"
	}
	entry.Response.Headers = headerPairs(resp.Header)
	entry.Response.Cookies = []any{}
	entry.Response.RedirectURL = resp.Header.Get("Location")
	entry.Response.HeadersSize = estimateResponseHeaderSize(resp)
	entry.Response.BodySize = len(body)
	entry.Response.Content = harContent{
		Size:     len(body),
		MimeType: resp.Header.Get("Content-Type"),
		Text:     string(body),
	}
	if entry.Response.Content.MimeType == "" {
		entry.Response.Content.MimeType = "application/octet-stream"
	}
}

func readRequestBody(req *http.Request) []byte {
	if req.Body == nil {
		return nil
	}
	if getBody, err := req.GetBody(); err == nil && getBody != nil {
		defer getBody.Close()
		body, err := io.ReadAll(getBody)
		if err == nil {
			return body
		}
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

func headerPairs(header http.Header) []harNameValue {
	if len(header) == 0 {
		return []harNameValue{}
	}
	pairs := make([]harNameValue, 0, len(header))
	for name, values := range header {
		for _, value := range values {
			pairs = append(pairs, harNameValue{Name: name, Value: value})
		}
	}
	return pairs
}

func queryPairs(rawQuery string) []harNameValue {
	if rawQuery == "" {
		return []harNameValue{}
	}
	pairs := make([]harNameValue, 0)
	for _, part := range strings.Split(rawQuery, "&") {
		if part == "" {
			continue
		}
		name, value, _ := strings.Cut(part, "=")
		if isSensitiveHARField(name) {
			value = harRedactedSecret
		}
		pairs = append(pairs, harNameValue{Name: name, Value: value})
	}
	return pairs
}

func redactSensitiveHARBody(mimeType string, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	switch mediaType {
	case "application/json", "text/json":
		return redactJSONSecrets(body)
	case "application/x-www-form-urlencoded":
		return redactFormSecrets(body)
	default:
		return string(body)
	}
}

func redactJSONSecrets(body []byte) string {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return string(body)
	}
	redactJSONValue(value)
	out, err := json.Marshal(value)
	if err != nil {
		return string(body)
	}
	return string(out)
}

func redactJSONValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			if isSensitiveHARField(key) {
				typed[key] = harRedactedSecret
				continue
			}
			redactJSONValue(item)
		}
	case []any:
		for _, item := range typed {
			redactJSONValue(item)
		}
	}
}

func redactFormSecrets(body []byte) string {
	values, err := url.ParseQuery(string(body))
	if err != nil {
		return string(body)
	}
	for key := range values {
		if isSensitiveHARField(key) {
			values[key] = []string{harRedactedSecret}
		}
	}
	return values.Encode()
}

func isSensitiveHARField(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "password", "passwd", "pwd":
		return true
	default:
		return false
	}
}

func estimateHeaderSize(req *http.Request) int {
	data, err := httputil.DumpRequestOut(req, false)
	if err != nil {
		return -1
	}
	return len(data)
}

func estimateResponseHeaderSize(resp *http.Response) int {
	data, err := httputil.DumpResponse(resp, false)
	if err != nil {
		return -1
	}
	return len(data)
}
