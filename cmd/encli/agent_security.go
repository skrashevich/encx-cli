package main

// AgentSecurityMode controls whether the agent may run tools that change remote or local state.
type AgentSecurityMode string

const (
	SecurityModeFull     AgentSecurityMode = "full"
	SecurityModeReadonly AgentSecurityMode = "readonly"
	SecurityModeApprove  AgentSecurityMode = "approve"
)

func parseAgentSecurityMode(s string) (AgentSecurityMode, bool) {
	switch AgentSecurityMode(s) {
	case SecurityModeFull, SecurityModeReadonly, SecurityModeApprove:
		return AgentSecurityMode(s), true
	default:
		return "", false
	}
}

func (m AgentSecurityMode) effective() AgentSecurityMode {
	if m == "" {
		return SecurityModeFull
	}
	return m
}

// isMutationTool reports tools that create, update, or delete game/session state.
func isMutationTool(name string) bool {
	if isAdminMutationTool(name) {
		return true
	}
	switch name {
	case "login", "logout", "enter", "send_code", "hint", "propose_admin_fix":
		return true
	default:
		return false
	}
}

func shouldExposeTool(name string, mode AgentSecurityMode, reviewMode bool) bool {
	if reviewMode {
		return shouldExposeToolInReview(name)
	}
	if mode == SecurityModeReadonly && isMutationTool(name) {
		return false
	}
	return true
}

func getToolsForSession(session *llmSession) []llmTool {
	review := session != nil && session.reviewApprovalMode
	mode := SecurityModeFull
	if session != nil {
		mode = session.securityMode.effective()
	}
	all := getTools(review)
	if mode == SecurityModeFull && !review {
		return all
	}
	filtered := make([]llmTool, 0, len(all))
	for _, tool := range all {
		if shouldExposeTool(tool.Function.Name, mode, review) {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

func securityBlocksMutation(session *llmSession, toolName string) bool {
	if session == nil {
		return false
	}
	mode := session.securityMode.effective()
	if mode != SecurityModeReadonly {
		return false
	}
	return isMutationTool(toolName)
}

func securityRequiresApproval(session *llmSession, toolName string) bool {
	if session == nil {
		return false
	}
	if session.applyingApprovedFix {
		return false
	}
	mode := session.securityMode.effective()
	if mode != SecurityModeApprove {
		return false
	}
	return isMutationTool(toolName)
}

func securityModeSystemPromptAddendum(session *llmSession) string {
	if session == nil {
		return ""
	}
	mode := session.securityMode.effective()
	switch mode {
	case SecurityModeReadonly:
		if session.preferRussian {
			return `
- РЕЖИМ ТОЛЬКО ЧТЕНИЕ: запрещены любые изменения игры, сессии и отправка кодов. Используй только инструменты чтения и анализа. Если нужна правка — опиши её текстом, не вызывай мутирующие инструменты.`
		}
		return `
- READ-ONLY MODE: do not call tools that modify game data, session, or submit answers. Use read/inspect tools only. Describe desired changes in text instead of executing them.`
	case SecurityModeApprove:
		if session.preferRussian {
			return `
- РЕЖИМ С СОГЛАСОВАНИЕМ: перед каждым изменяющим действием интерфейс запросит подтверждение пользователя. Не предполагай, что правка уже применена, пока инструмент не выполнен успешно.`
		}
		return `
- APPROVAL MODE: the UI will ask the user to confirm each mutating tool call before it runs. Do not assume a change succeeded until the tool returns success.`
	default:
		return ""
	}
}
