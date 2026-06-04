package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveHAROutPathDefault(t *testing.T) {
	path, err := resolveHAROutPath("", "tech.en.cx")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Ext(path) != ".har" {
		t.Fatalf("expected .har extension, got %q", path)
	}
	if !strings.Contains(filepath.Base(path), "tech-en-cx") {
		t.Fatalf("expected sanitized domain in filename, got %q", path)
	}
}

func TestResolveHAROutPathExplicitFile(t *testing.T) {
	path, err := resolveHAROutPath("/tmp/session.har", "tech.en.cx")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/session.har" {
		t.Fatalf("got %q", path)
	}
}

func TestResolveHAROutPathDirectory(t *testing.T) {
	dir := t.TempDir()
	path, err := resolveHAROutPath(dir, "demo.en.cx")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("expected dir %q, got %q", dir, filepath.Dir(path))
	}
}
