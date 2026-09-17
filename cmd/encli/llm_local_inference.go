package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hybridgroup/yzma/pkg/llama"
	"github.com/hybridgroup/yzma/pkg/message"
	"github.com/hybridgroup/yzma/pkg/template"
	"github.com/sipeed/picoclaw/pkg/providers"
)

// authMethodLocal runs the agent on this machine, with llama.cpp loaded through
// yzma. It is the transport an operator gets when nothing else is configured:
// encli is useful out of the box rather than refusing to start until an API key
// exists somewhere.
const authMethodLocal = "local"

const (
	// defaultLocalContextSize is the window the local context is created with.
	//
	// It is not a round number picked for comfort. The agent's system prompt plus
	// its tool catalog measures about 6700 tokens before the conversation starts,
	// so an 8k window leaves barely a turn of room and every second message would
	// be refused for overflow. The KV cache is allocated up front — roughly
	// 200 MB at this size for a 0.5B model — which is what a larger window costs.
	defaultLocalContextSize = 65536

	// defaultLocalMaxTokens caps one reply. A local model that starts repeating
	// itself will do so until something stops it, and a turn is an agent turn:
	// what matters is the tool call at the start of it, not paragraph forty.
	defaultLocalMaxTokens = 2048

	// defaultLocalTemperature is low on purpose. Every turn of this agent is
	// either a tool call in a strict JSON shape or a report about one, and a
	// small model gets both measurably more right when it is not being creative.
	defaultLocalTemperature = 0.2

	// localContextHeadroom is the number of tokens kept free for the reply when
	// judging whether a prompt fits.
	localContextHeadroom = 256
)

var (
	llmLocalModelEnvVars = []string{"LLM_LOCAL_MODEL"}
	// YZMA_LIB is accepted as well as encli's own name because it is what the
	// yzma command line tool exports, and an operator who installed the
	// libraries that way should not have to set a second variable.
	llmLocalLibEnvVars     = []string{"LLM_LOCAL_LIB", "YZMA_LIB"}
	llmLocalContextEnvVars = []string{"LLM_LOCAL_CONTEXT"}
)

// localLLMConfig is the resolved local-inference setup: which weights to run,
// where the llama.cpp libraries are, and how large a window to give them.
type localLLMConfig struct {
	modelRef    string // a file path, or a URL to download once
	libPath     string // "" means the managed install under localLibDir()
	contextSize int
}

// localConfigFrom resolves the local settings the same way every other LLM field
// is resolved: the environment first, then what the settings panel stored, then
// the built-in default.
func localConfigFrom(stored llmSettings) localLLMConfig {
	cfg := localLLMConfig{
		modelRef:    resolveLLMField("", llmLocalModelEnvVars, stored.LocalModel, defaultLocalModelURL).Value,
		libPath:     resolveLLMField("", llmLocalLibEnvVars, stored.LocalLibPath, "").Value,
		contextSize: defaultLocalContextSize,
	}
	if raw := resolveLLMField("", llmLocalContextEnvVars, "", "").Value; raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			cfg.contextSize = n
		} else {
			debugf("local inference: ignoring LLM_LOCAL_CONTEXT=%q, expected a positive integer", raw)
		}
	}
	return cfg
}

