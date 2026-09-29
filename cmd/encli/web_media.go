package main

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
)

// httpMedia lets WebUI display Encounter images with the saved session. It
// also corrects legacy hosts that serve valid WebP bytes as text/plain.
func (h *webHub) httpMedia(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("url")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || !isFetchableScheme(u.Scheme) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "absolute http(s) image URL required"})
		return
	}

	client := fetchHTTPClient
	if domain := strings.TrimSpace(r.URL.Query().Get("domain")); domain != "" {
		encounter := h.registry.Get(domain, encOptsFromConfig(h.cfg))
		client = encounter.SessionHTTPClient(fetchHTTPClient)
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u.String(), nil)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid image URL"})
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "cannot load image"})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "image host returned an error"})
		return
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, encx.DefaultResourceMaxBytes+1))
	if err != nil || len(data) > encx.DefaultResourceMaxBytes {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "image exceeds size limit"})
		return
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "unsupported image"})
		return
	}
	var contentType string
	switch format {
	case "jpeg":
		contentType = "image/jpeg"
	case "png":
		contentType = "image/png"
	case "gif":
		contentType = "image/gif"
	case "webp":
		contentType = "image/webp"
	default:
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "unsupported image"})
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}
