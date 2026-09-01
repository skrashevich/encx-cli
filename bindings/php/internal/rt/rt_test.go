package rt

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"

	"github.com/skrashevich/encx-cli/mobile/encxmobile"
)

// --- Registry ---

func TestRegistryAddGet(t *testing.T) {
	r := NewRegistry()
	c := encxmobile.NewClient("example.test", false)

	h, err := r.Add(c)
	if err != nil {
		t.Fatalf("Add returned error: %v", err)
	}
	if h == 0 {
		t.Fatalf("Add returned zero handle")
	}

	got, err := r.Get(h)
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got != c {
		t.Fatalf("Get returned a different client than was added")
	}
}

func TestRegistryFree(t *testing.T) {
	r := NewRegistry()
	c := encxmobile.NewClient("example.test", false)

	h, err := r.Add(c)
	if err != nil {
		t.Fatalf("Add returned error: %v", err)
	}

	if !r.Free(h) {
		t.Fatalf("Free returned false for a live handle")
	}
	if r.Free(h) {
		t.Fatalf("second Free returned true")
	}

	_, err = r.Get(h)
	if err == nil {
		t.Fatalf("Get after Free did not return an error")
	}
	if !strings.Contains(err.Error(), fmt.Sprintf("%d", h)) {
		t.Fatalf("Get error %q does not mention handle %d", err.Error(), h)
	}
}

func TestRegistryHandlesNotReused(t *testing.T) {
	r := NewRegistry()
	c1 := encxmobile.NewClient("example.test", false)
	c2 := encxmobile.NewClient("example.test", false)

	h1, err := r.Add(c1)
	if err != nil {
		t.Fatalf("Add c1 returned error: %v", err)
	}
	if !r.Free(h1) {
		t.Fatalf("Free(h1) returned false")
	}
	h2, err := r.Add(c2)
	if err != nil {
		t.Fatalf("Add c2 returned error: %v", err)
	}
	if h2 == h1 {
		t.Fatalf("handle %d was reused after Free", h1)
	}
}

func TestRegistryGetZero(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get(0)
	if err == nil {
		t.Fatalf("Get(0) did not return an error")
	}
}

func TestRegistryAddNil(t *testing.T) {
	r := NewRegistry()
	h, err := r.Add(nil)
	if err == nil {
		t.Fatalf("Add(nil) did not return an error")
	}
	if h != 0 {
		t.Fatalf("Add(nil) returned non-zero handle %d", h)
	}
	if !errors.Is(err, ErrNilClient) {
		t.Fatalf("Add(nil) error = %v, want ErrNilClient", err)
	}
}

func TestRegistryConcurrent(t *testing.T) {
	r := NewRegistry()
	const goroutines = 32
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				c := encxmobile.NewClient("example.test", false)
				h, err := r.Add(c)
				if err != nil {
					t.Errorf("Add returned error: %v", err)
					return
				}
				if _, err := r.Get(h); err != nil {
					t.Errorf("Get(%d) returned error: %v", h, err)
					return
				}
				r.Free(h)
				_ = r.Len()
			}
		}()
	}
	wg.Wait()
}

// --- Envelope ---

func TestOKString(t *testing.T) {
	out := OK("hello")
	var got struct {
		Ok    bool   `json:"ok"`
		Value string `json:"value"`
	}
	mustUnmarshal(t, out, &got)
	if !got.Ok || got.Value != "hello" {
		t.Fatalf("OK(%q) = %s", "hello", out)
	}
}

func TestOKInt64(t *testing.T) {
	out := OK(int64(42))
	var got struct {
		Ok    bool  `json:"ok"`
		Value int64 `json:"value"`
	}
	mustUnmarshal(t, out, &got)
	if !got.Ok || got.Value != 42 {
		t.Fatalf("OK(int64(42)) = %s", out)
	}
}

func TestOKBool(t *testing.T) {
	out := OK(true)
	var got struct {
		Ok    bool `json:"ok"`
		Value bool `json:"value"`
	}
	mustUnmarshal(t, out, &got)
	if !got.Ok || !got.Value {
		t.Fatalf("OK(true) = %s", out)
	}
}

func TestOKBytesBase64(t *testing.T) {
	payload := []byte{0x00, 0x01, 0xFF, 'h', 'i'}
	out := OK(payload)
	var got struct {
		Ok    bool   `json:"ok"`
		Value string `json:"value"`
	}
	mustUnmarshal(t, out, &got)
	if !got.Ok {
		t.Fatalf("OK([]byte) not ok: %s", out)
	}
	decoded, err := base64.StdEncoding.DecodeString(got.Value)
	if err != nil {
		t.Fatalf("value %q is not base64: %v", got.Value, err)
	}
	if string(decoded) != string(payload) {
		t.Fatalf("decoded %v != original %v", decoded, payload)
	}
}

