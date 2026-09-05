package genphp

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/bindings/php/internal/surface"
)

// update rewrites the committed PHP files instead of comparing against them,
// so that a change to the emitter is applied with `go test -update`.
var update = flag.Bool("update", false, "rewrite bindings/php/src/Encx/*.php from the emitters")

// mobileDir is the Go package the bindings project onto PHP.
const mobileDir = "../../../../mobile/encxmobile"

// clientFixedMethods counts the public members Client declares regardless of
// the model: __destruct and close. __construct and handle are private and do
// not show up in a public method count.
const clientFixedMethods = 2

func loadModel(t *testing.T) *surface.Model {
	t.Helper()
	m, err := surface.Load(mobileDir)
	if err != nil {
		t.Fatalf("surface.Load(%s): %v", mobileDir, err)
	}
	return m
}

func generate(t *testing.T) (client, helpers []byte, m *surface.Model) {
	t.Helper()
	m = loadModel(t)

	client, err := ClientFile(m)
	if err != nil {
		t.Fatalf("ClientFile: %v", err)
	}
	helpers, err = HelpersFile(m)
	if err != nil {
		t.Fatalf("HelpersFile: %v", err)
	}
	return client, helpers, m
}

func TestMethodName(t *testing.T) {
	cases := []struct {
		goName string
		want   string
	}{
		{"GetGameModel", "getGameModel"},
		{"NewClient", "newClient"},
		{"NewClientWithOptions", "newClientWithOptions"},
		{"APIBaseURL", "apiBaseURL"},
		{"ExportHAR", "exportHAR"},
		{"HAREntryCount", "harEntryCount"},
		{"SetHARRecordingEnabled", "setHARRecordingEnabled"},
		{"ClearHARFirst", "clearHARFirst"},
		{"ExportHARSnapshot", "exportHARSnapshot"},
		{"Domain", "domain"},
		{"SetGameRequestMinIntervalMillis", "setGameRequestMinIntervalMillis"},
		{"ParseTeamLinks", "parseTeamLinks"},
	}
	for _, c := range cases {
		t.Run(c.goName, func(t *testing.T) {
			if got := MethodName(c.goName); got != c.want {
				t.Errorf("MethodName(%q) = %q, want %q", c.goName, got, c.want)
			}
		})
	}
}

func TestGeneratedFilesAreDeterministic(t *testing.T) {
	firstClient, firstHelpers, _ := generate(t)
	secondClient, secondHelpers, _ := generate(t)

	if !bytes.Equal(firstClient, secondClient) {
		t.Error("ClientFile is not byte-for-byte reproducible")
	}
	if !bytes.Equal(firstHelpers, secondHelpers) {
		t.Error("HelpersFile is not byte-for-byte reproducible")
	}
}

func TestGeneratedFilesCarryTheMarker(t *testing.T) {
	client, helpers, _ := generate(t)

	for _, f := range []struct {
		name string
		data []byte
	}{
		{"Client.php", client},
		{"Helpers.php", helpers},
	} {
		if !bytes.HasPrefix(f.data, []byte("<?php\n")) {
			t.Errorf("%s does not open with <?php", f.name)
		}
		if !bytes.Contains(f.data, []byte(generatedMarker)) {
			t.Errorf("%s is missing the %q marker", f.name, generatedMarker)
		}
		if !bytes.Contains(f.data, []byte("declare(strict_types=1);")) {
			t.Errorf("%s is missing the strict_types declaration", f.name)
		}
		if !bytes.Contains(f.data, []byte("namespace Encx;")) {
			t.Errorf("%s is missing the Encx namespace", f.name)
		}
		if !bytes.HasSuffix(f.data, []byte("\n")) {
			t.Errorf("%s does not end with a newline", f.name)
		}
	}
}

// publicMethods is anchored at one indentation level so that the word
// "function" inside a doc comment or a nested expression cannot match.
var publicMethods = regexp.MustCompile(`(?m)^    public (?:static )?function (\w+)\(`)

