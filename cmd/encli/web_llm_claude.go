package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type claudeModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type claudeModelCatalog struct {
	Models       []claudeModelInfo `json:"models"`
	DefaultModel string            `json:"default_model"`
}

// Only the server contacts Anthropic; the subscription token never reaches
// the browser. Generic provider endpoint/key settings do not apply here.
func (h *webHub) httpClaudeModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cred, err := loadClaudeCredential()
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Сначала сохраните токен подписки Claude."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	catalog, err := fetchClaudeModels(ctx, http.DefaultClient, "https://api.anthropic.com/v1/models", cred.AccessToken)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, catalog)
}

func fetchClaudeModels(ctx context.Context, client *http.Client, endpoint, token string) (claudeModelCatalog, error) {
	var catalog claudeModelCatalog
	// Do not forward a bearer token to a redirect target, including same-host
	// redirects. Copy the client to leave other provider requests unchanged.
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	seenModels, seenCursors := map[string]bool{}, map[string]bool{}
	cursor := ""
	for range 10 {
		u, err := url.Parse(endpoint)
		if err != nil {
			return catalog, errors.New("Некорректный адрес каталога Claude.")
		}
		query := u.Query()
		query.Set("limit", "1000")
		if cursor != "" {
			query.Set("after_id", cursor)
		}
		u.RawQuery = query.Encode()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return catalog, errors.New("Не удалось создать запрос каталога Claude.")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("anthropic-beta", "oauth-2025-04-20")
		res, err := bounded.Do(req)
		if err != nil {
			return catalog, errors.New("Не удалось загрузить модели Claude. Проверьте соединение и повторите.")
		}
		body, readErr := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			// Upstream error bodies can contain credential-bearing diagnostics.
			return catalog, fmt.Errorf("Каталог Claude недоступен (HTTP %d). Проверьте токен и повторите.", res.StatusCode)
		}
		if readErr != nil || len(body) > 2<<20 {
			return catalog, errors.New("Не удалось прочитать каталог Claude.")
		}
		var page struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
				Lifecycle   string `json:"lifecycle"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if json.Unmarshal(body, &page) != nil {
			return catalog, errors.New("Claude вернул некорректный каталог моделей.")
		}
		for _, model := range page.Data {
			if !strings.HasPrefix(model.ID, "claude-") || model.Lifecycle == "retired" || seenModels[model.ID] {
				continue
			}
			seenModels[model.ID] = true
			name := model.DisplayName
			if name == "" {
				name = model.ID
			}
			catalog.Models = append(catalog.Models, claudeModelInfo{ID: model.ID, Name: name})
		}
		if !page.HasMore {
			if len(catalog.Models) == 0 {
				return catalog, errors.New("Claude не вернул доступных моделей.")
			}
			catalog.DefaultModel = defaultClaudeModel
			if !seenModels[catalog.DefaultModel] {
				catalog.DefaultModel = catalog.Models[0].ID
			}
			return catalog, nil
		}
		if page.LastID == "" || seenCursors[page.LastID] {
			return catalog, errors.New("Claude повторил страницу каталога моделей.")
		}
		cursor = page.LastID
		seenCursors[cursor] = true
	}
	return catalog, errors.New("Каталог Claude содержит слишком много страниц.")
}
