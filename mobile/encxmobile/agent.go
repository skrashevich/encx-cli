package encxmobile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/tools"
	"github.com/skrashevich/encx-cli/agenttools"
)

// readCacheTTL bounds how long a memoized engine read may be reused. It is a
// backstop only: the cache is cleared at the start of every turn and whenever a
// mutating tool runs.
const readCacheTTL = 2 * time.Minute

// unlimitedIterations is what a negative max_iterations resolves to. The turn is
// still bounded in practice: the player can stop it, and every step costs a
// provider call.
const unlimitedIterations = math.MaxInt32

const (
	// defaultAgentMaxIterations is generous because useful requests are not
	// always short — "try codes 1 through 90" is one question and ninety steps.
	defaultAgentMaxIterations = 100
	defaultAgentSystemPrompt  = "You are the in-app assistant of an Encounter (en.cx) player. " +
		"Answer in the language the player writes in. Keep answers short and practical: " +
		"they are read on a phone, usually mid-game."
)

// AgentDelegate is the host-side callback surface of an AgentSession. It is a
// Go interface so Swift can implement it directly through the gomobile binding.
type AgentDelegate interface {
	// OnEvent delivers a JSON-encoded progress event. See AgentSession for the
	// event shapes.
	OnEvent(eventJSON string)
	// OnConfirmationRequest asks the host to authorize a mutating engine call.
	// The host must answer exactly once by calling ResolveConfirmation with the
	// same callID; until then the agent turn is blocked.
	//
	// turn identifies the agent turn that raised the call. A host that keeps a
	// queue should drop requests from a turn that has already ended: the
	// notification is delivered asynchronously and can outlive its turn.
	OnConfirmationRequest(callID string, turn int64, toolName string, argsJSON string)
}

// agentConfig is the JSON contract between the host app and the agent.
type agentConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIBase  string `json:"api_base"`
	APIKey   string `json:"api_key"`
	// AuthMethod selects "codex" (a ChatGPT subscription) instead of an API key.
	AuthMethod string `json:"auth_method"`
	// CodexCredential is the JSON returned by CodexDeviceLogin, required when
	// AuthMethod is "codex".
	CodexCredential string `json:"codex_credential"`
	Policy          string `json:"policy"`
	SystemPrompt    string `json:"system_prompt"`
	// WebToolsEnabled adds DuckDuckGo search and a page reader to the toolset.
	WebToolsEnabled bool `json:"web_tools"`
	// MaxIterations caps the tool-call steps in one turn. Zero means the default;
	// a negative value removes the cap.
	MaxIterations         int     `json:"max_iterations"`
	MaxTokens             int     `json:"max_tokens"`
	Temperature           float64 `json:"temperature"`
	RequestTimeoutSeconds int     `json:"request_timeout_seconds"`
}

var silenceLoggingOnce sync.Once

// silenceAgentLogging stops PicoClaw's console logger.
//
// Its default sink is os.Stdout, and it logs every tool call together with the
// arguments — which for this toolset includes the codes a player submits. In a
// phone process that lands in the system log, readable by anyone with the
// device attached. Nothing in the app consumes those logs.
func silenceAgentLogging() {
	silenceLoggingOnce.Do(logger.DisableConsole)
}

func parseAgentConfig(configJSON string) (agentConfig, agenttools.Policy, error) {
	var cfg agentConfig
	if strings.TrimSpace(configJSON) == "" {
		return cfg, "", errors.New("encxmobile: agent config JSON is required")
	}
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return cfg, "", fmt.Errorf("encxmobile: parse agent config: %w", err)
	}
	cfg.AuthMethod = strings.ToLower(strings.TrimSpace(cfg.AuthMethod))
	if strings.TrimSpace(cfg.Model) == "" {
		return cfg, "", errors.New("encxmobile: agent config needs a model")
	}
	policy, err := agenttools.ParsePolicy(strings.TrimSpace(cfg.Policy))
	if err != nil {
		return cfg, "", fmt.Errorf("encxmobile: %w", err)
	}
	if cfg.AuthMethod == AuthMethodCodex && strings.TrimSpace(cfg.CodexCredential) == "" {
		return cfg, "", errors.New("encxmobile: ChatGPT sign-in is selected but no credential was supplied")
	}
	switch {
	case cfg.MaxIterations < 0:
		cfg.MaxIterations = unlimitedIterations
	case cfg.MaxIterations == 0:
		cfg.MaxIterations = defaultAgentMaxIterations
	}
	if strings.TrimSpace(cfg.SystemPrompt) == "" {
		cfg.SystemPrompt = defaultAgentSystemPrompt
	}
	return cfg, policy, nil
}

