package main

import (
	"fmt"
	"net/http"

	"github.com/skrashevich/encx-cli/encx/enapi"
)

// Export the mutable admin state, not the startup fixture, so import round trips
// exercise the values actually accepted by the HTTP mutation handlers.
func (s *server) handleScenarioExport(w http.ResponseWriter, r *http.Request) {
	st := s.adminGameFrom(w, r)
	if st == nil {
		return
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	doc := enapi.GameScenario{Game: &enapi.LocalizedGame{ID: st.gameID, GameNum: st.gameNum, Title: st.title}}
	for i, level := range st.levels {
		out := enapi.LevelScenario{LevelID: level.id, LevelNumber: i + 1, LevelName: level.name, Comment: level.comment}
		if level.timeoutSec > 0 {
			out.AutopassText = fmt.Sprintf("через %d секунд", level.timeoutSec)
		}
		if level.timeoutAwardSec < 0 {
			out.AutopassText += fmt.Sprintf(", %d секунд штрафа", -level.timeoutAwardSec)
		}
		if level.requiredSectorsCount > 0 {
			out.SectorsCompletionRule = fmt.Sprintf("необходимо выполнить %d секторов", level.requiredSectorsCount)
		}
		for _, task := range level.tasks {
			out.Tasks = append(out.Tasks, enapi.LevelTaskScenario{TaskID: task.id, TaskText: task.text})
		}
		for _, help := range level.helps {
			h := enapi.LevelHelpScenario{HelpID: help.id, HelpText: help.text, Timeout: help.timeout, IsPenalty: help.isPenalty, PenaltyTime: help.penaltyTime, PenaltyComment: help.penaltyComment, RequestPenaltyConfirm: help.requestConfirm}
			if help.isPenalty {
				out.PenaltyHelps = append(out.PenaltyHelps, h)
			} else {
				out.Helps = append(out.Helps, h)
			}
		}
		for _, sector := range level.sectors {
			sec := enapi.LevelSectorScenario{SectorID: sector.id, SectorName: sector.name}
			for _, a := range level.answers {
				if a.sectorID == sector.id {
					sec.Answers = append(sec.Answers, enapi.LevelAnswerScenario{AnswerID: a.id, AnswerText: a.text})
				}
			}
			out.Sectors = append(out.Sectors, sec)
		}
		for _, bonus := range level.bonuses {
			b := enapi.BonusScenario{BonusID: bonus.id, BonusName: bonus.name, Task: bonus.task, BonusHelp: bonus.help, Answers: bonus.answers, BonusTime: bonus.bonusTime}
			if bonus.negative {
				b.BonusTimeText = fmt.Sprintf("Штрафное время: %d секунд", bonus.bonusTime)
			}
			out.Bonuses = append(out.Bonuses, b)
		}
		doc.Levels = append(doc.Levels, out)
	}
	writeJSON(w, http.StatusOK, doc)
}
