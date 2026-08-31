package encxmobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/skrashevich/encx-cli/agenttools"
	"github.com/skrashevich/encx-cli/encx"
)

// fakeEngine is a scripted agenttools.Engine that records the calls it received.
type fakeEngine struct {
	mu    sync.Mutex
	calls []string
	model *encx.GameModel
}

func (e *fakeEngine) record(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, name)
}

func (e *fakeEngine) called(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, call := range e.calls {
		if call == name {
			return true
		}
	}
	return false
}

func (e *fakeEngine) GetDomainGames(context.Context) ([]encx.DomainGame, error) {
	e.record("GetDomainGames")
	return nil, nil
}

func (e *fakeEngine) GetGameList(context.Context, ...int) (*encx.GameListResponse, error) {
	e.record("GetGameList")
	return &encx.GameListResponse{}, nil
}

func (e *fakeEngine) GetGameModel(context.Context, int, ...url.Values) (*encx.GameModel, error) {
	e.record("GetGameModel")
	return e.model, nil
}

func (e *fakeEngine) GetGameModelLevel(context.Context, int, int) (*encx.GameModel, error) {
	e.record("GetGameModelLevel")
	return e.model, nil
}

func (e *fakeEngine) GetGameStatistics(context.Context, int) (*encx.GameStatisticsResponse, error) {
	e.record("GetGameStatistics")
	return &encx.GameStatisticsResponse{}, nil
}

func (e *fakeEngine) GetTimeoutToGame(context.Context, int) (*int, error) {
	e.record("GetTimeoutToGame")
	return nil, nil
}

func (e *fakeEngine) GetProfile(context.Context) (*encx.Profile, error) {
	e.record("GetProfile")
	return &encx.Profile{Login: "player", TeamID: 11}, nil
}

func (e *fakeEngine) GetTeamManagementInfo(context.Context, int) (*encx.TeamManagementInfo, error) {
	e.record("GetTeamManagementInfo")
	return &encx.TeamManagementInfo{TeamID: 11}, nil
}

func (e *fakeEngine) EnterGame(context.Context, int) (string, error) {
	e.record("EnterGame")
	return "", nil
}

func (e *fakeEngine) SendCode(context.Context, int, int, int, string) (*encx.GameModel, error) {
	e.record("SendCode")
	return e.model, nil
}

func (e *fakeEngine) SendBonusCode(context.Context, int, int, int, string) (*encx.GameModel, error) {
	e.record("SendBonusCode")
	return e.model, nil
}

func (e *fakeEngine) GetPenaltyHint(context.Context, int, int) (*encx.GameModel, error) {
	e.record("GetPenaltyHint")
	return e.model, nil
}

func (e *fakeEngine) FetchResource(_ context.Context, rawURL string, _ ...encx.ResourceOptions) (*encx.Resource, error) {
	e.record("FetchResource")
	return &encx.Resource{URL: rawURL, ContentType: "image/png", Data: []byte("png-bytes")}, nil
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{
		model: &encx.GameModel{
			GameId:    42,
			GameTitle: "Тестовая игра",
			Level: &encx.Level{
				LevelId: 77,
				Number:  3,
				Tasks:   []encx.LevelTask{{TaskText: "Найдите табличку"}},
			},
		},
	}
}

// scriptedProvider replays a fixed list of LLM responses, one per Chat call.
type scriptedProvider struct {
	mu        sync.Mutex
	responses []*providers.LLMResponse
	calls     int
	block     chan struct{}
}

func (p *scriptedProvider) Chat(
	ctx context.Context,
	_ []providers.Message,
	_ []providers.ToolDefinition,
	_ string,
	_ map[string]any,
) (*providers.LLMResponse, error) {
	if p.block != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-p.block:
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls >= len(p.responses) {
		return &providers.LLMResponse{Content: "no more scripted responses"}, nil
	}
	response := p.responses[p.calls]
	p.calls++
	return response, nil
}

func (p *scriptedProvider) GetDefaultModel() string { return "scripted" }

func toolCallResponse(name, argsJSON string) *providers.LLMResponse {
	return multiToolCallResponse(scriptedCall{name: name, args: argsJSON})
}

type scriptedCall struct {
	name string
	args string
}

// multiToolCallResponse emits several tool calls in one assistant message, the
// way a model asks for parallel work.
func multiToolCallResponse(calls ...scriptedCall) *providers.LLMResponse {
	response := &providers.LLMResponse{}
	for i, call := range calls {
		response.ToolCalls = append(response.ToolCalls, providers.ToolCall{
			ID:       fmt.Sprintf("llm-call-%d", i+1),
			Type:     "function",
			Function: &providers.FunctionCall{Name: call.name, Arguments: call.args},
		})
	}
	return response
}

