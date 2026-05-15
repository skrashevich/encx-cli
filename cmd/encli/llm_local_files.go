package main

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultLocalReadMaxBytes = 64 * 1024
	maxLocalReadMaxBytes     = 512 * 1024
	defaultLocalSearchLimit  = 50
	maxLocalSearchLimit      = 200
)

func localFilesRoot() (string, error) {
	root := strings.TrimSpace(os.Getenv("LLM_FILES_ROOT"))
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	return filepath.Abs(root)
}

func resolveLocalPath(userPath string) (string, error) {
	root, err := localFilesRoot()
	if err != nil {
		return "", err
	}
	if userPath == "" {
		userPath = "."
	}
	clean := filepath.Clean(userPath)
	var abs string
	if filepath.IsAbs(clean) {
		abs, err = filepath.Abs(clean)
	} else {
		abs, err = filepath.Abs(filepath.Join(root, clean))
	}
	if err != nil {
		return "", err
	}
	rootWithSep := root + string(os.PathSeparator)
	if abs != root && !strings.HasPrefix(abs, rootWithSep) {
		return "", errors.New("path is outside LLM_FILES_ROOT")
	}
	return abs, nil
}

func toolReadLocalFile(path string, maxBytes, offset int) {
	if maxBytes <= 0 {
		maxBytes = defaultLocalReadMaxBytes
	}
	if maxBytes > maxLocalReadMaxBytes {
		maxBytes = maxLocalReadMaxBytes
	}
	if offset < 0 {
		offset = 0
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

	f, err := os.Open(abs)
	if err != nil {
		fatal("%v", err)
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(int64(offset), 0); err != nil {
			fatal("%v", err)
		}
	}

	data := make([]byte, maxBytes+1)
	n, err := f.Read(data)
	if err != nil && n == 0 {
		fatal("%v", err)
	}
	truncated := n > maxBytes
	if truncated {
		n = maxBytes
	}

	outputJSON(map[string]any{
		"path":      abs,
		"size":      info.Size(),
		"offset":    offset,
		"read":      n,
		"truncated": truncated,
		"content":   string(data[:n]),
	})
}

func toolListLocalDir(path string, recursive bool) {
	abs, err := resolveLocalPath(path)
	if err != nil {
		fatal("%v", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		fatal("%v", err)
	}
	if !info.IsDir() {
		fatal("path is not a directory")
	}

	type dirEntry struct {
		Name  string `json:"name"`
		Path  string `json:"path"`
		IsDir bool   `json:"is_dir"`
		Size  int64  `json:"size,omitempty"`
	}

	var entries []dirEntry
	appendEntry := func(name string, fi fs.FileInfo, fullPath string) {
		entries = append(entries, dirEntry{
			Name:  name,
			Path:  fullPath,
			IsDir: fi.IsDir(),
			Size:  fi.Size(),
		})
	}

	if !recursive {
		items, err := os.ReadDir(abs)
		if err != nil {
			fatal("%v", err)
		}
		for _, item := range items {
			fi, err := item.Info()
			if err != nil {
				continue
			}
			appendEntry(item.Name(), fi, filepath.Join(abs, item.Name()))
		}
		outputJSON(map[string]any{
			"path":    abs,
			"count":   len(entries),
			"entries": entries,
		})
		return
	}

	const maxEntries = 500
	err = filepath.WalkDir(abs, func(fullPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if fullPath == abs {
			return nil
		}
		rel, err := filepath.Rel(abs, fullPath)
		if err != nil {
			return err
		}
		depth := strings.Count(rel, string(os.PathSeparator)) + 1
		if depth > 3 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		appendEntry(rel, fi, fullPath)
		if len(entries) >= maxEntries {
			return fs.ErrExist
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrExist) {
		fatal("%v", err)
	}

	outputJSON(map[string]any{
		"path":      abs,
		"recursive": true,
		"count":     len(entries),
		"entries":   entries,
		"truncated": len(entries) >= maxEntries,
	})
}

func toolSearchLocalFiles(rootPath, pattern, glob string, maxMatches int) {
	if strings.TrimSpace(pattern) == "" {
		fatal("pattern is required")
	}
	if maxMatches <= 0 {
		maxMatches = defaultLocalSearchLimit
	}
	if maxMatches > maxLocalSearchLimit {
		maxMatches = maxLocalSearchLimit
	}

	abs, err := resolveLocalPath(rootPath)
	if err != nil {
		fatal("%v", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		fatal("%v", err)
	}
	searchRoot := abs
	if !info.IsDir() {
		searchRoot = filepath.Dir(abs)
	}

	type match struct {
		Path    string `json:"path"`
		Line    int    `json:"line"`
		Snippet string `json:"snippet"`
	}

	var matches []match
	patternLower := strings.ToLower(pattern)

	err = filepath.WalkDir(searchRoot, func(fullPath string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if glob != "" {
			ok, _ := filepath.Match(glob, d.Name())
			if !ok {
				return nil
			}
		}
		fi, err := d.Info()
		if err != nil || fi.Size() > maxLocalReadMaxBytes {
			return nil
		}

		f, err := os.Open(fullPath)
		if err != nil {
			return nil
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if !strings.Contains(strings.ToLower(line), patternLower) {
				continue
			}
			matches = append(matches, match{
				Path:    fullPath,
				Line:    lineNum,
				Snippet: summarizeDebugText(strings.TrimSpace(line), 240),
			})
			if len(matches) >= maxMatches {
				return fs.ErrExist
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrExist) {
		fatal("%v", err)
	}

	outputJSON(map[string]any{
		"root":      searchRoot,
		"pattern":   pattern,
		"glob":      glob,
		"count":     len(matches),
		"truncated": len(matches) >= maxMatches,
		"matches":   matches,
	})
}
