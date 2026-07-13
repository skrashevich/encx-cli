package encx

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeGameModelJSONEmptyBody(t *testing.T) {
	_, err := decodeGameModelJSON(nil, "game model")
	if err == nil {
		t.Fatal("expected error for empty body")
	}
	if !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("expected empty response error, got %v", err)
	}
}

func TestGameModelRoundTripsStructuredAndNullAnswers(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"GameId": 1,
		"Level": {
			"LevelId": 3,
			"Number": 3,
			"Sectors": [{"SectorId": 1, "Answer": {"Answer": "synthetic", "Login": "redacted"}}],
			"Bonuses": [{"BonusId": 2, "Answer": null}]
		}
	}`)
	var model GameModel
	if err := json.Unmarshal(raw, &model); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got struct {
		Level struct {
			Sectors []struct {
				Answer json.RawMessage `json:"Answer"`
			} `json:"Sectors"`
			Bonuses []struct {
				Answer json.RawMessage `json:"Answer"`
			} `json:"Bonuses"`
		} `json:"Level"`
	}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("Unmarshal encoded model: %v", err)
	}
	if !json.Valid(got.Level.Sectors[0].Answer) || got.Level.Sectors[0].Answer[0] != '{' {
		t.Fatalf("sector answer = %s, want object", got.Level.Sectors[0].Answer)
	}
	if string(got.Level.Bonuses[0].Answer) != "null" {
		t.Fatalf("bonus answer = %s, want null", got.Level.Bonuses[0].Answer)
	}
}

func TestGameModelRoundTripsNullLevelAndSummaryFields(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"Event": 19,
		"Levels": [{
			"LevelId": 3,
			"LevelNumber": 3,
			"LevelName": "synthetic",
			"Dismissed": false,
			"IsPassed": false,
			"Task": null,
			"LevelAction": null
		}],
		"Level": null,
		"EngineAction": null
	}`)
	var model GameModel
	if err := json.Unmarshal(raw, &model); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if model.Level != nil {
		t.Fatalf("Level = %#v, want nil", model.Level)
	}
	encoded, err := json.Marshal(model)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("Unmarshal encoded model: %v", err)
	}
	if got["Level"] != nil {
		t.Fatalf("Level = %#v, want null", got["Level"])
	}
	summary := got["Levels"].([]any)[0].(map[string]any)
	for _, key := range []string{"Task", "LevelAction"} {
		if value, ok := summary[key]; !ok || value != nil {
			t.Errorf("%s = %#v, present = %v; want null", key, value, ok)
		}
	}
}

func TestNullableStringFieldsPreserveNullAndMutations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []byte
		mutate   func(*Bonus, *CodeAction, *GameInfo)
		wantNull []string
		wantText map[string]string
	}{
		{
			name:  "preserves decoded nulls",
			input: []byte(`{"Bonus":{"Name":null,"Task":null,"Help":null},"Action":{"LocDateTime":null},"Game":{"FeeName":null}}`),
			wantNull: []string{
				"Bonus.Name", "Bonus.Task", "Bonus.Help", "Action.LocDateTime", "Game.FeeName",
			},
		},
		{
			name:  "uses mutated public strings",
			input: []byte(`{"Bonus":{"Name":null,"Task":null,"Help":null},"Action":{"LocDateTime":null},"Game":{"FeeName":null}}`),
			mutate: func(bonus *Bonus, action *CodeAction, game *GameInfo) {
				bonus.Name = "name"
				bonus.Task = "task"
				bonus.Help = "help"
				action.LocDateTime = "date"
				game.FeeName = "fee"
			},
			wantText: map[string]string{
				"Bonus.Name": "name", "Bonus.Task": "task", "Bonus.Help": "help",
				"Action.LocDateTime": "date", "Game.FeeName": "fee",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var decoded struct {
				Bonus  Bonus      `json:"Bonus"`
				Action CodeAction `json:"Action"`
				Game   GameInfo   `json:"Game"`
			}
			if err := json.Unmarshal(tt.input, &decoded); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if tt.mutate != nil {
				tt.mutate(&decoded.Bonus, &decoded.Action, &decoded.Game)
			}
			encoded, err := json.Marshal(decoded)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			var values map[string]any
			if err := json.Unmarshal(encoded, &values); err != nil {
				t.Fatalf("Unmarshal encoded JSON: %v", err)
			}
			for _, path := range tt.wantNull {
				if valueAtPath(t, values, path) != nil {
					t.Errorf("%s = %v, want null", path, valueAtPath(t, values, path))
				}
			}
			for path, want := range tt.wantText {
				if got := valueAtPath(t, values, path); got != want {
					t.Errorf("%s = %v, want %q", path, got, want)
				}
			}
		})
	}
}

