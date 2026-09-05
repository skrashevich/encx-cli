// Package php holds the PHP bindings for encx: a C-shared library built from
// mobile/encxmobile and the PHP classes that call it through FFI.
//
// The cgo export file, the C header, the surface manifest and the PHP classes
// are all generated from the Go source of mobile/encxmobile, and a test
// regenerates them and compares byte for byte, so a change to the bound
// surface fails the build until they are regenerated. What the model records,
// and therefore what the drift check sees, is: which symbols are bound and why
// the rest are not, their signatures, their doc comments, and the shape of
// every returned struct down to the field names, field types and json tags.
// Method bodies are not part of it, and deliberately so: reimplementing a
// bound method without touching its signature leaves the bindings correct.
//
// Run `go generate ./bindings/...` after changing mobile/encxmobile.
package php

//go:generate go run ./cmd/encxphpgen -out .