func (c agentConfig) modelConfig() *config.ModelConfig {
	model := &config.ModelConfig{
		ModelName:      c.Model,
		Provider:       strings.TrimSpace(c.Provider),
		Model:          c.Model,
		APIBase:        strings.TrimSpace(c.APIBase),
		RequestTimeout: c.RequestTimeoutSeconds,
		Enabled:        true,
	}
	if key := strings.TrimSpace(c.APIKey); key != "" {
		// Set() stores the key verbatim; NewSecureString would try to interpret
		// enc:// and file:// prefixes that a phone keychain value never has.
		model.APIKeys = config.SecureStrings{new(config.SecureString).Set(key)}
	}
	return model
}

func (c agentConfig) llmOptions() map[string]any {
	options := map[string]any{}
	if c.MaxTokens > 0 {
		options["max_tokens"] = c.MaxTokens
	}
	if c.Temperature > 0 {
		options["temperature"] = c.Temperature
	}
	return options
}

// AgentSession is a PicoClaw agent wired to one Encounter client.
//
// Progress reaches the host as JSON events through AgentDelegate.OnEvent. Every
// event has a "type" and a "turn" field:
//
//	{"type":"turn_started","turn":1}
//	{"type":"tool_started","turn":1,"tool":"enc_game_state","args":{"game_id":42}}
//	{"type":"tool_finished","turn":1,"tool":"enc_game_state","is_error":false,"result":"…"}
//	{"type":"turn_finished","turn":1,"content":"…"}
//	{"type":"turn_failed","turn":1,"error":"…"}
//
// Mutating tools do not emit an event; they call OnConfirmationRequest and wait.
type AgentSession struct {
	provider providers.LLMProvider
	model    string
	registry *tools.ToolRegistry
	catalog  *agenttools.Catalog
	options  map[string]any

	maxIterations int
	systemPrompt  string
	// endpoint is the configured API base, kept only to name it in failures.
	endpoint string
	// codex is non-nil when the session runs on a ChatGPT subscription; it holds
	// the refreshable OAuth credential.
	codex *codexTokenStore

	mu       sync.Mutex
	delegate AgentDelegate
	history  []providers.Message
	pending  map[string]chan bool
	cancel   context.CancelFunc
	turn     int64
	callSeq  int64
}

// NewAgentSession builds an agent over this client. configJSON carries the LLM
// provider, model, credentials and the engine access policy:
//
//	{"provider":"openai","model":"gpt-5","api_key":"…","policy":"approve"}
//
// Under the default "approve" policy a delegate must be attached before the
// first mutating call, otherwise the agent refuses it.
func (c *EncClient) NewAgentSession(configJSON string) (*AgentSession, error) {
	cfg, policy, err := parseAgentConfig(configJSON)
	if err != nil {
		return nil, err
	}

	var (
		provider   providers.LLMProvider
		codexStore *codexTokenStore
	)
	switch {
	case cfg.AuthMethod == AuthMethodCodex:
		provider, codexStore, err = newCodexProvider(cfg.CodexCredential)
		if err != nil {
			return nil, err
		}
	default:
		provider, err = newHTTPProvider(cfg)
		if err != nil {
			return nil, err
		}
	}

	session, err := newAgentSession(c.client, cfg, policy, provider)
	if err != nil {
		return nil, err
	}
	session.codex = codexStore
	return session, nil
}

