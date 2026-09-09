package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/providers"
)

// newTestGigaChatProvider builds a provider pointed at stub, with a token that
// is already valid so no exchange is attempted.
func newTestGigaChatProvider(t *testing.T, baseURL string, client *http.Client) *gigachatProvider {
	t.Helper()
	tokens := newGigaChatTokenStore(gigachatConfig{
		credentials: "key-1",
		scope:       defaultGigaChatScope,
		authURL:     baseURL + "/unused",
	}, client)
	tokens.token = "tok-1"
	tokens.expiresAt = time.Now().Add(time.Hour)
	return newTestGigaChatProviderWithTokens(baseURL, client, tokens)
}

func newTestGigaChatProviderWithTokens(
	baseURL string, client *http.Client, tokens *gigachatTokenStore,
) *gigachatProvider {
	return &gigachatProvider{
		baseURL:        baseURL,
		model:          defaultGigaChatModel,
		tokens:         tokens,
		client:         client,
		functionsState: map[string]string{},
	}
}

// gigachatChatStub serves /chat/completions and records the decoded request.
type gigachatChatStub struct {
	server *httptest.Server

	// mu guards the recordings: they are written from the handler goroutine and
	// read from the test one.
	mu       sync.Mutex
	requests []map[string]any
	auths    []string
}

func (s *gigachatChatStub) request(index int) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.requests[index]
}

func (s *gigachatChatStub) authorizations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.auths...)
}

func newGigaChatChatStub(t *testing.T, respond func(w http.ResponseWriter)) *gigachatChatStub {
	t.Helper()
	stub := &gigachatChatStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The 404 path asks for a model listing; this stub does not serve one, so
		// the error it decorates has to survive without it.
		if r.URL.Path == "/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %q, want /chat/completions", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Errorf("decode request: %v", err)
		}
		stub.mu.Lock()
		stub.auths = append(stub.auths, r.Header.Get("Authorization"))
		stub.requests = append(stub.requests, decoded)
		stub.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		respond(w)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func testToolDefinitions() []providers.ToolDefinition {
	return []providers.ToolDefinition{{
		Type: "function",
		Function: providers.ToolFunctionDefinition{
			Name:        "admin_levels",
			Description: "List the levels of the current game",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"game_id": map[string]any{"type": "integer"}},
				"required":   []any{"game_id"},
			},
		},
	}}
}