// recordingDelegate collects events and answers confirmations with a fixed verdict.
type recordingDelegate struct {
	mu              sync.Mutex
	events          []map[string]any
	confirmations   []string
	confirmationIDs []string
	session         *AgentSession
	approve         bool
	ignore          bool
}

func (d *recordingDelegate) OnEvent(eventJSON string) {
	var event map[string]any
	if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.events = append(d.events, event)
}

func (d *recordingDelegate) OnConfirmationRequest(callID string, turn int64, toolName, argsJSON string) {
	d.mu.Lock()
	d.confirmations = append(d.confirmations, toolName+" "+argsJSON)
	d.confirmationIDs = append(d.confirmationIDs, callID)
	ignore := d.ignore
	d.mu.Unlock()
	if ignore {
		return
	}
	_ = d.session.ResolveConfirmation(callID, d.approve)
}

func (d *recordingDelegate) pendingCallIDs() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.confirmationIDs...)
}

// eventCallIDs returns the call_id of every event of the given type.
func (d *recordingDelegate) eventCallIDs(kind string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var ids []string
	for _, event := range d.events {
		if event["type"] != kind {
			continue
		}
		if id, ok := event["call_id"].(string); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func (d *recordingDelegate) eventTypes() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	types := make([]string, 0, len(d.events))
	for _, event := range d.events {
		if kind, ok := event["type"].(string); ok {
			types = append(types, kind)
		}
	}
	return types
}

func (d *recordingDelegate) confirmationCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.confirmations)
}

func contains(list []string, want string) bool {
	return hasEvent(list, want)
}

func hasEvent(types []string, want string) bool {
	for _, kind := range types {
		if kind == want {
			return true
		}
	}
	return false
}

func newTestSession(
	t *testing.T,
	engine agenttools.Engine,
	policy agenttools.Policy,
	provider providers.LLMProvider,
) *AgentSession {
	t.Helper()
	cfg, parsedPolicy, err := parseAgentConfig(`{"model":"scripted","policy":"` + string(policy) + `"}`)
	if err != nil {
		t.Fatalf("parseAgentConfig: %v", err)
	}
	session, err := newAgentSession(engine, cfg, parsedPolicy, provider)
	if err != nil {
		t.Fatalf("newAgentSession: %v", err)
	}
	return session
}

func TestParseAgentConfigAppliesDefaults(t *testing.T) {
	cfg, policy, err := parseAgentConfig(`{"model":"gpt-5"}`)
	if err != nil {
		t.Fatalf("parseAgentConfig: %v", err)
	}
	if policy != agenttools.DefaultPolicy {
		t.Fatalf("an omitted policy should fall back to %q, got %q", agenttools.DefaultPolicy, policy)
	}
	if cfg.MaxIterations != defaultAgentMaxIterations {
		t.Fatalf("max_iterations should default to %d, got %d", defaultAgentMaxIterations, cfg.MaxIterations)
	}
	// "Try codes 1 through 90" is one question and ninety steps, so the default
	// has to leave room for long jobs.
	if defaultAgentMaxIterations < 90 {
		t.Fatalf("the default step budget is too small for a long job: %d", defaultAgentMaxIterations)
	}
	if cfg.SystemPrompt == "" {
		t.Fatal("a default system prompt should be applied")
	}
}

func TestHTTPProviderResolvesEndpointsWithoutTheFullFactory(t *testing.T) {
	for provider, wantBase := range map[string]string{
		"openai":     "https://api.openai.com/v1",
		"anthropic":  "https://api.anthropic.com/v1",
		"openrouter": "https://openrouter.ai/api/v1",
		"":           "https://api.openai.com/v1",
	} {
		cfg := agentConfig{Provider: provider, Model: "m", APIKey: "k"}
		if _, err := newHTTPProvider(cfg); err != nil {
			t.Fatalf("provider %q should build: %v", provider, err)
		}
		if got := defaultAPIBases[cmpOrDefault(provider)]; got != wantBase && provider != "" {
			t.Fatalf("provider %q default base = %q, want %q", provider, got, wantBase)
		}
	}
}

func cmpOrDefault(provider string) string {
	if provider == "" {
		return providerOpenAI
	}
	return provider
}

func TestHTTPProviderRejectsUnreachableProviders(t *testing.T) {
	// Azure, Bedrock and Copilot are deliberately not linked: the app cannot
	// offer them, and their SDKs cost ~24 MB in the framework.
	if _, err := newHTTPProvider(agentConfig{Provider: "bedrock", Model: "m", APIKey: "k"}); err == nil {
		t.Fatal("an unsupported provider should be reported, not silently accepted")
	}
	if _, err := newHTTPProvider(agentConfig{Provider: "openai", Model: "m"}); err == nil {
		t.Fatal("a missing API key should be reported")
	}
}

