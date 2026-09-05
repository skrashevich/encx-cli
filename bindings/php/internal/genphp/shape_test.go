package genphp

import (
	"strings"
	"testing"

	"github.com/skrashevich/encx-cli/bindings/php/internal/surface"
)

func TestPHPDocType(t *testing.T) {
	cases := []struct {
		goType string
		want   string
	}{
		{"string", "string"},
		{"int", "int"},
		{"int64", "int"},
		{"bool", "bool"},
		{"float64", "float"},
		{"byte", "int"},
		// encoding/json writes a byte slice as base64 text, so it arrives in
		// PHP as a string and not as a list of numbers.
		{"[]byte", "string"},
		{"[]uint8", "string"},
		{"[]string", "list<string>"},
		{"[][]string", "list<list<string>>"},
		{"map[string]int64", "array<string, int>"},
		{"map[string][]string", "array<string, list<string>>"},
		{"map[string]map[string]bool", "array<string, array<string, bool>>"},
	}
	for _, c := range cases {
		t.Run(c.goType, func(t *testing.T) {
			if got := phpDocType(c.goType); got != c.want {
				t.Errorf("phpDocType(%q) = %q, want %q", c.goType, got, c.want)
			}
		})
	}
}

func TestArrayShape(t *testing.T) {
	cases := []struct {
		name   string
		fields []surface.Field
		want   string
	}{
		{
			name:   "no fields",
			fields: nil,
			want:   "array<string, mixed>",
		},
		{
			name: "every field dropped by its tag",
			fields: []surface.Field{
				{Name: "Internal", JSONName: "", Type: "string"},
			},
			want: "array<string, mixed>",
		},
		{
			name: "declaration order is the rendered order",
			fields: []surface.Field{
				{Name: "JSON", JSONName: "JSON", Type: "string"},
				{Name: "EntryCount", JSONName: "EntryCount", Type: "int64"},
			},
			want: "array{JSON: string, EntryCount: int}",
		},
		{
			name: "omitempty makes the key optional",
			fields: []surface.Field{
				{Name: "Count", JSONName: "count", OmitEmpty: true, Type: "int64"},
			},
			want: "array{count?: int}",
		},
		{
			name: "a key outside the identifier grammar is quoted",
			fields: []surface.Field{
				{Name: "Weird", JSONName: "content-type", Type: "string"},
			},
			want: `array{"content-type": string}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := arrayShape(c.fields); got != c.want {
				t.Errorf("arrayShape() = %q, want %q", got, c.want)
			}
		})
	}
}

// TestReturnShapeReachesTheDoc is the second place a struct rename becomes
// visible, next to the manifest: the PHPDoc of the generated method spells the
// keys out, so a consumer reading the wrong key is flagged by static analysis
// rather than at runtime.
func TestReturnShapeReachesTheDoc(t *testing.T) {
	const method = "\n\nfunc (c *EncClient) Snap() (*Snapshot, error) { return nil, nil }\n"

	render := func(t *testing.T, decl string) string {
		t.Helper()
		client, err := ClientFile(loadFixture(t, decl+method))
		if err != nil {
			t.Fatalf("ClientFile: %v", err)
		}
		return methodBody(t, client, "snap")
	}

	base := render(t, "type Snapshot struct {\n\tJSON string\n\tEntryCount int64\n}")
	if !strings.Contains(base, "@return array{JSON: string, EntryCount: int}") {
		t.Fatalf("the doc does not spell out the returned shape:\n%s", base)
	}

	for _, tc := range []struct {
		name string
		decl string
		want string
	}{
		{
			name: "renamed field",
			decl: "type Snapshot struct {\n\tDocument string\n\tEntryCount int64\n}",
			want: "@return array{Document: string, EntryCount: int}",
		},
		{
			name: "json tag",
			decl: "type Snapshot struct {\n\tJSON string `json:\"har\"`\n\tEntryCount int64\n}",
			want: "@return array{har: string, EntryCount: int}",
		},
		{
			name: "omitempty",
			decl: "type Snapshot struct {\n\tJSON string `json:\"har,omitempty\"`\n\tEntryCount int64\n}",
			want: "@return array{har?: string, EntryCount: int}",
		},
		{
			name: "field dropped from the json",
			decl: "type Snapshot struct {\n\tJSON string `json:\"-\"`\n\tEntryCount int64\n}",
			want: "@return array{EntryCount: int}",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := render(t, tc.decl)
			if !strings.Contains(got, tc.want) {
				t.Errorf("the doc does not carry %q:\n%s", tc.want, got)
			}
			if got == base {
				t.Errorf("a %s left the generated method unchanged, so drift would go unnoticed", tc.name)
			}
		})
	}
}
