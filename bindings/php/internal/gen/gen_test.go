package gen

import (
	"bytes"
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/bindings/php/internal/surface"
)

// mobilePkgDir is the package the bindings are generated from.
const mobilePkgDir = "../../../../mobile/encxmobile"

// exportsGenPath is the generated cgo file kept in the tree.
const exportsGenPath = "../../cshared/exports_gen.go"

// wantExports is the number of //export directives exports_gen.go must carry:
// one per bindable symbol and not one more, because the two runtime exports
// (encx_string_free, encx_client_free) are hand-written in runtime.go.
const wantExports = 49

// generatedMarker is the marker Go tooling recognises on generated files.
var generatedMarker = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)

// protoName matches a C prototype line and captures the function name.
var protoName = regexp.MustCompile(`^(?:char \*|void )([A-Za-z_][A-Za-z0-9_]*)\(`)

// exportName matches an //export directive and captures the C symbol.
var exportName = regexp.MustCompile(`(?m)^//export ([A-Za-z_][A-Za-z0-9_]*)$`)

func loadModel(t *testing.T) *surface.Model {
	t.Helper()
	m, err := surface.Load(mobilePkgDir)
	if err != nil {
		t.Fatalf("surface.Load(%s): %v", mobilePkgDir, err)
	}
	return m
}

func mustCgo(t *testing.T, m *surface.Model) []byte {
	t.Helper()
	b, err := CgoFile(m)
	if err != nil {
		t.Fatalf("CgoFile: %v", err)
	}
	return b
}

func mustHeader(t *testing.T, m *surface.Model) []byte {
	t.Helper()
	b, err := HeaderFile(m)
	if err != nil {
		t.Fatalf("HeaderFile: %v", err)
	}
	return b
}

func mustManifest(t *testing.T, m *surface.Model) []byte {
	t.Helper()
	b, err := ManifestFile(m)
	if err != nil {
		t.Fatalf("ManifestFile: %v", err)
	}
	return b
}

// TestDeterministic checks that two independently loaded models produce byte
// identical output, which is what makes the generated files committable.
func TestDeterministic(t *testing.T) {
	first, second := loadModel(t), loadModel(t)

	for _, tc := range []struct {
		name string
		emit func(*testing.T, *surface.Model) []byte
	}{
		{"cgo", mustCgo},
		{"header", mustHeader},
		{"manifest", mustManifest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := tc.emit(t, first), tc.emit(t, second)
			if !bytes.Equal(a, b) {
				t.Fatalf("%s output is not deterministic: %d vs %d bytes", tc.name, len(a), len(b))
			}
			// A second call on the same model must not drift either.
			if c := tc.emit(t, first); !bytes.Equal(a, c) {
				t.Fatalf("%s output changed on a repeated call with the same model", tc.name)
			}
		})
	}
}

func TestGeneratedMarker(t *testing.T) {
	m := loadModel(t)

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"cgo", mustCgo(t, m)},
		{"header", mustHeader(t, m)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			first, _, _ := bytes.Cut(tc.data, []byte("\n"))
			if !generatedMarker.Match(first) {
				t.Fatalf("first line %q does not match the generated-code marker", first)
			}
		})
	}
}

func TestManifestGeneratedKeyIsFirst(t *testing.T) {
	data := mustManifest(t, loadModel(t))
	const want = "{\n  \"_generated\": \"encxphpgen; DO NOT EDIT\",\n"
	if !bytes.HasPrefix(data, []byte(want)) {
		t.Fatalf("manifest does not open with the _generated key:\n%s", firstLines(data, 3))
	}
}

func TestExportCount(t *testing.T) {
	m := loadModel(t)
	got := exportedNames(string(mustCgo(t, m)))

	if len(got) != len(m.Bindable()) {
		t.Errorf("got %d //export directives, want %d (one per bindable symbol)", len(got), len(m.Bindable()))
	}
	if len(got) != wantExports {
		t.Errorf("got %d //export directives, want the pinned %d", len(got), wantExports)
	}
	for _, name := range []string{"encx_string_free", "encx_client_free"} {
		if slicesContains(got, name) {
			t.Errorf("%s is exported from exports_gen.go, but it is hand-written in runtime.go", name)
		}
	}
}

// TestSymbolsMatchHeader checks the ABI is closed: everything the library
// exports is declared in the header PHP parses, and nothing more.
func TestSymbolsMatchHeader(t *testing.T) {
	m := loadModel(t)

	fromCgo := exportedNames(string(mustCgo(t, m)))
	fromCgo = append(fromCgo, "encx_string_free", "encx_client_free")
	sort.Strings(fromCgo)

	fromHeader := prototypeNames(string(mustHeader(t, m)))

	if missing := difference(fromCgo, fromHeader); len(missing) > 0 {
		t.Errorf("exported but not declared in encx.h: %v", missing)
	}
	if extra := difference(fromHeader, fromCgo); len(extra) > 0 {
		t.Errorf("declared in encx.h but not exported: %v", extra)
	}
}

// TestHeaderIsPreprocessorFree guards the one constraint FFI::cdef imposes:
// it sees no preprocessor, so a directive anywhere makes the header unusable.
func TestHeaderIsPreprocessorFree(t *testing.T) {
	header := string(mustHeader(t, loadModel(t)))

	for i, line := range strings.Split(header, "\n") {
		if strings.Contains(line, "#") {
			t.Errorf("encx.h:%d contains a preprocessor character: %q", i+1, line)
		}
	}
	if !strings.HasSuffix(header, "\n") {
		t.Error("encx.h does not end with a newline")
	}
}