func TestClientDeclaresEveryBoundSymbol(t *testing.T) {
	client, _, m := generate(t)

	got := publicMethods.FindAllStringSubmatch(string(client), -1)
	want := len(m.Constructors) + len(m.Methods) + clientFixedMethods
	if len(got) != want {
		t.Fatalf("Client.php declares %d public methods, want %d (%d constructors + %d methods + %d fixed)",
			len(got), want, len(m.Constructors), len(m.Methods), clientFixedMethods)
	}
	// The surface of encxmobile as it stands today; a change here means the
	// bound Go API changed and the PHP file must be regenerated.
	if want != 47 {
		t.Errorf("expected 47 public methods on Client, model now yields %d", want)
	}

	names := map[string]bool{}
	for _, match := range got {
		if names[strings.ToLower(match[1])] {
			t.Errorf("Client.php declares %s twice", match[1])
		}
		names[strings.ToLower(match[1])] = true
	}
	for _, f := range append(append([]surface.Func{}, m.Constructors...), m.Methods...) {
		if !names[strings.ToLower(MethodName(f.Name))] {
			t.Errorf("Client.php is missing %s (Go %s)", MethodName(f.Name), f.Name)
		}
	}
}

func TestHelpersDeclaresEveryBoundFunction(t *testing.T) {
	_, helpers, m := generate(t)

	got := publicMethods.FindAllStringSubmatch(string(helpers), -1)
	if len(got) != len(m.Functions) {
		t.Fatalf("Helpers.php declares %d public methods, want %d", len(got), len(m.Functions))
	}

	names := map[string]bool{}
	for _, match := range got {
		if names[strings.ToLower(match[1])] {
			t.Errorf("Helpers.php declares %s twice", match[1])
		}
		names[strings.ToLower(match[1])] = true
	}
	for _, f := range m.Functions {
		if !names[strings.ToLower(MethodName(f.Name))] {
			t.Errorf("Helpers.php is missing %s (Go %s)", MethodName(f.Name), f.Name)
		}
	}
}

func TestHelpersFileIsEmittedForAnEmptyModel(t *testing.T) {
	data, err := HelpersFile(&surface.Model{Package: "encxmobile"})
	if err != nil {
		t.Fatalf("HelpersFile on an empty model: %v", err)
	}
	if !bytes.Contains(data, []byte("final class Helpers")) {
		t.Error("an empty model must still yield the Helpers class")
	}
	if publicMethods.Match(data) {
		t.Error("an empty model must not yield any Helpers method")
	}
}

// methodBody returns the text of one generated method, from its doc block to
// the closing brace at one indentation level.
func methodBody(t *testing.T, file []byte, name string) string {
	t.Helper()
	re := regexp.MustCompile(`(?ms)^    public (?:static )?function ` + regexp.QuoteMeta(name) + `\(.*?^    \}$`)
	loc := re.FindIndex(file)
	if loc == nil {
		t.Fatalf("method %s not found in the generated file", name)
	}
	start := loc[0]
	if doc := bytes.LastIndex(file[:start], []byte(indent+"/**\n")); doc >= 0 {
		start = doc
	}
	return string(file[start:loc[1]])
}

func TestBytesParameterPassesPointerAndLength(t *testing.T) {
	client, _, _ := generate(t)
	body := methodBody(t, client, "importCookies")

	const want = "Ffi::call('encx_client_import_cookies', [$this->handle(), $data, strlen($data)]);"
	if !strings.Contains(body, want) {
		t.Errorf("importCookies must forward three C arguments, got:\n%s", body)
	}
	if !strings.Contains(body, "public function importCookies(string $data): void") {
		t.Errorf("importCookies must take a single PHP string, got:\n%s", body)
	}
}

func TestBytesResultIsBase64Decoded(t *testing.T) {
	client, _, _ := generate(t)
	body := methodBody(t, client, "exportCookies")

	const want = "return Ffi::decodeBytes(Ffi::call('encx_client_export_cookies', [$this->handle()]));"
	if !strings.Contains(body, want) {
		t.Errorf("exportCookies must decode its base64 payload, got:\n%s", body)
	}
}

