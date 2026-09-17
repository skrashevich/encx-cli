package main

import (
	"strings"
	"testing"

	"github.com/hybridgroup/yzma/pkg/message"
	"github.com/hybridgroup/yzma/pkg/template"
	"github.com/sipeed/picoclaw/pkg/providers"
)

// The whole reason this file does not use yzma's message.ParseToolCalls: that
// parser decodes arguments into map[string]string, so a game id arrives quoted
// and the tool that takes an integer rejects it.
func TestParseLocalToolCallsKeepsArgumentTypes(t *testing.T) {
	calls, spoken := parseLocalToolCalls(
		"Сейчас посмотрю.\n<tool_call>\n{\"name\": \"admin_game_info\", \"arguments\": {\"game_id\": 82448, \"verbose\": true, \"domain\": \"svk.en.cx\"}}\n</tool_call>")

	if len(calls) != 1 {
		t.Fatalf("calls = %+v, want one", calls)
	}
	if spoken != "Сейчас посмотрю." {
		t.Errorf("spoken text = %q, want the sentence before the call", spoken)
	}
	call := calls[0]
	if call.Name != "admin_game_info" || call.Type != "function" || call.ID == "" {
		t.Fatalf("call = %+v", call)
	}
	if got, ok := call.Arguments["game_id"].(float64); !ok || got != 82448 {
		t.Errorf("game_id = %#v, want the number 82448", call.Arguments["game_id"])
	}
	if got, ok := call.Arguments["verbose"].(bool); !ok || !got {
		t.Errorf("verbose = %#v, want the boolean true", call.Arguments["verbose"])
	}
	if call.Function == nil || !strings.Contains(call.Function.Arguments, `"game_id":82448`) {
		t.Errorf("serialized arguments = %+v, want an unquoted number", call.Function)
	}
}

func TestParseLocalToolCallsHandlesSeveralCalls(t *testing.T) {
	calls, spoken := parseLocalToolCalls(
		`<tool_call>{"name":"a","arguments":{}}</tool_call> и ещё <tool_call>{"name":"b","arguments":{"n":2}}</tool_call>`)

	if len(calls) != 2 || calls[0].Name != "a" || calls[1].Name != "b" {
		t.Fatalf("calls = %+v, want a and b in order", calls)
	}
	if calls[0].ID == calls[1].ID {
		t.Errorf("both calls got the id %q; the tool loop matches results by id", calls[0].ID)
	}
	if !strings.Contains(spoken, "и ещё") {
		t.Errorf("spoken text = %q, want the words between the calls", spoken)
	}
}

// A reply that hits the token limit mid-call still carries a complete JSON
// object more often than not. Throwing the turn away would cost an iteration.
func TestParseLocalToolCallsAcceptsAnUnterminatedBlock(t *testing.T) {
	calls, _ := parseLocalToolCalls(`<tool_call>{"name":"admin_levels","arguments":{"game_id":1}}`)
	if len(calls) != 1 || calls[0].Name != "admin_levels" {
		t.Fatalf("calls = %+v, want the call parsed anyway", calls)
	}
}

// Some models answer a tool-calling prompt with the bare object, sometimes
// inside a fenced block.
func TestParseLocalToolCallsReadsABareObject(t *testing.T) {
	for _, body := range []string{
		`{"name":"admin_levels","arguments":{"game_id":7}}`,
		"```json\n{\"name\":\"admin_levels\",\"args\":{\"game_id\":7}}\n```",
	} {
		calls, spoken := parseLocalToolCalls(body)
		if len(calls) != 1 || calls[0].Name != "admin_levels" {
			t.Fatalf("calls for %q = %+v", body, calls)
		}
		if got, ok := calls[0].Arguments["game_id"].(float64); !ok || got != 7 {
			t.Errorf("game_id for %q = %#v", body, calls[0].Arguments["game_id"])
		}
		if spoken != "" {
			t.Errorf("spoken text for %q = %q, want none", body, spoken)
		}
	}
}

