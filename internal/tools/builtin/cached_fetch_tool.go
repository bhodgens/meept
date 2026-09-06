package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// deterministicFetchNames are the tool names wrapped in deterministic
// (cached-fetch) mode: anything that reaches the live web.
var deterministicFetchNames = map[string]bool{
	"web_fetch": true,
	"websearch": true,
	// The registry historically exposed the search tool under both
	// spellings (config.DefaultAlwaysFullTools lists "websearch"); accept
	// the snake_case form too so a rename cannot silently escape the gate.
	"web_search": true,
}

// DeterministicFetchNames reports whether name is a live-web tool that
// deterministic mode wraps. Exported so the daemon wiring helper can
// enumerate the wrapped set without duplicating the list.
func DeterministicFetchNames(name string) bool {
	return deterministicFetchNames[name]
}

// fetchArgKey returns the args key holding the resource identifier for a
// wrapped tool: "url" for web_fetch, "query" for websearch. The query
// string doubles as the cache address for search (fixtures are captured
// per query).
func fetchArgKey(toolName string) string {
	if toolName == "web_fetch" {
		return "url"
	}
	return "query"
}

// CachedFetchTool wraps a web fetch/search tool and, when deterministic
// mode is on, serves results exclusively from the local fixture cache —
// a cache hit returns the captured body, a miss fails with an explicit
// "cache-miss: <url> not captured" error WITHOUT any network access.
// When the gate is off the wrapper is a pass-through to the inner tool,
// so non-deterministic behavior is identical to the unwrapped tool.
type CachedFetchTool struct {
	inner tools.Tool
	// cacheDir is the fixture directory; empty means DeterministicCacheDir().
	cacheDir string
	// enabled is the static config gate ([agent.tools].deterministic_tools).
	// The env override ORs with this at call time so a benchmark run can
	// flip the mode via the daemon environment.
	enabled bool
}

// NewCachedFetchTool wraps tool. enabled is the [agent.tools]
// deterministic_tools config value; the MEEPT_DETERMINISTIC_TOOLS env
// override is consulted per call on top of it.
func NewCachedFetchTool(tool tools.Tool, enabled bool) *CachedFetchTool {
	return &CachedFetchTool{inner: tool, enabled: enabled}
}

// active reports whether the deterministic gate is on for this call.
func (t *CachedFetchTool) active() bool {
	return t.enabled || DeterministicToolsEnvEnabled()
}

// SetCacheDir overrides the fixture directory (tests; production callers
// use MEEPT_TOOL_CACHE_DIR).
func (t *CachedFetchTool) SetCacheDir(dir string) { t.cacheDir = dir }

// Inner returns the wrapped tool, so wiring code can detect and unwrap
// double-wrapping (e.g. a config reload re-applying the gate).
func (t *CachedFetchTool) Inner() tools.Tool { return t.inner }

func (t *CachedFetchTool) cacheDirOr() string {
	if t.cacheDir != "" {
		return t.cacheDir
	}
	return DeterministicCacheDir()
}

func (t *CachedFetchTool) Name() string        { return t.inner.Name() }
func (t *CachedFetchTool) Description() string { return t.inner.Description() }
func (t *CachedFetchTool) Parameters() llm.FunctionParameters {
	return t.inner.Parameters()
}
func (t *CachedFetchTool) IsReadOnly(in map[string]any) bool {
	return t.inner.IsReadOnly(in)
}
func (t *CachedFetchTool) IsConcurrencySafe(in map[string]any) bool {
	return t.inner.IsConcurrencySafe(in)
}

// Category delegates to the inner tool's optional Categorizer (tools.
// GetCategory handles the interface check; the wrapper preserves "web").
func (t *CachedFetchTool) Category() string {
	if c, ok := t.inner.(tools.Categorizer); ok {
		return c.Category()
	}
	return "general"
}

// cacheKey extracts the cache address from args. A missing/empty key is
// not a cache question — return ok=false and let the inner tool produce
// its own "no URL specified" style error.
func (t *CachedFetchTool) cacheKey(args map[string]any) (string, bool) {
	key, _ := args[fetchArgKey(t.inner.Name())].(string)
	key = strings.TrimSpace(key)
	return key, key != ""
}

// lookup serves args from the fixture cache. handled=false means "no
// opinion" (gate off or missing argument) — the caller delegates to the
// inner tool.
func (t *CachedFetchTool) lookup(args map[string]any) (any, bool, error) {
	if !t.active() {
		return nil, false, nil
	}
	key, ok := t.cacheKey(args)
	if !ok {
		// Let the inner tool report the missing argument itself; the
		// deterministic gate adds no semantics for malformed calls.
		return nil, false, nil
	}
	entry, found, err := LookupCacheEntry(t.cacheDirOr(), key)
	if err != nil {
		return nil, true, err
	}
	if !found {
		return nil, true, fmt.Errorf("%w: %s not captured", ErrCacheMiss, key)
	}
	hit := tools.ToolResult{
		Success: true,
		Result: FetchResult{
			Content:     entry.Body,
			URL:         entry.URL,
			StatusCode:  200,
			ContentType: "text/plain; charset=utf-8",
		},
	}
	return hit, true, nil
}

// Execute implements tools.Tool.
func (t *CachedFetchTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	res, handled, fail := t.lookup(args)
	if !handled {
		return t.inner.Execute(ctx, args)
	}
	if fail != nil {
		return nil, fail
	}
	return res, nil
}

// ExecuteStreaming implements tools.StreamingTool by delegating to the
// inner tool's streaming path. CRITICAL: the agent executor prefers
// ExecuteStreaming on StreamingTool tools when a bus is present
// (internal/agent/executor.go executeToolWithRetry), so a wrapper without
// this method would silently bypass the cache gate on every live fetch.
func (t *CachedFetchTool) ExecuteStreaming(ctx context.Context, args map[string]any, onUpdate func(tools.ProgressUpdate)) (any, error) {
	res, handled, fail := t.lookup(args)
	if !handled {
		if st, ok := t.inner.(tools.StreamingTool); ok {
			return st.ExecuteStreaming(ctx, args, onUpdate)
		}
		return t.inner.Execute(ctx, args)
	}
	if fail != nil {
		return nil, fail
	}
	return res, nil
}

// Ensure CachedFetchTool implements the tool interfaces it must.
var (
	_ tools.Tool          = (*CachedFetchTool)(nil)
	_ tools.StreamingTool = (*CachedFetchTool)(nil)
)
