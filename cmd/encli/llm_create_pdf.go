package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
)

const maxCreatedPDFContent = 1 << 20

var pdfArtifactNameRE = regexp.MustCompile(`^[a-f0-9]{32}\.pdf$`)
var webChatIDRE = regexp.MustCompile(`^[a-f0-9]{16}$`)

func webArtifactsRoot() string {
	return filepath.Join(sessionDir(), "web", "artifacts")
}

func toolCreatePDF(ctx context.Context, cfg *config, client *encx.Client, session *llmSession, title, content string) {
	if session == nil || !webChatIDRE.MatchString(session.webChatID) {
		fatal("create_pdf is available only in a WebUI chat")
	}
	title = strings.TrimSpace(title)
	if title == "" || strings.TrimSpace(content) == "" {
		fatal("title and content are required")
	}
	if len(title) > 200 || len(content) > maxCreatedPDFContent {
		fatal("PDF title or content is too long")
	}
	var loader pdfImageLoader
	if client != nil && cfg != nil && cfg.domain != "" {
		loader = func(ctx context.Context, src string) ([]byte, error) {
			resource, err := client.FetchResource(ctx, src)
			if err != nil {
				return nil, err
			}
			return resource.Data, nil
		}
	} else {
		loader = fetchPublicPDFImage
	}
	data, err := renderChatPDF(ctx, ChatSnapshot{
		Title: title, Messages: []UIMessage{{Role: UIMessageRoleAssistant, Content: content}},
	}, loader)
	if err != nil {
		fatal("cannot generate PDF: %v", err)
	}
	link, err := savePDFArtifact(session.webChatID, data)
	if err != nil {
		fatal("cannot save PDF: %v", err)
	}
	outputJSON(map[string]any{"title": title, "url": link, "markdown": fmt.Sprintf("[Скачать PDF](%s)", link), "bytes": len(data)})
}

func fetchPublicPDFImage(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || !isFetchableScheme(u.Scheme) {
		return nil, fmt.Errorf("absolute http(s) image URL required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := fetchHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image request returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, encx.DefaultResourceMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > encx.DefaultResourceMaxBytes {
		return nil, fmt.Errorf("image exceeds size limit")
	}
	return data, nil
}

func toolCreateScenarioPDF(ctx context.Context, client *encx.Client, session *llmSession, gameID, from, to int) {
	if session == nil || !webChatIDRE.MatchString(session.webChatID) {
		fatal("create_scenario_pdf is available only in a WebUI chat")
	}
	if gameID <= 0 || from < 0 || to < 0 || (from > 0 && to > 0 && from > to) {
		fatal("invalid game ID or level range")
	}
	if client == nil {
		fatal("Encounter client is unavailable")
	}
	if err := client.VerifyAdminSession(ctx); err != nil {
		fatalEncx("Author session required", err)
	}
	doc, err := client.GetAdminGameScenario(ctx, gameID)
	if err != nil {
		fatalEncx("Read game scenario", err)
	}
	if from > 0 || to > 0 {
		selected := selectScenarioLevels(doc, from, to)
		if len(selected.Levels) == 0 {
			fatal("no levels in the requested range")
		}
		doc = &selected.Document
	}
	data, err := renderScenarioPDF(ctx, doc, func(ctx context.Context, src string) ([]byte, error) {
		resource, err := client.FetchResource(ctx, src)
		if err != nil {
			return nil, err
		}
		return resource.Data, nil
	})
	if err != nil {
		fatal("cannot generate scenario PDF: %v", err)
	}
	link, err := savePDFArtifact(session.webChatID, data)
	if err != nil {
		fatal("cannot save scenario PDF: %v", err)
	}
	outputJSON(map[string]any{
		"game_id": gameID, "levels": len(doc.Levels), "from_level": from, "to_level": to,
		"url": link, "markdown": fmt.Sprintf("[Скачать PDF сценария](%s)", link), "bytes": len(data),
	})
}

func savePDFArtifact(chatID string, data []byte) (string, error) {
	if !webChatIDRE.MatchString(chatID) {
		return "", fmt.Errorf("invalid chat ID")
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", err
	}
	name := hex.EncodeToString(token[:]) + ".pdf"
	dir := filepath.Join(webArtifactsRoot(), chatID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("write PDF: %v, close: %v", writeErr, closeErr)
	}
	return fmt.Sprintf("/api/v1/chats/%s/artifacts/%s", chatID, name), nil
}

func (h *webHub) httpGetChatArtifact(w http.ResponseWriter, r *http.Request) {
	chatID := r.PathValue("id")
	name := r.PathValue("name")
	if _, ok := h.store.Get(chatID); !ok || !webChatIDRE.MatchString(chatID) || !pdfArtifactNameRE.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	path := filepath.Join(webArtifactsRoot(), chatID, name)
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="document.pdf"`)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, path)
}