// newAgentSession wires the engine toolset, the observation layer and the LLM
// provider together. It is separate from NewAgentSession so tests can drive a
// scripted provider over a scripted engine.
func newAgentSession(
	engine agenttools.Engine,
	cfg agentConfig,
	policy agenttools.Policy,
	provider providers.LLMProvider,
) (*AgentSession, error) {
	silenceAgentLogging()

	session := &AgentSession{
		provider:      provider,
		model:         cfg.Model,
		options:       cfg.llmOptions(),
		maxIterations: cfg.MaxIterations,
		endpoint:      strings.TrimSpace(cfg.APIBase),
		pending:       map[string]chan bool{},
	}

	// An LLM runs the tool calls of one turn in parallel, so a single question can
	// fire several engine requests at once. Encounter answers a burst with its
	// anti-spam page instead of JSON, which looks to the player like a broken
	// session on an account that is perfectly fine.
	catalog, err := agenttools.NewCatalog(agenttools.NewPaced(engine, 0), agenttools.Options{
		Policy: policy,
		// Within one answer the model re-reads the same facts several times, and
		// each read is a paced round trip. The cache is cleared at the start of
		// every turn and by any mutation, so it never answers a new question with
		// a stale level.
		ReadCacheTTL: readCacheTTL,
		// The confirmer is a separate adapter rather than a method on
		// AgentSession: gomobile binds every exported method, and a
		// ConfirmToolCall(context.Context, agenttools.ConfirmRequest) signature
		// is not expressible in Swift.
		Confirmer: sessionConfirmer{session: session},
	})
	if err != nil {
		return nil, fmt.Errorf("encxmobile: build engine toolset: %w", err)
	}
	session.catalog = catalog
	session.systemPrompt = cfg.SystemPrompt + "\n\n" + catalog.SystemPromptAddendum()

	registry := tools.NewToolRegistry()
	for _, tool := range catalog.Tools() {
		registry.Register(session.observe(tool))
	}
	if cfg.WebToolsEnabled {
		registerWebTools(registry, session.observe)
	}
	session.registry = registry
	return session, nil
}

// SetDelegate attaches (or, with nil, detaches) the host callbacks.
func (s *AgentSession) SetDelegate(delegate AgentDelegate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.delegate = delegate
}

// Policy reports the engine access policy the session was built with.
func (s *AgentSession) Policy() string {
	return string(s.catalog.Policy())
}

// ToolCount reports how many engine tools the model can see.
func (s *AgentSession) ToolCount() int64 {
	return int64(len(s.catalog.Tools()))
}

// SendMessage runs one agent turn and returns the assistant's reply.
//
// Only the visible transcript is remembered between turns. Tool output is
// deliberately not replayed: game state changes while the player reads, so a
// cached level or code log would be worse than a fresh read.
func (s *AgentSession) SendMessage(text string) (string, error) {
	message := strings.TrimSpace(text)
	if message == "" {
		return "", errors.New("encxmobile: the message is empty")
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		cancel()
		return "", errors.New("encxmobile: a turn is already running")
	}
	s.cancel = cancel
	s.turn++
	turn := s.turn
	conversation := make([]providers.Message, 0, len(s.history)+2)
	conversation = append(conversation, providers.Message{Role: "system", Content: s.systemPrompt})
	conversation = append(conversation, s.history...)
	conversation = append(conversation, providers.Message{Role: "user", Content: message})
	s.mu.Unlock()

	defer func() {
		cancel()
		s.mu.Lock()
		s.cancel = nil
		s.failPendingLocked()
		s.mu.Unlock()
	}()

	// A new question deserves fresh facts: the game moved while the player typed.
	s.catalog.InvalidateCache()

	s.emit(turn, map[string]any{"type": "turn_started"})

	result, err := tools.RunToolLoop(ctx, tools.ToolLoopConfig{
		Provider:      s.provider,
		Model:         s.model,
		Tools:         s.registry,
		MaxIterations: s.maxIterations,
		LLMOptions:    s.options,
	}, conversation, "encx-app", "agent")
	if err != nil {
		err = s.describeTurnFailure(err)
		s.emit(turn, map[string]any{"type": "turn_failed", "error": err.Error()})
		return "", err
	}

	content := strings.TrimSpace(result.Content)
	if content == "" {
		// RunToolLoop leaves the content empty when the model spent every
		// iteration on tool calls. Reporting that as an empty reply would show a
		// blank bubble and poison the history with an empty assistant message.
		err := errors.New("encxmobile: the model ran out of tool steps without answering")
		s.emit(turn, map[string]any{"type": "turn_failed", "error": err.Error()})
		return "", err
	}

	s.mu.Lock()
	s.history = append(s.history,
		providers.Message{Role: "user", Content: message},
		providers.Message{Role: "assistant", Content: content},
	)
	s.mu.Unlock()

	s.emit(turn, map[string]any{"type": "turn_finished", "content": content})
	return content, nil
}

