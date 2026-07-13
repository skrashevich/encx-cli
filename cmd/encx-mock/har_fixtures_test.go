package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeriveFixturesFromHARBuildsSanitizedProfile(t *testing.T) {
	derived, err := deriveFixturesFromHAR(filepath.Join("fixtures", "har_protocol_sample.har"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := derived.profile.LevelCount(), 2; got != want {
		t.Fatalf("profile levels = %d, want %d", got, want)
	}
	if got, want := derived.profile.TransitionEvents, []int{19, 22}; !sameInts(got, want) {
		t.Fatalf("transition events = %v, want %v", got, want)
	}
	if !derived.profile.HasAntiBotRedirect {
		t.Fatal("expected anti-bot redirect")
	}
	if !derived.profile.HasTransportFailure {
		t.Fatal("expected transport failure")
	}

	records := derived.profile.Records
	if len(records) != 6 {
		t.Fatalf("records = %d, want 6 after duplicate deduplication", len(records))
	}
	for i := 1; i < len(records); i++ {
		if records[i-1].StartedAt.After(records[i].StartedAt) {
			t.Fatalf("records are not timestamp ordered: %v then %v", records[i-1], records[i])
		}
	}
	if records[1].Kind != "engine-poll" || records[1].Count != 2 {
		t.Fatalf("deduplicated engine poll = %+v, want count 2", records[1])
	}
	if records[1].Variant != "normal" || records[2].Kind != "level-action" || records[2].Variant != "transition" || records[3].Kind != "bonus-action" || records[3].Variant != "transition" {
		t.Fatalf("record classification = %+v", records)
	}
	if got := derived.profile.Levels[0]; got.SectorCount != 2 || got.RequiredSectorCount != 1 || got.BonusCount != 1 || got.HintCount != 1 || got.MessageCount != 1 {
		t.Fatalf("level topology = %+v", got)
	}

	assertNoSensitiveData(t, map[string]any{
		"profile":   derived.profile,
		"gameModel": json.RawMessage(derived.gameModel),
		"gameInfo":  json.RawMessage(derived.gameInfo),
		"user":      derived.userDetails,
	},
		"private-answer", "private-cookie", "private-login", "private-team",
		"private-task", "private-hint", "private-message", "private-return",
		"private-token", "private-transport-error", "private-game-title",
	)
}

func TestDeriveFixturesFromHARRejectsMalformedAndInvalidBase64(t *testing.T) {
	dir := t.TempDir()
	malformed := filepath.Join(dir, "malformed.har")
	if err := os.WriteFile(malformed, []byte(`{"log":{"entries":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := deriveFixturesFromHAR(malformed); err == nil {
		t.Fatal("expected malformed HAR error")
	}

	malformedURL := filepath.Join(dir, "malformed-url.har")
	const sentinel = "captured-url-secret"
	data := `{"log":{"entries":[{"startedDateTime":"2026-07-12T10:00:00Z","request":{"method":"GET","url":"https://demo.en.cx/%zz?token=` + sentinel + `"},"response":{"status":200,"content":{"text":"{}"}}}]}}`
	if err := os.WriteFile(malformedURL, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := deriveFixturesFromHAR(malformedURL); err == nil || strings.Contains(err.Error(), sentinel) {
		t.Fatalf("malformed URL error leaked captured data: %v", err)
	}

	invalidBase64 := filepath.Join(dir, "invalid-base64.har")
	data = `{"log":{"entries":[{"startedDateTime":"2026-07-12T10:00:00Z","request":{"method":"GET","url":"https://demo.en.cx/home/?json=1"},"response":{"status":200,"content":{"encoding":"base64","text":"%%%"}}}]}}`
	if err := os.WriteFile(invalidBase64, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := deriveFixturesFromHAR(invalidBase64); err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("invalid base64 error = %v, want base64 error", err)
	}
}

func TestDeriveFixturesFromHARSkipsUnrelatedMalformedBase64Asset(t *testing.T) {
	derived, err := deriveFixturesFromHAR(filepath.Join("fixtures", "har_with_unrelated_asset.har"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := derived.profile.LevelCount(), 1; got != want {
		t.Fatalf("profile levels = %d, want %d", got, want)
	}
	if len(derived.profile.Records) != 2 {
		t.Fatalf("records = %d, want home and engine poll", len(derived.profile.Records))
	}
}

func TestDeriveFixturesFromHARMissingRequiredResponses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.har")
	data := `{"log":{"entries":[{"startedDateTime":"2026-07-12T10:00:00Z","request":{"method":"GET","url":"https://demo.en.cx/home/?json=1"},"response":{"status":200,"content":{"text":"{\"ActiveGames\":[]}"}}}]}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := deriveFixturesFromHAR(path); err == nil || !strings.Contains(err.Error(), "game play") {
		t.Fatalf("missing game play error = %v", err)
	}
}

func TestLoadFixturesKeepsHARTemplatesUsableWithProfile(t *testing.T) {
	t.Setenv("ENCX_MOCK_HAR", filepath.Join("fixtures", "har_protocol_sample.har"))
	fixtures, err := loadFixtures()
	if err != nil {
		t.Fatal(err)
	}
	if fixtures.profile == nil || fixtures.profile.LevelCount() != 2 {
		t.Fatalf("loaded profile = %#v", fixtures.profile)
	}
	if fixtures.gameModelTemplate["Level"] == nil || fixtures.gameInfoTemplate["GameID"] != float64(mockGameID) {
		t.Fatalf("unexpected profile fixtures: %#v %#v", fixtures.gameModelTemplate, fixtures.gameInfoTemplate)
	}
}

func assertNoSensitiveData(t *testing.T, value any, sensitive ...string) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range sensitive {
		if strings.Contains(string(raw), value) {
			t.Fatalf("serialized value contains sensitive data %q: %s", value, raw)
		}
	}
}

func sameInts(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
