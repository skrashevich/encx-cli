// Package surface models the exported surface of the encxmobile Go package as
// it can be projected onto a C ABI.
//
// It answers one question for every exported function and method: can this
// symbol be reached from PHP through a c-shared library and FFI, and if not,
// why not. The answer is a value, not a side effect — nothing is written and
// nothing is logged, so the PHP binding generator can diff two models byte for
// byte. Every list is sorted and no map ever reaches the output.
//
// The model is built by parsing the package with go/parser. No type checking
// happens, which keeps the loader free of the dependency tree the target
// package pulls in.
package surface

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// handleTypeName is the Go struct projected onto an opaque C handle.
const handleTypeName = "EncClient"

// Type is a parameter or result value type that can cross the C ABI.
type Type int

const (
	// TypeVoid marks the absence of a value.
	TypeVoid Type = iota
	TypeString
	TypeInt64
	TypeBool
	TypeBytes
)

// String returns the Go spelling of the type.
func (t Type) String() string {
	switch t {
	case TypeString:
		return "string"
	case TypeInt64:
		return "int64"
	case TypeBool:
		return "bool"
	case TypeBytes:
		return "[]byte"
	default:
		return "void"
	}
}

// ResultKind classifies the shape of a function's return values.
type ResultKind int

const (
	// KindVoid is a function returning nothing.
	KindVoid ResultKind = iota
	// KindError is a function returning only error.
	KindError
	// KindValue is a function returning one bindable value and no error.
	KindValue
	// KindValueErr is a function returning (T, error) with T bindable.
	KindValueErr
	// KindJSONStruct is a function returning (*S, error) with S a
	// JSON-serializable struct of this package.
	KindJSONStruct
	// KindHandle is a function returning the opaque client handle.
	KindHandle
)

// String returns a stable identifier for the kind.
func (k ResultKind) String() string {
	switch k {
	case KindError:
		return "error"
	case KindValue:
		return "value"
	case KindValueErr:
		return "value_err"
	case KindJSONStruct:
		return "json_struct"
	case KindHandle:
		return "handle"
	default:
		return "void"
	}
}

// Param is one bindable function parameter.
type Param struct {
	Name string
	Type Type
}

// Result describes what a bindable function returns.
type Result struct {
	Kind ResultKind
	// Type is the value type, or TypeVoid for kinds carrying no value.
	Type Type
	// StructRef names the struct for KindJSONStruct and is empty otherwise.
	StructRef string
}

// Func is one bindable symbol.
type Func struct {
	// Name is the Go identifier, without the receiver.
	Name string
	// CName is the exported C symbol.
	CName string
	// Doc is the Go doc comment with the comment markers stripped.
	Doc    string
	Params []Param
	Result Result
}

// Skipped is an exported symbol that cannot be bound, with the reason why.
type Skipped struct {
	// Name is the Go identifier, prefixed with the receiver type for methods.
	Name string
	// Reason explains the rejection and is never empty.
	Reason string
}

// Model is the full exported surface of one package.
type Model struct {
	Package    string
	ImportPath string
	// Constructors are package functions returning the client handle.
	Constructors []Func
	// Methods have a *EncClient receiver.
	Methods []Func
	// Functions are the remaining bindable package-level functions.
	Functions []Func
	// Skipped holds every exported symbol that gets no C symbol.
	Skipped []Skipped
}

// Bindable returns every symbol that gets a C symbol, sorted by C name.
func (m *Model) Bindable() []Func {
	all := make([]Func, 0, len(m.Constructors)+len(m.Methods)+len(m.Functions))
	all = append(all, m.Constructors...)
	all = append(all, m.Methods...)
	all = append(all, m.Functions...)
	sort.Slice(all, func(i, j int) bool { return all[i].CName < all[j].CName })
	return all
}

