package agenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	toolshared "github.com/sipeed/picoclaw/pkg/tools/shared"
)

// Tool is one engine capability exposed to an LLM agent.
type Tool struct {
	name        string
	description string
	parameters  map[string]any
	mutating    bool
	// noCache excludes a read tool from memoization. Set it for results that are
	// too large to keep around, such as an inlined image.
	noCache bool
	run     func(ctx context.Context, args arguments) (any, error)
	gate    *gate
	cache   *readCache
}

// toolOutput lets a tool return media next to its JSON payload. PicoClaw carries
// ToolResult.Media into the next LLM request, which is how an image reaches a
// multimodal model.
type toolOutput struct {
	value any
	media []string
}

var _ toolshared.Tool = (*Tool)(nil)

// Name implements toolshared.Tool.
func (t *Tool) Name() string { return t.name }

// Description implements toolshared.Tool.
func (t *Tool) Description() string { return t.description }

// Parameters implements toolshared.Tool. The returned schema is a copy, so a
// caller handing it to a provider cannot corrupt the catalog.
func (t *Tool) Parameters() map[string]any { return cloneSchema(t.parameters) }

// Mutating reports whether the tool changes engine state and therefore needs
// authorization.
func (t *Tool) Mutating() bool { return t.mutating }

// Execute implements toolshared.Tool. Mutating tools are authorized first; the
// engine result is returned as JSON.
func (t *Tool) Execute(ctx context.Context, args map[string]any) *toolshared.ToolResult {
	if t.mutating {
		if allowed, refusal := t.gate.authorize(ctx, t.name, args); !allowed {
			return toolshared.ErrorResult(refusal)
		}
	}

	key, cacheable := "", false
	if !t.mutating && !t.noCache {
		key, cacheable = cacheKey(t.name, args)
		if cacheable {
			if value, hit := t.cache.get(key); hit {
				return encodeToolValue(t.name, value)
			}
		}
	}

	value, err := t.run(ctx, arguments(args))
	if err != nil {
		return toolshared.ErrorResult(fmt.Sprintf("%s failed: %v", t.name, err)).WithError(err)
	}

	if t.mutating {
		// The engine state this tool just changed invalidates every read.
		t.cache.clear()
	} else if cacheable {
		t.cache.put(key, value)
	}
	return encodeToolValue(t.name, value)
}

func encodeToolValue(name string, value any) *toolshared.ToolResult {
	var media []string
	if output, ok := value.(toolOutput); ok {
		media = output.media
		value = output.value
	}

	payload, err := json.Marshal(value)
	if err != nil {
		return toolshared.ErrorResult(
			fmt.Sprintf("%s produced a result that could not be encoded: %v", name, err),
		).WithError(err)
	}

	result := toolshared.SilentResult(string(payload))
	result.Media = media
	return result
}

func cloneSchema(v map[string]any) map[string]any {
	if v == nil {
		return nil
	}
	out := make(map[string]any, len(v))
	for key, value := range v {
		switch typed := value.(type) {
		case map[string]any:
			out[key] = cloneSchema(typed)
		case []string:
			out[key] = append([]string(nil), typed...)
		default:
			out[key] = value
		}
	}
	return out
}

// arguments is the decoded tool-call payload. LLM providers are inconsistent
// about number and string typing, so every accessor tolerates both forms.
type arguments map[string]any

func (a arguments) optionalInt(key string) (int, bool) {
	raw, ok := a[key]
	if !ok || raw == nil {
		return 0, false
	}
	switch v := raw.(type) {
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return int(n), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func (a arguments) requireInt(key string) (int, error) {
	value, ok := a.optionalInt(key)
	if !ok {
		return 0, fmt.Errorf("argument %q is required and must be an integer", key)
	}
	return value, nil
}

func (a arguments) optionalString(key string) (string, bool) {
	raw, ok := a[key]
	if !ok || raw == nil {
		return "", false
	}
	s, ok := raw.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	return s, s != ""
}

func (a arguments) requireString(key string) (string, error) {
	value, ok := a.optionalString(key)
	if !ok {
		return "", fmt.Errorf("argument %q is required and must be a non-empty string", key)
	}
	return value, nil
}
