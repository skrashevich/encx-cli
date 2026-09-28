package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
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
