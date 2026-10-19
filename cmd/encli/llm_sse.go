package main

import (
	"io"
	"net/http"

	"github.com/openai/openai-go/v3/packages/ssestream"
)

// OpenAI SDK v3.22 emits events for blank lines and comment-only keepalives;
// its JSON stream then fails with "unexpected end of JSON input". Use its
// decoder extension point to skip only events without data. Codex can omit
// Content-Type entirely, so that case must use the same filter. Parsing, close,
// and error handling remain owned by the SDK. Register once at startup, since
// its registry is not safe for concurrent writes.
//
// Upstream fixes this in v3.43, but that version also changes output-item
// Arguments from string to a union, incompatible with PicoClaw v0.3.1.
func init() {
	for _, contentType := range []string{"", "text/event-stream", "text/event-stream; charset=utf-8", "text/event-stream;charset=utf-8"} {
		ssestream.RegisterDecoder(contentType, func(body io.ReadCloser) ssestream.Decoder {
			// A private unregistered content type selects the SDK's built-in
			// decoder without recursing into our empty-header registration.
			return &nonemptySSEDecoder{Decoder: ssestream.NewDecoder(&http.Response{
				Header: http.Header{"Content-Type": []string{"application/x-encli-sse-base"}}, Body: body,
			})}
		})
	}
}

type nonemptySSEDecoder struct{ ssestream.Decoder }

func (d *nonemptySSEDecoder) Next() bool {
	for d.Decoder.Next() {
		if len(d.Decoder.Event().Data) > 0 {
			return true
		}
	}
	return false
}