// localModelDisplayName is what the run report and the settings panel call the
// model. The reference itself is a URL or an absolute path, neither of which
// belongs in a cost report line.
func localModelDisplayName(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		ref = defaultLocalModelURL
	}
	name := ref
	if cut := strings.LastIndexAny(name, `/\`); cut >= 0 {
		name = name[cut+1:]
	}
	if question := strings.IndexByte(name, '?'); question >= 0 {
		name = name[:question]
	}
	if name == "" {
		return ref
	}
	return name
}

// --- the loaded model ---

// llama.cpp is loaded into the process once. purego's dlopen cannot be undone,
// and the function pointers yzma resolves are package globals, so a second Load
// from a different directory would rebind them under a model that is still
// running on the first.
var (
	llamaLoadOnce sync.Once
	llamaLoadPath string
	llamaLoadErr  error
)

func loadLlamaLibraries(libPath string) error {
	llamaLoadOnce.Do(func() {
		if llamaLoadErr = llama.Load(libPath); llamaLoadErr != nil {
			return
		}
		llamaLoadPath = libPath
		if !debugMode {
			// llama.cpp narrates model loading on stderr. In an agent run that
			// stderr is the operator's transcript, and in -web it is the log.
			llama.LogSet(llama.LogSilent())
		}
		llama.Init()
	})
	if llamaLoadErr != nil {
		return fmt.Errorf("load the llama.cpp libraries from %s: %w", libPath, llamaLoadErr)
	}
	if libPath != llamaLoadPath {
		return fmt.Errorf("llama.cpp is already loaded from %s; restart encli to use %s instead",
			llamaLoadPath, libPath)
	}
	return nil
}

// localRuntime is one loaded model with its context, sampler and chat template.
//
// It is kept for the life of the process because a provider lives for a single
// agent run — that is, one user message — while loading the weights and
// allocating the KV cache costs seconds. Reloading per message would spend more
// time opening the model than answering with it.
type localRuntime struct {
	// mu serialises generation. One llama context holds one KV cache; two chats
	// decoding into it at once would interleave their tokens.
	mu sync.Mutex

	assets      localAssets
	contextSize int
	displayName string

	model    llama.Model
	lctx     llama.Context
	vocab    llama.Vocab
	sampler  llama.Sampler
	template string
	nBatch   int

	// closed is set by close and read by generate, both under mu. See close.
	closed bool

	// abort is read by llama.cpp itself, between compute steps, through the
	// callback installed in sharedLocalRuntime. It is what lets close interrupt
	// a decode that is already running instead of waiting for mu.
	abort atomic.Bool
}

var (
	localRuntimeMu      sync.Mutex
	localRuntimeCurrent *localRuntime
)

// sharedLocalRuntime returns the loaded model, opening it on first use and
// reopening it if the configuration now names a different one.
func sharedLocalRuntime(assets localAssets, contextSize int, displayName string) (*localRuntime, error) {
	localRuntimeMu.Lock()
	defer localRuntimeMu.Unlock()

	if current := localRuntimeCurrent; current != nil {
		if current.assets == assets && current.contextSize == contextSize {
			return current, nil
		}
		current.close()
		localRuntimeCurrent = nil
	}

	if err := loadLlamaLibraries(assets.libPath); err != nil {
		return nil, err
	}

	model, err := llama.ModelLoadFromFile(assets.modelPath, llama.ModelDefaultParams())
	if err != nil {
		return nil, fmt.Errorf("load the model %s: %w", assets.modelPath, err)
	}

	params := llama.ContextDefaultParams()
	params.NCtx = uint32(contextSize)

	lctx, err := llama.InitFromModel(model, params)
	if err != nil {
		llama.ModelFree(model)
		return nil, fmt.Errorf("create a llama context for %s: %w", assets.modelPath, err)
	}

	chatTemplate := llama.ModelChatTemplate(model, "")
	if strings.TrimSpace(chatTemplate) == "" {
		llama.Free(lctx)
		llama.ModelFree(model)
		return nil, fmt.Errorf("the model %s carries no chat template; the agent needs one to offer its tools",
			assets.modelPath)
	}

	samplerParams := llama.DefaultSamplerParams()
	samplerParams.Temp = defaultLocalTemperature

	runtime := &localRuntime{
		assets:      assets,
		contextSize: contextSize,
		displayName: displayName,
		model:       model,
		lctx:        lctx,
		vocab:       llama.ModelGetVocab(model),
		sampler:     llama.NewSampler(model, llama.DefaultSamplers, samplerParams),
		template:    chatTemplate,
		nBatch:      int(llama.NBatch(lctx)),
	}
	if runtime.nBatch <= 0 {
		runtime.nBatch = 512
	}
	// One callback per loaded model. purego caps how many a process may create,
	// and a reload only happens when the configuration names a different model,
	// so the count follows the operator's edits rather than the workload.
	llama.SetAbortCallback(lctx, runtime.aborted)

	localRuntimeCurrent = runtime
	debugf("local inference: loaded %s (context %d, batch %d)", assets.modelPath, contextSize, runtime.nBatch)
	return runtime, nil
}

// aborted is what llama.cpp asks between compute steps.
func (r *localRuntime) aborted() bool { return r.abort.Load() }

// close releases the model. It takes the same lock generation does, so it cannot
// free a context that is being decoded, and marks the runtime closed so a
// provider still holding a pointer to it cannot decode into freed memory
// afterwards. That is not hypothetical: changing the model in the -web settings
// panel replaces the runtime while an older chat may still be mid-run.
func (r *localRuntime) close() {
	// Asked for before the lock, not after it. llama.cpp reads this between
	// compute steps, so a decode already running unwinds in milliseconds and
	// releases mu — where waiting for it politely would mean waiting out the
	// whole reply, up to a minute on a slow machine, with the process trying to
	// exit the entire time.
	r.abort.Store(true)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	llama.SamplerFree(r.sampler)
	llama.Free(r.lctx)
	llama.ModelFree(r.model)
}

// shutdownLocalInference releases the loaded model before the process exits.
//
// It is not housekeeping that the operating system would do anyway. ggml's
// Metal backend frees its device from a C++ static destructor, which runs during
// exit() and asserts that nothing still holds a resource set:
//
//	ggml-metal-device.m:1025: GGML_ASSERT([rsets->data count] == 0) failed
//
// A llama context that is still open holds those, so a run that answered
// perfectly well ended in an abort and a Go traceback after its final report.
// Every path out of the process therefore closes the runtime first.
func shutdownLocalInference() {
	localRuntimeMu.Lock()
	defer localRuntimeMu.Unlock()
	if localRuntimeCurrent == nil {
		return
	}
	localRuntimeCurrent.close()
	localRuntimeCurrent = nil
}

// localCompletion is one reply from the model.
type localCompletion struct {
	text             string
	promptTokens     int
	completionTokens int

	// finishReason is what stopped the reply, in the vocabulary the rest of the
	// agent already speaks: "stop" for an end-of-generation token, "length" for
	// the token budget. The difference is the difference between an answer and
	// half an answer, so it is reported rather than assumed.
	finishReason string
}

const (
	localFinishStop   = "stop"
	localFinishLength = "length"
)

// decodeBatch runs one llama_decode and turns its verdict into an error.
//
// The error llama.Decode returns only ever reports a nil context; what llama.cpp
// actually says is the int beside it — 0 for success, 1 for "no room in the KV
// cache", anything negative for a failure. Dropping that is how a failed decode
// turns into a fluent answer to a prompt the model never saw: sampling reads
// whatever logits were left at the previous position, and the run reports
// success. yzma's own examples check it.
func (r *localRuntime) decodeBatch(batch llama.Batch, what string) error {
	status, err := llama.Decode(r.lctx, batch)
	if err != nil {
		return fmt.Errorf("decode %s: %w", what, err)
	}
	switch {
	case status == 0:
		return nil
	case status == 2:
		// The abort callback said stop, which only close does. Reported as an
		// error so the turn ends rather than sampling from a half-built graph.
		return fmt.Errorf("the local model was unloaded while decoding %s", what)
	case status == 1:
		// No KV slot left. Worded for isContextOverflowError, because trimming
		// the transcript is the only thing that can help — and the agent knows
		// how to do that by itself.
		return fmt.Errorf("the local model context window of %d tokens has no room left to decode %s: "+
			"shorten the conversation", r.contextSize, what)
	default:
		return fmt.Errorf("llama.cpp refused to decode %s: status %d", what, status)
	}
}

// generate runs one completion and reports the text with the token counts.
//
// The KV cache is cleared first: the agent hands the whole conversation over on
// every turn, so anything left from the previous one is the same prefix a second
// time and would be answered as if the user had repeated themselves.
func (r *localRuntime) generate(ctx context.Context, prompt string, maxTokens int) (localCompletion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return localCompletion{}, fmt.Errorf("the local model was unloaded; start the run again")
	}

	tokens := llama.Tokenize(r.vocab, prompt, true, true)
	if len(tokens) == 0 {
		return localCompletion{}, fmt.Errorf("the prompt tokenized to nothing")
	}
	result := localCompletion{promptTokens: len(tokens), finishReason: localFinishStop}
	if len(tokens)+localContextHeadroom > r.contextSize {
		return result, localContextOverflowError(r.contextSize, len(tokens)+localContextHeadroom)
	}

	memory, err := llama.GetMemory(r.lctx)
	if err != nil {
		return result, fmt.Errorf("read the llama context memory: %w", err)
	}
	if err := llama.MemoryClear(memory, true); err != nil {
		return result, fmt.Errorf("clear the llama context memory: %w", err)
	}
	llama.SamplerReset(r.sampler)

	// The prompt is decoded in batches of NBatch. llama.cpp refuses a batch
	// larger than that, and a transcript of a few thousand tokens exceeds it
	// easily.
	for start := 0; start < len(tokens); start += r.nBatch {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		end := min(start+r.nBatch, len(tokens))
		if err := r.decodeBatch(llama.BatchGetOne(tokens[start:end]), "the prompt"); err != nil {
			return result, err
		}
	}

	var out strings.Builder
	piece := make([]byte, 256)
	for result.completionTokens < maxTokens {
		if err := ctx.Err(); err != nil {
			result.text = out.String()
			return result, err
		}
		token := llama.SamplerSample(r.sampler, r.lctx, -1)
		if llama.VocabIsEOG(r.vocab, token) {
			result.text = out.String()
			return result, nil
		}
		n := llama.TokenToPiece(r.vocab, token, piece, 0, true)
		if n > 0 {
			out.Write(piece[:n])
		}
		result.completionTokens++
		if len(tokens)+result.completionTokens >= r.contextSize {
			// The window filled up mid-reply. Stopping here keeps what was said;
			// decoding past the end would be refused by llama.cpp instead.
			break
		}
		if err := r.decodeBatch(llama.BatchGetOne([]llama.Token{token}), "a sampled token"); err != nil {
			result.text = out.String()
			return result, err
		}
	}

	// The loop ran out of budget or of window rather than reaching an
	// end-of-generation token, so the reply is cut off and says so.
	result.text = out.String()
	result.finishReason = localFinishLength
	return result, nil
}

// localContextOverflowError reports a prompt that does not fit.
//
// The wording is not free: isContextOverflowError reads provider errors for the
// one failure the agent can fix by itself, and a message it does not recognise
// turns a recoverable turn — re-send a trimmed transcript — into a failed run.
func localContextOverflowError(contextSize, needed int) error {
	return fmt.Errorf(
		"the local model context window is %d tokens and this request needs %d: shorten the conversation",
		contextSize, needed)
}

// --- the provider ---

// localProvider adapts the loaded model to the interface the agent loop speaks.
type localProvider struct {
	runtime   *localRuntime
	maxTokens int
}

// newLocalProvider prepares the libraries and the weights — downloading either
// on a machine that has neither — and opens the model.
//
// The preparation happens here rather than on the first token so that a first
// run reports "downloading the model" while it downloads, instead of appearing
// to hang for the minutes a 400 MB file takes.
func newLocalProvider(ctx context.Context, cfg localLLMConfig, onProgress func(string)) (providers.LLMProvider, error) {
	assets, err := localAssetsManager.prepare(ctx, cfg, onProgress)
	if err != nil {
		return nil, err
	}
	runtime, err := sharedLocalRuntime(assets, cfg.contextSize, localModelDisplayName(cfg.modelRef))
	if err != nil {
		return nil, err
	}
	return &localProvider{runtime: runtime, maxTokens: defaultLocalMaxTokens}, nil
}

// localPrefetchTrigger says how sure encli has to be that local inference is
// wanted before spending the download on it.
//
// The distinction exists because resolveAgentConfig answers "local" to two
// different questions. One is "the operator asked for local inference". The
// other is "nothing at all is configured" — which is the state of a machine that
// has just been installed, and the state the first-run wizard exists to resolve.
type localPrefetchTrigger int

const (
	// prefetchWhenResolved downloads whenever local inference is the transport
	// that would run. It fits -chat, which has no way to change the transport:
	// what resolves at startup is what the first message will use, so fetching it
	// early only moves the wait.
	prefetchWhenResolved localPrefetchTrigger = iota

	// prefetchWhenInvited downloads once the operator has been offered the
	// choice of transport — either because they made it, or because they have
	// been through the first-run wizard. It fits -web, which is opened to
	// configure a provider or to log in to en.cx as often as to talk to the
	// agent, and whose wizard deliberately leaves its LLM step open on a bare
	// machine so the size of this download is shown before it happens. Spending
	// it while that question is still on screen would answer it for them.
	//
	// The gate is on the offer, not on the answer: someone who skipped the
	// wizard, or went through it and left local inference as it was, gets the
	// download at startup like everyone else. See onboardingLLMStep.
	prefetchWhenInvited
)

// startLocalPrefetch is how the -web handlers reach the prefetch.
//
// It is a package var for one reason: those handlers run in every web test, and
// starting a 400 MB download is not a side effect a test suite may have. Tests
// replace it to observe the decision without making it. Nothing else reassigns
// it — see isolateLLMEnv.
var startLocalPrefetch = prefetchLocalInference

// localPrefetchInvited reports whether the operator has been shown the choice of
// transport yet.
func localPrefetchInvited(cfg *config) bool {
	if localTransportChosen(cfg) {
		return true
	}
	state, err := loadOnboardingState()
	return err == nil && state.Completed
}

// localTransportChosen reports whether local inference was asked for, rather
// than arrived at because nothing else was configured. It reads the same
// precedence resolveAgentConfig does, minus the fallback.
func localTransportChosen(cfg *config) bool {
	var requested string
	if cfg != nil {
		requested = cfg.llmAuth
	}
	stored, err := loadLLMSettings()
	if err != nil {
		// An unreadable settings file is reported by the run that needs it. Here
		// it means no choice can be read, which is not a choice.
		return false
	}
	return strings.ToLower(resolveLLMField(requested, llmAuthEnvVars, stored.AuthMethod, "").Value) == authMethodLocal
}

// The prefetch that is running, if one is: its cancel, the configuration it is
// fetching for, and a serial number that lets the goroutine tell whether the
// registration still belongs to it.
var (
	localPrefetchMu     sync.Mutex
	localPrefetchCancel context.CancelFunc
	localPrefetchFor    localLLMConfig
	localPrefetchSeq    uint64
)

// prefetchLocalInference starts acquiring the libraries and the weights in the
// background, when local inference is the transport a long-running UI would use.
//
// Nothing here is work the first agent request would not do anyway, and the
// acquisition is idempotent: a request that arrives mid-download waits on the
// same lock and is shown the same progress. What it buys is that the first
// message a user types is answered rather than spending several minutes on a
// transfer they did not know was pending.
//
// It is started only for -web and -chat; a one-shot command has nothing to
// prefetch ahead of. What the two of them require of it differs — see
// localPrefetchTrigger.
func prefetchLocalInference(ctx context.Context, cfg *config, trigger localPrefetchTrigger, onProgress func(string)) {
	agentCfg, err := resolveAgentConfig(cfg)
	if err != nil || agentCfg.AuthMethod != authMethodLocal {
		// A transport that cannot be resolved is reported by the run that needs
		// it, in the place the operator is looking. Not here.
		return
	}
	if trigger == prefetchWhenInvited && !localPrefetchInvited(cfg) {
		return
	}

	ctx, cancel := context.WithCancel(ctx)

	localPrefetchMu.Lock()
	if localPrefetchCancel != nil && localPrefetchFor == agentCfg.Local {
		// Already fetching exactly this. Restarting would throw away whatever
		// has come down so far — and the settings panel calls this on every
		// save, including the ones that changed something else entirely.
		localPrefetchMu.Unlock()
		cancel()
		return
	}
	if localPrefetchCancel != nil {
		// A prefetch for a different model or library path. What it is fetching
		// is no longer what will be used.
		localPrefetchCancel()
	}
	localPrefetchSeq++
	seq := localPrefetchSeq
	localPrefetchCancel, localPrefetchFor = cancel, agentCfg.Local
	localPrefetchMu.Unlock()

	go func() {
		defer func() {
			// Deregister, but only while this is still the prefetch on record:
			// a newer one may have replaced it, and clearing that would leave it
			// running with nothing able to stop it.
			localPrefetchMu.Lock()
			if localPrefetchSeq == seq {
				localPrefetchCancel, localPrefetchFor = nil, localLLMConfig{}
			}
			localPrefetchMu.Unlock()
			cancel()
		}()

		_, err := localAssetsManager.prepare(ctx, agentCfg.Local, onProgress)
		switch {
		case err == nil, errors.Is(err, context.Canceled):
			// A cancelled prefetch is stopLocalPrefetch having done its job, not
			// a failure worth a line on the operator's terminal.
		default:
			debugf("local inference: prefetch failed: %v", err)
			if onProgress != nil {
				onProgress(fmt.Sprintf("local inference is not ready: %v", err))
			}
		}
	}()
}

// stopLocalPrefetch abandons a prefetch in flight.
//
// It is called when the operator chooses a cloud transport, which is the moment
// the background download stops being preparation and becomes several hundred
// megabytes nobody asked for — on a connection that may well be metered. The
// partial file is removed by downloadFile on the way out, so nothing is left to
// clean up later.
func stopLocalPrefetch() {
	localPrefetchMu.Lock()
	defer localPrefetchMu.Unlock()
	if localPrefetchCancel != nil {
		localPrefetchCancel()
		localPrefetchCancel, localPrefetchFor = nil, localLLMConfig{}
	}
}

func (p *localProvider) GetDefaultModel() string { return p.runtime.displayName }

func (p *localProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	_ string,
	options map[string]any,
) (*providers.LLMResponse, error) {
	prompt, err := template.ApplyWithTools(p.runtime.template, localMessages(messages), localToolDefinitions(tools), true)
	if err != nil {
		return nil, fmt.Errorf("apply the model's chat template: %w", err)
	}

	maxTokens := p.maxTokens
	if requested := optionInt(options, "max_tokens"); requested != nil && *requested > 0 {
		maxTokens = *requested
	}

	debugf("local inference: prompt %d bytes, %d tools, max_tokens=%d", len(prompt), len(tools), maxTokens)
	completion, err := p.runtime.generate(ctx, prompt, maxTokens)
	if err != nil {
		return nil, err
	}
	debugf("local inference: completion %d tokens (%s): %s",
		completion.completionTokens, completion.finishReason, summarizeDebugText(completion.text, 0))

	calls, spoken := parseLocalToolCalls(completion.text)
	response := &providers.LLMResponse{
		Content:      spoken,
		ToolCalls:    calls,
		FinishReason: completion.finishReason,
		Usage: &providers.UsageInfo{
			PromptTokens:     completion.promptTokens,
			CompletionTokens: completion.completionTokens,
			TotalTokens:      completion.promptTokens + completion.completionTokens,
		},
	}
	// A reply that was cut off mid-sentence is still cut off even if a complete
	// tool call happened to come out of it first, so "length" is not overwritten:
	// the loop is entitled to know the model did not finish.
	if len(calls) > 0 && completion.finishReason == localFinishStop {
		response.FinishReason = "tool_calls"
	}
	return response, nil
}

// --- conversation shapes ---

// localChatMessage is a plain turn. yzma's message.Chat would do, but the two
// richer shapes below have to be defined here anyway (see localAssistantMessage),
// and one family of types keeps the conversion in one place.
type localChatMessage struct {
	role    string
	content string
}

func (m localChatMessage) GetRole() string { return m.role }
func (m localChatMessage) GetContent() map[string]any {
	return map[string]any{"content": m.content}
}

// localAssistantMessage is an assistant turn that called tools.
//
// It exists instead of yzma's message.Tool because that type holds arguments as
// map[string]string. Replaying a call as {"game_id": "82448"} when the model
// wrote {"game_id": 82448} teaches it, one turn at a time, to quote its numbers
// — and a quoted number is rejected by the tool schema.
type localAssistantMessage struct {
	content string
	calls   []localToolCallView
}

type localToolCallView struct {
	name      string
	arguments map[string]any
}

func (m localAssistantMessage) GetRole() string { return "assistant" }
func (m localAssistantMessage) GetContent() map[string]any {
	calls := make([]any, 0, len(m.calls))
	for _, call := range m.calls {
		arguments := call.arguments
		if arguments == nil {
			arguments = map[string]any{}
		}
		calls = append(calls, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":      call.name,
				"arguments": arguments,
			},
		})
	}
	content := map[string]any{"tool_calls": calls}
	if m.content != "" {
		content["content"] = m.content
	}
	return content
}

// localToolResultMessage is one tool's output going back to the model.
type localToolResultMessage struct {
	name    string
	content string
}

func (m localToolResultMessage) GetRole() string { return "tool" }
func (m localToolResultMessage) GetContent() map[string]any {
	content := map[string]any{"content": m.content}
	if m.name != "" {
		content["name"] = m.name
	}
	return content
}

// localMessages converts the agent's conversation into the shapes the chat
// template renders.
//
// A tool result is matched back to the name of the call it answers, because the
// template writes the name and the loop keys results by call id alone.
func localMessages(messages []providers.Message) []message.Message {
	names := map[string]string{}
	out := make([]message.Message, 0, len(messages))
	for _, msg := range messages {
		switch {
		case msg.Role == "tool":
			out = append(out, localToolResultMessage{name: names[msg.ToolCallID], content: msg.Content})
		case msg.Role == "assistant" && len(msg.ToolCalls) > 0:
			converted := localAssistantMessage{content: msg.Content}
			for _, call := range msg.ToolCalls {
				names[call.ID] = call.Name
				converted.calls = append(converted.calls, localToolCallView{
					name:      call.Name,
					arguments: localCallArguments(call),
				})
			}
			out = append(out, converted)
		default:
			out = append(out, localChatMessage{role: msg.Role, content: msg.Content})
		}
	}
	return out
}

// localCallArguments recovers a call's arguments as an object, from whichever of
// the two fields the loop filled in at this stage.
func localCallArguments(call providers.ToolCall) map[string]any {
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

func localToolDefinitions(tools []providers.ToolDefinition) []message.ToolDefinition {
	if len(tools) == 0 {
		return nil
	}
	out := make([]message.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		out = append(out, message.ToolDefinition{
			Type: "function",
			Function: message.ToolFunctionDefinition{
				Name:        tool.Function.Name,
				Description: tool.Function.Description,
				Parameters:  tool.Function.Parameters,
			},
		})
	}
	return out
}

// --- reading the completion back ---

// localCallSeq numbers the call ids. The tool loop matches a result to its call
// by id, so every call in a process needs its own.
var localCallSeq atomic.Int64

// localToolCallEnvelope is one call as a model writes it.
//
// Three spellings of the same field are accepted because three model families
// disagree about it: "arguments" is what the Qwen template asks for, "args" is
// what several fine-tunes emit, and "parameters" is Llama 3.x. LLM_LOCAL_MODEL
// takes any GGUF and the README shows swapping the model, so these are the
// models an operator actually reaches for, not a defensive list.
type localToolCallEnvelope struct {
	Name       string          `json:"name"`
	Arguments  json.RawMessage `json:"arguments"`
	Args       json.RawMessage `json:"args"`
	Parameters json.RawMessage `json:"parameters"`
}

// arguments reports the call's arguments under whichever name they arrived.
func (e localToolCallEnvelope) arguments() json.RawMessage {
	for _, raw := range []json.RawMessage{e.Arguments, e.Args, e.Parameters} {
		if len(raw) > 0 {
			return raw
		}
	}
	return nil
}

const (
	localToolCallOpen  = "<tool_call>"
	localToolCallClose = "</tool_call>"

	// localToolCallsPrefix is how Mistral announces calls: the marker, then a
	// JSON array of them.
	localToolCallsPrefix = "[TOOL_CALLS]"

	// localPythonTag prefixes a call from Llama 3.1/3.2, with nothing after it
	// but the object.
	localPythonTag = "<|python_tag|>"
)

// parseLocalToolCalls separates the tool calls a completion requested from the
// text it spoke.
//
// yzma ships message.ParseToolCalls, which recognises more model dialects than
// this does, and it is deliberately not used: it decodes arguments into
// map[string]string, so {"game_id": 82448} arrives as the string "82448" and the
// tool that takes an integer rejects it. Everything here keeps the JSON value
// the model actually wrote.
func parseLocalToolCalls(response string) ([]providers.ToolCall, string) {
	var calls []providers.ToolCall
	var spoken strings.Builder

	rest := response
	for {
		open := strings.Index(rest, localToolCallOpen)
		if open < 0 {
			spoken.WriteString(rest)
			break
		}
		spoken.WriteString(rest[:open])
		body := rest[open+len(localToolCallOpen):]

		// An unterminated block is a reply that hit the token limit mid-call. It
		// is still parsed: the JSON is usually complete and only the closing tag
		// is missing.
		if end := strings.Index(body, localToolCallClose); end >= 0 {
			rest = body[end+len(localToolCallClose):]
			body = body[:end]
		} else {
			rest = ""
		}
		if call, ok := localToolCallFrom(body); ok {
			calls = append(calls, call)
		}
	}

	text := strings.TrimSpace(spoken.String())
	if len(calls) > 0 {
		return calls, text
	}
	return parseUnwrappedToolCalls(text)
}

// parseUnwrappedToolCalls reads the dialects that do not use the <tool_call>
// envelope at all.
//
// Qwen2.5 — the model encli installs by default — always uses the envelope, so
// none of this is on the path a default install takes. It is here because
// LLM_LOCAL_MODEL takes any GGUF: a Llama 3.x model announces a call with
// <|python_tag|> and spells the arguments "parameters", and Mistral writes
// [TOOL_CALLS] followed by an array. Without this, pointing encli at either of
// them gives an agent that narrates tool calls as prose and never makes one.
func parseUnwrappedToolCalls(text string) ([]providers.ToolCall, string) {
	body := stripCodeFence(text)
	body = strings.TrimSpace(strings.TrimPrefix(body, localPythonTag))

	if rest, found := strings.CutPrefix(body, localToolCallsPrefix); found {
		if calls := localToolCallsFromArray(strings.TrimSpace(rest)); len(calls) > 0 {
			return calls, ""
		}
		return nil, text
	}
	if calls := localToolCallsFromArray(body); len(calls) > 0 {
		return calls, ""
	}
	if call, ok := localToolCallFrom(body); ok {
		return []providers.ToolCall{call}, ""
	}
	return nil, text
}

// localToolCallsFromArray reads a JSON array of calls. A partly unusable array
// is refused whole: half a batch of calls is not what the model asked for.
func localToolCallsFromArray(body string) []providers.ToolCall {
	if !strings.HasPrefix(body, "[") {
		return nil
	}
	var bodies []json.RawMessage
	if err := json.Unmarshal([]byte(body), &bodies); err != nil {
		debugf("local inference: unparseable tool call array %s: %v", summarizeDebugText(body, 0), err)
		return nil
	}
	calls := make([]providers.ToolCall, 0, len(bodies))
	for _, raw := range bodies {
		call, ok := localToolCallFrom(string(raw))
		if !ok {
			return nil
		}
		calls = append(calls, call)
	}
	return calls
}

// localToolCallFrom decodes one call body. A body that is not a call — ordinary
// prose, or JSON without a name — is reported as such rather than guessed at.
func localToolCallFrom(body string) (providers.ToolCall, bool) {
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, "{") {
		return providers.ToolCall{}, false
	}
	var envelope localToolCallEnvelope
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		debugf("local inference: unparseable tool call %s: %v", summarizeDebugText(body, 0), err)
		return providers.ToolCall{}, false
	}
	if strings.TrimSpace(envelope.Name) == "" {
		return providers.ToolCall{}, false
	}

	raw := envelope.arguments()
	arguments := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &arguments); err != nil {
			debugf("local inference: tool %s sent unparseable arguments %s: %v",
				envelope.Name, summarizeDebugText(string(raw), 0), err)
			return providers.ToolCall{}, false
		}
	}
	encoded, err := json.Marshal(arguments)
	if err != nil {
		return providers.ToolCall{}, false
	}

	return providers.ToolCall{
		ID:        fmt.Sprintf("local_call_%d", localCallSeq.Add(1)),
		Type:      "function",
		Name:      envelope.Name,
		Arguments: arguments,
		Function: &providers.FunctionCall{
			Name:      envelope.Name,
			Arguments: string(encoded),
		},
	}, true
}

// stripCodeFence unwraps a ```json block so the object inside it can be read.
func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	if newline := strings.IndexByte(s, '\n'); newline >= 0 {
		// Drop the language tag on the opening line.
		if !strings.Contains(strings.TrimSpace(s[:newline]), "{") {
			s = s[newline+1:]
		}
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}