func TestGigaChatProviderSendsLegacyFunctions(t *testing.T) {
	stub := newGigaChatChatStub(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"готово"},` +
			`"index":0,"finish_reason":"stop"}],"usage":{"prompt_tokens":11,"completion_tokens":3,"total_tokens":14}}`))
	})
	provider := newTestGigaChatProvider(t, stub.server.URL, stub.server.Client())

	response, err := provider.Chat(context.Background(), []providers.Message{
		{Role: "system", Content: "you are an agent"},
		{Role: "user", Content: "сколько уровней?"},
	}, testToolDefinitions(), "GigaChat-2-Pro", nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if got := stub.authorizations(); len(got) != 1 || got[0] != "Bearer tok-1" {
		t.Fatalf("Authorization = %q, want the access token as a bearer", got)
	}
	request := stub.request(0)
	if request["model"] != "GigaChat-2-Pro" {
		t.Fatalf("model = %v, want the per-call model", request["model"])
	}
	if _, present := request["tools"]; present {
		t.Fatal("request carried `tools`; GigaChat only reads `functions`")
	}
	if request["function_call"] != "auto" {
		t.Fatalf("function_call = %v, want auto", request["function_call"])
	}
	functions, ok := request["functions"].([]any)
	if !ok || len(functions) != 1 {
		t.Fatalf("functions = %v, want one entry", request["functions"])
	}
	function := functions[0].(map[string]any)
	if function["name"] != "admin_levels" || function["description"] != "List the levels of the current game" {
		t.Fatalf("function = %v", function)
	}
	if _, ok := function["parameters"].(map[string]any); !ok {
		t.Fatalf("function parameters = %v, want the JSON schema", function["parameters"])
	}

	messages := request["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("messages = %v, want both to survive", messages)
	}
	if first := messages[0].(map[string]any); first["role"] != "system" {
		t.Fatalf("messages[0] = %v", first)
	}

	if response.Content != "готово" || response.FinishReason != "stop" {
		t.Fatalf("response = %+v", response)
	}
	if response.Usage == nil || response.Usage.PromptTokens != 11 ||
		response.Usage.CompletionTokens != 3 || response.Usage.TotalTokens != 14 {
		t.Fatalf("usage = %+v, want the reported token counts", response.Usage)
	}
	if len(response.ToolCalls) != 0 {
		t.Fatalf("tool calls = %v, want none for a plain answer", response.ToolCalls)
	}
}

func TestGigaChatProviderOmitsFunctionCallWithoutTools(t *testing.T) {
	stub := newGigaChatChatStub(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	})
	provider := newTestGigaChatProvider(t, stub.server.URL, stub.server.Client())

	if _, err := provider.Chat(context.Background(),
		[]providers.Message{{Role: "user", Content: "привет"}}, nil, "", nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	request := stub.request(0)
	if _, present := request["functions"]; present {
		t.Fatalf("functions = %v, want the field omitted with no tools", request["functions"])
	}
	if _, present := request["function_call"]; present {
		t.Fatalf("function_call = %v, want the field omitted with no tools", request["function_call"])
	}
	if request["model"] != defaultGigaChatModel {
		t.Fatalf("model = %v, want the provider default when the call names none", request["model"])
	}
}

func TestGigaChatProviderParsesFunctionCall(t *testing.T) {
	stub := newGigaChatChatStub(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"",` +
			`"function_call":{"name":"admin_levels","arguments":{"game_id":77}},` +
			`"functions_state_id":"state-42"},"index":0,"finish_reason":"function_call"}],` +
			`"usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`))
	})
	provider := newTestGigaChatProvider(t, stub.server.URL, stub.server.Client())

	response, err := provider.Chat(context.Background(),
		[]providers.Message{{Role: "user", Content: "покажи уровни"}}, testToolDefinitions(), "", nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(response.ToolCalls) != 1 {
		t.Fatalf("tool calls = %v, want exactly one", response.ToolCalls)
	}
	call := response.ToolCalls[0]
	if call.Name != "admin_levels" || call.Type != "function" {
		t.Fatalf("call = %+v", call)
	}
	if call.ID == "" {
		t.Fatal("call carried no id; the tool loop matches results by id")
	}
	if got, ok := call.Arguments["game_id"].(float64); !ok || got != 77 {
		t.Fatalf("arguments = %v, want game_id 77", call.Arguments)
	}
	if call.Function == nil || call.Function.Arguments != `{"game_id":77}` {
		t.Fatalf("function = %+v, want the arguments also as a JSON string", call.Function)
	}
	if response.FinishReason != "function_call" {
		t.Fatalf("finish reason = %q", response.FinishReason)
	}
	if got := provider.functionsStateFor(call.ID); got != "state-42" {
		t.Fatalf("functions state for %s = %q, want state-42", call.ID, got)
	}
}

