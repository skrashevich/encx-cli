package encx

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// parityMatrixPath is the document these tests keep honest.
const parityMatrixPath = "../docs/newengine/parity.md"

// engineIndependentMethods are the exported Client methods that do not go
// through the backend interface, with the reason each one is exempt. A method
// that lands here by accident rather than by design is a silent single-engine
// method, so the list is written out and reviewed rather than inferred.
var engineIndependentMethods = map[string]string{
	"AdminCopyGame": "composed entirely of dispatched methods",
	"AdminWipeGame": "composed entirely of dispatched methods",

	"ExportCookies": "session persistence, engine-aware inside",
	"ImportCookies": "session persistence, engine-aware inside",
	"APIToken":      "new-engine session accessor",
	"SetAPIToken":   "new-engine session accessor",

	"APIBaseURL": "engine selection",
	"Engine":     "engine selection",
	"EngineMode": "engine selection",
	"SetEngine":  "engine selection",

	"AdminDelay":             "ASP form pacing, not applicable to REST",
	"SetAdminDelay":          "ASP form pacing, not applicable to REST",
	"AdminGETDelay":          "ASP form pacing, not applicable to REST",
	"ClearHAR":               "traffic capture, shared by both engines",
	"ClearHARFirst":          "traffic capture, shared by both engines",
	"ExportHARJSON":          "traffic capture, shared by both engines",
	"HAREntryCount":          "traffic capture, shared by both engines",
	"SetHARRecordingEnabled": "traffic capture, shared by both engines",
	"ExportHARSnapshot":      "traffic capture, shared by both engines",

	"LoginForAntiSpamRecovery": "falls through to Login on the new engine",
	"LoginViaLoginPage":        "legacy-only: Login.aspx exists only in the ASP engine",
	"ResolveAntiSpamLoginURL":  "legacy-only: parses NotHumanRequest.aspx",
}

// dispatchedMethods returns the exported Client methods that the backend
// interface routes per engine.
func dispatchedMethods() []string {
	iface := reflect.TypeOf((*backend)(nil)).Elem()
	inBackend := make(map[string]bool, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		inBackend[iface.Method(i).Name] = true
	}

	client := reflect.TypeOf(&Client{})
	names := make([]string, 0, client.NumMethod())
	for i := 0; i < client.NumMethod(); i++ {
		if name := client.Method(i).Name; inBackend[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// TestEveryExportedMethodPicksAnEngine is the guard against a method quietly
// staying on one engine: an exported Client method is either dispatched through
// the backend interface or listed as engine-independent with a reason.
func TestEveryExportedMethodPicksAnEngine(t *testing.T) {
	iface := reflect.TypeOf((*backend)(nil)).Elem()
	inBackend := make(map[string]bool, iface.NumMethod())
	for i := 0; i < iface.NumMethod(); i++ {
		inBackend[iface.Method(i).Name] = true
	}

	client := reflect.TypeOf(&Client{})
	for i := 0; i < client.NumMethod(); i++ {
		name := client.Method(i).Name
		if inBackend[name] {
			continue
		}
		if _, ok := engineIndependentMethods[name]; !ok {
			t.Errorf("Client.%s is neither dispatched through backend nor listed in "+
				"engineIndependentMethods; it would silently run the legacy implementation "+
				"on the new engine", name)
		}
	}
}

// TestEngineIndependentListHasNoStaleEntries keeps the exemption list from
// outliving the methods it excuses.
func TestEngineIndependentListHasNoStaleEntries(t *testing.T) {
	client := reflect.TypeOf(&Client{})
	for name := range engineIndependentMethods {
		if _, ok := client.MethodByName(name); !ok {
			t.Errorf("engineIndependentMethods lists %q, which Client no longer has", name)
		}
	}
}

// TestBothEnginesImplementTheWholeContract restates at runtime what the
// compile-time assertions in backend.go already require, so a reader of this
// test sees the invariant spelled out.
func TestBothEnginesImplementTheWholeContract(t *testing.T) {
	iface := reflect.TypeOf((*backend)(nil)).Elem()
	for _, impl := range []reflect.Type{
		reflect.TypeOf((*legacyEngine)(nil)),
		reflect.TypeOf((*newEngine)(nil)),
	} {
		if !impl.Implements(iface) {
			t.Errorf("%s does not implement backend", impl)
		}
	}
	if iface.NumMethod() == 0 {
		t.Fatal("the backend interface is empty")
	}
}

var parityMethodRe = regexp.MustCompile("^\\| `([A-Za-z]+)` \\|")

// parityMatrixMethods reads the method names the matrix documents.
func parityMatrixMethods(t *testing.T) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(parityMatrixPath)
	if err != nil {
		t.Fatalf("read %s: %v", parityMatrixPath, err)
	}
	names := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		if m := parityMethodRe.FindStringSubmatch(line); m != nil {
			names[m[1]] = true
		}
	}
	return names
}

// TestEngineParityMatrixCoversEveryMethod is the reason the matrix can be
// trusted: documentation that drifts from the code fails the build.
func TestEngineParityMatrixCoversEveryMethod(t *testing.T) {
	documented := parityMatrixMethods(t)
	if len(documented) == 0 {
		t.Fatalf("%s documents no methods", parityMatrixPath)
	}

	for _, name := range dispatchedMethods() {
		if !documented[name] {
			t.Errorf("Client.%s is dispatched per engine but missing from %s",
				name, parityMatrixPath)
		}
	}
	for name := range engineIndependentMethods {
		if !documented[name] {
			t.Errorf("Client.%s is engine-independent but missing from %s",
				name, parityMatrixPath)
		}
	}
}

// TestEngineParityMatrixHasNoPhantomMethods catches rows left behind by a
// rename or a removal.
func TestEngineParityMatrixHasNoPhantomMethods(t *testing.T) {
	client := reflect.TypeOf(&Client{})
	for name := range parityMatrixMethods(t) {
		if _, ok := client.MethodByName(name); !ok {
			t.Errorf("%s documents Client.%s, which does not exist", parityMatrixPath, name)
		}
	}
}

var parityRowRe = regexp.MustCompile("^\\| `([A-Za-z]+)` \\| ([^|]*) \\| ([^|]*) \\|")

// TestEngineParityMatrixDescribesBothEngines guards the content of the table,
// not only the method names: a row that leaves a column blank, or repeats the
// same cell twice, documents nothing while looking complete.
func TestEngineParityMatrixDescribesBothEngines(t *testing.T) {
	data, err := os.ReadFile(parityMatrixPath)
	if err != nil {
		t.Fatalf("read %s: %v", parityMatrixPath, err)
	}

	described := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		m := parityRowRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name, legacy, modern := m[1], strings.TrimSpace(m[2]), strings.TrimSpace(m[3])
		described[name] = true
		if legacy == "" || modern == "" {
			t.Errorf("%s: a column is empty", name)
		}
		if legacy == modern {
			t.Errorf("%s: both columns say %q, so the row explains nothing", name, legacy)
		}
	}

	// Rows in the engine-independent table have two columns, so only the
	// dispatched methods are expected here.
	for _, name := range dispatchedMethods() {
		if !described[name] {
			t.Errorf("Client.%s has no three-column row in %s", name, parityMatrixPath)
		}
	}
}