func TestHTTPProviderHonoursACustomEndpoint(t *testing.T) {
	// A custom gateway is the whole reason api_base exists; an unknown provider
	// name is fine as long as the caller says where to send the request.
	cfg := agentConfig{Provider: "my-gateway", Model: "m", APIKey: "k", APIBase: "https://example.test/v1/"}
	if _, err := newHTTPProvider(cfg); err != nil {
		t.Fatalf("a custom endpoint should be accepted: %v", err)
	}
}

func TestNegativeMaxIterationsRemovesTheStepCap(t *testing.T) {
	cfg, _, err := parseAgentConfig(`{"model":"gpt-5","max_iterations":-1}`)
	if err != nil {
		t.Fatalf("parseAgentConfig: %v", err)
	}
	if cfg.MaxIterations != unlimitedIterations {
		t.Fatalf("a negative budget should lift the cap, got %d", cfg.MaxIterations)
	}

	explicit, _, err := parseAgentConfig(`{"model":"gpt-5","max_iterations":7}`)
	if err != nil {
		t.Fatalf("parseAgentConfig: %v", err)
	}
	if explicit.MaxIterations != 7 {
		t.Fatalf("an explicit budget should be honoured, got %d", explicit.MaxIterations)
	}
}

func TestOnDeviceSessionRunsToolsWithoutAProvider(t *testing.T) {
	engine := newFakeEngine()
	cfg, policy, err := parseAgentConfig(`{"auth_method":"on-device","policy":"readonly"}`)
	if err != nil {
		t.Fatalf("an on-device config needs no model name: %v", err)
	}
	session, err := newAgentSession(engine, cfg, policy, nil)
	if err != nil {
		t.Fatalf("newAgentSession: %v", err)
	}

	// The host owns the conversation, so the built-in loop must refuse instead of
	// dereferencing a provider that was never built.
	if _, err := session.SendMessage("привет"); err == nil {
		t.Fatal("a session without a provider should refuse SendMessage")
	}

	catalogJSON, err := session.ToolCatalogJSON()
	if err != nil {
		t.Fatalf("ToolCatalogJSON: %v", err)
	}
	var described []hostToolDescription
	if err := json.Unmarshal([]byte(catalogJSON), &described); err != nil {
		t.Fatalf("the catalog should be JSON: %v", err)
	}
	if len(described) == 0 {
		t.Fatal("the host needs the tool list to build its own loop")
	}
	for _, tool := range described {
		if tool.Name == "" || tool.Parameters == nil {
			t.Fatalf("every tool needs a name and a schema, got %+v", tool)
		}
	}

	turn, err := session.BeginHostTurn()
	if err != nil {
		t.Fatalf("BeginHostTurn: %v", err)
	}
	if turn != 1 {
		t.Fatalf("the first host turn should be 1, got %d", turn)
	}
	if _, err := session.BeginHostTurn(); err == nil {
		t.Fatal("a second concurrent turn should be rejected")
	}

	result, err := session.InvokeTool("enc_game_state", `{"game_id":42}`)
	if err != nil {
		t.Fatalf("InvokeTool: %v", err)
	}
	if !strings.Contains(result, `"game_id":42`) {
		t.Fatalf("the tool result should carry engine data, got %q", result)
	}
	if !engine.called("GetGameModel") {
		t.Fatal("the tool should reach the engine")
	}

	session.EndHostTurn("Вы на третьем уровне.", "")
	session.RecordHostExchange("где я?", "Вы на третьем уровне.")
	historyJSON, _ := session.HistoryJSON()
	if !strings.Contains(historyJSON, "где я?") {
		t.Fatalf("a host-driven exchange should be remembered, got %s", historyJSON)
	}
}

func TestOnDeviceInvokeToolStillHonoursThePolicyGate(t *testing.T) {
	engine := newFakeEngine()
	cfg, policy, err := parseAgentConfig(`{"auth_method":"on-device","policy":"approve"}`)
	if err != nil {
		t.Fatalf("parseAgentConfig: %v", err)
	}
	session, err := newAgentSession(engine, cfg, policy, nil)
	if err != nil {
		t.Fatalf("newAgentSession: %v", err)
	}
	delegate := &recordingDelegate{approve: false}
	delegate.session = session
	session.SetDelegate(delegate)

	if _, err := session.BeginHostTurn(); err != nil {
		t.Fatalf("BeginHostTurn: %v", err)
	}
	defer session.EndHostTurn("done", "")

	// A host-driven loop must not be a way around the confirmation the player
	// configured.
	if _, err := session.InvokeTool("enc_send_code", `{"game_id":42,"code":"bravo"}`); err == nil {
		t.Fatal("a declined mutation should fail the tool call")
	}
	if engine.called("SendCode") {
		t.Fatal("a declined mutation must not reach the engine")
	}
	if delegate.confirmationCount() != 1 {
		t.Fatalf("the host path should still ask for confirmation, got %d", delegate.confirmationCount())
	}
}

