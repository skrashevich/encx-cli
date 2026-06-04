package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type derivedFixtures struct {
	gameModel   []byte
	gameInfo    []byte
	userDetails string
}

func deriveFixturesFromHAR(path string) (*derivedFixtures, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var doc struct {
		Log struct {
			Entries []struct {
				Request struct {
					Method string `json:"method"`
					URL    string `json:"url"`
				} `json:"request"`
				Response struct {
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"response"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse HAR: %w", err)
	}

	out := &derivedFixtures{}
	for _, entry := range doc.Log.Entries {
		url := entry.Request.URL
		body := entry.Response.Content.Text
		switch {
		case entry.Request.Method == "POST" && strings.Contains(url, "/gameengines/encounter/play/") && out.gameModel == nil && body != "":
			model, err := adaptGameModelFromHAR(body)
			if err != nil {
				return nil, err
			}
			out.gameModel = model
		case entry.Request.Method == "GET" && strings.Contains(url, "/home/?json=1") && out.gameInfo == nil && body != "":
			info, err := adaptGameInfoFromHAR(body)
			if err != nil {
				return nil, err
			}
			out.gameInfo = info
		case entry.Request.Method == "GET" && strings.Contains(url, "/UserDetails.aspx") && out.userDetails == "" && body != "":
			out.userDetails = minimalUserDetailsHTML()
		}
	}

	if out.gameModel == nil {
		return nil, fmt.Errorf("no game play JSON response found in HAR")
	}
	if out.gameInfo == nil {
		return nil, fmt.Errorf("no /home/?json=1 response found in HAR")
	}
	return out, nil
}

func adaptGameModelFromHAR(body string) ([]byte, error) {
	var model map[string]any
	if err := json.Unmarshal([]byte(body), &model); err != nil {
		return nil, err
	}

	model["GameId"] = mockGameID
	model["GameTitle"] = "Mock Game 2026"
	model["TeamId"] = mockTeamID
	model["TeamName"] = "MockTeam"
	model["Login"] = "{{LOGIN}}"
	model["EngineAction"] = map[string]any{
		"LevelNumber": 0,
		"LevelAction": map[string]any{"Answer": nil, "IsCorrectAnswer": nil},
		"BonusAction": map[string]any{"Answer": nil, "IsCorrectAnswer": nil},
		"PenaltyAction": map[string]any{
			"PenaltyId":  0,
			"ActionType": 0,
		},
		"GameId":  mockGameID,
		"LevelId": 0,
	}

	if level, ok := model["Level"].(map[string]any); ok {
		level["MixedActions"] = []any{}
		level["PassedSectorsCount"] = 0
		level["SectorsLeftToClose"] = level["RequiredSectorsCount"]
		level["IsPassed"] = false
		if sectors, ok := level["Sectors"].([]any); ok {
			for i, item := range sectors {
				sector, ok := item.(map[string]any)
				if !ok {
					continue
				}
				sector["IsAnswered"] = false
				sector["Answer"] = nil
				sector["Order"] = i + 1
			}
		}
	}

	if levels, ok := model["Levels"].([]any); ok && len(levels) > mockLevelCount {
		model["Levels"] = levels[:mockLevelCount]
	}

	return json.MarshalIndent(model, "", "  ")
}

func adaptGameInfoFromHAR(body string) ([]byte, error) {
	var home map[string]any
	if err := json.Unmarshal([]byte(body), &home); err != nil {
		return nil, err
	}
	active, ok := home["ActiveGames"].([]any)
	if !ok || len(active) == 0 {
		return nil, fmt.Errorf("ActiveGames missing in HAR home response")
	}
	game, ok := active[0].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("ActiveGames[0] has unexpected shape")
	}
	game["GameID"] = mockGameID
	game["Title"] = "Mock Game 2026"
	game["GameNum"] = 1
	game["LevelNumber"] = mockLevelCount
	game["AlwaysAvailable"] = true
	game["PublicAccess"] = true
	return json.MarshalIndent(game, "", "  ")
}

func minimalUserDetailsHTML() string {
	return embeddedUserDetailsTemplate
}