// Cancel aborts the running turn. SendMessage then returns the cancellation
// error and any pending confirmation is released as declined.
func (s *AgentSession) Cancel() {
	s.mu.Lock()
	cancel := s.cancel
	s.failPendingLocked()
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ResolveConfirmation delivers the host's decision for a mutating call.
func (s *AgentSession) ResolveConfirmation(callID string, approved bool) error {
	s.mu.Lock()
	waiter, ok := s.pending[callID]
	if ok {
		delete(s.pending, callID)
	}
	s.mu.Unlock()

	if !ok {
		return fmt.Errorf("encxmobile: no confirmation is pending for %q", callID)
	}
	waiter <- approved
	return nil
}

// CodexCredentialJSON returns the current ChatGPT credential so the host can
// persist a token that was refreshed during the session. It returns an empty
// string for sessions that authenticate with an API key.
func (s *AgentSession) CodexCredentialJSON() (string, error) {
	if s.codex == nil {
		return "", nil
	}
	return s.codex.credentialJSON()
}

// HistoryJSON returns the remembered transcript as a JSON array of
// {"role","content"} objects.
func (s *AgentSession) HistoryJSON() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	type entry struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	entries := make([]entry, 0, len(s.history))
	for _, message := range s.history {
		entries = append(entries, entry{Role: message.Role, Content: message.Content})
	}
	return marshalJSON(entries)
}

// ResetHistory forgets the transcript without rebuilding the session.
func (s *AgentSession) ResetHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = nil
}

// sessionConfirmer routes catalog authorization requests to the host delegate.
type sessionConfirmer struct {
	session *AgentSession
}

// ConfirmToolCall implements agenttools.Confirmer.
func (c sessionConfirmer) ConfirmToolCall(ctx context.Context, req agenttools.ConfirmRequest) (bool, error) {
	return c.session.confirm(ctx, req)
}

func (s *AgentSession) confirm(ctx context.Context, req agenttools.ConfirmRequest) (bool, error) {
	// The observation layer already stamped this execution with an ID; reusing it
	// lets the host tie the confirmation to the activity row it is showing.
	callID, ok := callIDFrom(ctx)
	if !ok {
		callID = s.nextCallID()
	}

	s.mu.Lock()
	delegate := s.delegate
	if delegate == nil {
		s.mu.Unlock()
		return false, errors.New("no confirmation delegate is attached to the session")
	}
	turn := s.turn
	waiter := make(chan bool, 1)
	s.pending[callID] = waiter
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.pending, callID)
		s.mu.Unlock()
	}()

	// Cancel can resolve this call in the window between registering the waiter
	// and notifying the host. Asking anyway would raise a dialog for a call that
	// has already been refused.
	if approved, decided := takeDecision(waiter); decided {
		return approved, nil
	}

	argsJSON, err := json.Marshal(req.Args)
	if err != nil {
		argsJSON = []byte("{}")
	}
	delegate.OnConfirmationRequest(callID, turn, req.Tool, string(argsJSON))

	select {
	case approved := <-waiter:
		return approved, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// describeTurnFailure names the endpoint and model in a failed turn.
//
// Providers report transport failures as a bare status code, which leaves the
// player guessing whether the endpoint, the model or the key is wrong. A 404
// from an OpenAI-compatible base is almost always one of two things: the base
// does not serve /chat/completions, or it does not know the model.
func (s *AgentSession) describeTurnFailure(err error) error {
	if err == nil {
		return nil
	}

	endpoint := s.endpoint
	if endpoint == "" {
		endpoint = "the provider's default endpoint"
	}
	detail := fmt.Sprintf("%v (endpoint: %s, model: %s)", err, endpoint, s.model)
	if strings.Contains(err.Error(), "404") {
		detail += ". A 404 here means the endpoint does not serve /chat/completions" +
			" or does not know this model; check both."
	}
	return errors.New(detail)
}

// takeDecision reports a verdict already delivered to a waiter without blocking.
//
// Cancel resolves every pending confirmation, which can land after the waiter is
// registered but before the host has been asked. Checking first keeps a dialog
// from being raised for a call that has already been refused — the host would
// otherwise show it after the turn had ended.
func takeDecision(waiter chan bool) (approved bool, decided bool) {
	select {
	case verdict := <-waiter:
		return verdict, true
	default:
		return false, false
	}
}

// isRunning reports whether a turn is in flight.
func (s *AgentSession) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancel != nil
}

// failPendingLocked declines every waiting confirmation. The caller holds s.mu.
func (s *AgentSession) failPendingLocked() {
	for callID, waiter := range s.pending {
		delete(s.pending, callID)
		waiter <- false
	}
}
