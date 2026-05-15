package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func withLocalFilesRoot(t *testing.T, root string, fn func()) {
	t.Helper()
	prev := os.Getenv("LLM_FILES_ROOT")
	t.Setenv("LLM_FILES_ROOT", root)
	t.Cleanup(func() { _ = os.Setenv("LLM_FILES_ROOT", prev) })
	fn()
}

func TestGetToolsIncludesLocalAndWikipediaTools(t *testing.T) {
	t.Parallel()
	want := []string{
		"read_local_file",
		"list_local_dir",
		"search_local_files",
		"wikipedia_search",
		"wikipedia_article",
	}
	for _, name := range want {
		found := false
		for _, tool := range getTools(false) {
			if tool.Function.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("tool %q is not registered", name)
		}
	}
}

func TestReadLocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("hello quest"), 0o644); err != nil {
		t.Fatal(err)
	}
	withLocalFilesRoot(t, dir, func() {
		out := captureStdout(t, func() {
			toolReadLocalFile("note.txt", 0, 0)
		})
		if !strings.Contains(out, "hello quest") {
			t.Fatalf("unexpected output: %s", out)
		}
	})
}

func TestListLocalDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	withLocalFilesRoot(t, dir, func() {
		out := captureStdout(t, func() {
			toolListLocalDir(".", false)
		})
		if !strings.Contains(out, `"a.txt"`) || !strings.Contains(out, `"sub"`) {
			t.Fatalf("unexpected output: %s", out)
		}
	})
}

func TestSearchLocalFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "one.md"), []byte("line1\nneedle here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	withLocalFilesRoot(t, dir, func() {
		out := captureStdout(t, func() {
			toolSearchLocalFiles(".", "needle", "*.md", 10)
		})
		if !strings.Contains(out, `"line": 2`) || !strings.Contains(out, "needle here") {
			t.Fatalf("unexpected output: %s", out)
		}
	})
}

func TestResolveLocalPathRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	withLocalFilesRoot(t, dir, func() {
		_, err := resolveLocalPath("../outside.txt")
		if err == nil {
			t.Fatal("expected path traversal to be rejected")
		}
	})
}

func TestReadLocalFileOffset(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "big.txt"), []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	withLocalFilesRoot(t, dir, func() {
		out := captureStdout(t, func() {
			toolReadLocalFile("big.txt", 3, 2)
		})
		if !strings.Contains(out, `"content": "cde"`) {
			t.Fatalf("unexpected output: %s", out)
		}
	})
}
