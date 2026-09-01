package surface

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// mobilePkgDir is the package this model is built for.
const mobilePkgDir = "../../../../mobile/encxmobile"

func load(t *testing.T) *Model {
	t.Helper()
	m, err := Load(mobilePkgDir)
	if err != nil {
		t.Fatalf("Load(%s): %v", mobilePkgDir, err)
	}
	return m
}

func TestLoadPackageIdentity(t *testing.T) {
	m := load(t)
	if m.Package != "encxmobile" {
		t.Errorf("Package = %q, want encxmobile", m.Package)
	}
	want := "github.com/skrashevich/encx-cli/mobile/encxmobile"
	if m.ImportPath != want {
		t.Errorf("ImportPath = %q, want %q", m.ImportPath, want)
	}
}

// countExportedFuncDecls counts every exported func declaration of the package
// independently of the loader, so the accounting test cannot drift with it.
// countExportedSurfaceDecls counts the declarations a caller outside the package
// could reach: exported functions, and exported methods on exported types. A
// capitalised method on an unexported type is unreachable, so it is not part of
// the surface the model has to account for.
func countExportedSurfaceDecls(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(mobilePkgDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	fset := token.NewFileSet()
	total := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(mobilePkgDir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || !fn.Name.IsExported() {
				continue
			}
			if recv, isMethod := receiverType(fn); isMethod && !ast.IsExported(recv) {
				continue
			}
			total++
		}
	}
	if total == 0 {
		t.Fatal("found no exported functions; the package path is probably wrong")
	}
	return total
}

func TestEveryExportedSymbolIsAccountedFor(t *testing.T) {
	m := load(t)
	got := len(m.Constructors) + len(m.Methods) + len(m.Functions) + len(m.Skipped)
	if want := countExportedSurfaceDecls(t); got != want {
		t.Errorf("model covers %d exported symbols, package declares %d", got, want)
	}
}

func TestSkippedReasonsAreNonEmpty(t *testing.T) {
	m := load(t)
	for _, s := range m.Skipped {
		if strings.TrimSpace(s.Reason) == "" {
			t.Errorf("Skipped %q has an empty reason", s.Name)
		}
	}
}

func methodByName(t *testing.T, m *Model, name string) Func {
	t.Helper()
	for _, f := range m.Methods {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("method %s not found in Methods; skipped: %v", name, skippedReason(m, "EncClient."+name))
	return Func{}
}

func funcByName(t *testing.T, list []Func, name string) Func {
	t.Helper()
	for _, f := range list {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("function %s not found", name)
	return Func{}
}

func skippedReason(m *Model, name string) string {
	for _, s := range m.Skipped {
		if s.Name == name {
			return s.Reason
		}
	}
	return ""
}

func TestMethodResults(t *testing.T) {
	m := load(t)
	tests := []struct {
		method string
		kind   ResultKind
		typ    Type
	}{
		{"GetGameModel", KindValueErr, TypeString},
		{"ExportCookies", KindValueErr, TypeBytes},
		{"ImportCookies", KindError, TypeVoid},
		{"GetTimeoutToGame", KindValueErr, TypeInt64},
		{"Domain", KindValue, TypeString},
		{"Engine", KindValue, TypeString},
		{"APIBaseURL", KindValue, TypeString},
		{"HAREntryCount", KindValue, TypeInt64},
		{"ClearHAR", KindVoid, TypeVoid},
		{"SetEngine", KindVoid, TypeVoid},
		{"SetHARRecordingEnabled", KindVoid, TypeVoid},
		{"ExportHAR", KindValueErr, TypeString},
	}
	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			f := methodByName(t, m, tc.method)
			if f.Result.Kind != tc.kind {
				t.Errorf("Result.Kind = %v, want %v", f.Result.Kind, tc.kind)
			}
			if f.Result.Type != tc.typ {
				t.Errorf("Result.Type = %v, want %v", f.Result.Type, tc.typ)
			}
		})
	}
}

