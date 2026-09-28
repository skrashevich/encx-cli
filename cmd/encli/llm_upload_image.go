package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"html"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/image/webp"

	"github.com/skrashevich/encx-cli/encx"
)

const maxGameImageBytes = 20 << 20

func readAgentImage(userPath string) (string, []byte, error) {
	if strings.TrimSpace(userPath) == "" {
		return "", nil, fmt.Errorf("image path is required")
	}
	abs, err := resolveLocalPath(userPath)
	if err != nil {
		return "", nil, err
	}
	localRoot, err := localFilesRoot()
	if err != nil {
		return "", nil, err
	}
	rootPath := chatUploadsRoot()
	if withinRoot(abs, localRoot) {
		rootPath = localRoot
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return "", nil, err
	}
	defer root.Close()
	rel, err := filepath.Rel(rootPath, abs)
	if err != nil {
		return "", nil, err
	}
	f, err := root.Open(rel)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return "", nil, err
	}
	if !stat.Mode().IsRegular() || stat.Size() == 0 || stat.Size() > maxGameImageBytes {
		return "", nil, fmt.Errorf("image must be a regular file between 1 byte and 20 MiB")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxGameImageBytes+1))
	if err != nil {
		return "", nil, err
	}
	if len(data) == 0 || len(data) > maxGameImageBytes {
		return "", nil, fmt.Errorf("image must be between 1 byte and 20 MiB")
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", nil, fmt.Errorf("image cannot be decoded: %w", err)
	}
	if format != "png" && format != "jpeg" && format != "gif" && format != "webp" {
		return "", nil, fmt.Errorf("unsupported image format %q", format)
	}
	return imageUploadName(abs), data, nil
}

func imageUploadName(abs string) string {
	name := filepath.Base(abs)
	if !withinRoot(abs, chatUploadsRoot()) {
		return name
	}
	prefix, original, ok := strings.Cut(name, "_")
	if ok && original != "" && (prefix == "upload" || len(prefix) == 8) {
		if prefix == "upload" {
			return original
		}
		if _, err := hex.DecodeString(prefix); err == nil {
			return original
		}
	}
	return name
}

func imageNameMatchesData(name string, data []byte) bool {
	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch format {
	case "jpeg":
		return ext == ".jpg" || ext == ".jpeg"
	case "png", "gif", "webp":
		return ext == "."+format
	}
	return false
}

func toolAdminUploadImage(ctx context.Context, cfg *config, client *encx.Client, path, name string) {
	if cfg.gameId <= 0 {
		fatal("game_id must be positive")
	}
	localName, data, err := readAgentImage(path)
	if err != nil {
		fatal("Cannot read image: %v", err)
	}
	if name == "" {
		name = localName
	}
	if !imageNameMatchesData(name, data) {
		fatal("Image filename %q has the wrong extension for its content", name)
	}
	file, err := client.AdminUploadGameImage(ctx, cfg.gameId, name, data)
	if err != nil {
		fatal("Cannot upload image: %v", err)
	}
	outputJSON(map[string]any{
		"game_id": cfg.gameId,
		"name":    file.Name,
		"url":     file.URL,
		"size":    len(data),
		"html":    fmt.Sprintf(`<img src="%s">`, html.EscapeString(file.URL)),
	})
}