// Load parses the Go package in dir and returns its binding surface.
// Test files are ignored; every other exported function or method ends up
// either in a bindable list or in Skipped.
func Load(dir string) (*Model, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("surface: resolve %s: %w", dir, err)
	}

	files, pkgName, err := parsePackage(abs)
	if err != nil {
		return nil, err
	}

	importPath, err := importPathFor(abs, pkgName)
	if err != nil {
		return nil, err
	}

	l := &loader{
		model:   &Model{Package: pkgName, ImportPath: importPath},
		structs: collectStructs(files),
	}
	for _, f := range files {
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() || !hasExportedReceiver(fn) {
				continue
			}
			l.add(fn)
		}
	}
	l.sortAll()
	return l.model, nil
}

// parsePackage reads every non-test Go file of dir, in sorted file order.
func parsePackage(dir string) ([]*ast.File, string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", fmt.Errorf("surface: read %s: %w", dir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, "", fmt.Errorf("surface: no Go files in %s", dir)
	}

	fset := token.NewFileSet()
	var (
		files   []*ast.File
		pkgName string
	)
	for _, name := range names {
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, "", fmt.Errorf("surface: parse %s: %w", name, err)
		}
		if pkgName == "" {
			pkgName = f.Name.Name
		} else if f.Name.Name != pkgName {
			return nil, "", fmt.Errorf("surface: %s declares package %s, expected %s", name, f.Name.Name, pkgName)
		}
		files = append(files, f)
	}
	return files, pkgName, nil
}

// importPathFor derives the package import path from the nearest go.mod.
func importPathFor(dir, pkgName string) (string, error) {
	root := dir
	for {
		data, err := os.ReadFile(filepath.Join(root, "go.mod"))
		if err == nil {
			module, ok := modulePath(string(data))
			if !ok {
				return "", fmt.Errorf("surface: no module directive in %s/go.mod", root)
			}
			rel, err := filepath.Rel(root, dir)
			if err != nil {
				return "", fmt.Errorf("surface: relate %s to %s: %w", dir, root, err)
			}
			if rel == "." {
				return module, nil
			}
			return path.Join(module, filepath.ToSlash(rel)), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("surface: read go.mod near %s: %w", dir, err)
		}
		parent := filepath.Dir(root)
		if parent == root {
			return pkgName, nil
		}
		root = parent
	}
}

func modulePath(gomod string) (string, bool) {
	for _, line := range strings.Split(gomod, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "module")
		if !ok || (rest != "" && !isSpace(rest[0])) {
			continue
		}
		if p := strings.Trim(strings.TrimSpace(rest), `"`); p != "" {
			return p, true
		}
	}
	return "", false
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' }

// collectStructs indexes the struct types declared in the package so that
// (*S, error) results can be validated against them.
func collectStructs(files []*ast.File) map[string]*ast.StructType {
	structs := map[string]*ast.StructType{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if st, ok := ts.Type.(*ast.StructType); ok {
					structs[ts.Name.Name] = st
				}
			}
		}
	}
	return structs
}

type loader struct {
	model   *Model
	structs map[string]*ast.StructType
}

func (l *loader) skip(name, reason string) {
	l.model.Skipped = append(l.model.Skipped, Skipped{Name: name, Reason: reason})
}

// add classifies one exported declaration into the model.
func (l *loader) add(fn *ast.FuncDecl) {
	recv, isMethod := receiverType(fn)
	name := fn.Name.Name
	if isMethod {
		name = recv + "." + name
	}

	if fn.Type.TypeParams != nil {
		l.skip(name, "generic functions have no C ABI representation")
		return
	}
	if isMethod && recv != handleTypeName {
		l.skip(name, fmt.Sprintf("method receiver is %s, not *%s; only the client handle is bound", receiverString(fn), handleTypeName))
		return
	}

	params, err := l.params(fn)
	if err != nil {
		l.skip(name, err.Error())
		return
	}
	result, err := l.result(fn)
	if err != nil {
		l.skip(name, err.Error())
		return
	}

	bound := Func{
		Name:   fn.Name.Name,
		Doc:    strings.TrimSpace(fn.Doc.Text()),
		Params: params,
		Result: result,
	}
	switch {
	case isMethod:
		bound.CName = MethodCName(bound.Name)
		l.model.Methods = append(l.model.Methods, bound)
	case result.Kind == KindHandle:
		bound.CName = FuncCName(bound.Name)
		l.model.Constructors = append(l.model.Constructors, bound)
	default:
		bound.CName = FuncCName(bound.Name)
		l.model.Functions = append(l.model.Functions, bound)
	}
}