func TestOnDeviceSessionDropsWebTools(t *testing.T) {
	cfg, _, err := parseAgentConfig(`{"auth_method":"on-device","web_tools":true}`)
	if err != nil {
		t.Fatalf("parseAgentConfig: %v", err)
	}
	// The on-device model has a short context; a page of search results would
	// crowd out the game state it needs.
	if cfg.WebToolsEnabled {
		t.Fatal("web tools should be off for the on-device model")
	}
}

func TestParseAgentConfigRejectsBadInput(t *testing.T) {
	for name, input := range map[string]string{
		"empty":       "",
		"not json":    "{",
		"no model":    `{"policy":"full"}`,
		"bad policy":  `{"model":"gpt-5","policy":"yolo"}`,
		"blank model": `{"model":"   "}`,
	} {
		if _, _, err := parseAgentConfig(input); err == nil {
			t.Fatalf("%s config should be rejected", name)
		}
	}
}

func TestModelConfigCarriesCredentials(t *testing.T) {
	cfg, _, err := parseAgentConfig(
		`{"model":"gpt-5","provider":"openai","api_base":"https://example.test/v1","api_key":"secret","request_timeout_seconds":45}`,
	)
	if err != nil {
		t.Fatalf("parseAgentConfig: %v", err)
	}
	model := cfg.modelConfig()
	if model.APIKey() != "secret" {
		t.Fatalf("the API key should reach the provider config, got %q", model.APIKey())
	}
	if model.APIBase != "https://example.test/v1" || model.Provider != "openai" || model.RequestTimeout != 45 {
		t.Fatalf("provider config is wrong: %+v", model)
	}
	if !model.Enabled {
		t.Fatal("the model entry should be enabled")
	}
}

func TestSessionExposesEngineToolsPerPolicy(t *testing.T) {
	readonly := newTestSession(t, newFakeEngine(), agenttools.PolicyReadonly, &scriptedProvider{})
	full := newTestSession(t, newFakeEngine(), agenttools.PolicyFull, &scriptedProvider{})

	if readonly.Policy() != string(agenttools.PolicyReadonly) {
		t.Fatalf("Policy() should report the configured policy, got %q", readonly.Policy())
	}
	if readonly.ToolCount() == 0 {
		t.Fatal("read tools should be registered even under the read-only policy")
	}
	if full.ToolCount() <= readonly.ToolCount() {
		t.Fatalf("the full policy should expose more tools: full=%d readonly=%d",
			full.ToolCount(), readonly.ToolCount())
	}
}

func TestSendMessageRunsToolLoopAndReportsProgress(t *testing.T) {
	engine := newFakeEngine()
	provider := &scriptedProvider{responses: []*providers.LLMResponse{
		toolCallResponse("enc_game_state", `{"game_id":42}`),
		{Content: "Вы на третьем уровне."},
	}}
	session := newTestSession(t, engine, agenttools.PolicyReadonly, provider)
	delegate := &recordingDelegate{}
	delegate.session = session
	session.SetDelegate(delegate)

	reply, err := session.SendMessage("где я?")
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if reply != "Вы на третьем уровне." {
		t.Fatalf("unexpected reply: %q", reply)
	}
	if !engine.called("GetGameModel") {
		t.Fatal("the tool call should reach the engine")
	}

	types := delegate.eventTypes()
	for _, want := range []string{"turn_started", "tool_started", "tool_finished", "turn_finished"} {
		if !hasEvent(types, want) {
			t.Fatalf("event %q is missing from %v", want, types)
		}
	}

	historyJSON, err := session.HistoryJSON()
	if err != nil {
		t.Fatalf("HistoryJSON: %v", err)
	}
	if !strings.Contains(historyJSON, "где я?") || !strings.Contains(historyJSON, "третьем уровне") {
		t.Fatalf("the transcript should hold both turns, got %s", historyJSON)
	}

	session.ResetHistory()
	if historyJSON, _ = session.HistoryJSON(); historyJSON != "[]" {
		t.Fatalf("ResetHistory should clear the transcript, got %s", historyJSON)
	}
}

