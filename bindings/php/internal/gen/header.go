package gen

import (
	"fmt"
	"sort"
	"strings"

	"github.com/skrashevich/encx-cli/bindings/php/internal/surface"
)

// runtimeFunc is an export that is written by hand in bindings/php/cshared/runtime.go
// and therefore never emitted into the cgo file — but it is part of the C ABI,
// so it still has to reach the header and the manifest.
type runtimeFunc struct {
	CName     string
	Prototype string
	Doc       []string
}

// runtimeFuncs are the two fixed entry points every binding depends on.
var runtimeFuncs = []runtimeFunc{
	{
		CName:     "encx_client_free",
		Prototype: "char *encx_client_free(long long handle);",
		Doc: []string{
			"Releases the client behind handle and returns an envelope. Handles are",
			"never reused, so freeing an already freed handle fails with an error",
			"envelope instead of releasing an unrelated client.",
		},
	},
	{
		CName:     "encx_string_free",
		Prototype: "void encx_string_free(char *s);",
		Doc: []string{
			"Frees a string returned by any other encx_ function. Passing a null",
			"pointer is a no-op.",
		},
	},
}

// runtimeExports returns the fixed runtime functions sorted by C symbol name.
func runtimeExports() []runtimeFunc {
	out := append([]runtimeFunc(nil), runtimeFuncs...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].CName < out[j].CName })
	return out
}

// headerPreamble explains the constraints the file is written under. PHP parses
// it with FFI::cdef, which runs no preprocessor, so the file must contain no
// directives at all -- not even a comment may carry the directive character.
var headerPreamble = []string{
	"",
	"C ABI of the encx PHP bindings, read by PHP through FFI::cdef.",
	"",
	"FFI::cdef runs no preprocessor and understands no typedefs it was not given,",
	"so this file holds nothing but prototypes and comments, and uses no type",
	"beyond char, int, long long, void and pointers to them.",
	"",
	"Every function returning char * hands ownership of that string to the caller:",
	"it is a malloc'd JSON envelope that must be released with encx_string_free.",
	"Methods take the opaque client handle produced by a constructor as their",
	"first argument; the handle is released with encx_client_free.",
}

// HeaderFile returns the contents of bindings/php/encx.h.
func HeaderFile(m *surface.Model) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("gen: nil model")
	}

	var b strings.Builder
	b.WriteString(marker + "\n")
	for _, line := range headerPreamble {
		writeCComment(&b, line)
	}

	for _, rf := range runtimeExports() {
		b.WriteString("\n")
		for _, line := range rf.Doc {
			writeCComment(&b, line)
		}
		b.WriteString(rf.Prototype + "\n")
	}

	sections := [][]surface.Func{m.Constructors, m.Methods, m.Functions}
	groups := []group{groupConstructor, groupMethod, groupFunction}
	for i, section := range sections {
		funcs := append([]surface.Func(nil), section...)
		sort.SliceStable(funcs, func(a, c int) bool { return funcs[a].CName < funcs[c].CName })
		for _, fn := range funcs {
			b.WriteString("\n")
			writeCDoc(&b, fn.Doc)
			proto, err := prototype(bound{fn: fn, group: groups[i]})
			if err != nil {
				return nil, err
			}
			b.WriteString(proto + "\n")
		}
	}

	return []byte(b.String()), nil
}

// writeCDoc carries a Go doc comment over to the header as // comment lines.
func writeCDoc(b *strings.Builder, doc string) {
	if strings.TrimSpace(doc) == "" {
		return
	}
	for _, line := range strings.Split(doc, "\n") {
		writeCComment(b, strings.TrimRight(line, " \t"))
	}
}

// writeCComment emits one comment line, dropping any character FFI::cdef would
// read as the start of a preprocessor directive.
func writeCComment(b *strings.Builder, line string) {
	line = strings.ReplaceAll(line, "#", "")
	if line == "" {
		b.WriteString("//\n")
		return
	}
	b.WriteString("// " + line + "\n")
}

// prototype renders the C declaration of one bound symbol.
func prototype(bd bound) (string, error) {
	params, err := cParams(bd)
	if err != nil {
		return "", err
	}
	if len(params) == 0 {
		params = []string{"void"}
	}
	return "char *" + bd.fn.CName + "(" + strings.Join(params, ", ") + ");", nil
}

// cParams renders the C parameter list, expanding []byte into a pointer and a
// length and prepending the client handle for methods.
func cParams(bd bound) ([]string, error) {
	var params []string
	if bd.isMethod() {
		params = append(params, "long long "+handleParam)
	}
	for _, p := range bd.fn.Params {
		cn := surface.SnakeCase(p.Name)
		switch p.Type {
		case surface.TypeString:
			params = append(params, "char *"+cn)
		case surface.TypeInt64:
			params = append(params, "long long "+cn)
		case surface.TypeBool:
			params = append(params, "int "+cn)
		case surface.TypeBytes:
			params = append(params, "char *"+cn, "long long "+cn+"_len")
		default:
			return nil, fmt.Errorf("gen: %s: parameter %q has type %s, which has no C representation", bd.fn.CName, p.Name, p.Type)
		}
	}
	return params, nil
}
