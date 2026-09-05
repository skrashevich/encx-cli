package gen

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/bindings/php/internal/surface"
)

// loadFixture writes src as the only Go file of a throwaway package and
// returns its surface model. The go.mod pins the import path so the fixture
// does not depend on where the test's temporary directory happens to live.
func loadFixture(t *testing.T, src string) *surface.Model {
	t.Helper()
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("go.mod", "module trap\n\ngo 1.24\n")
	write("trap.go", "package trap\n\ntype EncClient struct{}\n\n"+src)

	m, err := surface.Load(dir)
	if err != nil {
		t.Fatalf("surface.Load(fixture): %v", err)
	}
	return m
}

// TestCParamCollisionsAreRejected covers the Go signatures whose C wrapper
// would declare the same argument twice. That is a Go compile error the
// generator cannot see — format.Source parses the file and a repeated
// parameter is a type error, not a syntax error — and a silently discarded
// duplicate in FFI::cdef, so both emitters have to refuse the model.
func TestCParamCollisionsAreRejected(t *testing.T) {
	tests := []struct {
		name string
		src  string
		// want are substrings the message must carry: enough to lead the
		// reader to the Go declaration that has to change.
		want []string
	}{
		{
			name: "method parameter named handle",
			src:  "func (c *EncClient) Shadow(handle int64) error { return nil }",
			want: []string{"EncClient.Shadow", `"handle"`, "the client handle every method receives"},
		},
		{
			name: "explicit length after a byte slice",
			src:  "func (c *EncClient) Blob(data []byte, dataLen int64) error { return nil }",
			want: []string{"EncClient.Blob", `"data_len"`, `Go parameter "dataLen"`, `[]byte parameter "data"`},
		},
		{
			name: "explicit length before a byte slice",
			src:  "func (c *EncClient) Blob(dataLen int64, data []byte) error { return nil }",
			want: []string{"EncClient.Blob", `"data_len"`, `the length of []byte parameter "data"`},
		},
		{
			name: "two Go names lowering onto one C name",
			src:  "func (c *EncClient) Fetch(gameID int64, gameId int64) error { return nil }",
			want: []string{"EncClient.Fetch", `"game_id"`, `Go parameter "gameID"`},
		},
		{
			name: "package function whose names collide",
			src:  "func Fetch(gameID int64, gameId int64) error { return nil }",
			want: []string{"Fetch", `"game_id"`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := loadFixture(t, tc.src)

			for _, emitter := range []struct {
				name string
				emit func(*surface.Model) ([]byte, error)
			}{
				{"CgoFile", CgoFile},
				{"HeaderFile", HeaderFile},
				{"ManifestFile", ManifestFile},
			} {
				out, err := emitter.emit(m)
				if emitter.name == "ManifestFile" {
					// The manifest lists Go parameters, not C ones, so it
					// stays renderable; it is here only to show that the
					// two emitters that do produce C are the ones failing.
					if err != nil {
						t.Fatalf("ManifestFile should still render: %v", err)
					}
					continue
				}
				if err == nil {
					t.Fatalf("%s accepted a colliding signature and emitted %d bytes:\n%s",
						emitter.name, len(out), out)
				}
				for _, want := range tc.want {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("%s error does not mention %s: %v", emitter.name, want, err)
					}
				}
			}
		})
	}
}

// TestHandleParamIsFreeForPackageFunctions guards the other side of the check:
// only a method receives the handle argument, so a package-level function may
// still name a parameter "handle".
func TestHandleParamIsFreeForPackageFunctions(t *testing.T) {
	m := loadFixture(t, "func Release(handle int64) error { return nil }")

	src, err := CgoFile(m)
	if err != nil {
		t.Fatalf("CgoFile rejected a package function taking a handle: %v", err)
	}
	if !strings.Contains(string(src), "func encx_release(handle C.longlong)") {
		t.Errorf("the wrapper does not take the parameter as declared:\n%s", src)
	}
	if _, err := HeaderFile(m); err != nil {
		t.Fatalf("HeaderFile rejected a package function taking a handle: %v", err)
	}
}

// TestBlankParamIsNamed pins the emitted spelling of a parameter declared as
// the blank identifier: `C.GoString(_)` is not valid Go, so the model has to
// have named it before it got here.
func TestBlankParamIsNamed(t *testing.T) {
	m := loadFixture(t, "func (c *EncClient) Ignore(_ string, keep int64) error { return nil }")

	src, err := CgoFile(m)
	if err != nil {
		t.Fatalf("CgoFile: %v", err)
	}
	if strings.Contains(string(src), "C.GoString(_)") {
		t.Errorf("the blank identifier reached the wrapper verbatim:\n%s", src)
	}
	if !strings.Contains(string(src), "C.GoString(arg0)") {
		t.Errorf("the blank parameter was not given a usable name:\n%s", src)
	}

	header, err := HeaderFile(m)
	if err != nil {
		t.Fatalf("HeaderFile: %v", err)
	}
	if !strings.Contains(string(header), "char *encx_client_ignore(long long handle, char *arg0, long long keep);") {
		t.Errorf("the header does not declare the blank parameter by its synthesised name:\n%s", header)
	}
}