// hasExportedReceiver reports whether fn is reachable from outside the package.
// A method on an unexported type is not part of the exported surface even when
// the method name is capitalised, so it is ignored rather than reported as
// skipped: a caller could never have bound it in the first place.
func hasExportedReceiver(fn *ast.FuncDecl) bool {
	recv, isMethod := receiverType(fn)
	if !isMethod {
		return true
	}
	return ast.IsExported(recv)
}

// receiverType returns the receiver's type name with any pointer stripped.
func receiverType(fn *ast.FuncDecl) (string, bool) {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return "", false
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name, true
	}
	return types.ExprString(expr), true
}

func receiverString(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return ""
	}
	return types.ExprString(fn.Recv.List[0].Type)
}

// params flattens the parameter list, rejecting anything the C ABI cannot carry.
func (l *loader) params(fn *ast.FuncDecl) ([]Param, error) {
	if fn.Type.Params == nil {
		return nil, nil
	}
	var out []Param
	index := 0
	for _, field := range fn.Type.Params.List {
		names := fieldNames(field, "arg", &index)
		if _, ok := field.Type.(*ast.Ellipsis); ok {
			return nil, fmt.Errorf("parameter %q is variadic (%s), which cannot cross the C ABI",
				names[0], types.ExprString(field.Type))
		}
		t, ok := bindableType(field.Type)
		if !ok {
			return nil, unbindableParam(names[0], field.Type)
		}
		for _, n := range names {
			out = append(out, Param{Name: n, Type: t})
		}
	}
	return out, nil
}

func unbindableParam(name string, expr ast.Expr) error {
	spelled := types.ExprString(expr)
	if spelled == "error" {
		return fmt.Errorf("parameter %q has type error, which cannot cross the C ABI: a Go error is an interface value with no C representation", name)
	}
	return fmt.Errorf("parameter %q has unsupported type %s; only string, int64, bool and []byte cross the C ABI", name, spelled)
}

// result classifies the return values.
func (l *loader) result(fn *ast.FuncDecl) (Result, error) {
	results := flattenResults(fn.Type.Results)
	switch len(results) {
	case 0:
		return Result{Kind: KindVoid, Type: TypeVoid}, nil
	case 1:
		if isError(results[0]) {
			return Result{Kind: KindError, Type: TypeVoid}, nil
		}
		if isHandle(results[0]) {
			return Result{Kind: KindHandle, Type: TypeVoid}, nil
		}
		if t, ok := bindableType(results[0]); ok {
			return Result{Kind: KindValue, Type: t}, nil
		}
		return Result{}, unsupportedResult(results)
	case 2:
		if !isError(results[1]) {
			return Result{}, unsupportedResult(results)
		}
		if isHandle(results[0]) {
			return Result{Kind: KindHandle, Type: TypeVoid}, nil
		}
		if t, ok := bindableType(results[0]); ok {
			return Result{Kind: KindValueErr, Type: t}, nil
		}
		name, ok := pointerToLocalStruct(results[0])
		if !ok {
			return Result{}, unsupportedResult(results)
		}
		st, declared := l.structs[name]
		if !declared {
			return Result{}, fmt.Errorf("result *%s is not a struct declared in this package, so it cannot be marshalled to JSON", name)
		}
		if err := jsonBindableStruct(name, st); err != nil {
			return Result{}, err
		}
		return Result{Kind: KindJSONStruct, Type: TypeVoid, StructRef: name}, nil
	default:
		return Result{}, unsupportedResult(results)
	}
}

func unsupportedResult(results []ast.Expr) error {
	spelled := make([]string, len(results))
	for i, r := range results {
		spelled[i] = types.ExprString(r)
	}
	return fmt.Errorf("unsupported result signature (%s); expected none, error, T, (T, error) with T in {string, int64, bool, []byte}, (*Struct, error), or *%s",
		strings.Join(spelled, ", "), handleTypeName)
}