func TestJSONStructResultIsAnArray(t *testing.T) {
	client, _, _ := generate(t)
	body := methodBody(t, client, "exportHARSnapshot")

	if !strings.Contains(body, "public function exportHARSnapshot(): array") {
		t.Errorf("exportHARSnapshot must return array, got:\n%s", body)
	}
	if !strings.Contains(body, "return (array) Ffi::call('encx_client_export_har_snapshot', [$this->handle()]);") {
		t.Errorf("exportHARSnapshot must cast the envelope value to array, got:\n%s", body)
	}
}

func TestConstructorReturnsSelfAndNarrowsBool(t *testing.T) {
	client, _, _ := generate(t)
	body := methodBody(t, client, "newClient")

	if !strings.Contains(body, "public static function newClient(string $domain, bool $insecureTLS): self") {
		t.Errorf("newClient has an unexpected signature:\n%s", body)
	}
	const want = "return new self((int) Ffi::call('encx_new_client', [$domain, $insecureTLS ? 1 : 0]));"
	if !strings.Contains(body, want) {
		t.Errorf("newClient must wrap the handle in a new instance, got:\n%s", body)
	}
}

func TestCloseClearsTheHandleBeforeFreeingIt(t *testing.T) {
	client, _, _ := generate(t)
	body := methodBody(t, client, "close")

	clear := strings.Index(body, "$this->handle = null;")
	free := strings.Index(body, "Ffi::call('encx_client_free'")
	if clear < 0 || free < 0 {
		t.Fatalf("close() must clear the handle and free it, got:\n%s", body)
	}
	if clear > free {
		t.Errorf("close() must clear the handle before freeing it, got:\n%s", body)
	}
	if !strings.Contains(body, "if ($handle === null) {") {
		t.Errorf("close() must be safe to call twice, got:\n%s", body)
	}
}

func TestEveryBoundMethodCarriesItsGoDoc(t *testing.T) {
	client, helpers, m := generate(t)

	check := func(file []byte, fs []surface.Func) {
		for _, f := range fs {
			body := methodBody(t, file, MethodName(f.Name))
			if !strings.Contains(body, "@throws EncxException") {
				t.Errorf("%s does not document that it can throw:\n%s", f.Name, body)
			}
			if f.Doc == "" {
				continue
			}
			for _, line := range strings.Split(f.Doc, "\n") {
				want := strings.TrimRight(" * "+line, " ")
				if !strings.Contains(body, want) {
					t.Errorf("%s lost the Go doc line %q:\n%s", f.Name, line, body)
				}
			}
		}
	}
	check(client, m.Constructors)
	check(client, m.Methods)
	check(helpers, m.Functions)
}

func TestCommittedFilesAreUpToDate(t *testing.T) {
	client, helpers, _ := generate(t)

	for _, f := range []struct {
		path string
		want []byte
	}{
		{"../../src/Encx/Client.php", client},
		{"../../src/Encx/Helpers.php", helpers},
	} {
		if *update {
			if err := os.WriteFile(f.path, f.want, 0o644); err != nil {
				t.Fatalf("write %s: %v", f.path, err)
			}
			continue
		}
		got, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatalf("read %s: %v", f.path, err)
		}
		if !bytes.Equal(got, f.want) {
			t.Errorf("%s is stale; regenerate it with `go test ./php/internal/genphp/ -update`", filepath.Clean(f.path))
		}
	}
}

func TestGeneratedFilesParse(t *testing.T) {
	php, err := exec.LookPath("php")
	if err != nil {
		t.Skip("php not found in PATH; skipping the syntax check of the generated bindings")
	}

	for _, path := range []string{"../../src/Encx/Client.php", "../../src/Encx/Helpers.php"} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			out, err := exec.Command(php, "-l", path).CombinedOutput()
			if err != nil {
				t.Fatalf("php -l %s failed: %v\n%s", path, err, out)
			}
			t.Logf("php -l %s: %s", path, strings.TrimSpace(string(out)))
		})
	}
}
