package encx

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestNewEngineAdminUploadGameImage(t *testing.T) {
	var uploaded bool
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/games/42/files" || r.Header.Get("X-En-Domain") == "" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			if uploaded {
				_, _ = io.WriteString(w, `{"items":[{"name":"clue.png","url":"/data/games/42/clue.png","size":9}]}`)
			} else {
				_, _ = io.WriteString(w, `{"items":[]}`)
			}
		case http.MethodPost:
			uploaded = true
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			file, _, err := r.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			got, _ := io.ReadAll(file)
			if string(got) != "PNG bytes" {
				t.Errorf("uploaded %q", got)
			}
			_, _ = io.WriteString(w, `{"items":[{"name":"clue.png","url":"/data/games/42/clue.png","size":9}]}`)
		}
	})
	file, err := c.AdminUploadGameImage(t.Context(), 42, "clue.png", []byte("PNG bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if !uploaded || file.Name != "clue.png" || !strings.HasSuffix(file.URL, "/data/games/42/clue.png") {
		t.Fatalf("upload result = %+v, posted = %v", file, uploaded)
	}
}

type uploadRoundTripper func(*http.Request) (*http.Response, error)

func (f uploadRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func uploadResponse(status int, body []byte) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}
}

func TestLegacyAdminUploadGameImage(t *testing.T) {
	var uploaded bool
	data := []byte("image bytes")
	c := New("example.en.cx", WithEngine(EngineLegacy), WithAdminDelay(0))
	c.httpClient.Transport = uploadRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/LevelManager.aspx"):
			return uploadResponse(200, []byte(`<html><span>https://cdn.endata.cx/data/games/42</span><span>https://d1.endata.cx/data/games/42</span></html>`)), nil
		case strings.HasSuffix(r.URL.Path, "/FileUploader.aspx") && r.Method == http.MethodGet:
			return uploadResponse(200, []byte(`<form method="post" enctype="multipart/form-data"><input name="inputFile1" type="file"></form>`)), nil
		case strings.HasSuffix(r.URL.Path, "/FileUploader.aspx") && r.Method == http.MethodPost:
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Fatal(err)
			}
			f, _, err := r.FormFile("inputFile1")
			if err != nil {
				t.Fatal(err)
			}
			got, _ := io.ReadAll(f)
			f.Close()
			if !bytes.Equal(got, data) || r.FormValue("ctl03.x") != "1" {
				t.Errorf("wrong upload body: %q, x=%q", got, r.FormValue("ctl03.x"))
			}
			uploaded = true
			return uploadResponse(200, []byte(`<html>uploaded</html>`)), nil
		case strings.HasPrefix(r.URL.Path, "/data/games/42/"):
			if uploaded {
				return uploadResponse(200, data), nil
			}
			return uploadResponse(404, nil), nil
		default:
			return nil, fmt.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
	})
	file, err := c.AdminUploadGameImage(context.Background(), 42, "clue.png", data)
	if err != nil {
		t.Fatal(err)
	}
	if !uploaded || file.URL != "https://d1.endata.cx/data/games/42/clue.png" {
		t.Fatalf("upload result = %+v, posted = %v", file, uploaded)
	}
}

func TestAdminUploadGameImageRejectsExistingFile(t *testing.T) {
	var posted bool
	c := newEngineClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posted = true
		}
		_, _ = io.WriteString(w, `{"items":[{"name":"clue.png","url":"/data/games/42/clue.png"}]}`)
	})
	if _, err := c.AdminUploadGameImage(t.Context(), 42, "clue.png", []byte("data")); err == nil {
		t.Fatal("existing file accepted")
	}
	if posted {
		t.Fatal("existing file was overwritten")
	}
}
