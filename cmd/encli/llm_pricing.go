package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// llmPricing holds per-token costs fetched from the provider API.
type llmPricing struct {
	promptCostPerToken     float64
	completionCostPerToken float64
	isLocal                bool // local proxy = free subscription
	isSubscription         bool // ChatGPT plan: tokens are not billed per call
}

// llmCatalogModel is one entry of an OpenAI-compatible /models catalog, as
// OpenRouter and polza.ai publish it.
type llmCatalogModel struct {
	ID            string `json:"id"`
	ContextLength int    `json:"context_length"`
	TopProvider   *struct {
		ContextLength int `json:"context_length"`
	} `json:"top_provider"`
	Pricing *struct {
		Prompt     string `json:"prompt"`
		Completion string `json:"completion"`
	} `json:"pricing"`
}

// llmCatalogTTL keeps one catalog per base URL for the process: it runs to
// megabytes and is asked for at the start of every agent run.
const llmCatalogTTL = time.Hour

var llmCatalogCache = struct {
	sync.Mutex
	entries map[string]llmCatalogEntry
}{entries: map[string]llmCatalogEntry{}}

type llmCatalogEntry struct {
	models  []llmCatalogModel
	fetched time.Time
}

// llmCatalogFailureTTL remembers a catalog that could not be fetched, so a
// provider whose /models hangs costs one 10 s timeout, not one per message.
const llmCatalogFailureTTL = 2 * time.Minute

func (e llmCatalogEntry) ttl() time.Duration {
	if e.models == nil {
		return llmCatalogFailureTTL
	}
	return llmCatalogTTL
}

// llmCatalogPublishes reports the providers whose /models carries prices and
// context windows. A generic OpenAI-compatible endpoint lists bare IDs, so
// asking it only delays the run.
func llmCatalogPublishes(baseURL string) bool {
	return strings.Contains(baseURL, "openrouter.ai") || strings.Contains(baseURL, "polza.ai")
}

func fetchLLMCatalog(ctx context.Context, baseURL, apiKey string) []llmCatalogModel {
	key := strings.TrimRight(baseURL, "/")
	llmCatalogCache.Lock()
	entry, ok := llmCatalogCache.entries[key]
	llmCatalogCache.Unlock()
	if ok && time.Since(entry.fetched) < entry.ttl() {
		return entry.models
	}
	models := fetchLLMCatalogUncached(ctx, baseURL, apiKey)
	// A run cancelled mid-fetch says nothing about the provider.
	if ctx.Err() == nil {
		llmCatalogCache.Lock()
		llmCatalogCache.entries[key] = llmCatalogEntry{models: models, fetched: time.Now()}
		llmCatalogCache.Unlock()
	}
	return models
}

func fetchLLMCatalogUncached(ctx context.Context, baseURL, apiKey string) []llmCatalogModel {
	modelsURL := strings.TrimRight(baseURL, "/") + "/models"
	debugf("fetching model catalog from %s", modelsURL)

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, "GET", modelsURL, nil)
	if err != nil {
		debugf("catalog fetch: create request failed: %v", err)
		return nil
	}
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		debugf("catalog fetch: request failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		debugf("catalog fetch: HTTP %d", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	if err != nil {
		debugf("catalog fetch: read failed: %v", err)
		return nil
	}

	var result struct {
		Data []llmCatalogModel `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		debugf("catalog fetch: parse failed: %v", err)
		return nil
	}
	if result.Data == nil {
		result.Data = []llmCatalogModel{}
	}
	return result.Data
}

// findLLMCatalogModel matches the configured model, ignoring :free, :extended
// and similar routing suffixes.
func findLLMCatalogModel(models []llmCatalogModel, model string) (llmCatalogModel, bool) {
	baseModel := model
	if before, _, ok := strings.CutLast(model, ":"); ok && before != "" {
		baseModel = before
	}
	for _, m := range models {
		if m.ID == model || m.ID == baseModel {
			return m, true
		}
	}
	debugf("catalog: model %q not found in %d models", model, len(models))
	return llmCatalogModel{}, false
}

// contextTokens is the window a request to this model can count on. OpenRouter
// lists the largest window among its providers next to the one it routes to
// first (1.3M against 1M for DeepSeek V4 Flash), and polza.ai fills only the
// latter; the smaller of the two is the one no route refuses.
func (m llmCatalogModel) contextTokens() int {
	window := m.ContextLength
	if m.TopProvider != nil && m.TopProvider.ContextLength > 0 &&
		(window <= 0 || m.TopProvider.ContextLength < window) {
		window = m.TopProvider.ContextLength
	}
	return max(window, 0)
}

// fetchLLMContextWindow returns the model's context window in tokens, or 0
// when the provider does not publish one.
func fetchLLMContextWindow(ctx context.Context, baseURL, apiKey, model string) int {
	if !llmCatalogPublishes(baseURL) {
		return 0
	}
	m, ok := findLLMCatalogModel(fetchLLMCatalog(ctx, baseURL, apiKey), model)
	if !ok {
		return 0
	}
	debugf("context window resolved: model=%s tokens=%d", model, m.contextTokens())
	return m.contextTokens()
}

// fetchLLMPricing resolves pricing for the model at session start.
// Returns nil if cost cannot be determined.
func fetchLLMPricing(ctx context.Context, baseURL, apiKey, model string) *llmPricing {
	if strings.Contains(baseURL, "localhost") || strings.Contains(baseURL, "127.0.0.1") {
		return &llmPricing{isLocal: true}
	}

	// polza.ai prices in roubles per million; only OpenRouter's USD per token
	// is what computeLLMCost reports.
	if !strings.Contains(baseURL, "openrouter.ai") {
		return nil
	}

	m, ok := findLLMCatalogModel(fetchLLMCatalog(ctx, baseURL, apiKey), model)
	if !ok || m.Pricing == nil {
		return nil
	}
	promptCost, _ := strconv.ParseFloat(m.Pricing.Prompt, 64)
	completionCost, _ := strconv.ParseFloat(m.Pricing.Completion, 64)
	if promptCost == 0 && completionCost == 0 {
		return &llmPricing{} // free model
	}
	debugf("pricing resolved: model=%s prompt=%.10f/tok completion=%.10f/tok", model, promptCost, completionCost)
	return &llmPricing{
		promptCostPerToken:     promptCost,
		completionCostPerToken: completionCost,
	}
}

// computeLLMCost returns the total cost in USD given pricing and token counts.
func computeLLMCost(pricing *llmPricing, promptTokens, completionTokens int) float64 {
	if pricing == nil || pricing.isLocal || pricing.isSubscription {
		return 0
	}
	return float64(promptTokens)*pricing.promptCostPerToken +
		float64(completionTokens)*pricing.completionCostPerToken
}
