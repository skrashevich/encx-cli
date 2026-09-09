package main

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// gigachatErrorBodyLimit caps how much of a GigaChat error body is quoted back
// into an error message.
const gigachatErrorBodyLimit = 400

// gigachatProvider speaks the GigaChat chat API as a PicoClaw provider.
//
// GigaChat is not the OpenAI-compatible endpoint the HTTP provider assumes.
// It carries a bearer token that expires every 30 minutes rather than a static
// key, and — the part that decides the shape of this file — it exposes tools
// through the legacy `functions` / `function_call` fields, not `tools` /
// `tool_calls`. An OpenAI-shaped request reaches it without error and simply
// silently loses every tool, which for an agent whose entire job is tool calls
// means it can never do anything.
type gigachatProvider struct {
	baseURL string
	model   string
	tokens  *gigachatTokenStore
	client  *http.Client

	// states maps a tool call id to the functions_state_id GigaChat returned
	// with it. The model expects that id back on the assistant message that
	// carried the call, and PicoClaw's Message has no field to carry it through
	// the loop.
	states *gigachatFunctionStates
}

// gigachatFunctionStates remembers functions_state_id per tool call id, and
// mints the ids themselves.
//
// It is shared by every provider in the process because a provider lives for
// one agent run — that is, one user message — while a chat replays its whole
// history on the next one. Kept per provider, the id was known only for the
// calls made since the last message the user typed, and every earlier call went
// back to the model without one, which is what breaks the continuation of a
// tool dialogue.
type gigachatFunctionStates struct {
	mu sync.Mutex
	// prefix makes minted ids unique to this process, so a persisted chat whose
	// calls were numbered by an earlier run cannot collide with a fresh id and
	// pick up a state that belongs to another call.
	prefix string
	states map[string]string
	// order tracks insertion so the oldest ids can be evicted: a server that
	// stays up for weeks would otherwise hold every call it ever made.
	order []string
	next  int
}

// gigachatFunctionStateLimit caps the remembered ids. An evicted one degrades to
// an omitted functions_state_id, which is what a restart does anyway.
const gigachatFunctionStateLimit = 4096

var gigachatStates = &gigachatFunctionStates{prefix: newRqUID(), states: map[string]string{}}

func (s *gigachatFunctionStates) get(callID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.states[callID]
}

// callID settles on an id for a call and remembers the functions_state_id that
// came with it. The third model generation stamps its own id; the second sends
// none, so one is invented — the tool loop matches results to calls by id.
func (s *gigachatFunctionStates) callID(stamped, functionsStateID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := stamped
	if id == "" {
		s.next++
		id = fmt.Sprintf("gigachat-call-%s-%d", s.prefix, s.next)
	}
	if functionsStateID == "" {
		return id
	}
	if _, seen := s.states[id]; !seen {
		s.order = append(s.order, id)
	}
	s.states[id] = functionsStateID
	for len(s.order) > gigachatFunctionStateLimit {
		delete(s.states, s.order[0])
		s.order = s.order[1:]
	}
	return id
}

func newGigaChatProvider(cfg gigachatConfig) (providers.LLMProvider, error) {
	tokens, err := sharedGigaChatTokenStore(cfg)
	if err != nil {
		return nil, err
	}
	client, err := gigachatHTTPClient(cfg, gigachatRequestTimeout)
	if err != nil {
		return nil, err
	}
	return &gigachatProvider{
		baseURL: cfg.baseURL,
		model:   cfg.model,
		tokens:  tokens,
		client:  client,
		states:  gigachatStates,
	}, nil
}

func (p *gigachatProvider) GetDefaultModel() string { return p.model }

// --- wire types ---

type gigachatMessage struct {
	Role string `json:"role"`
	// Content is a pointer because GigaChat separates an absent content from an
	// empty one: an assistant message that carries a function_call must send
	// content null, and "" there is refused with a 422 whose body reports a
	// JSON parse error on an empty document.
	Content          *string               `json:"content"`
	Name             string                `json:"name,omitempty"`
	FunctionCall     *gigachatFunctionCall `json:"function_call,omitempty"`
	FunctionsStateID string                `json:"functions_state_id,omitempty"`
}

// gigachatContent wraps message text for the wire.
func gigachatContent(text string) *string { return &text }

// gigachatText reads a content field that may be null.
func gigachatText(content *string) string {
	if content == nil {
		return ""
	}
	return *content
}

