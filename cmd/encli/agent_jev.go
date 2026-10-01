package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/skrashevich/encx-cli/internal/jevguard"
)

func startJEVTurn(ctx context.Context, cfg AgentConfig, input *AgentRunInput, cb AgentCallbacks) {
	// Sessions survive provider changes: always clear the previous evaluator.
	input.Session.jev = nil
	if !jevguard.Enabled(cfg.BaseURL, cfg.AuthMethod) {
		return
	}
	var users []string
	for _, message := range input.Messages {
		if message.Role == "user" {
			users = append(users, message.Content)
		}
	}
	emitStatus(cb, "jev", input.Session.reviewText("Checking task scope with JEV…", "JEV: определяю рамки запроса…"))
	turn, err := jevguard.New(cfg.APIKey).Start(ctx, users)
	input.Session.jev = turn
	if err != nil {
		emitAgent(cb, AgentEvent{Type: agentEventWarning, Message: input.Session.reviewText(
			"JEV unavailable: reads remain available; mutations require confirmation of the exact call.",
			"JEV недоступна: чтение доступно, изменения потребуют подтверждения конкретного вызова.")})
		return
	}
	debugf("JEV intent: model=%s intent=%s confidence=%.2f explicit_change=%.2f cost_rub=%.6f", turn.Model, turn.Intent, turn.Confidence, turn.ExplicitChange, turn.CostRub)
}

func (r *picoLegacyToolRuntime) checkJEVCall(ctx context.Context, name, argsJSON string) jevguard.Decision {
	session := r.input.Session
	if session.jev == nil {
		return jevguard.Decision{Action: "allow"}
	}
	var args map[string]any
	if argsJSON != "" && json.Unmarshal([]byte(argsJSON), &args) != nil {
		return jevguard.Decision{Action: "deny", Reason: "Invalid tool arguments"}
	}
	decision := session.jev.Check(ctx, jevguard.Call{
		Name: name, Arguments: args, Mutating: isMutationTool(name),
		Context:         map[string]any{"domain": r.input.Cfg.domain, "game_id": r.input.Cfg.gameId},
		ContentMutation: isAdminMutationTool(name), Proposal: name == "propose_admin_fix",
		Approved: session.applyingApprovedFix,
	})
	debugf("JEV tool: name=%s action=%s model=%s cost_rub=%.6f", name, decision.Action, decision.Model, decision.CostRub)
	return decision
}

func jevRefusal(decision jevguard.Decision) string {
	body, _ := json.Marshal(map[string]any{"error": decision.Reason, "code": "jev_scope_" + decision.Action})
	return string(body)
}

func jevApprovalStatus(session *llmSession, decision jevguard.Decision) string {
	return session.reviewText(fmt.Sprintf("JEV requires confirmation: %s", decision.Reason), fmt.Sprintf("JEV требует согласования: %s", decision.Reason))
}
