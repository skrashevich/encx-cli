package main

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/encx"
)

func TestReadAgentImageKeepsFilesInsideRoot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LLM_FILES_ROOT", root)
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.White)
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "clue.png"), buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	name, data, err := readAgentImage("clue.png")
	if err != nil || name != "clue.png" || !bytes.Equal(data, buf.Bytes()) {
		t.Fatalf("readAgentImage = %q, %d bytes, %v", name, len(data), err)
	}
	if !imageNameMatchesData(name, data) || imageNameMatchesData("clue.jpg", data) {
		t.Fatal("image extension did not match content")
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "other.png"), buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "other.png"), filepath.Join(root, "escape.png")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readAgentImage("escape.png"); err == nil {
		t.Fatal("readAgentImage followed a symlink outside LLM_FILES_ROOT")
	}
}

func TestAdminUploadImageToolIsMutation(t *testing.T) {
	if !isAdminMutationTool("admin_upload_image") {
		t.Fatal("admin_upload_image was not classified as an admin write")
	}
	for _, tool := range getTools() {
		if tool.Function.Name == "admin_upload_image" {
			return
		}
	}
	t.Fatal("admin_upload_image is missing from agent tools")
}

func TestImageUploadNameKeepsOriginalChatFilename(t *testing.T) {
	got := imageUploadName(filepath.Join(chatUploadsRoot(), "chat-id", "deadbeef_clue.png"))
	if got != "clue.png" {
		t.Fatalf("imageUploadName = %q", got)
	}
}

func TestAgentImageUploadNameRequiresExplicitRequest(t *testing.T) {
	cases := []struct {
		request string
		name    string
		want    bool
	}{
		{"Загрузи схему доезда до часовни", "doezd-chapel.png", false},
		{"Загрузи файл doezd-chapel.png", "doezd-chapel.png", false},
		{"Загрузи схему; имя файла: doezd-chapel.png", "doezd-chapel.png", true},
		{"Назови схему doezd-chapel.png", "doezd-chapel.png", true},
		{"Загрузи схему\n\n[Прикреплённые файлы]\nимя файла: doezd-chapel.png", "doezd-chapel.png", false},
		{"[Прикреплённые файлы]\nимя файла: doezd-chapel.png", "doezd-chapel.png", false},
	}
	for _, tc := range cases {
		got := explicitlyRequestedImageName(&llmSession{latestUserMessage: tc.request}, tc.name)
		if got != tc.want {
			t.Errorf("explicitlyRequestedImageName(%q, %q) = %t, want %t", tc.request, tc.name, got, tc.want)
		}
	}

	session := &llmSession{latestUserMessage: "Загрузи схему доезда до часовни"}
	first := agentImageUploadName(session, "doezd-chapel.png", "png")
	second := agentImageUploadName(session, "doezd-chapel.png", "png")
	for _, name := range []string{first, second} {
		if !strings.HasPrefix(name, "image-") || !strings.HasSuffix(name, ".png") || len(name) < len("image-")+26+len(".png") || strings.Contains(name, "chapel") {
			t.Errorf("predictable upload filename %q", name)
		}
	}
	if first == second {
		t.Fatal("random upload filenames repeated")
	}
	if got := agentImageUploadName(&llmSession{latestUserMessage: "имя файла: clue.jpg"}, "clue.jpg", "jpeg"); got != "clue.jpg" {
		t.Errorf("explicit filename = %q", got)
	}
	if got := agentImageUploadName(session, "", "jpeg"); !strings.HasSuffix(got, ".jpg") {
		t.Errorf("JPEG filename = %q", got)
	}
}

func TestAdminUploadImageUsesRandomServerFilename(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LLM_FILES_ROOT", root)
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "doezd-chapel.png"), buf.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}

	var uploaded string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/admin/games/42/files" {
			t.Errorf("unexpected path %q", r.URL.Path)
			return
		}
		if r.Method == http.MethodPost {
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Error(err)
				return
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Error(err)
				return
			}
			file.Close()
			uploaded = header.Filename
		}
		items := []map[string]string{}
		if uploaded != "" {
			items = append(items, map[string]string{"name": uploaded, "url": "/data/games/42/" + uploaded})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
	}))
	defer srv.Close()
	client := encx.New("demo.en.cx", encx.WithEngine(encx.EngineNew), encx.WithAPIBaseURL(srv.URL), encx.WithAdminDelay(0))
	raw := captureStdout(t, func() {
		toolAdminUploadImage(t.Context(), &config{gameId: 42}, client,
			&llmSession{latestUserMessage: "Загрузи схему доезда до часовни"}, "doezd-chapel.png", "doezd-chapel.png")
	})
	var result struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatal(err)
	}
	if uploaded == "doezd-chapel.png" || !strings.HasPrefix(uploaded, "image-") || len(uploaded) < len("image-")+26+len(".png") || result.Name != uploaded || !strings.HasSuffix(result.URL, "/data/games/42/"+uploaded) {
		t.Fatalf("uploaded=%q result=%+v", uploaded, result)
	}
}
