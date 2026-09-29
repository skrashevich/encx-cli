package main

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"strconv"
	"strings"

	"github.com/skrashevich/encx-cli/encx"
)

// Pointers distinguish an omitted field from an explicit zero or false.
type adminBonusPatch struct {
	LevelID      *int      `json:"level_id"`
	Name         *string   `json:"name"`
	Task         *string   `json:"task"`
	Hint         *string   `json:"hint"`
	Answers      *[]string `json:"answers"`
	AwardHours   *int      `json:"award_hours"`
	AwardMinutes *int      `json:"award_minutes"`
	AwardSeconds *int      `json:"award_seconds"`
	Negative     *bool     `json:"negative"`
}

func (p adminBonusPatch) apply(b *encx.AdminBonus) error {
	if p.LevelID != nil && *p.LevelID < -1 {
		return fmt.Errorf("level_id must be a positive level ID, or 0/-1 for all levels")
	}
	for key, value := range map[string]*int{
		"award_hours": p.AwardHours, "award_minutes": p.AwardMinutes, "award_seconds": p.AwardSeconds,
	} {
		if value != nil && *value < 0 {
			return fmt.Errorf("%s must be a non-negative integer; use negative=true for a penalty", key)
		}
	}
	if p.LevelID != nil {
		b.LevelID = *p.LevelID
		b.LevelIDs = []int{}
		if *p.LevelID > 0 {
			b.LevelIDs = []int{*p.LevelID}
		}
	}
	if p.Name != nil {
		b.Name = *p.Name
	}
	if p.Task != nil {
		b.Task = *p.Task
	}
	if p.Hint != nil {
		b.Hint = *p.Hint
	}
	if p.Answers != nil {
		b.Answers = *p.Answers
	}
	if p.AwardHours != nil {
		b.AwardHours = *p.AwardHours
	}
	if p.AwardMinutes != nil {
		b.AwardMinutes = *p.AwardMinutes
	}
	if p.AwardSeconds != nil {
		b.AwardSeconds = *p.AwardSeconds
	}
	if p.Negative != nil {
		b.Negative = *p.Negative
	}
	return nil
}

func parseAdminBonusPatch(args []string) (adminBonusPatch, error) {
	var p adminBonusPatch
	for _, arg := range args {
		key, val, ok := strings.Cut(arg, "=")
		if !ok {
			return p, fmt.Errorf("arguments must be in key=value format: %s", arg)
		}
		switch strings.ToLower(key) {
		case "level_id":
			n, err := strconv.Atoi(val)
			if err != nil || n < -1 {
				return p, fmt.Errorf("level_id must be a positive level ID, or 0/-1 for all levels")
			}
			p.LevelID = new(n)
		case "name":
			p.Name = new(val)
		case "task":
			p.Task = new(val)
		case "hint":
			p.Hint = new(val)
		case "answers":
			p.Answers = new(strings.Split(val, ","))
		case "award_hours", "award_minutes", "award_seconds":
			n, err := strconv.Atoi(val)
			if err != nil || n < 0 {
				return p, fmt.Errorf("%s must be a non-negative integer", key)
			}
			switch strings.ToLower(key) {
			case "award_hours":
				p.AwardHours = new(n)
			case "award_minutes":
				p.AwardMinutes = new(n)
			case "award_seconds":
				p.AwardSeconds = new(n)
			}
		case "negative":
			v, err := strconv.ParseBool(val)
			if err != nil {
				return p, fmt.Errorf("negative must be true or false")
			}
			p.Negative = new(v)
		default:
			return p, fmt.Errorf("unknown field: %s (supported: level_id, name, task, hint, answers, award_hours, award_minutes, award_seconds, negative)", key)
		}
	}
	return p, nil
}

func decodeAdminBonusPatch(raw string) adminBonusPatch {
	var p adminBonusPatch
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		fatal("Invalid bonus fields: %v", err)
	}
	// Keep the same bare-Unicode normalization as other LLM mutation tools.
	for _, value := range []*string{p.Name, p.Task, p.Hint} {
		if value != nil {
			*value = decodeBareUnicodeEscapes(*value)
		}
	}
	if p.Answers != nil {
		for i, answer := range *p.Answers {
			(*p.Answers)[i] = decodeBareUnicodeEscapes(answer)
		}
	}
	if err := p.apply(&encx.AdminBonus{}); err != nil {
		fatal("Invalid bonus fields: %v", err)
	}
	return p
}

func updateAdminBonus(ctx context.Context, cfg *config, client *encx.Client, level, id int, patch adminBonusPatch, session *llmSession) {
	requireGameId(cfg)
	if level <= 0 || id <= 0 {
		fatal("Level number and bonus ID must be positive")
	}
	if patch == (adminBonusPatch{}) {
		fatal("Specify at least one bonus field to update")
	}
	if err := patch.apply(&encx.AdminBonus{}); err != nil {
		fatal("%v", err)
	}
	bonus, err := client.AdminGetBonus(ctx, cfg.gameId, level, id)
	if err != nil {
		fatal("Failed to read current bonus: %v", err)
	}
	if err := patch.apply(bonus); err != nil {
		fatal("%v", err)
	}
	if session != nil && patch.Answers != nil {
		if err := checkRequestedCodeSuffix(session.latestUserMessage, bonus.Name, bonus.Answers); err != nil {
			fatal("%v", err)
		}
	}
	if err := client.AdminUpdateBonus(ctx, cfg.gameId, level, id, *bonus); err != nil {
		fatal("Failed to update bonus: %v", err)
	}
	if cfg.jsonOutput {
		outputJSON(map[string]any{
			"success": true, "bonus_id": id, "verified": false,
			"verification_note": "Update request completed; stored values have not been read back. Do not report verified success without a separate read.",
		})
		return
	}
	fmt.Printf("Bonus %d updated\n", id)
}
