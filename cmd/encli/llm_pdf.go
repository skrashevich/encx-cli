package main

import (
	"bytes"
	"os"

	"github.com/ledongthuc/pdf"
)

// toolReadPdfFile extracts plain text from a local PDF, reusing the same
// LLM_FILES_ROOT sandbox as toolReadLocalFile. Without page it reads the
// whole document; a positive page reads that page alone, which keeps a
// multi-hundred-page manual from blowing the byte budget on the first call.
func toolReadPdfFile(path string, page, maxBytes int) {
	if maxBytes <= 0 {
		maxBytes = defaultLocalReadMaxBytes
	}
	if maxBytes > maxLocalReadMaxBytes {
		maxBytes = maxLocalReadMaxBytes
	}

	abs, err := resolveLocalPath(path)
	if err != nil {
		fatal("%v", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		fatal("%v", err)
	}
	if info.IsDir() {
		fatal("path is a directory, use list_local_dir")
	}

	f, r, err := pdf.Open(abs)
	if err != nil {
		fatal("%s could not be read as a PDF: %v", abs, err)
	}
	defer f.Close()

	numPages := r.NumPage()

	var content string
	if page > 0 {
		if page > numPages {
			fatal("page %d is out of range; %s has %d pages", page, abs, numPages)
		}
		content, err = r.Page(page).GetPlainText(nil)
		if err != nil {
			fatal("extract text from page %d of %s: %v", page, abs, err)
		}
	} else {
		text, err := r.GetPlainText()
		if err != nil {
			fatal("extract text from %s: %v", abs, err)
		}
		var buf bytes.Buffer
		if _, err := buf.ReadFrom(text); err != nil {
			fatal("read extracted text from %s: %v", abs, err)
		}
		content = buf.String()
	}

	truncated := len(content) > maxBytes
	if truncated {
		content = content[:maxBytes]
	}

	result := map[string]any{
		"path":      abs,
		"num_pages": numPages,
		"read":      len(content),
		"truncated": truncated,
		"content":   content,
	}
	if page > 0 {
		result["page"] = page
	}
	outputJSON(result)
}
