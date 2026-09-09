package main

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func multipartUploadBody(t *testing.T, fieldName, fileName string, content []byte) (body *bytes.Buffer, contentType string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile(fieldName, fileName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestHttpUploadChatFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	hub := &webHub{cfg: &config{}, registry: NewAuthRegistry(), store: NewChatStore(), sse: newSSEHub()}
	srv := httptest.NewTestServer(t, hub.newMux())
	srv.Start()

	res, err := http.Post(srv.URL+"/api/v1/chats", "application/json", stringsReader(`{"domain":"d","game_id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var snap ChatSnapshot
	_ = json.NewDecoder(res.Body).Decode(&snap)
	res.Body.Close()

	body, ct := multipartUploadBody(t, "file", "notes.txt", []byte("hello agent"))
	res, err = http.Post(srv.URL+"/api/v1/chats/"+snap.ID+"/files", ct, body)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("upload status %d", res.StatusCode)
	}
	var info uploadedFileInfo
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		t.Fatal(err)
	}
	if info.Name != "notes.txt" || info.Size != int64(len("hello agent")) || info.Path == "" {
		t.Fatalf("unexpected upload info: %+v", info)
	}
	if !withinRoot(info.Path, chatUploadsRoot()) {
		t.Fatalf("upload path %q escaped chatUploadsRoot", info.Path)
	}

	// The uploaded file must be readable through the same sandbox check the
	// read_local_file / read_pdf_file tools use.
	if _, err := resolveLocalPath(info.Path); err != nil {
		t.Fatalf("resolveLocalPath rejected upload: %v", err)
	}
}

func TestHttpUploadChatFileUnknownChat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	hub := &webHub{cfg: &config{}, registry: NewAuthRegistry(), store: NewChatStore(), sse: newSSEHub()}
	srv := httptest.NewTestServer(t, hub.newMux())
	srv.Start()

	body, ct := multipartUploadBody(t, "file", "notes.txt", []byte("x"))
	res, err := http.Post(srv.URL+"/api/v1/chats/does-not-exist/files", ct, body)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", res.StatusCode)
	}
}

func TestHttpPostMessageWithFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	hub := &webHub{cfg: &config{}, registry: NewAuthRegistry(), store: NewChatStore(), sse: newSSEHub(), runTurn: func(ctx context.Context, h *webHub, chatID string) {}}
	srv := httptest.NewTestServer(t, hub.newMux())
	srv.Start()

	res, err := http.Post(srv.URL+"/api/v1/chats", "application/json", stringsReader(`{"domain":"d","game_id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	var snap ChatSnapshot
	_ = json.NewDecoder(res.Body).Decode(&snap)
	res.Body.Close()

	msgBody := `{"content":"see attached","files":[{"path":"/tmp/x/doc.pdf","name":"doc.pdf"}]}`
	res, err = http.Post(srv.URL+"/api/v1/chats/"+snap.ID+"/messages", "application/json", stringsReader(msgBody))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("post message status %d", res.StatusCode)
	}
	var accepted struct {
		Message UIMessage `json:"message"`
	}
	if err := json.NewDecoder(res.Body).Decode(&accepted); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(accepted.Message.Content, "doc.pdf") || !strings.Contains(accepted.Message.Content, "read_pdf_file") {
		t.Fatalf("expected attachment note in stored message, got %q", accepted.Message.Content)
	}
}

func TestAppendUploadedFilesNote(t *testing.T) {
	got := appendUploadedFilesNote("please review", []uploadedFileRef{
		{Path: "/root/uploads/1/a_doc.pdf", Name: "doc.pdf"},
		{Path: "/root/uploads/1/b_notes.txt", Name: "notes.txt"},
	})
	if !strings.Contains(got, "please review") {
		t.Fatalf("original content lost: %q", got)
	}
	if !strings.Contains(got, "read_pdf_file") || !strings.Contains(got, "read_local_file") {
		t.Fatalf("expected both tool hints: %q", got)
	}
}