func TestGigaChatProviderRoundTripsToolResult(t *testing.T) {
	responses := [][]byte{
		[]byte(`{"choices":[{"message":{"role":"assistant","content":"",` +
			`"function_call":{"name":"admin_levels","arguments":{"game_id":77}},` +
			`"functions_state_id":"state-42"},"finish_reason":"function_call"}]}`),
		[]byte(`{"choices":[{"message":{"role":"assistant","content":"три уровня"},"finish_reason":"stop"}]}`),
	}
	turn := 0
	stub := newGigaChatChatStub(t, func(w http.ResponseWriter) {
		_, _ = w.Write(responses[turn])
		turn++
	})
	provider := newTestGigaChatProvider(t, stub.server.URL, stub.server.Client())

	conversation := []providers.Message{{Role: "user", Content: "покажи уровни"}}
	first, err := provider.Chat(context.Background(), conversation, testToolDefinitions(), "", nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	call := first.ToolCalls[0]

	// This is the shape PicoClaw's tool loop appends after running the tool.
	conversation = append(conversation,
		providers.Message{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{
			ID:        call.ID,
			Type:      "function",
			Name:      call.Name,
			Arguments: call.Arguments,
			Function:  &providers.FunctionCall{Name: call.Name, Arguments: `{"game_id":77}`},
		}}},
		providers.Message{Role: "tool", ToolCallID: call.ID, Content: `{"levels":3}`},
	)
	if _, err := provider.Chat(context.Background(), conversation, testToolDefinitions(), "", nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	messages := stub.request(1)["messages"].([]any)
	if len(messages) != 3 {
		t.Fatalf("messages = %v, want user, assistant and function", messages)
	}

	assistant := messages[1].(map[string]any)
	if assistant["role"] != "assistant" {
		t.Fatalf("messages[1] = %v", assistant)
	}
	if _, present := assistant["tool_calls"]; present {
		t.Fatal("assistant message carried `tool_calls`; GigaChat reads `function_call`")
	}
	functionCall, ok := assistant["function_call"].(map[string]any)
	if !ok || functionCall["name"] != "admin_levels" {
		t.Fatalf("function_call = %v", assistant["function_call"])
	}
	arguments, ok := functionCall["arguments"].(map[string]any)
	if !ok || arguments["game_id"].(float64) != 77 {
		t.Fatalf("arguments = %v, want an object, not a string", functionCall["arguments"])
	}
	if assistant["functions_state_id"] != "state-42" {
		t.Fatalf("functions_state_id = %v, want the id the model returned with the call",
			assistant["functions_state_id"])
	}

	result := messages[2].(map[string]any)
	if result["role"] != "function" {
		t.Fatalf("messages[2] role = %v, want function", result["role"])
	}
	if result["name"] != "admin_levels" {
		t.Fatalf("messages[2] name = %v, want the called function", result["name"])
	}
	if result["content"] != `{"levels":3}` {
		t.Fatalf("messages[2] content = %v", result["content"])
	}
	if _, present := result["tool_call_id"]; present {
		t.Fatal("function result carried tool_call_id, which GigaChat does not read")
	}
}

func TestGigaChatProviderRecoversArgumentsFromJSONString(t *testing.T) {
	stub := newGigaChatChatStub(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	})
	provider := newTestGigaChatProvider(t, stub.server.URL, stub.server.Client())

	// An assistant message whose arguments survived only as the JSON string.
	conversation := []providers.Message{
		{Role: "user", Content: "покажи уровни"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{
			ID:       "call-1",
			Name:     "admin_levels",
			Function: &providers.FunctionCall{Name: "admin_levels", Arguments: `{"game_id":5}`},
		}}},
		{Role: "tool", ToolCallID: "call-1", Content: "plain text, not JSON"},
	}
	if _, err := provider.Chat(context.Background(), conversation, testToolDefinitions(), "", nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	messages := stub.request(0)["messages"].([]any)
	functionCall := messages[1].(map[string]any)["function_call"].(map[string]any)
	arguments, ok := functionCall["arguments"].(map[string]any)
	if !ok || arguments["game_id"].(float64) != 5 {
		t.Fatalf("arguments = %v, want them decoded from the JSON string", functionCall["arguments"])
	}
	if _, present := messages[1].(map[string]any)["functions_state_id"]; present {
		t.Fatal("functions_state_id was sent for a call the model never stamped")
	}
	// Plain text has to reach GigaChat as a JSON string, since it parses the
	// content of a function result as JSON.
	if got := messages[2].(map[string]any)["content"]; got != `"plain text, not JSON"` {
		t.Fatalf("content = %v, want the text JSON-encoded", got)
	}
}

