// Command encxphpgen generates the PHP bindings for encx from the Go source of
// mobile/encxmobile.
//
// It writes the cgo export file, the FFI-parsable C header, the surface
// manifest and the PHP classes. Every one of those files is derived from the Go
// package, so the bindings cannot drift from it: regenerating is the only way to
// change them.
//
// With -check the command writes nothing and instead reports which files are
// out of date, which is what CI and the pre-commit hook use.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/skrashevich/encx-cli/bindings/php/internal/gen"
	"github.com/skrashevich/encx-cli/bindings/php/internal/genphp"
	"github.com/skrashevich/encx-cli/bindings/php/internal/surface"
)

func main() {
	out := flag.String("out", ".", "directory of the PHP bindings (the one holding src/ and cshared/)")
	mobile := flag.String("mobile", "", "directory of the mobile/encxmobile package (default: <out>/../../mobile/encxmobile)")
	check := flag.Bool("check", false, "report stale files instead of writing them")
	flag.Parse()

	if err := run(*out, *mobile, *check); err != nil {
		fmt.Fprintln(os.Stderr, "encxphpgen:", err)
		os.Exit(1)
	}
}

// artifact pairs a path relative to the bindings directory with its freshly
// generated content.
type artifact struct {
	path    string
	content []byte
}

func run(outDir, mobileDir string, check bool) error {
	if mobileDir == "" {
		mobileDir = filepath.Join(outDir, "..", "..", "mobile", "encxmobile")
	}

	model, err := surface.Load(mobileDir)
	if err != nil {
		return fmt.Errorf("loading %s: %w", mobileDir, err)
	}

	artifacts, err := build(model)
	if err != nil {
		return err
	}

	if check {
		return reportStale(outDir, artifacts)
	}
	return write(outDir, artifacts)
}

// build renders every generated artifact from the model.
func build(model *surface.Model) ([]artifact, error) {
	type renderer struct {
		path   string
		render func(*surface.Model) ([]byte, error)
	}

	renderers := []renderer{
		{filepath.Join("cshared", "exports_gen.go"), gen.CgoFile},
		{"encx.h", gen.HeaderFile},
		{"bindings.manifest.json", gen.ManifestFile},
		{filepath.Join("src", "Encx", "Client.php"), genphp.ClientFile},
		{filepath.Join("src", "Encx", "Helpers.php"), genphp.HelpersFile},
	}

	artifacts := make([]artifact, 0, len(renderers))
	for _, r := range renderers {
		content, err := r.render(model)
		if err != nil {
			return nil, fmt.Errorf("generating %s: %w", r.path, err)
		}
		artifacts = append(artifacts, artifact{path: r.path, content: content})
	}
	return artifacts, nil
}

func write(outDir string, artifacts []artifact) error {
	for _, a := range artifacts {
		target := filepath.Join(outDir, a.path)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", a.path, err)
		}
		if err := os.WriteFile(target, a.content, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", a.path, err)
		}
	}
	return nil
}

// reportStale lists the artifacts whose on-disk content differs from what the
// current Go source produces.
func reportStale(outDir string, artifacts []artifact) error {
	var stale []string
	for _, a := range artifacts {
		onDisk, err := os.ReadFile(filepath.Join(outDir, a.path))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				stale = append(stale, a.path+" (missing)")
				continue
			}
			return fmt.Errorf("reading %s: %w", a.path, err)
		}
		if !bytes.Equal(onDisk, a.content) {
			stale = append(stale, a.path)
		}
	}

	if len(stale) == 0 {
		return nil
	}

	sort.Strings(stale)
	return fmt.Errorf("bindings are stale, run `go generate ./bindings/...`:\n\t%s",
		joinLines(stale))
}

func joinLines(items []string) string {
	var b bytes.Buffer
	for i, item := range items {
		if i > 0 {
			b.WriteString("\n\t")
		}
		b.WriteString(item)
	}
	return b.String()
}
