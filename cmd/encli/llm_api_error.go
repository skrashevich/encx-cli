package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/openai/openai-go/v3"
)

// The SDK decodes only the error envelope. Codex also returns top-level detail.
func withAPIErrorDetail(err error) error {
	apiErr, ok := errors.AsType[*openai.Error](err)
	if !ok || apiErr.Response == nil || apiErr.Response.Body == nil || apiErr.RawJSON() != "" {
		return err
	}
	body, readErr := io.ReadAll(io.LimitReader(apiErr.Response.Body, 64<<10))
	if readErr != nil {
		return err
	}
	var payload struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(body, &payload) != nil || strings.TrimSpace(payload.Detail) == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, payload.Detail)
}
