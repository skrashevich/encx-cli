package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/skrashevich/encx-cli/encx"
)

func registerHARFlags(fs *flag.FlagSet, cfg *config) {
	fs.BoolVar(&cfg.harRecording, "har", envBool("ENCX_HAR"), "Record HTTP traffic as HAR 1.2 (env: ENCX_HAR)")
	fs.StringVar(&cfg.harOut, "har-out", os.Getenv("ENCX_HAR_OUT"), "HAR export path (file or directory; default: encounter-<domain>-<timestamp>.har)")
}

func appendEncOpts(cfg *config) []encx.Option {
	var opts []encx.Option
	if cfg.insecure {
		opts = append(opts, encx.WithInsecureTLS())
	}
	if cfg.useHTTP {
		opts = append(opts, encx.WithHTTP())
	}
	if cfg.debug {
		opts = append(opts, encx.WithDebugLogger(debugf))
	}
	if cfg.harRecording {
		opts = append(opts, encx.WithHARRecording(true))
	}
	return opts
}

func exportClientHAR(client *encx.Client, cfg *config) {
	if client == nil || client.HAREntryCount() == 0 {
		return
	}
	path, err := resolveHAROutPath(cfg.harOut, cfg.domain)
	if err != nil {
		fmt.Fprintf(os.Stderr, "har export: %v\n", err)
		return
	}
	if err := writeClientHAR(client, path); err != nil {
		fmt.Fprintf(os.Stderr, "har export: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "har: wrote %d entries to %s\n", client.HAREntryCount(), path)
}

func exportRegistryHAR(registry *AuthRegistry, cfg *config) {
	if registry == nil {
		return
	}
	registry.ForEachClient(func(domain string, client *encx.Client) {
		if client.HAREntryCount() == 0 {
			return
		}
		path, err := resolveHAROutPath(cfg.harOut, domain)
		if err != nil {
			fmt.Fprintf(os.Stderr, "har export (%s): %v\n", domain, err)
			return
		}
		if err := writeClientHAR(client, path); err != nil {
			fmt.Fprintf(os.Stderr, "har export (%s): %v\n", domain, err)
			return
		}
		fmt.Fprintf(os.Stderr, "har: wrote %d entries for %s to %s\n", client.HAREntryCount(), domain, path)
	})
}

func writeClientHAR(client *encx.Client, path string) error {
	raw, err := client.ExportHARJSON()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(raw), 0o644)
}

func resolveHAROutPath(harOut, domain string) (string, error) {
	safeDomain := sanitizeHARFilename(domain)
	timestamp := time.Now().UTC().Format("20060102T150405Z")
	defaultName := fmt.Sprintf("encounter-%s-%s.har", safeDomain, timestamp)

	trimmed := strings.TrimSpace(harOut)
	if trimmed == "" {
		return filepath.Join(".", defaultName), nil
	}

	info, err := os.Stat(trimmed)
	if err == nil && info.IsDir() {
		return filepath.Join(trimmed, defaultName), nil
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if strings.HasSuffix(trimmed, string(os.PathSeparator)) {
		return filepath.Join(trimmed, defaultName), nil
	}
	if strings.HasSuffix(strings.ToLower(trimmed), ".har") {
		return trimmed, nil
	}
	if err != nil && os.IsNotExist(err) {
		if strings.Contains(trimmed, string(os.PathSeparator)) {
			return trimmed, nil
		}
	}
	return filepath.Join(trimmed, defaultName), nil
}

func sanitizeHARFilename(domain string) string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range domain {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "unknown"
	}
	return out
}