func TestGetGameModelParams(t *testing.T) {
	f := methodByName(t, load(t), "GetGameModel")
	if len(f.Params) != 1 {
		t.Fatalf("Params = %v, want exactly one", f.Params)
	}
	if f.Params[0].Type != TypeInt64 {
		t.Errorf("param type = %v, want int64", f.Params[0].Type)
	}
	if f.Params[0].Name != "gameID" {
		t.Errorf("param name = %q, want gameID", f.Params[0].Name)
	}
	if f.CName != "encx_client_get_game_model" {
		t.Errorf("CName = %q", f.CName)
	}
	if !strings.HasPrefix(f.Doc, "GetGameModel returns") {
		t.Errorf("Doc = %q, want the Go doc comment with markers stripped", f.Doc)
	}
}

func TestImportCookiesTakesBytes(t *testing.T) {
	f := methodByName(t, load(t), "ImportCookies")
	if len(f.Params) != 1 || f.Params[0].Type != TypeBytes {
		t.Fatalf("Params = %v, want a single []byte", f.Params)
	}
}

func TestGroupedParamsAreExpanded(t *testing.T) {
	f := methodByName(t, load(t), "SendCode")
	want := []Param{
		{Name: "gameID", Type: TypeInt64},
		{Name: "levelID", Type: TypeInt64},
		{Name: "levelNumber", Type: TypeInt64},
		{Name: "code", Type: TypeString},
	}
	if !reflect.DeepEqual(f.Params, want) {
		t.Errorf("Params = %v, want %v", f.Params, want)
	}
}

func TestConstructors(t *testing.T) {
	m := load(t)
	names := make([]string, 0, len(m.Constructors))
	for _, f := range m.Constructors {
		names = append(names, f.Name)
		if f.Result.Kind != KindHandle {
			t.Errorf("%s: Result.Kind = %v, want KindHandle", f.Name, f.Result.Kind)
		}
	}
	want := []string{"NewClient", "NewClientWithOptions"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("Constructors = %v, want %v", names, want)
	}
	if c := funcByName(t, m.Constructors, "NewClient"); c.CName != "encx_new_client" {
		t.Errorf("NewClient CName = %q", c.CName)
	}
	if c := funcByName(t, m.Constructors, "NewClientWithOptions"); c.CName != "encx_new_client_with_options" {
		t.Errorf("NewClientWithOptions CName = %q", c.CName)
	}
}

func TestExportHARSnapshotIsJSONStruct(t *testing.T) {
	f := methodByName(t, load(t), "ExportHARSnapshot")
	if f.Result.Kind != KindJSONStruct {
		t.Fatalf("Result.Kind = %v, want KindJSONStruct", f.Result.Kind)
	}
	if f.Result.StructRef != "HARSnapshot" {
		t.Errorf("StructRef = %q, want HARSnapshot", f.Result.StructRef)
	}
	if f.CName != "encx_client_export_har_snapshot" {
		t.Errorf("CName = %q", f.CName)
	}
}

func TestPackageFunctions(t *testing.T) {
	m := load(t)
	f := funcByName(t, m.Functions, "LoginErrorText")
	if f.CName != "encx_login_error_text" {
		t.Errorf("CName = %q", f.CName)
	}
	if f.Result.Kind != KindValue || f.Result.Type != TypeString {
		t.Errorf("LoginErrorText result = %v/%v", f.Result.Kind, f.Result.Type)
	}
	p := funcByName(t, m.Functions, "ParseTeamLinks")
	if p.CName != "encx_parse_team_links" {
		t.Errorf("CName = %q", p.CName)
	}
	if p.Result.Kind != KindValueErr || p.Result.Type != TypeString {
		t.Errorf("ParseTeamLinks result = %v/%v", p.Result.Kind, p.Result.Type)
	}
}

func TestErrorTakingFunctionsAreSkipped(t *testing.T) {
	m := load(t)
	for _, name := range []string{"IsAntiSpamError", "AntiSpamURLFromError", "IsUndecodableAcceptedError"} {
		reason := skippedReason(m, name)
		if reason == "" {
			t.Errorf("%s: expected a skip reason, got none", name)
			continue
		}
		if !strings.Contains(reason, "error") {
			t.Errorf("%s: reason %q does not mention the error type", name, reason)
		}
	}
}

