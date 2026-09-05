package genphp

import (
	"fmt"

	"github.com/skrashevich/encx-cli/bindings/php/internal/surface"
)

// autoGlobals are the variables PHP populates itself. Naming a parameter after
// one of them is a fatal error at compile time: "Cannot re-assign auto-global
// variable". The list is case-sensitive, the way PHP treats variable names.
var autoGlobals = []string{
	"GLOBALS", "_SERVER", "_GET", "_POST", "_FILES",
	"_COOKIE", "_SESSION", "_REQUEST", "_ENV",
}

// checkParamNames rejects a Go parameter list that cannot be spelled as a PHP
// signature.
//
// A Go name reaches PHP unchanged, and Go accepts names PHP will not: $this is
// bound to the receiver and cannot be a parameter, the auto-globals cannot be
// re-assigned, and a name outside the PHP variable grammar is a parse error.
// Every one of those is a fatal error in the generated file, which no test of
// the Go generator would notice, so the generator refuses to write it. Two
// parameters landing on the same PHP name are refused for the same reason.
func checkParamNames(params []surface.Param) error {
	seen := make(map[string]bool, len(params))
	for _, p := range params {
		if err := checkParamName(p.Name); err != nil {
			return err
		}
		if seen[p.Name] {
			return fmt.Errorf("two parameters are named %q, which PHP rejects as a redefinition of $%s", p.Name, p.Name)
		}
		seen[p.Name] = true
	}
	return nil
}

func checkParamName(name string) error {
	if !isPHPVariableName(name) {
		return fmt.Errorf("parameter %q is not a valid PHP variable name, so it cannot be spelled as $%s", name, name)
	}
	if name == "this" {
		return fmt.Errorf("parameter %q collides with the PHP receiver: $this cannot be used as a parameter name; rename the Go parameter", name)
	}
	for _, g := range autoGlobals {
		if name == g {
			return fmt.Errorf("parameter %q collides with the PHP auto-global $%s, which cannot be re-assigned; rename the Go parameter", name, g)
		}
	}
	return nil
}

// isPHPVariableName reports whether name matches the PHP variable grammar,
// [a-zA-Z_\x80-\xff][a-zA-Z0-9_\x80-\xff]*. Bytes above 0x7f are accepted, as
// PHP accepts them, which keeps a non-ASCII Go identifier bindable.
func isPHPVariableName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '_', c >= 0x80:
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}
