package encx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// AdminGameFile is a file stored with a game, ready to reference in task HTML.
type AdminGameFile struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Size int64  `json:"size,omitempty"`
}

func gameFileName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\?&#") || len(name) > 255 {
		return fmt.Errorf("encx: invalid game file name %q", name)
	}
	for _, r := range name {
		if r < ' ' || r == 0x7f {
			return fmt.Errorf("encx: invalid game file name %q", name)
		}
	}
	return nil
}

func (c *Client) AdminUploadGameImage(ctx context.Context, gameID int, name string, data []byte) (*AdminGameFile, error) {
	if gameID <= 0 {
		return nil, fmt.Errorf("encx: game ID must be positive")
	}
	if err := gameFileName(name); err != nil {
		return nil, err
	}
	if len(data) == 0 || len(data) > 20<<20 {
		return nil, fmt.Errorf("encx: image must be between 1 byte and 20 MiB")
	}
	return c.engine(ctx).AdminUploadGameImage(ctx, gameID, name, data)
}

type adminGameFilesResponse struct {
	Items []AdminGameFile `json:"items"`
	Files []string        `json:"files"`
}

func adminGameFilesPath(gameID int) string {
	return fmt.Sprintf("/admin/games/%d/files", gameID)
}

func (e *newEngine) AdminUploadGameImage(ctx context.Context, gameID int, name string, data []byte) (*AdminGameFile, error) {
	path := adminGameFilesPath(gameID)
	var before adminGameFilesResponse
	if err := e.c.api().GetJSON(ctx, path, nil, &before); err != nil {
		return nil, fmt.Errorf("encx: list game files before upload: %w", err)
	}
	if findGameFile(before.Items, name) != nil || hasGameFileName(before.Files, name) {
		return nil, fmt.Errorf("encx: game file %q already exists", name)
	}
	var after adminGameFilesResponse
	if err := e.c.api().PostFile(ctx, path, "file", name, data, &after); err != nil {
		return nil, fmt.Errorf("encx: upload game image: %w; check game files before retrying", err)
	}
	file := findGameFile(after.Items, name)
	if file == nil || file.URL == "" {
		return nil, fmt.Errorf("encx: upload response did not confirm %q; check game files before retrying", name)
	}
	var confirmed adminGameFilesResponse
	if err := e.c.api().GetJSON(ctx, path, nil, &confirmed); err != nil {
		return nil, fmt.Errorf("encx: upload sent but game files could not be read back: %w; check before retrying", err)
	}
	if findGameFile(confirmed.Items, name) == nil {
		return nil, fmt.Errorf("encx: upload sent but %q is absent from game files; check before retrying", name)
	}
	if strings.HasPrefix(file.URL, "/") {
		file.URL = e.c.api().URL(file.URL, nil)
	}
	return file, nil
}

func findGameFile(files []AdminGameFile, name string) *AdminGameFile {
	for i := range files {
		if strings.EqualFold(files[i].Name, name) {
			return &files[i]
		}
	}
	return nil
}

func hasGameFileName(files []string, name string) bool {
	for _, file := range files {
		if strings.EqualFold(file, name) {
			return true
		}
	}
	return false
}

var gameFileURLPattern = regexp.MustCompile(`(?i)https?://[^"'<>\s]+/data/games/\d+/[^"'<>\s]+`)
var directGamePathPattern = regexp.MustCompile(`(?i)https?://[^"'<>\s]+/data/games/\d+`)

func legacyGameFiles(page string, gameID int) ([]AdminGameFile, string) {
	gamePath := fmt.Sprintf("/data/games/%d", gameID)
	files := make([]AdminGameFile, 0)
	for _, raw := range gameFileURLPattern.FindAllString(page, -1) {
		u, err := url.Parse(raw)
		if err != nil || !strings.HasPrefix(u.Path, gamePath+"/") {
			continue
		}
		name, err := url.PathUnescape(path.Base(u.Path))
		if err == nil && name != "" && findGameFile(files, name) == nil {
			files = append(files, AdminGameFile{Name: name, URL: raw})
		}
	}
	base := ""
	for _, raw := range directGamePathPattern.FindAllString(page, -1) {
		u, err := url.Parse(raw)
		if err == nil && u.Path == gamePath {
			base = raw
			if strings.HasPrefix(u.Hostname(), "d1.") {
				break
			}
		}
	}
	return files, base
}

func (e *legacyEngine) AdminUploadGameImage(ctx context.Context, gameID int, name string, data []byte) (*AdminGameFile, error) {
	managerURL := fmt.Sprintf("%s/Administration/Games/LevelManager.aspx?gid=%d", e.c.baseURL(), gameID)
	before, err := e.c.doGet(ctx, managerURL)
	if err != nil {
		return nil, fmt.Errorf("encx: list game files before upload: %w", err)
	}
	files, base := legacyGameFiles(before, gameID)
	if findGameFile(files, name) != nil {
		return nil, fmt.Errorf("encx: game file %q already exists", name)
	}
	if base == "" {
		return nil, fmt.Errorf("encx: game file storage path is unavailable")
	}
	fileURL := base + "/" + url.PathEscape(name)
	if _, err := e.c.FetchResource(ctx, fileURL, ResourceOptions{MaxBytes: 20 << 20}); err == nil {
		return nil, fmt.Errorf("encx: game file %q already exists", name)
	} else if !strings.Contains(err.Error(), "HTTP 404") {
		return nil, fmt.Errorf("encx: could not check whether game file exists: %w", err)
	}
	uploaderURL := fmt.Sprintf("%s/Administration/Games/FileUploader.aspx?gid=%d", e.c.baseURL(), gameID)
	form, err := e.c.doGet(ctx, uploaderURL)
	if err != nil {
		return nil, fmt.Errorf("encx: open file uploader: %w", err)
	}
	if !strings.Contains(form, `name="inputFile1"`) || !strings.Contains(form, `enctype="multipart/form-data"`) {
		return nil, fmt.Errorf("encx: game file upload form is unavailable")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("inputFile1", name)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(data); err != nil {
		return nil, err
	}
	if err := writer.WriteField("ctl03.x", "1"); err != nil {
		return nil, err
	}
	if err := writer.WriteField("ctl03.y", "1"); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploaderURL, bytes.NewReader(body.Bytes()))
	if err != nil {
		return nil, err
	}
	e.c.setHeaders(req)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := e.c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("encx: upload game image: %w; check game files before retrying", err)
	}
	defer resp.Body.Close()
	response, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("encx: read upload response: %w; check game files before retrying", err)
	}
	if err := guardHTTPLoginRedirect(resp.StatusCode, resp.Header, response); err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("encx: upload failed with HTTP %d", resp.StatusCode)
	}
	stored, err := e.c.FetchResource(ctx, fileURL, ResourceOptions{MaxBytes: 20 << 20})
	if err != nil || !bytes.Equal(stored.Data, data) {
		return nil, fmt.Errorf("encx: upload sent but %q could not be verified; check game files before retrying", name)
	}
	return &AdminGameFile{Name: name, URL: fileURL, Size: int64(len(data))}, nil
}
