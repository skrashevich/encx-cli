package encxmobile

import "github.com/skrashevich/encx-cli/encx"

// NewClientWithAPIOptions configures an explicit new-engine API host before
// any requests or engine detection. Empty apiBaseURL preserves normal detection.
// The caller must trust this host: it receives authentication credentials.
func NewClientWithAPIOptions(domain string, insecureTLS, useHTTP bool, timeoutSeconds int64, lang, apiBaseURL string) *EncClient {
	client := NewClientWithOptions(domain, insecureTLS, useHTTP, timeoutSeconds, lang)
	encx.WithAPIBaseURL(apiBaseURL)(client.client)
	return client
}