func TestGigaChatToolContent(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`{"levels":3}`, `{"levels":3}`},
		{"  [1,2]  ", "[1,2]"},
		{"plain", `"plain"`},
		{"", `""`},
		{"нет данных", `"нет данных"`},
	} {
		if got := gigachatToolContent(tc.in); got != tc.want {
			t.Fatalf("gigachatToolContent(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestGigaChatProviderHTTPError(t *testing.T) {
	stub := newGigaChatChatStub(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"status":429,"message":"Too many requests"}`))
	})
	provider := newTestGigaChatProvider(t, stub.server.URL, stub.server.Client())

	_, err := provider.Chat(context.Background(),
		[]providers.Message{{Role: "user", Content: "привет"}}, nil, "", nil)
	if err == nil {
		t.Fatal("Chat succeeded against a 429")
	}
	if !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "Too many requests") {
		t.Fatalf("error = %v, want the status and the body", err)
	}
	if !isRetryableLLMError(err) {
		t.Fatalf("error = %v, want it classified as retryable", err)
	}
}

func TestGigaChatProviderRejectsEmptyChoices(t *testing.T) {
	stub := newGigaChatChatStub(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	})
	provider := newTestGigaChatProvider(t, stub.server.URL, stub.server.Client())

	if _, err := provider.Chat(context.Background(),
		[]providers.Message{{Role: "user", Content: "привет"}}, nil, "", nil); err == nil {
		t.Fatal("Chat accepted a response with no choices")
	}
}

func TestGigaChatProviderSendsSamplingOptions(t *testing.T) {
	stub := newGigaChatChatStub(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	})
	provider := newTestGigaChatProvider(t, stub.server.URL, stub.server.Client())

	if _, err := provider.Chat(context.Background(),
		[]providers.Message{{Role: "user", Content: "привет"}}, nil, "",
		map[string]any{"temperature": 0.3, "max_tokens": 512}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	request := stub.request(0)
	if request["temperature"].(float64) != 0.3 {
		t.Fatalf("temperature = %v", request["temperature"])
	}
	if request["max_tokens"].(float64) != 512 {
		t.Fatalf("max_tokens = %v", request["max_tokens"])
	}
	if _, present := request["top_p"]; present {
		t.Fatalf("top_p = %v, want the unset option omitted", request["top_p"])
	}
}

func TestGigaChatProviderGetDefaultModel(t *testing.T) {
	provider := newTestGigaChatProvider(t, "https://giga.example/api/v1", http.DefaultClient)
	if got := provider.GetDefaultModel(); got != defaultGigaChatModel {
		t.Fatalf("GetDefaultModel = %q, want %q", got, defaultGigaChatModel)
	}
}

func TestNewPicoProviderBuildsAGigaChatProvider(t *testing.T) {
	isolateLLMEnv(t)
	t.Setenv("GIGACHAT_CREDENTIALS", "key-1")

	provider, err := newPicoProvider(AgentConfig{
		AuthMethod: authMethodGigaChat,
		Model:      defaultGigaChatModel,
		BaseURL:    defaultGigaChatBaseURL,
	})
	if err != nil {
		t.Fatalf("newPicoProvider: %v", err)
	}
	if _, ok := provider.(*gigachatProvider); !ok {
		t.Fatalf("provider = %T, want *gigachatProvider", provider)
	}
	if got := provider.GetDefaultModel(); got != defaultGigaChatModel {
		t.Fatalf("GetDefaultModel = %q, want %q", got, defaultGigaChatModel)
	}
}

// PicoClaw registers the level-review continuation guard with RegisterHidden,
// which keeps it out of ToProviderDefs — so its synthetic call names a function
// GigaChat was never shown. Sending it as a function_call risks a rejection and
// its result would orphan a function message, so both become plain text.
func TestGigaChatProviderDegradesUnadvertisedToolCall(t *testing.T) {
	stub := newGigaChatChatStub(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	})
	provider := newTestGigaChatProvider(t, stub.server.URL, stub.server.Client())

	conversation := []providers.Message{
		{Role: "user", Content: "проверь уровни"},
		{Role: "assistant", Content: "", ToolCalls: []providers.ToolCall{{
			ID:        "encx-level-review-1",
			Type:      "function",
			Name:      levelReviewNudgeTool,
			Arguments: map[string]any{"message": "дозагрузи уровни"},
		}}},
		{Role: "tool", ToolCallID: "encx-level-review-1", Content: "дозагрузи уровни"},
	}
	if _, err := provider.Chat(context.Background(), conversation, testToolDefinitions(), "", nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	messages := stub.request(0)["messages"].([]any)
	assistant := messages[1].(map[string]any)
	if _, present := assistant["function_call"]; present {
		t.Fatalf("assistant = %v, want no call to a function GigaChat was never shown", assistant)
	}
	result := messages[2].(map[string]any)
	if result["role"] != "user" {
		t.Fatalf("messages[2] role = %v, want the orphaned result carried as user text", result["role"])
	}
	if result["content"] != "дозагрузи уровни" {
		t.Fatalf("messages[2] content = %v, want the nudge text verbatim", result["content"])
	}
	if _, present := result["name"]; present {
		t.Fatalf("messages[2] = %v, want no function name on a user message", result)
	}
}

// A token the server rejects has to be dropped: isRetryableLLMError reads
// "unauthorized" as permanent, so resending it would end the run.
func TestGigaChatProviderRetriesOnceOnRejectedToken(t *testing.T) {
	issued := 0
	tokenStub := newGigaChatTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		issued++
		_, _ = w.Write([]byte(`{"access_token":"tok-` + strconv.Itoa(issued) + `","expires_at":` +
			strconv.FormatInt(time.Now().Add(30*time.Minute).UnixMilli(), 10) + `}`))
	})

	turn := 0
	stub := newGigaChatChatStub(t, func(w http.ResponseWriter) {
		turn++
		if turn == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"status":401,"message":"Unauthorized"}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	})

	cfg := gigachatConfig{credentials: "key-1", scope: defaultGigaChatScope, authURL: tokenStub.server.URL}
	provider := newTestGigaChatProviderWithTokens(stub.server.URL, stub.server.Client(),
		newGigaChatTokenStore(cfg, tokenStub.server.Client()))

	response, err := provider.Chat(context.Background(),
		[]providers.Message{{Role: "user", Content: "привет"}}, nil, "", nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if response.Content != "ok" {
		t.Fatalf("content = %q, want the answer from the retried request", response.Content)
	}
	auths := stub.authorizations()
	if len(auths) != 2 || auths[0] != "Bearer tok-1" || auths[1] != "Bearer tok-2" {
		t.Fatalf("authorizations = %v, want the rejected token replaced by a fresh one", auths)
	}
}

// A second 401 is a real authorization failure, not a stale token.
func TestGigaChatProviderGivesUpAfterASecondRejection(t *testing.T) {
	issued := 0
	tokenStub := newGigaChatTokenServer(t, func(w http.ResponseWriter, r *http.Request) {
		issued++
		_, _ = w.Write([]byte(`{"access_token":"tok-` + strconv.Itoa(issued) + `","expires_at":` +
			strconv.FormatInt(time.Now().Add(30*time.Minute).UnixMilli(), 10) + `}`))
	})
	stub := newGigaChatChatStub(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status":401,"message":"Unauthorized"}`))
	})

	cfg := gigachatConfig{credentials: "key-1", scope: defaultGigaChatScope, authURL: tokenStub.server.URL}
	provider := newTestGigaChatProviderWithTokens(stub.server.URL, stub.server.Client(),
		newGigaChatTokenStore(cfg, tokenStub.server.Client()))

	if _, err := provider.Chat(context.Background(),
		[]providers.Message{{Role: "user", Content: "привет"}}, nil, "", nil); err == nil {
		t.Fatal("Chat succeeded against an endpoint that always answers 401")
	}
	if got := len(stub.authorizations()); got != 2 {
		t.Fatalf("requests = %d, want the retry attempted exactly once", got)
	}
}

