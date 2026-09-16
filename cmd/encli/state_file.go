package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

// This file holds the load/save/delete layer shared by every small JSON state
// file encli keeps under sessionDir(): the LLM settings, the engine settings and
// the onboarding state. They differ only in what they hold, so the rules that
// are easy to get wrong — where the file lives, what a missing file means, and
// how it is written — are stated once here instead of three times.
//
// What deliberately stays with each type: normalisation (the trimmed() methods)
// and validation, because those are statements about the value, not about the
// file. Validation in particular has to run before anything is written, which is
// the caller's job to sequence.

// stateFilePath resolves a state file's location: envVar wins when it is set —
// tests use it to stay out of the developer's real home directory — and the
// default is a per-feature subdirectory of sessionDir().
//
// The subdirectory is not stylistic. AuthRegistry.ListStatus globs the
// non-recursive sessionDir()/*.json and reads every match as an Encounter domain
// session, so a state file sitting directly in sessionDir() would surface in the
// -web auth panel as a bogus domain whose Logout button deletes it. Routing
// every default path through here is what keeps that mistake from being made
// once per feature.
func stateFilePath(envVar, subdir, name string) string {
	if path := strings.TrimSpace(os.Getenv(envVar)); path != "" {
		return path
	}
	return filepath.Join(sessionDir(), subdir, name)
}

// loadJSONState reads one state file, where what is the operator-facing noun the
// errors are phrased with ("LLM settings", "onboarding state").
//
// A missing or whitespace-only file yields the zero value and no error: nothing
// configured yet is the normal state of a fresh machine.
//
// A file that does not parse IS an error, and callers are expected to propagate
// it. Treating unreadable content as "nothing configured" would silently run
// encli on choices the operator did not make, so the message names the path and
// says how to recover from it.
func loadJSONState[T any](path, what string) (T, error) {
	var zero T
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return zero, nil
		}
		return zero, fmt.Errorf("read %s %s: %w", what, path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return zero, nil
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return zero, fmt.Errorf("parse %s %s: %w (delete the file to start over)", what, path, err)
	}
	return v, nil
}

// saveJSONState writes one state file for the next process to read. Callers
// normalise and validate v first; by the time it arrives here the value is
// already the one that belongs on disk.
//
// These files hold API keys and the hosts credentials are sent to, so each is
// created 0600 inside a 0700 directory and written through a temp file:
// os.WriteFile applies its mode only when creating and would leave a
// pre-existing file readable by others, and a half-written file would make the
// feature's own state unreadable.
func saveJSONState[T any](path, envVar, what string, v T) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", what, err)
	}
	warnIfSessionGlobCollision(envVar, path, what)
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if err := fileutil.WriteFileAtomic(path, data, 0600); err != nil {
		return fmt.Errorf("write %s %s: %w", what, path, err)
	}
	return nil
}

// deleteJSONState removes a state file. An absent file is already the outcome
// the caller asked for, so it is not reported as a failure.
func deleteJSONState(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
