package gen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/skrashevich/encx-cli/bindings/php/internal/surface"
)

// generatedNote is the manifest's stand-in for the "Code generated" marker,
// which JSON has no room for. It is the first key of the document.
const generatedNote = generator + "; DO NOT EDIT"

// manifest is the machine-readable description of the generated C ABI. Field
// order is the emitted key order, so the document stays byte-stable.
type manifest struct {
	Generated  string            `json:"_generated"`
	Package    string            `json:"package"`
	ImportPath string            `json:"importPath"`
	Runtime    []manifestRuntime `json:"runtime"`
	Bindable   []manifestFunc    `json:"bindable"`
	Skipped    []manifestSkipped `json:"skipped"`
}

// manifestRuntime is one hand-written export from bindings/php/cshared/runtime.go.
type manifestRuntime struct {
	CName     string `json:"cName"`
	Prototype string `json:"prototype"`
}

// manifestFunc is one generated export.
type manifestFunc struct {
	GoName    string `json:"goName"`
	CName     string `json:"cName"`
	Kind      string `json:"kind"`
	ValueType string `json:"valueType"`
	StructRef string `json:"structRef,omitempty"`
	// StructFields is the shape of the struct named by StructRef, in
	// declaration order. Recording the shape and not just the name is what
	// makes a renamed field, a retyped field or an edited json tag show up
	// as a stale manifest instead of as a runtime surprise in PHP.
	StructFields []manifestField `json:"structFields,omitempty"`
	Params       []manifestParam `json:"params"`
}

// manifestField is one field of a JSON result struct as PHP will see it. A
// field dropped by `json:"-"` is listed with an empty jsonName, because its
// absence from the JSON is part of the shape too.
type manifestField struct {
	Name      string `json:"name"`
	JSONName  string `json:"jsonName"`
	OmitEmpty bool   `json:"omitEmpty"`
	Type      string `json:"type"`
}

// manifestParam is one Go parameter and the C parameter it maps to. A []byte
// parameter also produces a companion "<cName>_len" argument in C, which the
// header spells out and this entry implies.
type manifestParam struct {
	Name  string `json:"name"`
	CName string `json:"cName"`
	Type  string `json:"type"`
}

// manifestSkipped is one exported symbol that got no C symbol.
type manifestSkipped struct {
	GoName string `json:"goName"`
	Reason string `json:"reason"`
}

// ManifestFile returns the contents of bindings/php/bindings.manifest.json.
func ManifestFile(m *surface.Model) ([]byte, error) {
	if m == nil {
		return nil, fmt.Errorf("gen: nil model")
	}

	doc := manifest{
		Generated:  generatedNote,
		Package:    m.Package,
		ImportPath: m.ImportPath,
		Runtime:    []manifestRuntime{},
		Bindable:   []manifestFunc{},
		Skipped:    []manifestSkipped{},
	}

	for _, rf := range runtimeExports() {
		doc.Runtime = append(doc.Runtime, manifestRuntime{CName: rf.CName, Prototype: rf.Prototype})
	}

	for _, bd := range bindings(m) {
		entry := manifestFunc{
			GoName:    bd.fn.Name,
			CName:     bd.fn.CName,
			Kind:      bd.fn.Result.Kind.String(),
			ValueType: bd.fn.Result.Type.String(),
			StructRef: bd.fn.Result.StructRef,
			Params:    []manifestParam{},
		}
		for _, f := range bd.fn.Result.Fields {
			entry.StructFields = append(entry.StructFields, manifestField{
				Name:      f.Name,
				JSONName:  f.JSONName,
				OmitEmpty: f.OmitEmpty,
				Type:      f.Type,
			})
		}
		for _, p := range bd.fn.Params {
			entry.Params = append(entry.Params, manifestParam{
				Name:  p.Name,
				CName: surface.SnakeCase(p.Name),
				Type:  p.Type.String(),
			})
		}
		doc.Bindable = append(doc.Bindable, entry)
	}

	for _, s := range m.Skipped {
		doc.Skipped = append(doc.Skipped, manifestSkipped{GoName: s.Name, Reason: s.Reason})
	}
	sort.SliceStable(doc.Skipped, func(i, j int) bool {
		if doc.Skipped[i].GoName != doc.Skipped[j].GoName {
			return doc.Skipped[i].GoName < doc.Skipped[j].GoName
		}
		return doc.Skipped[i].Reason < doc.Skipped[j].Reason
	})

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// Reasons quote Go type expressions; HTML escaping would mangle them into
	// < sequences for no benefit here.
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, fmt.Errorf("gen: encode manifest: %w", err)
	}
	return buf.Bytes(), nil
}
