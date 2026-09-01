package php

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/bindings/php/internal/gen"
	"github.com/skrashevich/encx-cli/bindings/php/internal/genphp"
	"github.com/skrashevich/encx-cli/bindings/php/internal/surface"
)

// mobilePkgDir is the Go package every generated artifact is derived from.
const mobilePkgDir = "../../mobile/encxmobile"

// regenHint is the single command that fixes any failure in this file.
const regenHint = "run `go generate ./bindings/...` and commit the result"

// TestNoDriftFromGoSource regenerates all five committed artifacts in memory
// and compares them byte for byte with what is on disk. It is the consolidated
// drift gate: internal/gen and internal/genphp each check the files they own,
// but nothing else covers the C header and the surface manifest, and nothing
// else fails with a single message naming every stale file at once.
func TestNoDriftFromGoSource(t *testing.T) {
	model, err := surface.Load(mobilePkgDir)
	if err != nil {
		t.Fatalf("surface.Load(%s): %v", mobilePkgDir, err)
	}

	artifacts := []struct {
		path   string
		render func(*surface.Model) ([]byte, error)
	}{
		{filepath.Join("cshared", "exports_gen.go"), gen.CgoFile},
		{"encx.h", gen.HeaderFile},
		{"bindings.manifest.json", gen.ManifestFile},
		{filepath.Join("src", "Encx", "Client.php"), genphp.ClientFile},
		{filepath.Join("src", "Encx", "Helpers.php"), genphp.HelpersFile},
	}

	var stale []string
	for _, a := range artifacts {
		t.Run(a.path, func(t *testing.T) {
			want, err := a.render(model)
			if err != nil {
				t.Fatalf("generating %s: %v", a.path, err)
			}

			got, err := os.ReadFile(a.path)
			if err != nil {
				stale = append(stale, a.path)
				t.Fatalf("%s cannot be read, so it cannot match the generator: %v\n%s", a.path, err, regenHint)
			}

			if bytes.Equal(got, want) {
				return
			}

			stale = append(stale, a.path)
			line, n, w := firstDifference(got, want)
			t.Errorf("%s is stale (%d bytes on disk, %d generated).\n"+
				"first difference at line %d:\n"+
				"  on disk:   %s\n"+
				"  generated: %s\n"+
				"%s",
				a.path, len(got), len(want), line, quote(n), quote(w), regenHint)
		})
	}

	if len(stale) > 0 {
		t.Errorf("the PHP bindings no longer match mobile/encxmobile; stale files:\n\t%s\n%s",
			strings.Join(stale, "\n\t"), regenHint)
	}
}

// firstDifference returns the 1-based number of the first line where got and
// want diverge, together with that line from each side. A line that exists on
// only one side is reported as "<end of file>".
func firstDifference(got, want []byte) (line int, gotLine, wantLine string) {
	g := strings.Split(string(got), "\n")
	w := strings.Split(string(want), "\n")

	for i := 0; i < len(g) || i < len(w); i++ {
		gl, wl := "<end of file>", "<end of file>"
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return i + 1, gl, wl
		}
	}
	// The byte comparison said the files differ, so a line must too; fall back
	// to the last line rather than claiming they are identical.
	return len(w), "<identical lines, trailing bytes differ>", "<identical lines, trailing bytes differ>"
}

// quote renders a reported line so that whitespace-only differences stay
// visible, and truncates it so a long generated line does not bury the message
// it belongs to.
func quote(s string) string {
	const max = 160
	if len(s) > max {
		s = s[:max] + "..."
	}
	return strconv.Quote(s)
}
