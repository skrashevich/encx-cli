package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/skrashevich/encx-cli/encx"
)

type approvalAction string

const (
	approvalYes  approvalAction = "yes"
	approvalNo   approvalAction = "no"
	approvalQuit approvalAction = "quit"
)

type approvalGate struct {
	mu      sync.Mutex
	replyCh chan approvalAction
	closed  bool
}

func newApprovalGate() *approvalGate {
	return &approvalGate{replyCh: make(chan approvalAction, 1)}
}

func (g *approvalGate) respond(action approvalAction) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return fmt.Errorf("approval closed")
	}
	select {
	case g.replyCh <- action:
		return nil
	default:
		return fmt.Errorf("approval already waiting for response")
	}
}

func (g *approvalGate) wait(ctx context.Context) (approvalAction, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case a, ok := <-g.replyCh:
		if !ok {
			return "", fmt.Errorf("approval closed")
		}
		return a, nil
	}
}

func (g *approvalGate) close() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closed {
		g.closed = true
		close(g.replyCh)
	}
}

func (h *webHub) setApprovalGate(chatID string, gate *approvalGate) {
	h.approvalMu.Lock()
	defer h.approvalMu.Unlock()
	if h.approvals == nil {
		h.approvals = make(map[string]*approvalGate)
	}
	if old := h.approvals[chatID]; old != nil {
		old.close()
	}
	h.approvals[chatID] = gate
}

func (h *webHub) clearApprovalGate(chatID string) {
	h.approvalMu.Lock()
	defer h.approvalMu.Unlock()
	if g := h.approvals[chatID]; g != nil {
		g.close()
		delete(h.approvals, chatID)
	}
}

func (h *webHub) approvalGate(chatID string) *approvalGate {
	h.approvalMu.Lock()
	defer h.approvalMu.Unlock()
	return h.approvals[chatID]
}

func fixProposalPayload(session *llmSession, idx, total int, fix pendingAdminFix) map[string]any {
	steps := make([]string, len(fix.Steps))
	for i, step := range fix.Steps {
		steps[i] = describeProposalStep(step)
	}
	return map[string]any{
		"kind":         "fix",
		"index":        idx,
		"total":        total,
		"title":        fix.Title,
		"summary":      fix.Summary,
		"level_number": fix.LevelNumber,
		"steps":        steps,
	}
}

func runWebPendingFixApprovals(ctx context.Context, hub *webHub, chatID string, cfg *config, client *encx.Client, session *llmSession) {
	if len(session.pendingFixes) == 0 {
		return
	}
	gate := newApprovalGate()
	hub.setApprovalGate(chatID, gate)
	defer hub.clearApprovalGate(chatID)

	outcomes := make([]proposalOutcome, 0, len(session.pendingFixes))
	total := len(session.pendingFixes)
	idx := 0
	for len(session.pendingFixes) > 0 {
		idx++
		fix := session.pendingFixes[0]
		hub.publishSSE(chatID, "approval_prompt", fixProposalPayload(session, idx, total, fix))

		action, err := gate.wait(ctx)
		hub.publishSSE(chatID, "approval_resolved", map[string]any{
			"action": string(action),
			"error":  errString(err),
		})
		if err != nil {
			return
		}

		switch action {
		case approvalYes:
			out := applyPendingAdminFix(ctx, cfg, client, session, fix)
			outcomes = append(outcomes, out)
			hub.publishSSE(chatID, "approval_result", map[string]any{
				"title": fix.Title, "applied": out.Applied, "error": out.Error,
			})
			session.pendingFixes = session.pendingFixes[1:]
		case approvalQuit:
			outcomes = append(outcomes, proposalOutcome{Title: fix.Title, Stopped: true})
			hub.publishSSE(chatID, "approval_result", map[string]any{"title": fix.Title, "stopped": true})
			session.pendingFixes = nil
			printApprovalSummaryWeb(hub, chatID, session, outcomes)
			hub.store.Persist(chatID)
			return
		default:
			outcomes = append(outcomes, proposalOutcome{Title: fix.Title, Skipped: true})
			hub.publishSSE(chatID, "approval_result", map[string]any{"title": fix.Title, "skipped": true})
			session.pendingFixes = session.pendingFixes[1:]
		}
	}
	printApprovalSummaryWeb(hub, chatID, session, outcomes)
	session.pendingFixes = nil
	hub.store.Persist(chatID)
}

func printApprovalSummaryWeb(hub *webHub, chatID string, session *llmSession, outcomes []proposalOutcome) {
	if len(outcomes) == 0 {
		return
	}
	var applied, skipped, failed int
	for _, o := range outcomes {
		switch {
		case o.Applied:
			applied++
		case o.Error != "":
			failed++
		case o.Skipped:
			skipped++
		}
	}
	hub.publishSSE(chatID, "approval_summary", map[string]any{
		"applied": applied,
		"skipped": skipped,
		"failed":  failed,
	})
}

func (h *webHub) chatSession(chatID string) *llmSession {
	h.store.mu.Lock()
	t := h.store.chats[chatID]
	h.store.mu.Unlock()
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.session
}

func toolApprovalPayload(session *llmSession, toolName, argsJSON string) map[string]any {
	details := formatToolApprovalDetails(session, toolName, argsJSON)
	return map[string]any{
		"kind":    "tool",
		"tool":    toolName,
		"args":    argsJSON,
		"action":  formatToolApprovalAction(session, toolName, argsJSON),
		"details": details,
	}
}

func runWebToolApproval(ctx context.Context, hub *webHub, chatID, toolName, argsJSON string) (bool, error) {
	gate := newApprovalGate()
	hub.setApprovalGate(chatID, gate)
	defer hub.clearApprovalGate(chatID)

	session := hub.chatSession(chatID)
	hub.publishSSE(chatID, "approval_prompt", toolApprovalPayload(session, toolName, argsJSON))

	action, err := gate.wait(ctx)
	hub.publishSSE(chatID, "approval_resolved", map[string]any{
		"action": string(action),
		"error":  errString(err),
	})
	if err != nil {
		return false, err
	}
	switch action {
	case approvalYes:
		return true, nil
	case approvalQuit:
		return false, fmt.Errorf("tool approval stopped by user")
	default:
		return false, nil
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func parseApprovalAction(s string) (approvalAction, error) {
	switch s {
	case "yes", "y", "да", "д":
		return approvalYes, nil
	case "no", "n", "нет", "н", "":
		return approvalNo, nil
	case "quit", "q", "выход", "в":
		return approvalQuit, nil
	default:
		return "", fmt.Errorf("unknown action %q", s)
	}
}
