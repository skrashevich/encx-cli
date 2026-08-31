package enapi

import (
	"encoding/json"
	"os"
	"testing"
)

// specPath points at the vendored snapshot of https://api.en.cx/swagger/doc.json.
const specPath = "../../docs/newengine/swagger.json"

type swaggerSpec struct {
	Swagger     string                     `json:"swagger"`
	Paths       map[string]json.RawMessage `json:"paths"`
	Definitions map[string]json.RawMessage `json:"definitions"`
}

func loadSpec(t *testing.T) swaggerSpec {
	t.Helper()
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec swaggerSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatalf("parse spec: %v", err)
	}
	return spec
}

func TestSpecSnapshotIsValidSwagger(t *testing.T) {
	spec := loadSpec(t)
	if spec.Swagger != "2.0" {
		t.Fatalf("swagger version = %q, want 2.0", spec.Swagger)
	}
	if len(spec.Paths) < 470 {
		t.Errorf("paths = %d, want at least 470", len(spec.Paths))
	}
	if len(spec.Definitions) < 440 {
		t.Errorf("definitions = %d, want at least 440", len(spec.Definitions))
	}
}

func TestSpecContainsRoutesUsedByClient(t *testing.T) {
	spec := loadSpec(t)
	want := []string{
		"/login",
		"/auth/session",
		"/gameengines/{controller}/play/{gameId}",
		"/games/home",
		"/games/{id}/details",
		"/games/{id}/statistics",
		"/teams/{id}",
		"/admin/games",
		"/admin/games/{id}/levels",
	}
	for _, path := range want {
		if _, ok := spec.Paths[path]; !ok {
			t.Errorf("spec is missing path %s", path)
		}
	}
}

func TestSpecContainsModelsUsedByClient(t *testing.T) {
	spec := loadSpec(t)
	want := []string{
		"models.LoginRequest",
		"models.EngineState",
		"models.EngineClientMessage",
		"models.HomeGamesResponse",
		"models.GameStatisticsResponse",
	}
	for _, name := range want {
		if _, ok := spec.Definitions[name]; !ok {
			t.Errorf("spec is missing definition %s", name)
		}
	}
}
