package main

import (
	"strings"
	"testing"
)

func TestFormatMarkdownForTerminal_Table(t *testing.T) {
	in := "| № | Name |\n|---|------|\n| 1 | Alpha |\n| 2 | Beta |"
	out := formatMarkdownForTerminal(in)
	if strings.Contains(out, "|") {
		t.Fatalf("expected ASCII table without pipes, got:\n%s", out)
	}
	if !strings.Contains(out, "Alpha") || !strings.Contains(out, "Beta") {
		t.Fatalf("missing cells: %q", out)
	}
}

func TestFormatMarkdownForTerminal_Plain(t *testing.T) {
	in := "hello\nworld"
	if got := formatMarkdownForTerminal(in); got != in {
		t.Fatalf("plain text changed: %q", got)
	}
}
