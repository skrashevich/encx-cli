package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

type derivedFixtures struct {
	gameModel   []byte
	gameInfo    []byte
	userDetails string
	profile     *protocolProfile
}

func deriveFixturesFromHAR(path string) (*derivedFixtures, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	profile := &protocolProfile{}
	seenRecords := make(map[string]int)
	topology := make(map[int]levelTopology)
	err = streamHAREntries(file, func(entry harEntry, content, encoding string) error {
		record, body, err := classifyHAREntry(entry, content, encoding)
		if err != nil {
			return err
		}
		if record.Kind == "" {
			return nil
		}

		key := record.Kind + "|" + record.Variant + "|" + record.Method + "|" + fmt.Sprint(record.Status) + "|" + responseShape(record.Kind, body)
		if index, ok := seenRecords[key]; ok {
			profile.Records[index].Count++
			if record.StartedAt.Before(profile.Records[index].StartedAt) {
				profile.Records[index].StartedAt = record.StartedAt
			}
		} else {
			record.Count = 1
			seenRecords[key] = len(profile.Records)
			profile.Records = append(profile.Records, record)
		}

		switch record.Kind {
		case "anti-bot-redirect":
			profile.HasAntiBotRedirect = true
		case "transport-failure":
			profile.HasTransportFailure = true
		}
		if body == nil {
			return nil
		}
		if event, ok := asInt(body["Event"]); ok && isTransitionSnapshot(body) {
			profile.TransitionEvents = appendUniqueInt(profile.TransitionEvents, event)
		}
		if isEnginePath(entry.requestURL.Path) {
			for _, level := range extractTopology(body) {
				if existing, ok := topology[level.Number]; !ok || level.SectorCount > existing.SectorCount {
					topology[level.Number] = level
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("parse HAR: %w", err)
	}

	sort.SliceStable(profile.Records, func(i, j int) bool {
		return profile.Records[i].StartedAt.Before(profile.Records[j].StartedAt)
	})
	profile.Levels = orderedTopology(topology)
	if len(profile.Levels) == 0 {
		return nil, fmt.Errorf("no game play JSON response found in HAR")
	}
	if !hasRecord(profile.Records, "home") {
		return nil, fmt.Errorf("no /home/?json=1 response found in HAR")
	}

	gameModel, err := profileGameModelFixture(profile)
	if err != nil {
		return nil, err
	}
	gameInfo, err := profileGameInfoFixture(profile)
	if err != nil {
		return nil, err
	}
	return &derivedFixtures{
		gameModel:   gameModel,
		gameInfo:    gameInfo,
		userDetails: minimalUserDetailsHTML(),
		profile:     profile,
	}, nil
}

type harEntry struct {
	started    time.Time
	method     string
	requestURL *url.URL
	status     int
}

func streamHAREntries(file *os.File, handle func(harEntry, string, string) error) error {
	decoder := json.NewDecoder(file)
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("HAR root must be an object")
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		if key != "log" {
			var discard json.RawMessage
			if err := decoder.Decode(&discard); err != nil {
				return err
			}
			continue
		}
		return streamHARLog(decoder, handle)
	}
	return fmt.Errorf("HAR log missing")
}

func streamHARLog(decoder *json.Decoder, handle func(harEntry, string, string) error) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return fmt.Errorf("HAR log must be an object")
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return err
		}
		if key != "entries" {
			var discard json.RawMessage
			if err := decoder.Decode(&discard); err != nil {
				return err
			}
			continue
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		if delim, ok := token.(json.Delim); !ok || delim != '[' {
			return fmt.Errorf("HAR log.entries must be an array")
		}
		for decoder.More() {
			var raw struct {
				StartedDateTime string `json:"startedDateTime"`
				Request         struct {
					Method string `json:"method"`
					URL    string `json:"url"`
				} `json:"request"`
				Response struct {
					Status  int `json:"status"`
					Content struct {
						Text     string `json:"text"`
						Encoding string `json:"encoding"`
					} `json:"content"`
				} `json:"response"`
			}
			if err := decoder.Decode(&raw); err != nil {
				return err
			}
			started, err := time.Parse(time.RFC3339Nano, raw.StartedDateTime)
			if err != nil {
				return fmt.Errorf("invalid HAR startedDateTime")
			}
			requestURL, err := url.Parse(raw.Request.URL)
			if err != nil {
				return fmt.Errorf("invalid HAR request URL")
			}
			if err := handle(harEntry{
				started: started, method: strings.ToUpper(raw.Request.Method), requestURL: requestURL,
				status: raw.Response.Status,
			}, raw.Response.Content.Text, raw.Response.Content.Encoding); err != nil {
				return err
			}
		}
		if _, err := decoder.Token(); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("HAR log.entries missing")
}

func classifyHAREntry(entry harEntry, content, encoding string) (protocolRecord, map[string]any, error) {
	record := protocolRecord{
		Method:    entry.method,
		Status:    entry.status,
		StartedAt: entry.started,
	}
	if entry.status == 0 {
		record.Kind = "transport-failure"
		return record, nil, nil
	}

	if entry.status == 302 && isEnginePath(entry.requestURL.Path) && entry.method == "POST" {
		record.Kind = "anti-bot-redirect"
		return record, nil, nil
	}

	if entry.status != 200 {
		return record, nil, nil
	}
	switch {
	case isHomePath(entry.requestURL.Path) && entry.method == "GET":
		record.Kind = "home"
	case isEnginePath(entry.requestURL.Path) && entry.method == "GET":
		record.Kind = "engine-poll"
	case isEnginePath(entry.requestURL.Path) && entry.method == "POST":
		record.Kind = "level-action"
	default:
		return record, nil, nil
	}

	body, err := decodeHARContent(content, encoding)
	if err != nil {
		return protocolRecord{}, nil, err
	}
	var decoded map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &decoded); err != nil {
			return protocolRecord{}, nil, fmt.Errorf("decode HAR JSON response")
		}
	}

	if isEnginePath(entry.requestURL.Path) && entry.method == "POST" && hasBonusAction(decoded) {
		record.Kind = "bonus-action"
	}
	if isEnginePath(entry.requestURL.Path) && decoded != nil {
		if isTransitionSnapshot(decoded) {
			record.Variant = "transition"
			record.Event, _ = asInt(decoded["Event"])
		} else {
			record.Variant = "normal"
		}
	}
	return record, decoded, nil
}