func TestGameListResponsePreservesObservedHomeFields(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"ComingGames": [],
		"ActiveGames": [{
			"GameID": 1, "Owner": null, "Type": 0, "CertificatePlaces": 0,
			"CertificateAccessMode": 0, "DescrWrapped": "", "ShowFinishPlace": false,
			"StatusId": 0, "Status": 0, "IsAvailableAfterFinished": false,
			"StatAvailabilityTypeID": 0, "StatAvailabilityType": 0, "RateClosed": false,
			"LevelsSequenceId": 0, "LevelsSequence": 0, "QualityRateCalculated": false,
			"Zone": 0, "AllowMakeStakes": false, "HidePlayersList": false,
			"ReplaceNlToBr": false, "HideGameDescr": false, "DisplayAnnouncement": 0,
			"ForUserID": 0, "AFC": 0, "IsQualityRateVisible": false,
			"AuthorIndexCalculated": false, "FeeName": null, "State": 0,
			"IsModified": false, "IsNewObject": false, "ReadOnly": false, "SyncRoot": {}
		}],
		"Error": 0, "Message": null, "IpUnblockUrl": null, "BruteForceUnblockUrl": null,
		"ConfirmEmailUrl": null, "CaptchaUrl": null, "AdminWhoCanActivate": null
	}`)
	var response GameListResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("Unmarshal encoded JSON: %v", err)
	}
	for _, key := range []string{
		"Error", "Message", "IpUnblockUrl", "BruteForceUnblockUrl", "ConfirmEmailUrl", "CaptchaUrl", "AdminWhoCanActivate",
	} {
		if _, ok := got[key]; !ok {
			t.Errorf("missing top-level %s", key)
		}
	}
	game, ok := got["ActiveGames"].([]any)[0].(map[string]any)
	if !ok {
		t.Fatal("ActiveGames[0] is not an object")
	}
	for _, key := range []string{
		"Owner", "Type", "CertificatePlaces", "CertificateAccessMode", "DescrWrapped", "ShowFinishPlace",
		"StatusId", "Status", "IsAvailableAfterFinished", "StatAvailabilityTypeID", "StatAvailabilityType",
		"RateClosed", "LevelsSequenceId", "LevelsSequence", "QualityRateCalculated", "Zone", "AllowMakeStakes",
		"HidePlayersList", "ReplaceNlToBr", "HideGameDescr", "DisplayAnnouncement", "ForUserID", "AFC",
		"IsQualityRateVisible", "AuthorIndexCalculated", "FeeName", "State", "IsModified", "IsNewObject",
		"ReadOnly", "SyncRoot",
	} {
		if _, ok := game[key]; !ok {
			t.Errorf("missing game field %s", key)
		}
	}
}

func TestGameListResponseDecodesFractionalAFC(t *testing.T) {
	t.Parallel()

	// tech.en.cx returns AFC as a fractional number (e.g. 0.1) for some games.
	raw := []byte(`{
		"ComingGames": [],
		"ActiveGames": [{"GameID": 81701, "AFC": 0.1}]
	}`)
	var response GameListResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got := response.ActiveGames[0].AFC; got != 0.1 {
		t.Errorf("AFC = %v, want 0.1", got)
	}
}

func TestLevelDoesNotFabricateAbsentTask(t *testing.T) {
	t.Parallel()

	var level Level
	if err := json.Unmarshal([]byte(`{"LevelId":1}`), &level); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	encoded, err := json.Marshal(level)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("Unmarshal encoded JSON: %v", err)
	}
	if _, ok := got["Task"]; ok {
		t.Fatal("Task was encoded despite being absent")
	}
}

func valueAtPath(t *testing.T, values map[string]any, path string) any {
	t.Helper()
	var value any = values
	for _, key := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("%s does not contain object %s", path, key)
		}
		value, ok = object[key]
		if !ok {
			t.Fatalf("missing %s", path)
		}
	}
	return value
}
