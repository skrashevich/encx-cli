package main

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"os"
	"strings"

	"github.com/skrashevich/encx-cli/encx/scenario"
)

// maxScenarioBytesForLLM bounds one admin_game_scenario result when nothing
// better is known — outside an agent run, or before its first request. Inside a
// run the bound is the room the model's window actually has left; see
// observedPicoProvider.resultRoom.
const maxScenarioBytesForLLM = maxToolContentForLLM

// minScenarioPageBytes is the page no crowding shrinks below: a page that
// cannot hold one ordinary level only makes the model call again for nothing.
const minScenarioPageBytes = 4 * 1024

// scenarioPage is what admin_game_scenario prints. AllLevelNumbers is set when
// the result carries only some of the levels, so the run knows what the game
// has without taking a subset for the whole of it.
type scenarioPage struct {
	scenario.Document
	AllLevelNumbers []int `json:"all_level_numbers,omitempty"`
}

// selectScenarioLevels keeps levels from..to (either bound 0 means open).
func selectScenarioLevels(doc *scenario.Document, from, to int) scenarioPage {
	page := scenarioPage{Document: *doc}
	if from <= 0 && to <= 0 {
		return page
	}
	page.Levels = nil
	for _, level := range doc.Levels {
		page.AllLevelNumbers = append(page.AllLevelNumbers, level.Number)
		if (from <= 0 || level.Number >= from) && (to <= 0 || level.Number <= to) {
			page.Levels = append(page.Levels, level)
		}
	}
	if page.Levels == nil {
		page.Levels = []scenario.Level{}
	}
	return page
}

// outputCompactJSON prints v without indentation or HTML escaping. A scenario
// is mostly HTML, and "<" printed as \u003c costs six bytes where one would do
// on every turn the result stays in the conversation.
//
// It is the v1 encoder on purpose: scenario HTML comes from a remote site, and
// v1 repairs invalid UTF-8 where v2 refuses the whole document, and v1's
// omitempty drops the zero timers every level would otherwise print.
func outputCompactJSON(v any) {
	enc := jsonv1.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		fatal("encode result: %v", err)
	}
}

// wireBytes is what s costs inside a request: tool results travel as JSON
// strings, so quotes and newlines are paid for twice.
func wireBytes(s string) int {
	b, err := json.Marshal(s)
	if err != nil {
		return len(s)
	}
	return len(b)
}

// pageScenarioForLLM cuts a scenario that does not fit into room bytes of
// request into whole levels.
//
// A whole game is not bounded by anything: a 30-level championship came back
// as 560 KB, five times the request budget, and because the newest result is
// never compressed the next LLM call failed and ended the run. Levels are
// delivered in order while they fit, and the payload says which are missing
// and which from_level brings the next page, so the model walks the game
// instead of taking the first page for all of it. A single level that alone
// exceeds the room has its longest texts shortened and is listed in
// truncated_levels.
func pageScenarioForLLM(result string, room int) string {
	room = max(room, minScenarioPageBytes)
	if wireBytes(result) <= room {
		return result
	}
	var head map[string]jsontext.Value
	if json.Unmarshal([]byte(result), &head) != nil {
		return result
	}
	var levels []jsontext.Value
	if raw, ok := head["levels"]; !ok || json.Unmarshal(raw, &levels) != nil || len(levels) == 0 {
		return result
	}
	numbers := make([]int, len(levels))
	for i, raw := range levels {
		var level struct {
			Number int `json:"number"`
		}
		_ = json.Unmarshal(raw, &level)
		numbers[i] = level.Number
	}
	// A page of a ranged read already lists the whole game; decode it into a
	// slice of its own, or it overwrites the numbers of this page.
	all := numbers
	if raw, ok := head["all_level_numbers"]; ok {
		var listed []int
		if json.Unmarshal(raw, &listed) == nil && len(listed) > 0 {
			all = listed
		}
	}
	delete(head, "levels")
	// The envelope, the level lists and the hint are measured, not guessed: a
	// 200-level game lists its numbers twice.
	envelope, _ := json.Marshal(head)
	budget := max(room-wireBytes(string(envelope))-16*len(all)-1024, minScenarioPageBytes)

	var delivered []jsontext.Value
	var truncated []int
	used := 0
	for i, raw := range levels {
		cost := wireBytes(string(raw)) + 1
		if used+cost > budget {
			if i > 0 {
				break
			}
			raw = shrinkScenarioLevel(raw, budget)
			truncated = append(truncated, numbers[i])
			cost = wireBytes(string(raw)) + 1
		}
		delivered = append(delivered, raw)
		used += cost
	}

	set := func(key string, v any) {
		b, _ := json.Marshal(v)
		head[key] = b
	}
	set("levels", delivered)
	set("truncated", true)
	set("all_level_numbers", all)
	if len(truncated) > 0 {
		set("truncated_levels", truncated)
	}
	var hint strings.Builder
	fmt.Fprintf(&hint, "Only levels %d–%d are shown: the scenario is %d bytes and the model context has room for about %d now.",
		numbers[0], numbers[len(delivered)-1], len(result), room)
	// Pages go by position and from_level by number: a next number that is not
	// past the last one delivered would send the model back to a page it has.
	if len(delivered) < len(levels) && numbers[len(delivered)] > numbers[len(delivered)-1] {
		next := numbers[len(delivered)]
		set("next_from_level", next)
		set("remaining_level_numbers", numbers[len(delivered):])
		fmt.Fprintf(&hint, " Note what you need from these levels, then call admin_game_scenario with from_level=%d; "+
			"continue until next_from_level is absent before concluding anything about the whole game.", next)
	}
	if len(truncated) > 0 {
		fmt.Fprintf(&hint, " Long texts of level %d were shortened (marked [... N bytes omitted]); do not edit text you have not seen in full.", truncated[0])
	}
	set("hint", hint.String())

	out, err := json.Marshal(head, json.Deterministic(true))
	if err != nil {
		return result
	}
	return string(out)
}

// shrinkScenarioLevel caps every string in one level, halving the cap until
// the level fits the budget.
func shrinkScenarioLevel(raw jsontext.Value, budget int) jsontext.Value {
	var level any
	if json.Unmarshal(raw, &level) != nil {
		return raw
	}
	limit := budget / 2
	for {
		out, err := json.Marshal(capJSONStrings(level, limit))
		if err == nil && (wireBytes(string(out)) <= budget || limit <= 256) {
			return out
		}
		if err != nil {
			return raw
		}
		limit /= 2
	}
}

func capJSONStrings(v any, limit int) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, item := range val {
			out[k] = capJSONStrings(item, limit)
		}
		return out
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			out[i] = capJSONStrings(item, limit)
		}
		return out
	case string:
		if len(val) <= limit {
			return val
		}
		cut := truncateUTF8(val, limit)
		return fmt.Sprintf("%s[... %d bytes omitted]", cut, len(val)-len(cut))
	default:
		return v
	}
}