func decodeHARContent(text, encoding string) ([]byte, error) {
	if strings.EqualFold(encoding, "base64") {
		decoded, err := base64.StdEncoding.DecodeString(text)
		if err != nil {
			return nil, fmt.Errorf("decode HAR base64 content: %w", err)
		}
		return decoded, nil
	}
	return []byte(text), nil
}

func isEnginePath(path string) bool {
	return strings.HasPrefix(path, "/gameengines/encounter/play/")
}

func isHomePath(path string) bool {
	return path == "/home/" || path == "/home"
}

func isTransitionSnapshot(model map[string]any) bool {
	if model == nil {
		return false
	}
	level, hasLevel := model["Level"]
	levels, hasLevels := model["Levels"].([]any)
	return hasLevel && level == nil && hasLevels && len(levels) == 0
}

func hasBonusAction(model map[string]any) bool {
	action, _ := model["EngineAction"].(map[string]any)
	_, ok := action["BonusAction"]
	return ok
}

func responseShape(kind string, model map[string]any) string {
	if model == nil {
		return kind
	}
	raw, _ := json.Marshal(structuralShape(model))
	return kind + "|" + string(raw)
}

func structuralShape(value any) any {
	switch value := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, child := range value {
			switch key {
			case "Answer", "Login", "TeamName", "Task", "TaskText", "TaskTextFormatted", "Help", "Message", "Text", "Name", "Title":
				out[key] = "<redacted>"
			default:
				out[key] = structuralShape(child)
			}
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = structuralShape(child)
		}
		return out
	case string:
		return "<redacted>"
	default:
		return value
	}
}

