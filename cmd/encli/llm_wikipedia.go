package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const wikipediaUserAgent = "encx-cli/1.0 (https://github.com/skrashevich/encx-cli; LLM agent)"

var wikipediaHTTPClient = &http.Client{Timeout: 15 * time.Second}

func normalizeWikiLang(lang string) string {
	lang = strings.TrimSpace(strings.ToLower(lang))
	if lang == "" {
		lang = "ru"
	}
	if len(lang) > 12 {
		lang = lang[:12]
	}
	return lang
}

var wikipediaAPIBaseURL = func(lang string) string {
	return fmt.Sprintf("https://%s.wikipedia.org/w/api.php", normalizeWikiLang(lang))
}

func wikipediaFetch(ctx context.Context, lang string, params url.Values) ([]byte, error) {
	if params == nil {
		params = url.Values{}
	}
	params.Set("format", "json")
	reqURL := wikipediaAPIBaseURL(lang) + "?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", wikipediaUserAgent)

	resp, err := wikipediaHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Wikipedia API HTTP %d: %s", resp.StatusCode, summarizeDebugText(string(body), 200))
	}
	return body, nil
}

func toolWikipediaSearch(ctx context.Context, query, lang string, limit int) {
	query = strings.TrimSpace(query)
	if query == "" {
		fatal("query is required")
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	lang = normalizeWikiLang(lang)

	params := url.Values{}
	params.Set("action", "query")
	params.Set("list", "search")
	params.Set("srsearch", query)
	params.Set("srlimit", fmt.Sprintf("%d", limit))
	params.Set("utf8", "1")

	body, err := wikipediaFetch(ctx, lang, params)
	if err != nil {
		fatal("%v", err)
	}

	var payload struct {
		Query struct {
			Search []struct {
				PageID  int    `json:"pageid"`
				Title   string `json:"title"`
				Snippet string `json:"snippet"`
			} `json:"search"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		fatal("failed to parse Wikipedia search response: %v", err)
	}

	results := make([]map[string]any, 0, len(payload.Query.Search))
	for _, item := range payload.Query.Search {
		results = append(results, map[string]any{
			"pageid":  item.PageID,
			"title":   item.Title,
			"snippet": stripHTML(item.Snippet),
			"url":     wikipediaPageURL(lang, item.Title),
		})
	}

	outputJSON(map[string]any{
		"lang":    lang,
		"query":   query,
		"count":   len(results),
		"results": results,
	})
}

func toolWikipediaArticle(ctx context.Context, title, lang string) {
	title = strings.TrimSpace(title)
	if title == "" {
		fatal("title is required")
	}
	lang = normalizeWikiLang(lang)

	params := url.Values{}
	params.Set("action", "query")
	params.Set("prop", "extracts|info")
	params.Set("exintro", "1")
	params.Set("explaintext", "1")
	params.Set("inprop", "url")
	params.Set("redirects", "1")
	params.Set("titles", title)

	body, err := wikipediaFetch(ctx, lang, params)
	if err != nil {
		fatal("%v", err)
	}

	var payload struct {
		Query struct {
			Pages map[string]struct {
				PageID    int    `json:"pageid"`
				Title     string `json:"title"`
				Extract   string `json:"extract"`
				FullURL   string `json:"fullurl"`
				Missing   string `json:"missing"`
				Invalid   string `json:"invalid"`
				Redirect  string `json:"redir"`
				Canonical string `json:"canonicalurl"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		fatal("failed to parse Wikipedia article response: %v", err)
	}

	for _, page := range payload.Query.Pages {
		if page.Missing != "" || page.Invalid != "" {
			fatal("Wikipedia article not found: %q", title)
		}
		pageURL := page.FullURL
		if pageURL == "" {
			pageURL = wikipediaPageURL(lang, page.Title)
		}
		outputJSON(map[string]any{
			"lang":     lang,
			"title":    page.Title,
			"pageid":   page.PageID,
			"url":      pageURL,
			"extract":  summarizeDebugText(page.Extract, 12000),
			"redirect": page.Redirect != "",
		})
		return
	}
	fatal("Wikipedia article not found: %q", title)
}

func wikipediaPageURL(lang, title string) string {
	escaped := url.PathEscape(strings.ReplaceAll(title, " ", "_"))
	return fmt.Sprintf("https://%s.wikipedia.org/wiki/%s", normalizeWikiLang(lang), escaped)
}
