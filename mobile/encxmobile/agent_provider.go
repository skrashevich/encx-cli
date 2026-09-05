package encxmobile

import (
	"fmt"
	"strings"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
)

// Providers the app can actually reach with an API key. All three speak the
// OpenAI-compatible chat API and differ only in the endpoint.
const (
	providerOpenAI    = "openai"
	providerAnthropic = "anthropic"
	providerPolza     = "polza"
)

// defaultAPIBases is the endpoint used when the host does not override it.
var defaultAPIBases = map[string]string{
	providerOpenAI:    "https://api.openai.com/v1",
	providerAnthropic: "https://api.anthropic.com/v1",
	providerPolza:     "https://polza.ai/api/v1",
}

// newHTTPProvider builds the LLM provider for an API-key login.
//
// PicoClaw's own CreateProviderFromConfig switches over every backend it knows,
// which drags Azure identity, the AWS SDK and the Copilot SDK into the app for
// providers it can never reach — the app offers exactly these three plus a
// ChatGPT login. Constructing the HTTP provider directly keeps roughly 24 MB of
// unreachable SDKs out of the framework.
//
// A custom api_base is still honoured, so any OpenAI-compatible gateway works.
// It must serve /chat/completions: that is the path the provider builds.
func newHTTPProvider(cfg agentConfig) (providers.LLMProvider, error) {
	protocol := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if protocol == "" {
		protocol = providerOpenAI
	}

	apiBase := strings.TrimSpace(cfg.APIBase)
	if apiBase == "" {
		base, known := defaultAPIBases[protocol]
		if !known {
			return nil, fmt.Errorf(
				"encxmobile: provider %q is not supported; use %s, %s, %s, or set an api_base",
				protocol, providerOpenAI, providerAnthropic, providerPolza,
			)
		}
		apiBase = base
	}

	apiKey := cfg.modelConfig().APIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("encxmobile: provider %q needs an API key", protocol)
	}

	provider := providers.NewHTTPProviderWithMaxTokensFieldAndRequestTimeout(
		apiKey,
		strings.TrimRight(apiBase, "/"),
		"", // no proxy
		"", // let the provider pick max_tokens vs max_completion_tokens per model
		userAgent(),
		cfg.RequestTimeoutSeconds,
		aggregatorModelOverride(cfg.Model),
		nil,
	)
	// The provider name drives host-specific quirks such as per-host headers.
	provider.SetProviderName(protocol)
	return provider, nil
}

// aggregatorModelOverride pins the model name for aggregators that address
// models as "vendor/model".
//
// PicoClaw strips that prefix whenever it names a provider it knows — "google",
// "deepseek", "mistral" and a dozen more — which is right for a direct endpoint
// and wrong for an aggregator, where "google/gemini-3-flash-preview" is the whole
// name. Only OpenRouter is exempted by hostname, so every other aggregator loses
// the prefix and gets a 404 for a model the account can actually reach.
//
// extra_body is merged into the request body last, so putting the model there
// restores the name the player typed. Models without a prefix need no override:
// there is nothing to strip.
func aggregatorModelOverride(model string) map[string]any {
	trimmed := strings.TrimSpace(model)
	if !strings.Contains(trimmed, "/") {
		return nil
	}
	return map[string]any{"model": trimmed}
}

func userAgent() string {
	return "enkapp/" + config.Version
}