func extractTopology(model map[string]any) []levelTopology {
	if model == nil {
		return nil
	}
	levels := make(map[int]levelTopology)
	if summaries, ok := model["Levels"].([]any); ok {
		for index, item := range summaries {
			summary, ok := item.(map[string]any)
			if !ok {
				continue
			}
			number, ok := asInt(summary["LevelNumber"])
			if !ok {
				number = index + 1
			}
			levels[number] = levelTopology{
				Number: number, Dismissed: asBool(summary["Dismissed"]), Passed: asBool(summary["IsPassed"]),
			}
		}
	}
	active, ok := model["Level"].(map[string]any)
	if !ok {
		return mapsToTopology(levels)
	}
	number, ok := asInt(active["Number"])
	if !ok {
		number = 1
	}
	level := levels[number]
	level.Number = number
	level.SectorCount = lengthOf(active["Sectors"])
	level.RequiredSectorCount, _ = asInt(active["RequiredSectorsCount"])
	if level.RequiredSectorCount == 0 {
		level.RequiredSectorCount = level.SectorCount
	}
	level.BonusCount = lengthOf(active["Bonuses"])
	level.HintCount = lengthOf(active["Helps"])
	level.MessageCount = lengthOf(active["Messages"])
	level.Dismissed = asBool(active["Dismissed"])
	level.Passed = asBool(active["IsPassed"])
	levels[number] = level
	return mapsToTopology(levels)
}

func mapsToTopology(levels map[int]levelTopology) []levelTopology {
	out := make([]levelTopology, 0, len(levels))
	for _, level := range levels {
		out = append(out, level)
	}
	return out
}

func orderedTopology(levels map[int]levelTopology) []levelTopology {
	out := mapsToTopology(levels)
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

func profileGameModelFixture(profile *protocolProfile) ([]byte, error) {
	active := profile.Levels[0]
	sectors := make([]any, active.SectorCount)
	for i := range sectors {
		sectors[i] = map[string]any{
			"SectorId":   mockSectorBaseID + i + 1,
			"Order":      i + 1,
			"Name":       fmt.Sprintf("Sector %d", i+1),
			"IsAnswered": false,
			"Answer":     nil,
		}
	}
	bonuses := make([]any, active.BonusCount)
	for i := range bonuses {
		bonuses[i] = map[string]any{
			"BonusId": mockBonusBaseID + i + 1, "Number": i + 1, "Name": "",
			"Task": nil, "Help": nil, "IsAnswered": false, "Answer": nil,
		}
	}
	summaries := make([]any, len(profile.Levels))
	for i, level := range profile.Levels {
		summaries[i] = map[string]any{
			"LevelId": mockLevelBaseID + level.Number, "LevelNumber": level.Number,
			"LevelName": fmt.Sprintf("Level %d", level.Number), "Dismissed": level.Dismissed,
			"IsPassed": level.Passed, "Task": nil, "LevelAction": nil,
		}
	}
	model := map[string]any{
		"Level": map[string]any{
			"LevelId": mockLevelBaseID + active.Number, "Number": active.Number,
			"Name": fmt.Sprintf("Level %d", active.Number), "RequiredSectorsCount": active.RequiredSectorCount,
			"PassedSectorsCount": 0, "SectorsLeftToClose": active.RequiredSectorCount,
			"IsPassed": false, "Dismissed": active.Dismissed, "Sectors": sectors, "Bonuses": bonuses,
			"Tasks": emptyObjects(active.HintCount), "Messages": emptyObjects(active.MessageCount),
			"Helps": emptyObjects(active.HintCount), "MixedActions": []any{}, "PenaltyHelps": []any{},
		},
		"Levels": summaries, "GameId": mockGameID, "GameTitle": "Mock Game 2026",
		"TeamId": mockTeamID, "TeamName": "MockTeam", "Login": "{{LOGIN}}",
		"Event": 0, "EngineAction": idleEngineAction(mockGameID),
	}
	return json.Marshal(model)
}

func profileGameInfoFixture(profile *protocolProfile) ([]byte, error) {
	return json.Marshal(map[string]any{
		"GameID": mockGameID, "Title": "Mock Game 2026", "GameNum": 1,
		"LevelNumber": len(profile.Levels), "AlwaysAvailable": true, "PublicAccess": true,
	})
}

func emptyObjects(count int) []any {
	items := make([]any, count)
	for i := range items {
		items[i] = map[string]any{}
	}
	return items
}

func hasRecord(records []protocolRecord, kind string) bool {
	for _, record := range records {
		if record.Kind == kind {
			return true
		}
	}
	return false
}

func appendUniqueInt(items []int, value int) []int {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func asInt(value any) (int, bool) {
	number, ok := value.(float64)
	return int(number), ok
}

func asBool(value any) bool {
	result, _ := value.(bool)
	return result
}

func lengthOf(value any) int {
	items, _ := value.([]any)
	return len(items)
}

func minimalUserDetailsHTML() string {
	return embeddedUserDetailsTemplate
}