// LLM_LOCAL_MODEL takes any GGUF, and two of the families an operator would
// reach for do not use the <tool_call> envelope at all. Without these, pointing
// encli at one of them gives an agent that narrates tool calls and never makes
// one.
func TestParseLocalToolCallsReadsOtherModelDialects(t *testing.T) {
	cases := map[string]struct {
		response string
		names    []string
	}{
		"llama 3 python tag with parameters": {
			`<|python_tag|>{"name": "admin_levels", "parameters": {"game_id": 82448}}`,
			[]string{"admin_levels"},
		},
		"mistral tool calls array": {
			`[TOOL_CALLS] [{"name": "admin_levels", "arguments": {"game_id": 82448}}]`,
			[]string{"admin_levels"},
		},
		"bare array of calls": {
			`[{"name":"a","arguments":{"game_id":82448}},{"name":"b","args":{"game_id":82448}}]`,
			[]string{"a", "b"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			calls, spoken := parseLocalToolCalls(tc.response)
			if len(calls) != len(tc.names) {
				t.Fatalf("calls = %+v, want %d", calls, len(tc.names))
			}
			for i, want := range tc.names {
				if calls[i].Name != want {
					t.Errorf("call %d = %q, want %q", i, calls[i].Name, want)
				}
				// The reason this parser exists at all: the argument keeps its
				// JSON type whichever dialect delivered it.
				if got, ok := calls[i].Arguments["game_id"].(float64); !ok || got != 82448 {
					t.Errorf("call %d game_id = %#v, want the number", i, calls[i].Arguments["game_id"])
				}
			}
			if spoken != "" {
				t.Errorf("spoken text = %q, want none", spoken)
			}
		})
	}
}

// Half a batch of calls is not what the model asked for, so an array with one
// unusable entry is refused whole and reported as text.
func TestParseLocalToolCallsRefusesAPartlyBrokenArray(t *testing.T) {
	const body = `[{"name":"a","arguments":{}},{"arguments":{}}]`
	calls, spoken := parseLocalToolCalls(body)
	if len(calls) != 0 {
		t.Fatalf("calls = %+v, want none", calls)
	}
	if spoken != body {
		t.Errorf("spoken text = %q, want the response unchanged", spoken)
	}
}

func TestParseLocalToolCallsLeavesProseAlone(t *testing.T) {
	const answer = "Игра 82448 называется «Тест» и стартует завтра."
	calls, spoken := parseLocalToolCalls(answer)
	if len(calls) != 0 {
		t.Fatalf("calls = %+v, want none", calls)
	}
	if spoken != answer {
		t.Errorf("spoken text = %q, want the answer unchanged", spoken)
	}
}

// Anything that is not a call has to be reported as text rather than guessed
// into a malformed call the tool loop would then have to fail on.
func TestParseLocalToolCallsRejectsUnusableBodies(t *testing.T) {
	for _, body := range []string{
		`<tool_call>not json at all</tool_call>`,
		`<tool_call>{"arguments":{"game_id":1}}</tool_call>`,
		`<tool_call>{"name":"admin_levels","arguments":"не объект"}</tool_call>`,
	} {
		if calls, _ := parseLocalToolCalls(body); len(calls) != 0 {
			t.Errorf("parseLocalToolCalls(%q) = %+v, want no calls", body, calls)
		}
	}
}

// The conversation is replayed to the model through the chat template on every
// turn, so a call's arguments must survive the round trip as the values the
// model wrote — a quoted number teaches it to quote the next one.
func TestLocalMessagesPreserveToolCallArguments(t *testing.T) {
	converted := localMessages([]providers.Message{
		{Role: "system", Content: "правила"},
		{Role: "user", Content: "покажи игру"},
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{{
				ID:        "call-1",
				Name:      "admin_game_info",
				Arguments: map[string]any{"game_id": 82448},
			}},
		},
		{Role: "tool", ToolCallID: "call-1", Content: `{"name":"Тест"}`},
	})

	if len(converted) != 4 {
		t.Fatalf("converted %d messages, want 4", len(converted))
	}
	if converted[0].GetRole() != "system" || converted[1].GetRole() != "user" {
		t.Fatalf("roles = %q, %q", converted[0].GetRole(), converted[1].GetRole())
	}

	assistant := converted[2].GetContent()
	calls, ok := assistant["tool_calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("assistant content = %+v", assistant)
	}
	function := calls[0].(map[string]any)["function"].(map[string]any)
	arguments := function["arguments"].(map[string]any)
	if got, ok := arguments["game_id"].(int); !ok || got != 82448 {
		t.Errorf("game_id = %#v, want the number it was called with", arguments["game_id"])
	}

	// The template writes the name of the tool a result answers, and the loop
	// keys results by call id alone, so the name has to be carried across.
	if name := converted[3].GetContent()["name"]; name != "admin_game_info" {
		t.Errorf("tool result name = %v, want the name of the call it answers", name)
	}
}

