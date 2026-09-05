package genphp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/bindings/php/internal/surface"
)

// loadFixture writes src as the only Go file of a throwaway package and
// returns its surface model.
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

// TestParameterNamesPHPRejects covers the Go parameter names that reach PHP
// unchanged and make the generated file fatal. None of them is a Go error, so
// only this emitter can catch them, and it has to catch them before writing:
// a fatal parse error in Client.php takes down every consumer of the binding.
func TestParameterNamesPHPRejects(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "this on a method",
			src:  "func (c *EncClient) This(this string) error { return nil }",
			want: "$this cannot be used as a parameter name",
		},
		{
			name: "this on a package function",
			src:  "func Echo(this string) error { return nil }",
			want: "$this cannot be used as a parameter name",
		},
		{
			name: "auto-global GLOBALS",
			src:  "func (c *EncClient) Dump(GLOBALS string) error { return nil }",
			want: "auto-global $GLOBALS",
		},
		{
			name: "auto-global _SERVER",
			src:  "func (c *EncClient) Dump(_SERVER string) error { return nil }",
			want: "auto-global $_SERVER",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := loadFixture(t, tc.src)

			client, cErr := ClientFile(m)
			helpers, hErr := HelpersFile(m)

			// A method lands in Client and a package function in Helpers, so
			// the failure is expected from exactly one of the two.
			err := cErr
			if err == nil {
				err = hErr
			}
			if err == nil {
				t.Fatalf("the emitter accepted a name PHP rejects and produced:\n%s\n%s", client, helpers)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not explain the problem (%q): %v", tc.want, err)
			}
		})
	}
}

// TestParameterNamesPHPAccepts keeps the check from over-reaching: PHP
// variable names are case-sensitive, so $This and $_Server are ordinary
// variables and the Go names behind them stay bindable.
func TestParameterNamesPHPAccepts(t *testing.T) {
	m := loadFixture(t, "func (c *EncClient) Keep(This string, _Server string) error { return nil }")

	client, err := ClientFile(m)
	if err != nil {
		t.Fatalf("ClientFile rejected a legal PHP parameter name: %v", err)
	}
	if !strings.Contains(string(client), "public function keep(string $This, string $_Server): void") {
		t.Errorf("the signature was not emitted as declared:\n%s", client)
	}
}

// TestDuplicateParameterNamesAreRejected covers the redefinition PHP treats as
// fatal. It is reachable because the model synthesises a name for a blank
// parameter, which can land on a name the signature already uses.
func TestDuplicateParameterNamesAreRejected(t *testing.T) {
	m := loadFixture(t, "func (c *EncClient) Clash(_ string, arg0 string) error { return nil }")

	if _, err := ClientFile(m); err == nil {
		t.Fatal("the emitter accepted two parameters with the same PHP name")
	} else if !strings.Contains(err.Error(), "redefinition") {
		t.Errorf("error does not explain the redefinition: %v", err)
	}
}

// TestBlankParameterIsNamed pins what the blank identifier becomes on the PHP
// side: $_ would be legal but says nothing, and the C wrapper needs the same
// name, so both take the synthesised one.
func TestBlankParameterIsNamed(t *testing.T) {
	m := loadFixture(t, "func (c *EncClient) Ignore(_ string) error { return nil }")

	client, err := ClientFile(m)
	if err != nil {
		t.Fatalf("ClientFile: %v", err)
	}
	if !strings.Contains(string(client), "public function ignore(string $arg0): void") {
		t.Errorf("the blank parameter was not given a usable name:\n%s", client)
	}
}
