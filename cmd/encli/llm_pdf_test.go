package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildMinimalPDF assembles a valid single-page PDF with the given text drawn
// on it, computing real xref offsets — this library's reader has no repair
// path for a malformed xref table, so offsets have to be exact.
func buildMinimalPDF(t *testing.T, text string) []byte {
	t.Helper()

	var buf bytes.Buffer
	offsets := make([]int, 6)
	buf.WriteString("%PDF-1.4\n")

	writeObj := func(num int, body string) {
		offsets[num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}

	writeObj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObj(3, "<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 4 0 R >> >> "+
		"/MediaBox [0 0 200 200] /Contents 5 0 R >>")
	writeObj(4, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

	content := fmt.Sprintf("BT /F1 24 Tf 10 100 Td (%s) Tj ET", text)
	writeObj(5, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))

	xrefOffset := buf.Len()
	buf.WriteString("xref\n0 6\n")
	buf.WriteString("0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&buf, "%010d %05d n \n", offsets[i], 0)
	}
	buf.WriteString("trailer\n<< /Size 6 /Root 1 0 R >>\n")
	fmt.Fprintf(&buf, "startxref\n%d\n%%%%EOF", xrefOffset)

	return buf.Bytes()
}

func TestGetToolsIncludesReadPdfFile(t *testing.T) {
	t.Parallel()
	for _, tool := range getTools() {
		if tool.Function.Name == "read_pdf_file" {
			return
		}
	}
	t.Fatal("tool \"read_pdf_file\" is not registered")
}

func TestReadPdfFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, buildMinimalPDF(t, "Hello PDF"), 0o644); err != nil {
		t.Fatal(err)
	}
	withLocalFilesRoot(t, dir, func() {
		out := captureStdout(t, func() {
			toolReadPdfFile("doc.pdf", 0, 0)
		})
		if !strings.Contains(out, "Hello PDF") {
			t.Fatalf("unexpected output: %s", out)
		}
		if !strings.Contains(out, `"num_pages": 1`) {
			t.Fatalf("expected num_pages 1 in output: %s", out)
		}
	})
}

func TestReadPdfFileSpecificPage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, buildMinimalPDF(t, "Page One"), 0o644); err != nil {
		t.Fatal(err)
	}
	withLocalFilesRoot(t, dir, func() {
		out := captureStdout(t, func() {
			toolReadPdfFile("doc.pdf", 1, 0)
		})
		if !strings.Contains(out, "Page One") {
			t.Fatalf("unexpected output: %s", out)
		}
		if !strings.Contains(out, `"page": 1`) {
			t.Fatalf("expected page 1 in output: %s", out)
		}
	})
}

func TestReadPdfFilePageOutOfRange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(path, buildMinimalPDF(t, "Only Page"), 0o644); err != nil {
		t.Fatal(err)
	}
	withLocalFilesRoot(t, dir, func() {
		session := &llmSession{securityMode: SecurityModeFull}
		result := executeToolCallSafe(t.Context(), &config{}, nil, session, "read_pdf_file",
			`{"path":"doc.pdf","page":2}`)
		if !strings.Contains(result, "error") || !strings.Contains(result, "out of range") {
			t.Fatalf("expected an out-of-range error, got %q", result)
		}
	})
}
