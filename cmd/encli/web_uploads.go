package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// maxChatUploadBytes bounds one uploaded file. The agent only ever needs the
// extracted text, not the original bytes, so this is generous headroom for a
// scanned PDF rather than a limit tuned to typical size.
const maxChatUploadBytes = 20 << 20

// chatUploadsRoot is where files attached through the web UI land, kept apart
// from LLM_FILES_ROOT so a chat upload never depends on where `encli -web`
// happened to be started.
func chatUploadsRoot() string {
	return filepath.Join(sessionDir(), "web", "uploads")
}

// sanitizeUploadFilename strips any directory components and control
// characters from a client-supplied filename. The name is untrusted input.
func sanitizeUploadFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "" || name == "." || name == string(os.PathSeparator) {
		return "file"
	}
	return name
}

type uploadedFileInfo struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Size int64  `json:"size"`
}

func (h *webHub) httpUploadChatFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.store.Get(id); !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "chat not found"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxChatUploadBytes)
	if err := r.ParseMultipartForm(maxChatUploadBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file too large or malformed upload"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "form field \"file\" is required"})
		return
	}
	defer file.Close()

	dir := filepath.Join(chatUploadsRoot(), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	name := sanitizeUploadFilename(header.Filename)
	dest := filepath.Join(dir, uploadPrefix()+"_"+name)

	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer out.Close()

	n, err := io.Copy(out, file)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, uploadedFileInfo{Path: dest, Name: name, Size: n})
}

// uploadPrefix disambiguates two uploads sharing a filename in the same chat.
func uploadPrefix() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "upload"
	}
	return hex.EncodeToString(b[:])
}

type uploadedFileRef struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// appendUploadedFilesNote appends a machine-readable list of attached files to
// a user's message so the model knows to call read_local_file or
// read_pdf_file on them instead of being told about a link it cannot open.
func appendUploadedFilesNote(content string, files []uploadedFileRef) string {
	var b strings.Builder
	b.WriteString(content)
	if content != "" {
		b.WriteString("\n\n")
	}
	b.WriteString("[Прикреплённые файлы]\n")
	for _, f := range files {
		name := f.Name
		if name == "" {
			name = filepath.Base(f.Path)
		}
		tool := "read_local_file"
		if strings.EqualFold(filepath.Ext(f.Path), ".pdf") {
			tool = "read_pdf_file"
		}
		fmt.Fprintf(&b, "- %s: %s (прочитай через %s)\n", name, f.Path, tool)
	}
	return b.String()
}
