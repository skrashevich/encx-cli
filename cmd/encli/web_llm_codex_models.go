package main

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type codexModelInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type codexModelCatalog struct {
	Models       []codexModelInfo `json:"models"`
	DefaultModel string           `json:"default_model"`
}

func (h *webHub) httpCodexModels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	cred, err := loadCodexCredential()
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Сначала войдите через ChatGPT."})
		return
	}
	token, account, err := sharedCodexTokenStore(codexAuthFile(), cred).tokenSource()()
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Не удалось обновить подписку. Повторите вход через ChatGPT."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	catalog, err := fetchCodexModels(ctx, http.DefaultClient, "https://chatgpt.com/backend-api/codex/models?client_version=0.149.0", token, account)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, catalog)
}

// Use the subscription catalog, not the API-key /v1/models endpoint.
// Keep credentials on the server and never include upstream error bodies.
func fetchCodexModels(ctx context.Context, client *http.Client, endpoint, token, account string) (codexModelCatalog, error) {
	var catalog codexModelCatalog
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return catalog, errors.New("Некорректный адрес каталога Codex.")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if account != "" {
		req.Header.Set("ChatGPT-Account-ID", account)
	}
	req.Header.Set("originator", "codex_cli_rs")
	bounded := *client
	bounded.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := bounded.Do(req)
	if err != nil {
		return catalog, errors.New("Не удалось загрузить модели Codex. Проверьте соединение и повторите.")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return catalog, fmt.Errorf("Каталог Codex недоступен (HTTP %d). Повторите вход или обновите список.", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return catalog, errors.New("Не удалось прочитать каталог Codex.")
	}
	var payload struct {
		Models []struct {
			Slug        string `json:"slug"`
			DisplayName string `json:"display_name"`
			Visibility  string `json:"visibility"`
			Priority    int    `json:"priority"`
		} `json:"models"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return catalog, errors.New("Codex вернул некорректный каталог моделей.")
	}
	sort.SliceStable(payload.Models, func(i, j int) bool { return payload.Models[i].Priority < payload.Models[j].Priority })
	seen := map[string]bool{}
	for _, m := range payload.Models {
		if m.Visibility != "list" || seen[m.Slug] || strings.Contains(m.Slug, "/") || !(strings.HasPrefix(m.Slug, "gpt-") || strings.HasPrefix(m.Slug, "o3") || strings.HasPrefix(m.Slug, "o4")) {
			continue
		}
		seen[m.Slug] = true
		name := m.DisplayName
		if name == "" {
			name = m.Slug
		}
		catalog.Models = append(catalog.Models, codexModelInfo{ID: m.Slug, Name: name})
	}
	if len(catalog.Models) == 0 {
		return catalog, errors.New("Codex не вернул доступных моделей.")
	}
	catalog.DefaultModel = defaultCodexModel
	if !seen[catalog.DefaultModel] {
		catalog.DefaultModel = catalog.Models[0].ID
	}
	return catalog, nil
}