func TestApprovedMutationReachesEngine(t *testing.T) {
	engine := newFakeEngine()
	provider := &scriptedProvider{responses: []*providers.LLMResponse{
		toolCallResponse("enc_send_code", `{"game_id":42,"code":"bravo"}`),
		{Content: "Код отправлен."},
	}}
	session := newTestSession(t, engine, agenttools.PolicyApprove, provider)
	delegate := &recordingDelegate{approve: true}
	delegate.session = session
	session.SetDelegate(delegate)

	if _, err := session.SendMessage("отправь bravo"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if delegate.confirmationCount() != 1 {
		t.Fatalf("exactly one confirmation should be requested, got %d", delegate.confirmationCount())
	}
	if !engine.called("SendCode") {
		t.Fatal("an approved submission should reach the engine")
	}
}

func TestDeclinedMutationSkipsEngine(t *testing.T) {
	engine := newFakeEngine()
	provider := &scriptedProvider{responses: []*providers.LLMResponse{
		toolCallResponse("enc_send_code", `{"game_id":42,"code":"bravo"}`),
		{Content: "Хорошо, не отправляю."},
	}}
	session := newTestSession(t, engine, agenttools.PolicyApprove, provider)
	delegate := &recordingDelegate{approve: false}
	delegate.session = session
	session.SetDelegate(delegate)

	if _, err := session.SendMessage("отправь bravo"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if delegate.confirmationCount() != 1 {
		t.Fatalf("the host should still be asked, got %d requests", delegate.confirmationCount())
	}
	if engine.called("SendCode") {
		t.Fatal("a declined submission must not reach the engine")
	}
}

func TestMutationWithoutDelegateIsRefused(t *testing.T) {
	engine := newFakeEngine()
	provider := &scriptedProvider{responses: []*providers.LLMResponse{
		toolCallResponse("enc_send_code", `{"game_id":42,"code":"bravo"}`),
		{Content: "Не могу подтвердить."},
	}}
	session := newTestSession(t, engine, agenttools.PolicyApprove, provider)

	if _, err := session.SendMessage("отправь bravo"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if engine.called("SendCode") {
		t.Fatal("without a delegate the mutation must not reach the engine")
	}
}

func TestResolveConfirmationRejectsUnknownCall(t *testing.T) {
	session := newTestSession(t, newFakeEngine(), agenttools.PolicyFull, &scriptedProvider{})
	if err := session.ResolveConfirmation("call-404", true); err == nil {
		t.Fatal("resolving an unknown confirmation should fail")
	}
}

func TestCancelAbortsRunningTurn(t *testing.T) {
	provider := &scriptedProvider{block: make(chan struct{})}
	session := newTestSession(t, newFakeEngine(), agenttools.PolicyReadonly, provider)

	errCh := make(chan error, 1)
	go func() {
		_, err := session.SendMessage("подожди")
		errCh <- err
	}()

	// Let the turn reach the blocked provider before cancelling it.
	waitFor(t, func() bool { return session.isRunning() })
	session.Cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("a cancelled turn should return an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Cancel did not unblock SendMessage")
	}
}

func TestConcurrentTurnIsRejected(t *testing.T) {
	provider := &scriptedProvider{block: make(chan struct{})}
	session := newTestSession(t, newFakeEngine(), agenttools.PolicyReadonly, provider)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = session.SendMessage("первый")
	}()
	waitFor(t, func() bool { return session.isRunning() })

	if _, err := session.SendMessage("второй"); err == nil {
		t.Fatal("a second concurrent turn should be rejected")
	}

	session.Cancel()
	<-done
}

func TestConcurrentMutatingCallsGetDistinctConfirmations(t *testing.T) {
	engine := newFakeEngine()
	provider := &scriptedProvider{responses: []*providers.LLMResponse{
		multiToolCallResponse(
			scriptedCall{name: "enc_send_code", args: `{"game_id":42,"code":"alpha"}`},
			scriptedCall{name: "enc_send_bonus_code", args: `{"game_id":42,"code":"bravo"}`},
		),
		{Content: "Оба кода отправлены."},
	}}
	session := newTestSession(t, engine, agenttools.PolicyApprove, provider)
	delegate := &recordingDelegate{approve: true}
	delegate.session = session
	session.SetDelegate(delegate)

	if _, err := session.SendMessage("отправь оба"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	ids := delegate.pendingCallIDs()
	if len(ids) != 2 {
		t.Fatalf("both parallel calls should ask for confirmation, got %d: %v", len(ids), ids)
	}
	if ids[0] == ids[1] {
		t.Fatalf("each call needs its own id, got %v", ids)
	}
	if !engine.called("SendCode") || !engine.called("SendBonusCode") {
		t.Fatalf("both approved calls should reach the engine, calls: %v", engine.calls)
	}

	// The host has to be able to tie a confirmation to the activity row it shows,
	// so the same identifier must appear in the progress events.
	started := delegate.eventCallIDs("tool_started")
	for _, id := range ids {
		if !contains(started, id) {
			t.Fatalf("confirmation id %q is missing from tool_started events %v", id, started)
		}
	}
}

func TestSilentDelegateBlocksUntilCancel(t *testing.T) {
	engine := newFakeEngine()
	provider := &scriptedProvider{responses: []*providers.LLMResponse{
		toolCallResponse("enc_send_code", `{"game_id":42,"code":"bravo"}`),
		{Content: "Отменено."},
	}}
	session := newTestSession(t, engine, agenttools.PolicyApprove, provider)
	delegate := &recordingDelegate{ignore: true}
	delegate.session = session
	session.SetDelegate(delegate)

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = session.SendMessage("отправь bravo")
	}()

	// A host that never answers must not let the call through; the turn stays
	// parked until it is cancelled.
	waitFor(t, func() bool { return len(delegate.pendingCallIDs()) == 1 })
	if engine.called("SendCode") {
		t.Fatal("an unanswered confirmation must not reach the engine")
	}

	session.Cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Cancel did not release the pending confirmation")
	}
	if engine.called("SendCode") {
		t.Fatal("cancelling must not let the call through")
	}
}

