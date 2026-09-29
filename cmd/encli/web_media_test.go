package main

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestWebMediaUsesImageBytesForContentType(t *testing.T) {
	allowPrivateFetch(t)
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(pngBytes.Bytes())
	}))
	defer imageServer.Close()
	hub := &webHub{cfg: &config{}, registry: NewAuthRegistry(), store: NewChatStore(), sse: newSSEHub()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media?url="+url.QueryEscape(imageServer.URL), nil)
	w := httptest.NewRecorder()
	hub.newMux().ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" || !bytes.Equal(w.Body.Bytes(), pngBytes.Bytes()) {
		t.Fatalf("media response: status=%d type=%q body=%d", w.Code, w.Header().Get("Content-Type"), w.Body.Len())
	}
}

func TestWebMediaRejectsNonImage(t *testing.T) {
	allowPrivateFetch(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<script>alert(1)</script>"))
	}))
	defer server.Close()
	hub := &webHub{cfg: &config{}, registry: NewAuthRegistry(), store: NewChatStore(), sse: newSSEHub()}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media?url="+url.QueryEscape(server.URL), nil)
	w := httptest.NewRecorder()
	hub.newMux().ServeHTTP(w, req)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected unsupported media, got %d", w.Code)
	}
}