// The third model generation stamps its own id on a function call; the second
// sends none. Either way the id has to key the functions_state_id.
func TestGigaChatProviderKeepsAStampedCallID(t *testing.T) {
	stub := newGigaChatChatStub(t, func(w http.ResponseWriter) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"",` +
			`"function_call":{"id":"6127d50d-7b63-4d47-b051-b796abbd6fae","name":"admin_levels",` +
			`"arguments":{"game_id":7}},"functions_state_id":"state-9"},"finish_reason":"function_call"}]}`))
	})
	provider := newTestGigaChatProvider(t, stub.server.URL, stub.server.Client())

	response, err := provider.Chat(context.Background(),
		[]providers.Message{{Role: "user", Content: "уровни"}}, testToolDefinitions(), "", nil)
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	call := response.ToolCalls[0]
	if call.ID != "6127d50d-7b63-4d47-b051-b796abbd6fae" {
		t.Fatalf("call id = %q, want the id GigaChat stamped", call.ID)
	}
	if got := provider.functionsStateFor(call.ID); got != "state-9" {
		t.Fatalf("functions state = %q, want it keyed by the stamped id", got)
	}
}

// "No such model" is unactionable on its own: the two GigaChat hosts serve
// different model generations, so the error names what this one has.
func TestGigaChatProviderHintsAtAvailableModelsOn404(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/models" {
			_, _ = w.Write([]byte(`{"object":"list","data":[` +
				`{"id":"GigaChat-3-Ultra","type":"chat"},{"id":"Embeddings","type":"embedder"}]}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":404,"message":"No such model"}`))
	}))
	t.Cleanup(server.Close)
	provider := newTestGigaChatProvider(t, server.URL, server.Client())

	_, err := provider.Chat(context.Background(),
		[]providers.Message{{Role: "user", Content: "привет"}}, nil, "GigaChat-9", nil)
	if err == nil {
		t.Fatal("Chat succeeded against a 404")
	}
	if !strings.Contains(err.Error(), "No such model") {
		t.Fatalf("error = %v, want the server's own message", err)
	}
	if !strings.Contains(err.Error(), "GigaChat-3-Ultra") {
		t.Fatalf("error = %v, want the models this host serves", err)
	}
	if strings.Contains(err.Error(), "Embeddings") {
		t.Fatalf("error = %v, want only chat models listed", err)
	}
	if !strings.Contains(err.Error(), legacyGigaChatBaseURL) {
		t.Fatalf("error = %v, want the legacy host suggested for older names", err)
	}
}

// A listing that cannot be fetched must not replace the error it decorates.
func TestGigaChatProvider404SurvivesAnUnavailableListing(t *testing.T) {
	stub := newGigaChatChatStub(t, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"status":404,"message":"No such model"}`))
	})
	provider := newTestGigaChatProvider(t, stub.server.URL, stub.server.Client())

	_, err := provider.Chat(context.Background(),
		[]providers.Message{{Role: "user", Content: "привет"}}, nil, "GigaChat-9", nil)
	if err == nil || !strings.Contains(err.Error(), "No such model") {
		t.Fatalf("error = %v, want the 404 reported even without a model listing", err)
	}
}