func TestCancelReleasesPendingConfirmationAsRefusal(t *testing.T) {
	engine := newFakeEngine()
	provider := &scriptedProvider{responses: []*providers.LLMResponse{
		toolCallResponse("enc_take_penalty_hint", `{"game_id":42,"hint_id":9}`),
		{Content: "Подсказка не взята."},
	}}
	session := newTestSession(t, engine, agenttools.PolicyApprove, provider)
	delegate := &recordingDelegate{ignore: true}
	delegate.session = session
	session.SetDelegate(delegate)

	go func() { _, _ = session.SendMessage("возьми подсказку") }()
	waitFor(t, func() bool { return len(delegate.pendingCallIDs()) == 1 })

	session.Cancel()

	// A penalty hint costs the team real time and cannot be undone, so a
	// cancelled confirmation must resolve as a refusal, never as an approval.
	waitFor(t, func() bool { return !session.isRunning() })
	if engine.called("GetPenaltyHint") {
		t.Fatal("a cancelled confirmation must not take the penalty hint")
	}
}

func TestExhaustedIterationsReportAnError(t *testing.T) {
	engine := newFakeEngine()
	// The model never stops calling tools, so RunToolLoop returns empty content.
	responses := make([]*providers.LLMResponse, 0, 4)
	for range 4 {
		responses = append(responses, toolCallResponse("enc_game_state", `{"game_id":42}`))
	}
	provider := &scriptedProvider{responses: responses}
	session := newTestSession(t, engine, agenttools.PolicyReadonly, provider)
	session.maxIterations = 3

	reply, err := session.SendMessage("зациклись")
	if err == nil {
		t.Fatal("exhausting the iteration budget should be reported as an error")
	}
	if reply != "" {
		t.Fatalf("no reply should be returned, got %q", reply)
	}
	historyJSON, _ := session.HistoryJSON()
	if historyJSON != "[]" {
		t.Fatalf("a failed turn must not poison the transcript, got %s", historyJSON)
	}
}

func TestCodexConfigRequiresCredential(t *testing.T) {
	if _, _, err := parseAgentConfig(`{"model":"gpt-5-codex","auth_method":"codex"}`); err == nil {
		t.Fatal("selecting ChatGPT sign-in without a credential should be rejected")
	}
	cfg, _, err := parseAgentConfig(
		`{"model":"gpt-5-codex","auth_method":"Codex","codex_credential":"{\"access_token\":\"t\"}"}`,
	)
	if err != nil {
		t.Fatalf("a codex config with a credential should parse: %v", err)
	}
	if cfg.AuthMethod != AuthMethodCodex {
		t.Fatalf("auth_method should be normalized to %q, got %q", AuthMethodCodex, cfg.AuthMethod)
	}
}

func TestPendingDeviceApprovalIsNotAFailure(t *testing.T) {
	// The device endpoint reports "not approved yet" as an error, so the login
	// must not surface it to the player as a failure.
	if !isDeviceApprovalPending(errors.New("pending")) {
		t.Fatal("a bare pending error should read as awaiting approval")
	}
	if !isDeviceApprovalPending(fmt.Errorf("wrapped: %w", errors.New("Pending"))) {
		t.Fatal("a wrapped pending error should read as awaiting approval")
	}
	if isDeviceApprovalPending(errors.New("reading device token response: connection reset")) {
		t.Fatal("a transport failure must not be mistaken for pending approval")
	}
	if isDeviceApprovalPending(nil) {
		t.Fatal("no error is not a pending state")
	}
}

func TestCodexLoginWaitStopsWhenCancelled(t *testing.T) {
	login := &CodexDeviceLogin{interval: time.Millisecond}
	login.Cancel()

	if _, err := login.Wait(30); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("a cancelled login should report cancellation, got %v", err)
	}
}

func TestCodexLoginPollTreatsPendingAsNotYet(t *testing.T) {
	login := &CodexDeviceLogin{interval: time.Millisecond}

	// The first poll always runs before any human could have approved, so a
	// pending answer must not read as a failure.
	credentialJSON, err := login.pollOnce(func() (*auth.AuthCredential, error) {
		return nil, errors.New("pending")
	})
	if err != nil || credentialJSON != "" {
		t.Fatalf("a pending answer should be quiet, got %q %v", credentialJSON, err)
	}

	if _, err := login.pollOnce(func() (*auth.AuthCredential, error) {
		return nil, errors.New("reading device token response: connection reset")
	}); err == nil {
		t.Fatal("a real transport failure should still be reported")
	}

	credentialJSON, err = login.pollOnce(func() (*auth.AuthCredential, error) {
		return &auth.AuthCredential{AccessToken: "granted", AccountID: "acc"}, nil
	})
	if err != nil || !strings.Contains(credentialJSON, "granted") {
		t.Fatalf("an approval should return the credential, got %q %v", credentialJSON, err)
	}
}