// A call the loop only serialized as JSON — which is the shape it takes once a
// turn has been persisted — must convert just as well.
func TestLocalCallArgumentsReadsTheSerializedForm(t *testing.T) {
	got := localCallArguments(providers.ToolCall{
		Function: &providers.FunctionCall{Name: "admin_levels", Arguments: `{"game_id":9}`},
	})
	if value, ok := got["game_id"].(float64); !ok || value != 9 {
		t.Fatalf("arguments = %#v", got)
	}
}

// The prompt is what the model actually sees. Rendering it through Qwen2.5's own
// template proves the tool catalog reaches the model and the history replays as
// the model wrote it.
func TestLocalPromptOffersTheToolsToTheModel(t *testing.T) {
	chatTemplate, ok := template.BuiltinTemplate("qwen2.5-instruct")
	if !ok {
		t.Skip("yzma no longer ships the qwen2.5-instruct template")
	}

	tools := localToolDefinitions([]providers.ToolDefinition{{
		Type: "function",
		Function: providers.ToolFunctionDefinition{
			Name:        "admin_game_info",
			Description: "Show game settings",
			Parameters: map[string]any{
				"type":       "object",
				"properties": map[string]any{"game_id": map[string]any{"type": "integer"}},
				"required":   []string{"game_id"},
			},
		},
	}})
	if len(tools) != 1 || tools[0].Function.Name != "admin_game_info" {
		t.Fatalf("tools = %+v", tools)
	}

	messages := localMessages([]providers.Message{
		{Role: "system", Content: "ты агент"},
		{Role: "user", Content: "покажи игру 82448"},
		{
			Role:      "assistant",
			ToolCalls: []providers.ToolCall{{ID: "c1", Name: "admin_game_info", Arguments: map[string]any{"game_id": 82448}}},
		},
		{Role: "tool", ToolCallID: "c1", Content: `{"name":"Тест"}`},
	})

	prompt, err := template.ApplyWithTools(chatTemplate, messages, tools, true)
	if err != nil {
		t.Fatalf("ApplyWithTools: %v", err)
	}
	for _, want := range []string{"admin_game_info", "Show game settings", "<tool_call>", "82448"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt does not contain %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, `"82448"`) {
		t.Errorf("the replayed call quotes its game id:\n%s", prompt)
	}
}

// The agent recovers from an oversized request by trimming the transcript, but
// only if it recognises the refusal. The local provider has to phrase it in the
// terms isContextOverflowError reads.
func TestLocalContextOverflowErrorIsRecognised(t *testing.T) {
	err := localContextOverflowError(8192, 9001)
	if !isContextOverflowError(err) {
		t.Fatalf("isContextOverflowError(%v) = false, want the agent to trim and retry", err)
	}
	if isRetryableLLMError(err) {
		t.Errorf("the same request must not be retried unchanged: %v", err)
	}
}

// A run on this machine is not billed, and the OpenRouter catalog has never
// heard of a GGUF file on someone's disk. Asking it would be a network call that
// can only produce a wrong cost line.
func TestResolveAgentPricingSkipsLocalInference(t *testing.T) {
	if pricing := resolveAgentPricing(t.Context(), AgentConfig{
		AuthMethod: authMethodLocal,
		Model:      localModelDisplayName(defaultLocalModelURL),
	}); pricing != nil {
		t.Fatalf("pricing = %+v, want no cost line for a local run", pricing)
	}
}

// --- configuration ---

