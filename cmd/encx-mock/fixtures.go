package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Override with ENCX_MOCK_HAR to regenerate-compatible raw templates at startup.

//go:embed fixtures/game_model_template.json
var embeddedGameModelTemplate []byte

//go:embed fixtures/game_info.json
var embeddedGameInfoTemplate []byte

//go:embed fixtures/user_details.html
var embeddedUserDetailsTemplate string

type fixtureSet struct {
	gameModelTemplate map[string]any
	gameInfoTemplate  map[string]any
	userDetailsHTML   string
}

func loadFixtures() (*fixtureSet, error) {
	gameModelRaw := embeddedGameModelTemplate
	gameInfoRaw := embeddedGameInfoTemplate
	userDetails := embeddedUserDetailsTemplate

	if path := strings.TrimSpace(os.Getenv("ENCX_MOCK_HAR")); path != "" {
		derived, err := deriveFixturesFromHAR(path)
		if err != nil {
			return nil, fmt.Errorf("ENCX_MOCK_HAR: %w", err)
		}
		gameModelRaw = derived.gameModel
		gameInfoRaw = derived.gameInfo
		if derived.userDetails != "" {
			userDetails = derived.userDetails
		}
	}

	gameModel, err := parseJSONObject(gameModelRaw)
	if err != nil {
		return nil, fmt.Errorf("game model template: %w", err)
	}
	gameInfo, err := parseJSONObject(gameInfoRaw)
	if err != nil {
		return nil, fmt.Errorf("game info template: %w", err)
	}

	return &fixtureSet{
		gameModelTemplate: gameModel,
		gameInfoTemplate:  gameInfo,
		userDetailsHTML:   userDetails,
	}, nil
}

func parseJSONObject(raw []byte) (map[string]any, error) {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func cloneJSONObject(src map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(src)
	if err != nil {
		return nil, err
	}
	return parseJSONObject(raw)
}