func TestCodexLoginWaitSurvivesPendingPollsUntilApproval(t *testing.T) {
	login := &CodexDeviceLogin{interval: time.Millisecond}

	// This is the flow a player actually sees: several pending answers while they
	// switch apps and type the code, then approval. None of them may abort it.
	polls := 0
	credentialJSON, err := login.waitFor(time.Now().Add(5*time.Second), func() (string, error) {
		polls++
		if polls < 4 {
			return "", nil
		}
		return `{"access_token":"granted"}`, nil
	})
	if err != nil {
		t.Fatalf("pending polls must not abort the login: %v", err)
	}
	if !strings.Contains(credentialJSON, "granted") {
		t.Fatalf("the approved credential should be returned, got %q", credentialJSON)
	}
	if polls != 4 {
		t.Fatalf("the login should have kept polling, got %d attempts", polls)
	}
}

func TestCodexLoginWaitReportsTheLastFailureOnTimeout(t *testing.T) {
	login := &CodexDeviceLogin{interval: time.Millisecond}

	// Transient failures are tolerated while the player is away, but the real
	// reason has to survive to the timeout instead of a generic message.
	_, err := login.waitFor(time.Now().Add(20*time.Millisecond), func() (string, error) {
		return "", errors.New("issuer unreachable")
	})
	if err == nil || !strings.Contains(err.Error(), "issuer unreachable") {
		t.Fatalf("the last failure should be reported, got %v", err)
	}
}

func TestParseCodexCredentialRejectsUnusableInput(t *testing.T) {
	for name, input := range map[string]string{
		"empty":    "   ",
		"not json": "{",
		"no token": `{"refresh_token":"r"}`,
	} {
		if _, err := parseCodexCredential(input); err == nil {
			t.Fatalf("%s credential should be rejected", name)
		}
	}
	cred, err := parseCodexCredential(`{"access_token":"t","account_id":"acc","refresh_token":"r"}`)
	if err != nil {
		t.Fatalf("a valid credential should parse: %v", err)
	}
	if cred.AccessToken != "t" || cred.AccountID != "acc" {
		t.Fatalf("credential fields are wrong: %+v", cred)
	}
}

func TestCodexTokenStoreKeepsValidTokenAndReportsUnrefreshable(t *testing.T) {
	live := &codexCredential{AccessToken: "live", AccountID: "acc", ExpiresAt: time.Now().Add(time.Hour)}
	token, accountID, err := newCodexTokenStore(live).tokenSource()()
	if err != nil || token != "live" || accountID != "acc" {
		t.Fatalf("a valid token should be reused as-is: %q %q %v", token, accountID, err)
	}

	stale := &codexCredential{AccessToken: "old", ExpiresAt: time.Now().Add(-time.Hour)}
	if _, _, err := newCodexTokenStore(stale).tokenSource()(); err == nil {
		t.Fatal("an expired credential without a refresh token should fail loudly")
	}

	credentialJSON, err := newCodexTokenStore(live).credentialJSON()
	if err != nil || !strings.Contains(credentialJSON, "live") {
		t.Fatalf("the credential should round-trip for persistence: %q %v", credentialJSON, err)
	}
}

func TestCodexTokenRefreshLeavesHeadroomBeforeExpiry(t *testing.T) {
	// The provider fetches the token once per LLM call and a turn makes many, so
	// a token that expires "soon" must be renewed before it is ever attached.
	expiringSoon := newCodexTokenStore(&codexCredential{
		AccessToken: "almost-dead",
		ExpiresAt:   time.Now().Add(tokenRefreshSkew / 2),
	})
	if !expiringSoon.needsRefreshLocked() {
		t.Fatal("a token inside the refresh window should be renewed, not used")
	}

	comfortable := newCodexTokenStore(&codexCredential{
		AccessToken: "fresh",
		ExpiresAt:   time.Now().Add(tokenRefreshSkew * 4),
	})
	if comfortable.needsRefreshLocked() {
		t.Fatal("a token with plenty of life left should be used as-is")
	}
}

func TestCodexCredentialWithoutExpiryRefreshesOnlyOnce(t *testing.T) {
	store := newCodexTokenStore(&codexCredential{AccessToken: "t", RefreshToken: "r"})
	if !store.needsRefreshLocked() {
		t.Fatal("an expiry-less credential should be renewed once to learn its age")
	}

	// Renewing on every call would rotate the refresh token a dozen times a turn.
	store.refreshed = true
	if store.needsRefreshLocked() {
		t.Fatal("an expiry-less credential must not be renewed repeatedly")
	}

	noRefreshToken := newCodexTokenStore(&codexCredential{AccessToken: "t"})
	if noRefreshToken.needsRefreshLocked() {
		t.Fatal("without a refresh token there is nothing to renew")
	}
}