func TestHeaderDeclaresRuntime(t *testing.T) {
	header := string(mustHeader(t, loadModel(t)))

	for _, want := range []string{
		"void encx_string_free(char *s);",
		"char *encx_client_free(long long handle);",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("encx.h is missing the runtime declaration %q", want)
		}
	}
}

// TestHeaderBytesParam checks that a []byte parameter reaches C as a pointer
// and a length, since C has no way to carry a slice.
func TestHeaderBytesParam(t *testing.T) {
	header := string(mustHeader(t, loadModel(t)))

	const want = "char *encx_client_import_cookies(long long handle, char *data, long long data_len);"
	if !strings.Contains(header, want) {
		t.Fatalf("encx.h does not declare the bytes-taking method as %q\ngot: %s", want, grepLine(header, "encx_client_import_cookies"))
	}
}

func TestCgoFileParses(t *testing.T) {
	data := mustCgo(t, loadModel(t))

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "exports_gen.go", data, parser.ParseComments)
	if err != nil {
		t.Fatalf("generated cgo file is not valid Go: %v", err)
	}
	if file.Name.Name != "main" {
		t.Errorf("generated cgo file declares package %s, want main", file.Name.Name)
	}
}

func TestManifestShape(t *testing.T) {
	m := loadModel(t)
	data := mustManifest(t, m)

	var doc struct {
		Generated  string `json:"_generated"`
		Package    string `json:"package"`
		ImportPath string `json:"importPath"`
		Runtime    []struct {
			CName     string `json:"cName"`
			Prototype string `json:"prototype"`
		} `json:"runtime"`
		Bindable []struct {
			GoName string `json:"goName"`
			CName  string `json:"cName"`
			Kind   string `json:"kind"`
			Params []struct {
				Name  string `json:"name"`
				CName string `json:"cName"`
				Type  string `json:"type"`
			} `json:"params"`
		} `json:"bindable"`
		Skipped []struct {
			GoName string `json:"goName"`
			Reason string `json:"reason"`
		} `json:"skipped"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("manifest is not valid JSON: %v", err)
	}

	if doc.Package != m.Package {
		t.Errorf("manifest package = %q, want %q", doc.Package, m.Package)
	}
	if doc.ImportPath != m.ImportPath {
		t.Errorf("manifest importPath = %q, want %q", doc.ImportPath, m.ImportPath)
	}
	if len(doc.Runtime) != 2 {
		t.Errorf("manifest lists %d runtime functions, want 2", len(doc.Runtime))
	}
	if len(doc.Bindable) != len(m.Bindable()) {
		t.Errorf("manifest lists %d bindable symbols, want %d", len(doc.Bindable), len(m.Bindable()))
	}
	if len(doc.Skipped) != len(m.Skipped) {
		t.Errorf("manifest lists %d skipped symbols, want %d", len(doc.Skipped), len(m.Skipped))
	}

	for i := 1; i < len(doc.Bindable); i++ {
		if doc.Bindable[i-1].CName >= doc.Bindable[i].CName {
			t.Fatalf("bindable is not sorted by cName: %q before %q", doc.Bindable[i-1].CName, doc.Bindable[i].CName)
		}
	}
	for i := 1; i < len(doc.Skipped); i++ {
		if doc.Skipped[i-1].GoName > doc.Skipped[i].GoName {
			t.Fatalf("skipped is not sorted by goName: %q before %q", doc.Skipped[i-1].GoName, doc.Skipped[i].GoName)
		}
	}
	for _, s := range doc.Skipped {
		if strings.TrimSpace(s.Reason) == "" {
			t.Errorf("skipped symbol %q has no reason", s.GoName)
		}
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Error("manifest does not end with a newline")
	}
}

// TestExportsGenIsUpToDate is the drift check for the committed cgo file.
func TestExportsGenIsUpToDate(t *testing.T) {
	want := mustCgo(t, loadModel(t))

	got, err := os.ReadFile(exportsGenPath)
	if err != nil {
		t.Fatalf("read %s: %v", exportsGenPath, err)
	}
	if !bytes.Equal(got, want) {
		abs, _ := filepath.Abs(exportsGenPath)
		t.Fatalf("%s is stale (%d bytes on disk, %d generated); regenerate it", abs, len(got), len(want))
	}
}

func exportedNames(src string) []string {
	var out []string
	for _, m := range exportName.FindAllStringSubmatch(src, -1) {
		out = append(out, m[1])
	}
	sort.Strings(out)
	return out
}

func prototypeNames(header string) []string {
	var out []string
	for _, line := range strings.Split(header, "\n") {
		if m := protoName.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	sort.Strings(out)
	return out
}

// difference returns the elements of a that are absent from b.
func difference(a, b []string) []string {
	in := make(map[string]bool, len(b))
	for _, s := range b {
		in[s] = true
	}
	var out []string
	for _, s := range a {
		if !in[s] {
			out = append(out, s)
		}
	}
	return out
}

func slicesContains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func grepLine(text, needle string) string {
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, needle) {
			return line
		}
	}
	return "<not found>"
}

func firstLines(data []byte, n int) string {
	lines := strings.SplitN(string(data), "\n", n+1)
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
