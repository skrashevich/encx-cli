package enapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// PostFile sends one authenticated multipart file to a site-scoped API route.
func (c *Client) PostFile(ctx context.Context, path, field, name string, data []byte, out any) error {
	if c.baseURL == "" {
		return &MissingHostError{Domain: c.domain}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(field, name)
	if err != nil {
		return fmt.Errorf("enapi: create file part: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return fmt.Errorf("enapi: write file part: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("enapi: finish multipart body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL(path, nil), bytes.NewReader(body.Bytes()))
	if err != nil {
		return fmt.Errorf("enapi: create upload request: %w", err)
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("enapi: POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	response, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("enapi: POST %s: read response: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		return parseAPIError(http.MethodPost, path, resp.StatusCode, response)
	}
	if out != nil {
		if err := json.Unmarshal(response, out); err != nil {
			return fmt.Errorf("enapi: POST %s: decode response: %w", path, err)
		}
	}
	return nil
}
