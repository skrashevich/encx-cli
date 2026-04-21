package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// llmPricing holds per-token costs fetched from the provider API.
type llmPricing struct {
	promptCostPerToken     float64
	completionCostPerToken float64
	isLocal                bool // local proxy = free subscription
}

// fetchLLMPricing resolves pricing for the model at session start.
// Returns nil if cost cannot be determined.
func fetchLLMPricing(ctx context.Context, baseURL, apiKey, model string) *llmPricing {
	if strings.Contains(baseURL, "localhost") || strings.Contains(baseURL, "127.0.0.1") {
		return &llmPricing{isLocal: true}
	}

	if !strings.Contains(baseURL, "openrouter.ai") {
		return nil
	}

	modelsURL := strings.TrimRight(baseURL, "/") + "/models"
	debugf("fetching model pricing from %s for model=%s", modelsURL, model)

	reqCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, "GET", modelsURL, nil)
	if err != nil {
		debugf("pricing fetch: create request failed: %v", err)
		return nil
	}
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		debugf("pricing fetch: request failed: %v", err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		debugf("pricing fetch: HTTP %d", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
	if err != nil {
		debugf("pricing fetch: read failed: %v", err)
		return nil
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			Pricing *struct {
				Prompt     string `json:"prompt"`
				Completion string `json:"completion"`
			} `json:"pricing"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		debugf("pricing fetch: parse failed: %v", err)
		return nil
	}

	// Strip :free, :extended etc. suffixes for matching.
	baseModel := model
	if idx := strings.LastIndex(model, ":"); idx > 0 {
		baseModel = model[:idx]
	}

	for _, m := range result.Data {
		if m.ID == model || m.ID == baseModel {
			if m.Pricing == nil {
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
	}

	debugf("pricing fetch: model %q not found in %d models", model, len(result.Data))
	return nil
}

// computeLLMCost returns the total cost in USD given pricing and token counts.
func computeLLMCost(pricing *llmPricing, promptTokens, completionTokens int) float64 {
	if pricing == nil || pricing.isLocal {
		return 0
	}
	return float64(promptTokens)*pricing.promptCostPerToken +
		float64(completionTokens)*pricing.completionCostPerToken
}
