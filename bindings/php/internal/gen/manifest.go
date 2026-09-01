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
	GoName    string          `json:"goName"`
	CName     string          `json:"cName"`
	Kind      string          `json:"kind"`
	ValueType string          `json:"valueType"`
	StructRef string          `json:"structRef,omitempty"`
	Params    []manifestParam `json:"params"`
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