// flattenResults expands named result groups into one expression per value.
func flattenResults(list *ast.FieldList) []ast.Expr {
	if list == nil {
		return nil
	}
	var out []ast.Expr
	for _, field := range list.List {
		n := len(field.Names)
		if n == 0 {
			n = 1
		}
		for range n {
			out = append(out, field.Type)
		}
	}
	return out
}

// fieldNames returns one name per declared value, synthesising names for
// unnamed parameters so a reason string can always point at a position.
func fieldNames(field *ast.Field, prefix string, index *int) []string {
	if len(field.Names) == 0 {
		name := fmt.Sprintf("%s%d", prefix, *index)
		*index++
		return []string{name}
	}
	names := make([]string, 0, len(field.Names))
	for _, n := range field.Names {
		names = append(names, n.Name)
		*index++
	}
	return names
}

func bindableType(expr ast.Expr) (Type, bool) {
	switch t := expr.(type) {
	case *ast.Ident:
		switch t.Name {
		case "string":
			return TypeString, true
		case "int64":
			return TypeInt64, true
		case "bool":
			return TypeBool, true
		}
	case *ast.ArrayType:
		if t.Len != nil {
			return TypeVoid, false
		}
		if elem, ok := t.Elt.(*ast.Ident); ok && (elem.Name == "byte" || elem.Name == "uint8") {
			return TypeBytes, true
		}
	}
	return TypeVoid, false
}

func isError(expr ast.Expr) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == "error"
}

func isHandle(expr ast.Expr) bool {
	name, ok := pointerToLocalStruct(expr)
	return ok && name == handleTypeName
}

// pointerToLocalStruct reports the type name behind *T when T is a bare
// identifier, i.e. a type of the package under analysis.
func pointerToLocalStruct(expr ast.Expr) (string, bool) {
	star, ok := expr.(*ast.StarExpr)
	if !ok {
		return "", false
	}
	ident, ok := star.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// jsonBindableStruct checks that every field of st survives a JSON round trip
// through the C ABI: exported, and of a scalar type or a slice/map of them.
func jsonBindableStruct(name string, st *ast.StructType) error {
	if st.Fields == nil || len(st.Fields.List) == 0 {
		return fmt.Errorf("result struct %s has no fields to marshal", name)
	}
	for _, field := range st.Fields.List {
		if len(field.Names) == 0 {
			return fmt.Errorf("result struct %s embeds %s; embedded fields are not bound", name, types.ExprString(field.Type))
		}
		for _, fieldName := range field.Names {
			if !fieldName.IsExported() {
				return fmt.Errorf("result struct %s has unexported field %q, which JSON cannot carry", name, fieldName.Name)
			}
		}
		if !jsonBasicType(field.Type) {
			return fmt.Errorf("result struct %s field %q has type %s, which is not a basic JSON type",
				name, field.Names[0].Name, types.ExprString(field.Type))
		}
	}
	return nil
}

func jsonBasicType(expr ast.Expr) bool {
	switch t := expr.(type) {
	case *ast.Ident:
		switch t.Name {
		case "string", "int", "int64", "bool", "float64", "byte", "uint8":
			return true
		}
	case *ast.ArrayType:
		return t.Len == nil && jsonBasicType(t.Elt)
	case *ast.MapType:
		return jsonBasicType(t.Key) && jsonBasicType(t.Value)
	}
	return false
}

// sortAll makes the model order independent of file and declaration order.
func (l *loader) sortAll() {
	byName := func(s []Func) {
		sort.SliceStable(s, func(i, j int) bool { return s[i].Name < s[j].Name })
	}
	byName(l.model.Constructors)
	byName(l.model.Methods)
	byName(l.model.Functions)
	sort.SliceStable(l.model.Skipped, func(i, j int) bool { return l.model.Skipped[i].Name < l.model.Skipped[j].Name })
}
