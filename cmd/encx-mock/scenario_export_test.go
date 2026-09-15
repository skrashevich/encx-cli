package main

import (
	"github.com/skrashevich/encx-cli/encx"
	"testing"
)

func TestMockScenarioExportReadsEditedState(t *testing.T) {
	c, ctx := adminClient(t)
	if err := c.AdminUpdateComment(ctx, mockGameID, 1, "Edited", "Note"); err != nil {
		t.Fatal(err)
	}
	if err := c.AdminCreateTask(ctx, mockGameID, 1, encx.AdminTask{Text: "Source task"}); err != nil {
		t.Fatal(err)
	}
	doc, err := c.GetGameScenario(ctx, mockGameID)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Levels[0].Name != "Edited" || doc.Levels[0].Comment != "Note" {
		t.Fatalf("stale export: %+v", doc.Levels[0])
	}
	found := false
	for _, task := range doc.Levels[0].Tasks {
		found = found || task == "Source task"
	}
	if !found {
		t.Fatal("new task missing from export")
	}
}

func TestMockSectorCompletionRequiresExistingSectors(t *testing.T) {
	c, ctx := adminClient(t)
	if err := c.AdminClearLevelSectors(ctx, mockGameID, 1); err != nil {
		t.Fatal(err)
	}
	if err := c.AdminUpdateSectorCompletion(ctx, mockGameID, 1, 1); err == nil {
		t.Fatal("completion accepted before sectors exist")
	}
	if err := c.AdminCreateSector(ctx, mockGameID, 1, encx.AdminSector{Name: "Sector", Answers: []string{"code"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.AdminUpdateSectorCompletion(ctx, mockGameID, 1, 1); err != nil {
		t.Fatal(err)
	}
}