func TestOKStruct(t *testing.T) {
	type point struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	out := OK(point{X: 1, Y: 2})
	var got struct {
		Ok    bool  `json:"ok"`
		Value point `json:"value"`
	}
	mustUnmarshal(t, out, &got)
	if !got.Ok || got.Value != (point{X: 1, Y: 2}) {
		t.Fatalf("OK(point) = %s", out)
	}
}

func TestOKVoidNoValueField(t *testing.T) {
	out := OKVoid()
	var raw map[string]json.RawMessage
	mustUnmarshal(t, out, &raw)
	if _, present := raw["value"]; present {
		t.Fatalf("OKVoid() contains a \"value\" field: %s", out)
	}
	if ok, present := raw["ok"]; !present || string(ok) != "true" {
		t.Fatalf("OKVoid() missing/invalid \"ok\": %s", out)
	}
}

func TestFailNil(t *testing.T) {
	out := Fail(nil)
	var got struct {
		Ok    bool   `json:"ok"`
		Error string `json:"error"`
	}
	mustUnmarshal(t, out, &got)
	if got.Ok {
		t.Fatalf("Fail(nil) has ok=true: %s", out)
	}
	if got.Error == "" {
		t.Fatalf("Fail(nil) has empty error text: %s", out)
	}
}

func TestFailRoundTripsSpecialText(t *testing.T) {
	msg := "проверка \"кавычек\"\nи переноса строки"
	out := Fail(errors.New(msg))
	var got struct {
		Ok    bool   `json:"ok"`
		Error string `json:"error"`
	}
	mustUnmarshal(t, out, &got)
	if got.Ok {
		t.Fatalf("Fail(err) has ok=true: %s", out)
	}
	if got.Error != msg {
		t.Fatalf("Fail round-trip mismatch:\n got:  %q\n want: %q", got.Error, msg)
	}
}

func TestFailf(t *testing.T) {
	out := Failf("code %d: %s", 7, "boom")
	var got struct {
		Ok    bool   `json:"ok"`
		Error string `json:"error"`
	}
	mustUnmarshal(t, out, &got)
	if got.Ok || got.Error != "code 7: boom" {
		t.Fatalf("Failf(...) = %s", out)
	}
}

func TestOKUnmarshalableValueProducesErrorEnvelope(t *testing.T) {
	out := OK(math.NaN())
	if !json.Valid([]byte(out)) {
		t.Fatalf("OK(NaN) produced invalid JSON: %s", out)
	}
	var got struct {
		Ok    bool   `json:"ok"`
		Error string `json:"error"`
	}
	mustUnmarshal(t, out, &got)
	if got.Ok {
		t.Fatalf("OK(NaN) has ok=true, want an error envelope: %s", out)
	}
	if got.Error == "" {
		t.Fatalf("OK(NaN) error envelope has empty error text: %s", out)
	}
}

func TestAllEnvelopesAreValidJSON(t *testing.T) {
	cases := map[string]string{
		"OK string":      OK("x"),
		"OK int":         OK(1),
		"OK bool":        OK(false),
		"OK bytes":       OK([]byte("x")),
		"OK struct":      OK(struct{ A int }{A: 1}),
		"OK nil":         OK(nil),
		"OK NaN":         OK(math.NaN()),
		"OKVoid":         OKVoid(),
		"Fail nil":       Fail(nil),
		"Fail err":       Fail(errors.New("boom")),
		"Failf":          Failf("x=%d", 1),
		"Fail cyrillic":  Fail(errors.New("ошибка")),
		"Fail with quot": Fail(errors.New(`a "b" c`)),
	}
	for name, out := range cases {
		t.Run(name, func(t *testing.T) {
			if out == "" {
				t.Fatalf("%s returned empty string", name)
			}
			if !json.Valid([]byte(out)) {
				t.Fatalf("%s returned invalid JSON: %s", name, out)
			}
		})
	}
}

func mustUnmarshal(t *testing.T, s string, v any) {
	t.Helper()
	if s == "" {
		t.Fatalf("empty envelope string")
	}
	if err := json.Unmarshal([]byte(s), v); err != nil {
		t.Fatalf("json.Unmarshal(%q) failed: %v", s, err)
	}
}