// TestGeneratedCgoParamsAreUnique is the check TestCgoFileParses cannot make:
// parsing accepts a function that declares the same parameter twice, because
// that is a type error. Comparing names catches it without type checking, and
// therefore without building the dependency tree of the bound package.
func TestGeneratedCgoParamsAreUnique(t *testing.T) {
	data := mustCgo(t, loadModel(t))

	for _, dup := range duplicateParams(t, data) {
		t.Errorf("generated function %s declares the parameter %q twice, which will not compile", dup.fn, dup.param)
	}
}

// TestDuplicateParamsAreDetected shows the check above is not vacuous, by
// running it over a function that does declare a parameter twice.
func TestDuplicateParamsAreDetected(t *testing.T) {
	const src = "package main\n\nfunc encx_client_blob(data *C.char, data_len C.longlong, data_len2 C.longlong) {}\n"

	if got := duplicateParams(t, []byte(src)); len(got) != 0 {
		t.Fatalf("a function with distinct parameter names was reported: %v", got)
	}

	const bad = "package main\n\nfunc encx_client_blob(data *C.char, data_len C.longlong, data_len C.longlong) {}\n"
	got := duplicateParams(t, []byte(bad))
	if len(got) != 1 || got[0].fn != "encx_client_blob" || got[0].param != "data_len" {
		t.Fatalf("duplicateParams found %v, want one report of data_len in encx_client_blob", got)
	}
}

type dupParam struct {
	fn    string
	param string
}

// duplicateParams parses src and reports every parameter name a top-level
// function declares more than once.
func duplicateParams(t *testing.T, src []byte) []dupParam {
	t.Helper()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "exports_gen.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("generated cgo file is not valid Go: %v", err)
	}

	var out []dupParam
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Type.Params == nil {
			continue
		}
		seen := map[string]bool{}
		for _, field := range fn.Type.Params.List {
			for _, name := range field.Names {
				if name.Name == "_" {
					continue
				}
				if seen[name.Name] {
					out = append(out, dupParam{fn: fn.Name.Name, param: name.Name})
					continue
				}
				seen[name.Name] = true
			}
		}
	}
	return out
}

// TestManifestCarriesStructShape is the drift anchor for a JSON result: the
// manifest has to record the fields, not just the struct name, or a renamed
// field leaves every generated file byte-identical.
func TestManifestCarriesStructShape(t *testing.T) {
	m := loadFixture(t, `
type Snapshot struct {
	JSON       string
	EntryCount int64  `+"`json:\"entries,omitempty\"`"+`
	Internal   string `+"`json:\"-\"`"+`
}

func (c *EncClient) Snap() (*Snapshot, error) { return nil, nil }
`)

	got := string(mustManifest(t, m))
	for _, want := range []string{
		`"structRef": "Snapshot"`,
		`"name": "JSON"`,
		`"jsonName": "JSON"`,
		`"name": "EntryCount"`,
		`"jsonName": "entries"`,
		`"omitEmpty": true`,
		`"name": "Internal"`,
		`"jsonName": ""`,
		`"type": "int64"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("manifest does not record %s:\n%s", want, got)
		}
	}
}

// TestManifestShapeReactsToTheStruct is the property the drift check depends
// on: two models that differ only inside the result struct must not produce
// the same manifest.
func TestManifestShapeReactsToTheStruct(t *testing.T) {
	const method = "\n\nfunc (c *EncClient) Snap() (*Snapshot, error) { return nil, nil }\n"

	base := string(mustManifest(t, loadFixture(t,
		"type Snapshot struct {\n\tJSON string\n}"+method)))

	for _, tc := range []struct {
		name string
		src  string
	}{
		{"renamed field", "type Snapshot struct {\n\tDocument string\n}"},
		{"retyped field", "type Snapshot struct {\n\tJSON []string\n}"},
		{"added json tag", "type Snapshot struct {\n\tJSON string `json:\"har\"`\n}"},
		{"added omitempty", "type Snapshot struct {\n\tJSON string `json:\",omitempty\"`\n}"},
		{"dropped from json", "type Snapshot struct {\n\tJSON string `json:\"-\"`\n}"},
		{"added field", "type Snapshot struct {\n\tJSON string\n\tCount int64\n}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := string(mustManifest(t, loadFixture(t, tc.src+method)))
			if got == base {
				t.Errorf("the manifest is unchanged by a %s, so drift would go unnoticed:\n%s", tc.name, got)
			}
		})
	}
}