func TestLocalConfigFromResolvesLikeEveryOtherLLMField(t *testing.T) {
	isolateLLMEnv(t)

	// Nothing configured: the built-in model, the managed library directory.
	cfg := localConfigFrom(llmSettings{})
	if cfg.modelRef != defaultLocalModelURL {
		t.Errorf("model = %q, want the built-in default", cfg.modelRef)
	}
	if cfg.libPath != "" {
		t.Errorf("lib path = %q, want the managed install", cfg.libPath)
	}
	if cfg.contextSize != defaultLocalContextSize {
		t.Errorf("context size = %d, want %d", cfg.contextSize, defaultLocalContextSize)
	}

	// The settings panel supplies values the environment has not.
	cfg = localConfigFrom(llmSettings{LocalModel: "/models/stored.gguf", LocalLibPath: "/opt/llama"})
	if cfg.modelRef != "/models/stored.gguf" || cfg.libPath != "/opt/llama" {
		t.Errorf("config = %+v, want the stored values", cfg)
	}

	// And the environment outranks them, as it does for every other field.
	t.Setenv("LLM_LOCAL_MODEL", "/models/env.gguf")
	t.Setenv("LLM_LOCAL_CONTEXT", "4096")
	cfg = localConfigFrom(llmSettings{LocalModel: "/models/stored.gguf"})
	if cfg.modelRef != "/models/env.gguf" || cfg.contextSize != 4096 {
		t.Errorf("config = %+v, want the environment to win", cfg)
	}
}

// YZMA_LIB is what the yzma command line tool exports. An operator who
// installed the libraries that way should not have to set a second variable.
func TestLocalConfigFromAcceptsYzmaLib(t *testing.T) {
	isolateLLMEnv(t)
	t.Setenv("YZMA_LIB", "/opt/yzma")

	if cfg := localConfigFrom(llmSettings{}); cfg.libPath != "/opt/yzma" {
		t.Errorf("lib path = %q, want YZMA_LIB honoured", cfg.libPath)
	}
}

// A context size that is not a positive number is ignored rather than obeyed:
// an NCtx of 0 means "whatever the model was trained at", which on a 32k model
// allocates a KV cache most laptops cannot hold.
func TestLocalConfigFromIgnoresAnUnusableContextSize(t *testing.T) {
	isolateLLMEnv(t)
	for _, raw := range []string{"0", "-1", "много"} {
		t.Setenv("LLM_LOCAL_CONTEXT", raw)
		if cfg := localConfigFrom(llmSettings{}); cfg.contextSize != defaultLocalContextSize {
			t.Errorf("LLM_LOCAL_CONTEXT=%q gave %d, want the default %d", raw, cfg.contextSize, defaultLocalContextSize)
		}
	}
}

func TestLocalModelDisplayName(t *testing.T) {
	cases := map[string]string{
		defaultLocalModelURL:               "Qwen_Qwen3-VL-2B-Instruct-Q4_K_M.gguf",
		"/home/me/models/custom.gguf":      "custom.gguf",
		"https://example.test/m.gguf?dl=1": "m.gguf",
		"":                                 "Qwen_Qwen3-VL-2B-Instruct-Q4_K_M.gguf",
	}
	for ref, want := range cases {
		if got := localModelDisplayName(ref); got != want {
			t.Errorf("localModelDisplayName(%q) = %q, want %q", ref, got, want)
		}
	}
}

// The tool catalog has to survive the conversion with its schema intact: the
// template renders the parameters verbatim, and a lost schema means a model
// guessing argument names.
func TestLocalToolDefinitionsCarryTheSchema(t *testing.T) {
	parameters := map[string]any{"type": "object"}
	got := localToolDefinitions([]providers.ToolDefinition{{
		Type:     "function",
		Function: providers.ToolFunctionDefinition{Name: "t", Description: "d", Parameters: parameters},
	}})
	if len(got) != 1 {
		t.Fatalf("definitions = %+v", got)
	}
	want := message.ToolDefinition{
		Type:     "function",
		Function: message.ToolFunctionDefinition{Name: "t", Description: "d", Parameters: parameters},
	}
	if got[0].Function.Name != want.Function.Name ||
		got[0].Function.Description != want.Function.Description ||
		len(got[0].Function.Parameters) != len(want.Function.Parameters) {
		t.Errorf("definition = %+v, want %+v", got[0], want)
	}
	if localToolDefinitions(nil) != nil {
		t.Error("an empty catalog must stay empty so the template takes its plain path")
	}
}