type gigachatFunctionCall struct {
	// ID is stamped by the third model generation and absent from the second.
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

type gigachatFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type gigachatChatRequest struct {
	Model        string             `json:"model"`
	Messages     []gigachatMessage  `json:"messages"`
	Functions    []gigachatFunction `json:"functions,omitempty"`
	FunctionCall string             `json:"function_call,omitempty"`
	Temperature  *float64           `json:"temperature,omitempty"`
	TopP         *float64           `json:"top_p,omitempty"`
	MaxTokens    *int               `json:"max_tokens,omitempty"`
}

type gigachatChatResponse struct {
	Choices []struct {
		Message      gigachatMessage `json:"message"`
		FinishReason string          `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// --- request construction ---

// gigachatFunctions converts PicoClaw tool definitions into GigaChat's
// function catalog.
func gigachatFunctions(tools []providers.ToolDefinition) []gigachatFunction {
	if len(tools) == 0 {
		return nil
	}
	functions := make([]gigachatFunction, 0, len(tools))
	for _, tool := range tools {
		functions = append(functions, gigachatFunction{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			Parameters:  tool.Function.Parameters,
		})
	}
	return functions
}

// gigachatToolContent returns tool output in the form the function role
// requires: GigaChat parses the content of a function result as JSON, so plain
// text has to be handed over as a JSON string rather than raw.
func gigachatToolContent(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed != "" && json.Valid([]byte(trimmed)) {
		return trimmed
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return `""`
	}
	return string(encoded)
}

// gigachatMessages converts one conversation into GigaChat's message array.
//
// Tool results arrive from the tool loop as role "tool" keyed by call id, while
// GigaChat wants role "function" keyed by function name, so the call ids seen on
// preceding assistant messages resolve the name.
//
// A call the model was never shown is not sent back as a function_call. Two of
// those exist: PicoClaw registers the level-review continuation guard with
// RegisterHidden, which keeps it out of ToProviderDefs, and GigaChat answers
// with a single call, so anything past the first has no counterpart on the
// wire. Referring to an undefined function risks a rejection from GigaChat's
// stricter legacy-functions validator, and emitting its result would orphan a
// function message, so both degrade to plain text — which is all the guard
// wanted in the first place.
func (p *gigachatProvider) gigachatMessages(
	messages []providers.Message,
	functions []gigachatFunction,
) []gigachatMessage {
	advertised := make(map[string]struct{}, len(functions))
	for _, function := range functions {
		advertised[function.Name] = struct{}{}
	}

	names := map[string]string{}
	out := make([]gigachatMessage, 0, len(messages))

	for _, message := range messages {
		switch message.Role {
		case "tool":
			name, sentAsCall := names[message.ToolCallID]
			if !sentAsCall {
				out = append(out, gigachatMessage{Role: "user", Content: gigachatContent(message.Content)})
				continue
			}
			out = append(out, gigachatMessage{
				Role:    "function",
				Name:    name,
				Content: gigachatContent(gigachatToolContent(message.Content)),
			})
		case "assistant":
			converted := gigachatMessage{Role: "assistant", Content: gigachatContent(message.Content)}
			if len(message.ToolCalls) > 0 {
				call := message.ToolCalls[0]
				if _, ok := advertised[call.Name]; ok {
					names[call.ID] = call.Name
					converted.FunctionCall = &gigachatFunctionCall{
						Name:      call.Name,
						Arguments: gigachatCallArguments(call),
					}
					converted.FunctionsStateID = p.states.get(call.ID)
					// The call travels with content null. GigaChat reads the
					// content of a message that carries a function_call as a
					// document of its own, so the empty string PicoClaw leaves
					// there is refused: "JSON parse error at line 1 column 1:
					// The document is empty".
					converted.Content = nil
				}
			}
			out = append(out, converted)
		default:
			out = append(out, gigachatMessage{Role: message.Role, Content: gigachatContent(message.Content)})
		}
	}
	return out
}

// gigachatCallArguments recovers a call's arguments as an object. GigaChat takes
// them structured, while PicoClaw may carry either the map or the JSON string
// depending on which stage of the loop built the message.
func gigachatCallArguments(call providers.ToolCall) map[string]any {
	if len(call.Arguments) > 0 {
		return call.Arguments
	}
	if call.Function == nil || strings.TrimSpace(call.Function.Arguments) == "" {
		return nil
	}
	var arguments map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &arguments); err != nil {
		return nil
	}
	return arguments
}

// --- the chat call ---

func (p *gigachatProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	options map[string]any,
) (*providers.LLMResponse, error) {
	functions := gigachatFunctions(tools)
	request := gigachatChatRequest{
		Model:       cmp.Or(strings.TrimSpace(model), p.model),
		Messages:    p.gigachatMessages(messages, functions),
		Functions:   functions,
		Temperature: optionFloat(options, "temperature"),
		TopP:        optionFloat(options, "top_p"),
		MaxTokens:   optionInt(options, "max_tokens"),
	}
	if len(functions) > 0 {
		request.FunctionCall = "auto"
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode the GigaChat request: %w", err)
	}

	debugf("gigachat request: %s", summarizeDebugText(string(body), 4000))

	payload, err := p.post(ctx, body)
	if err != nil {
		return nil, err
	}

	var decoded gigachatChatResponse
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("parse the GigaChat response: %w", err)
	}
	if len(decoded.Choices) == 0 {
		return nil, errors.New("the GigaChat API returned no choices")
	}

	choice := decoded.Choices[0]
	response := &providers.LLMResponse{
		Content:      gigachatText(choice.Message.Content),
		FinishReason: choice.FinishReason,
	}
	if decoded.Usage != nil {
		response.Usage = &providers.UsageInfo{
			PromptTokens:     decoded.Usage.PromptTokens,
			CompletionTokens: decoded.Usage.CompletionTokens,
			TotalTokens:      decoded.Usage.TotalTokens,
		}
	}
	if call := choice.Message.FunctionCall; call != nil && call.Name != "" {
		arguments := call.Arguments
		if arguments == nil {
			arguments = map[string]any{}
		}
		argumentsJSON, err := json.Marshal(arguments)
		if err != nil {
			argumentsJSON = []byte("{}")
		}
		response.ToolCalls = []providers.ToolCall{{
			ID:        p.states.callID(call.ID, choice.Message.FunctionsStateID),
			Type:      "function",
			Name:      call.Name,
			Arguments: arguments,
			Function: &providers.FunctionCall{
				Name:      call.Name,
				Arguments: string(argumentsJSON),
			},
		}}
	}
	return response, nil
}

// post sends one request body, retrying once on a rejected token.
//
// A 401 means the token in hand is dead even though the store still considers
// it live — a revoked key, or a local clock more than the renewal skew fast.
// Without dropping it, every remaining turn would resend the same dead
// credential, and isRetryableLLMError reads "unauthorized" as permanent, so the
// run would fail with no way to recover short of restarting the process.
func (p *gigachatProvider) post(ctx context.Context, body []byte) ([]byte, error) {
	for attempt := range 2 {
		token, err := p.tokens.accessToken(ctx)
		if err != nil {
			return nil, err
		}
		payload, status, err := p.send(ctx, body, token)
		if err != nil {
			return nil, err
		}
		switch {
		case status == http.StatusOK:
			return payload, nil
		case status == http.StatusUnauthorized && attempt == 0:
			debugf("gigachat: the access token was rejected, exchanging a new one")
			p.tokens.invalidate(token)
		case status == http.StatusNotFound:
			// The two GigaChat hosts serve different model generations, and the
			// body says only "No such model". Name the ones this host has.
			return nil, fmt.Errorf("GigaChat API error: HTTP %d: %s%s",
				status, summarizeDebugText(string(payload), gigachatErrorBodyLimit),
				p.modelHint(ctx, token))
		default:
			// The status is spelled as "HTTP <code>" because that is the form
			// isRetryableLLMError matches when it decides whether to try again.
			return nil, fmt.Errorf("GigaChat API error: HTTP %d: %s",
				status, summarizeDebugText(string(payload), gigachatErrorBodyLimit))
		}
	}
	return nil, errors.New("the GigaChat API rejected a freshly issued access token")
}

// modelHint lists the chat models the endpoint serves, so a 404 for a model
// name says what to use instead. It returns an empty string when the listing is
// unavailable: this only ever decorates an error that is already being
// returned, and must not replace it with a second failure.
func (p *gigachatProvider) modelHint(ctx context.Context, token string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/models", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}
	var listing struct {
		Data []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		return ""
	}
	var models []string
	for _, model := range listing.Data {
		if model.Type == "chat" {
			models = append(models, model.ID)
		}
	}
	if len(models) == 0 {
		return ""
	}
	hint := fmt.Sprintf("\n%s serves: %s", p.baseURL, strings.Join(models, ", "))
	if p.baseURL != legacyGigaChatBaseURL {
		hint += fmt.Sprintf("\nOlder names live on the legacy host: GIGACHAT_BASE_URL=%s", legacyGigaChatBaseURL)
	}
	return hint
}

func (p *gigachatProvider) send(ctx context.Context, body []byte, token string) ([]byte, int, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("build the GigaChat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("X-Request-ID", newRqUID())
	httpReq.Header.Set("User-Agent", "encli/"+version)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, 0, fmt.Errorf("call the GigaChat API: %w", err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("read the GigaChat response: %w", err)
	}
	return payload, resp.StatusCode, nil
}

func optionFloat(options map[string]any, key string) *float64 {
	switch value := options[key].(type) {
	case float64:
		return &value
	case int:
		converted := float64(value)
		return &converted
	}
	return nil
}

func optionInt(options map[string]any, key string) *int {
	switch value := options[key].(type) {
	case int:
		return &value
	case float64:
		converted := int(value)
		return &converted
	}
	return nil
}