func TestDecisionDeliveredBeforeNotificationIsHonoured(t *testing.T) {
	// Cancel resolves pending confirmations, and that can land after the waiter
	// is registered but before the host is asked. The confirmation path must read
	// the verdict that is already there instead of raising a dialog for a call
	// that has been refused — such a dialog would appear after its turn ended.
	refused := make(chan bool, 1)
	refused <- false
	approved, decided := takeDecision(refused)
	if !decided || approved {
		t.Fatalf("a delivered refusal should be taken as-is, got approved=%v decided=%v", approved, decided)
	}

	granted := make(chan bool, 1)
	granted <- true
	if approved, decided := takeDecision(granted); !decided || !approved {
		t.Fatalf("a delivered approval should be taken as-is, got approved=%v decided=%v", approved, decided)
	}

	// With nothing delivered the caller must go on to ask the host.
	if _, decided := takeDecision(make(chan bool, 1)); decided {
		t.Fatal("an untouched waiter must not report a decision")
	}
}

func TestCancelDuringATurnNeverReachesTheEngine(t *testing.T) {
	engine := newFakeEngine()
	provider := &scriptedProvider{responses: []*providers.LLMResponse{
		toolCallResponse("enc_send_code", `{"game_id":42,"code":"bravo"}`),
		{Content: "Отменено."},
	}}
	session := newTestSession(t, engine, agenttools.PolicyApprove, provider)
	delegate := &cancellingDelegate{session: session}
	session.SetDelegate(delegate)

	if _, err := session.SendMessage("отправь bravo"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if engine.called("SendCode") {
		t.Fatal("a cancelled call must not reach the engine")
	}
	if !delegate.sawTool() {
		t.Fatal("the test did not exercise the tool path at all")
	}
}

// cancellingDelegate cancels the turn as soon as the tool starts.
type cancellingDelegate struct {
	mu       sync.Mutex
	session  *AgentSession
	toolSeen bool
}

func (d *cancellingDelegate) OnEvent(eventJSON string) {
	var event map[string]any
	if err := json.Unmarshal([]byte(eventJSON), &event); err != nil {
		return
	}
	if event["type"] != "tool_started" {
		return
	}
	d.mu.Lock()
	d.toolSeen = true
	d.mu.Unlock()
	d.session.Cancel()
}

func (d *cancellingDelegate) sawTool() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.toolSeen
}

func (d *cancellingDelegate) OnConfirmationRequest(callID string, turn int64, toolName, argsJSON string) {
	_ = d.session.ResolveConfirmation(callID, false)
}

func TestConfirmationRequestCarriesItsTurn(t *testing.T) {
	engine := newFakeEngine()
	provider := &scriptedProvider{responses: []*providers.LLMResponse{
		toolCallResponse("enc_send_code", `{"game_id":42,"code":"bravo"}`),
		{Content: "Готово."},
	}}
	session := newTestSession(t, engine, agenttools.PolicyApprove, provider)
	delegate := &turnRecordingDelegate{session: session}
	session.SetDelegate(delegate)

	if _, err := session.SendMessage("отправь bravo"); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	// The host queues confirmations and needs the turn to discard stale ones.
	if delegate.turn != 1 {
		t.Fatalf("the confirmation should name its turn, got %d", delegate.turn)
	}
}

type turnRecordingDelegate struct {
	session *AgentSession
	turn    int64
}

func (d *turnRecordingDelegate) OnEvent(string) {}

func (d *turnRecordingDelegate) OnConfirmationRequest(callID string, turn int64, toolName, argsJSON string) {
	d.turn = turn
	_ = d.session.ResolveConfirmation(callID, true)
}

func TestEmptyMessageIsRejected(t *testing.T) {
	session := newTestSession(t, newFakeEngine(), agenttools.PolicyReadonly, &scriptedProvider{})
	if _, err := session.SendMessage("   "); err == nil {
		t.Fatal("an empty message should be rejected")
	}
}

func TestToolEventResultIsTruncated(t *testing.T) {
	long := strings.Repeat("я", eventResultLimit+50)
	truncated := truncateForEvent(long)
	if len([]rune(truncated)) != eventResultLimit+1 {
		t.Fatalf("a long result should be truncated to %d runes plus the ellipsis, got %d",
			eventResultLimit, len([]rune(truncated)))
	}
	if short := truncateForEvent("коротко"); short != "коротко" {
		t.Fatalf("a short result should pass through unchanged, got %q", short)
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not met within the timeout")
}
