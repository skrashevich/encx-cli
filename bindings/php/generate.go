// Package php holds the PHP bindings for encx: a C-shared library built from
// mobile/encxmobile and the PHP classes that call it through FFI.
//
// The cgo export file, the C header, the surface manifest and the PHP classes
// are all generated from the Go source, so they cannot fall behind it. Run
// `go generate ./bindings/...` after changing mobile/encxmobile.
package php

//go:generate go run ./cmd/encxphpgen -out .
