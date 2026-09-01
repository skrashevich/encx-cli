// Command cshared is the c-shared library the PHP bindings load through FFI.
//
// The exported wrappers live in the generated exports_gen.go; this file holds
// the hand-written pieces they rely on: the conversion helpers and the two
// fixed entry points that manage memory and client lifetime.
//
// Ownership rule for the whole ABI: every encx_ function returning char *
// returns a malloc'd JSON envelope that belongs to the caller. PHP must release
// it with encx_string_free once the string has been copied into a PHP value.
// Nothing on the Go side keeps a reference to it, and Go's garbage collector
// never sees it, so a caller that forgets to free leaks.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"unsafe"

	"github.com/skrashevich/encx-cli/bindings/php/internal/rt"
)

// main exists only because buildmode=c-shared requires a main package with a
// main function. It is never executed.
func main() {}

// cEnvelope copies a JSON envelope into C memory. The returned pointer is
// owned by the caller and must be released with encx_string_free.
func cEnvelope(s string) *C.char {
	return C.CString(s)
}

// cBytes copies n bytes from p into a Go slice. A null pointer or a
// non-positive length yields a nil slice rather than an empty one, so an
// absent argument stays distinguishable from a zero-length one.
func cBytes(p *C.char, n C.longlong) []byte {
	if p == nil || n <= 0 {
		return nil
	}
	return C.GoBytes(unsafe.Pointer(p), C.int(n))
}

// Frees a string returned by any other encx_ function. Passing a null
// pointer is a no-op.
//
//export encx_string_free
func encx_string_free(s *C.char) {
	if s == nil {
		return
	}
	C.free(unsafe.Pointer(s))
}

// Releases the client behind handle and returns an envelope. Handles are
// never reused, so freeing an already freed handle fails with an error
// envelope instead of releasing an unrelated client.
//
//export encx_client_free
func encx_client_free(handle C.longlong) *C.char {
	h := int64(handle)
	if !rt.Default.Free(h) {
		return cEnvelope(rt.Failf("rt: unknown or freed handle %d", h))
	}
	return cEnvelope(rt.OKVoid())
}