func TestForeignReceiversAreSkipped(t *testing.T) {
	m := load(t)
	for _, name := range []string{"AgentSession.SendMessage", "CodexDeviceLogin.Poll"} {
		reason := skippedReason(m, name)
		if reason == "" {
			t.Fatalf("%s: expected a skip reason, got none", name)
		}
		if !strings.Contains(reason, "EncClient") {
			t.Errorf("%s: reason %q should name the expected receiver", name, reason)
		}
	}
}

func TestNonBindableResultsAreSkipped(t *testing.T) {
	m := load(t)
	// A method on the handle whose result is an opaque session pointer.
	if reason := skippedReason(m, "EncClient.NewAgentSession"); reason == "" {
		t.Error("EncClient.NewAgentSession: expected a skip reason, got none")
	}
	// A constructor for a type that is not the client handle.
	if reason := skippedReason(m, "StartCodexDeviceLogin"); reason == "" {
		t.Error("StartCodexDeviceLogin: expected a skip reason, got none")
	}
}

func TestCNamesAreUnique(t *testing.T) {
	m := load(t)
	seen := map[string]string{}
	for _, f := range m.Bindable() {
		if f.CName == "" {
			t.Errorf("%s has an empty CName", f.Name)
			continue
		}
		if prev, dup := seen[f.CName]; dup {
			t.Errorf("CName %q is used by both %s and %s", f.CName, prev, f.Name)
			continue
		}
		seen[f.CName] = f.Name
	}
}

func TestSnakeCase(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"GetGameModel", "get_game_model"},
		{"GetGameModelLevel", "get_game_model_level"},
		{"ExportHAR", "export_har"},
		{"APIBaseURL", "api_base_url"},
		{"SetHARRecordingEnabled", "set_har_recording_enabled"},
		{"HAREntryCount", "har_entry_count"},
		{"SetGameRequestMinIntervalMillis", "set_game_request_min_interval_millis"},
		{"ClearHARFirst", "clear_har_first"},
		{"ExportHARSnapshot", "export_har_snapshot"},
		{"ParseTeamLinks", "parse_team_links"},
		{"NewClient", "new_client"},
		{"NewClientWithOptions", "new_client_with_options"},
		{"LoginErrorText", "login_error_text"},
		{"Domain", "domain"},
		{"Engine", "engine"},
		{"ClearHAR", "clear_har"},
		{"ImportCookies", "import_cookies"},
	}
	for _, tc := range tests {
		if got := SnakeCase(tc.in); got != tc.want {
			t.Errorf("SnakeCase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCNamePrefixes(t *testing.T) {
	if got := FuncCName("ParseTeamLinks"); got != "encx_parse_team_links" {
		t.Errorf("FuncCName = %q", got)
	}
	if got := MethodCName("GetGameModel"); got != "encx_client_get_game_model" {
		t.Errorf("MethodCName = %q", got)
	}
}

func TestModelIsSorted(t *testing.T) {
	m := load(t)
	assertSorted := func(label string, names []string) {
		if !sort.StringsAreSorted(names) {
			t.Errorf("%s is not sorted: %v", label, names)
		}
	}
	collect := func(fs []Func) []string {
		names := make([]string, len(fs))
		for i, f := range fs {
			names[i] = f.Name
		}
		return names
	}
	assertSorted("Constructors", collect(m.Constructors))
	assertSorted("Methods", collect(m.Methods))
	assertSorted("Functions", collect(m.Functions))
	skipped := make([]string, len(m.Skipped))
	for i, s := range m.Skipped {
		skipped[i] = s.Name
	}
	assertSorted("Skipped", skipped)
}

func TestLoadIsDeterministic(t *testing.T) {
	first, err := Load(mobilePkgDir)
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	second, err := Load(mobilePkgDir)
	if err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Error("two Load calls produced different models")
	}
}

func TestLoadRejectsMissingDirectory(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("Load on a missing directory should fail")
	}
}
