package builtin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

func TestCacheHashURL(t *testing.T) {
	// sha1("https://example.com") — pins the shared bench/meept fixture
	// naming; the bench capture side must produce the identical stem.
	// Verified independently: printf 'https://example.com' | shasum.
	const want = "327c3fda87ce286848a574982ddd0b7c7487f816" //nolint:gosec // hardcoded test vector
	if got := CacheHashURL("https://example.com"); got != want {
		t.Errorf("CacheHashURL(example.com) = %q, want %q", got, want)
	}
	if CacheHashURL("https://a.com") == CacheHashURL("https://b.com") {
		t.Error("distinct URLs must hash distinctly")
	}
}

func TestCacheEntryRoundTrip(t *testing.T) {
	dir := t.TempDir()
	url := "https://example.com/page"
	fetched := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

	path, err := SaveCacheEntry(dir, url, "hello world", fetched)
	if err != nil {
		t.Fatalf("SaveCacheEntry: %v", err)
	}
	if filepath.Base(path) != CacheHashURL(url)+".json" {
		t.Errorf("fixture filename = %q, want sha1-stem.json", filepath.Base(path))
	}

	entry, found, err := LookupCacheEntry(dir, url)
	if err != nil || !found {
		t.Fatalf("LookupCacheEntry found=%v err=%v, want hit", found, err)
	}
	if entry.URL != url {
		t.Errorf("entry.URL = %q, want %q", entry.URL, url)
	}
	if entry.Body != "hello world" {
		t.Errorf("entry.Body = %q, want %q", entry.Body, "hello world")
	}
	if !entry.FetchedAt.Equal(fetched) {
		t.Errorf("entry.FetchedAt = %v, want %v", entry.FetchedAt, fetched)
	}
}

func TestLookupCacheMiss(t *testing.T) {
	dir := t.TempDir()
	if _, found, err := LookupCacheEntry(dir, "https://never-captured.example"); found || err != nil {
		t.Errorf("empty dir: found=%v err=%v, want found=false err=nil", found, err)
	}
}

func TestLookupCacheURLMismatchIsMiss(t *testing.T) {
	// A fixture file whose stored URL differs (hash collision or
	// hand-edited) must NOT be served for a different URL.
	dir := t.TempDir()
	if _, err := SaveCacheEntry(dir, "https://real.example", "body", time.Now()); err != nil {
		t.Fatal(err)
	}
	_, found, err := LookupCacheEntry(dir, "https://other.example")
	if found || err != nil {
		t.Errorf("mismatched fixture: found=%v err=%v, want found=false err=nil", found, err)
	}
}

func TestLookupCacheCorruptIsError(t *testing.T) {
	dir := t.TempDir()
	url := "https://corrupt.example"
	if _, err := SaveCacheEntry(dir, url, "x", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(CacheEntryPath(dir, url), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LookupCacheEntry(dir, url); err == nil {
		t.Error("corrupt fixture: want error, got nil")
	}
}

func TestDeterministicCacheDirEnvOverride(t *testing.T) {
	t.Setenv("MEEPT_TOOL_CACHE_DIR", "/tmp/fake-cache")
	if got := DeterministicCacheDir(); got != "/tmp/fake-cache" {
		t.Errorf("DeterministicCacheDir() = %q, want env override", got)
	}
}

func TestDeterministicToolsEnvEnabled(t *testing.T) {
	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"1", true}, {"true", true}, {"TRUE", true}, {"yes", true}, {"on", true},
		{"", false}, {"0", false}, {"false", false}, {"no", false}, {"off", false},
	} {
		t.Setenv("MEEPT_DETERMINISTIC_TOOLS", tc.val)
		if got := DeterministicToolsEnvEnabled(); got != tc.want {
			t.Errorf("MEEPT_DETERMINISTIC_TOOLS=%q: enabled=%v, want %v", tc.val, got, tc.want)
		}
	}
}

// fakeTool records Execute calls so tests can prove the network path was
// (or was not) reached.
type fakeTool struct {
	name  string
	calls int
}

func (f *fakeTool) Name() string                          { return f.name }
func (f *fakeTool) Description() string                   { return "fake" }
func (f *fakeTool) Parameters() llm.FunctionParameters    { return llm.FunctionParameters{} }
func (f *fakeTool) IsReadOnly(map[string]any) bool        { return true }
func (f *fakeTool) IsConcurrencySafe(map[string]any) bool { return true }
func (f *fakeTool) Execute(_ context.Context, _ map[string]any) (any, error) {
	f.calls++
	return "LIVE", nil
}

func TestCachedFetchHitServesBodyWithoutNetwork(t *testing.T) {
	dir := t.TempDir()
	if _, err := SaveCacheEntry(dir, "https://captured.example", "cached body", time.Now()); err != nil {
		t.Fatal(err)
	}
	inner := &fakeTool{name: "web_fetch"}
	wrapped := NewCachedFetchTool(inner, true)
	wrapped.SetCacheDir(dir)

	res, err := wrapped.Execute(t.Context(), map[string]any{"url": "https://captured.example"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if inner.calls != 0 {
		t.Fatalf("inner tool executed %d times; cache hit must not touch the network", inner.calls)
	}
	tr, ok := res.(tools.ToolResult)
	if !ok || !tr.Success {
		t.Fatalf("result = %#v, want successful ToolResult", res)
	}
	fr, ok := tr.Result.(FetchResult)
	if !ok || fr.Content != "cached body" {
		t.Errorf("served content = %#v, want cached body", tr.Result)
	}
}

func TestCachedFetchMissFailsExplicitlyWithoutNetwork(t *testing.T) {
	dir := t.TempDir() // no fixtures
	inner := &fakeTool{name: "web_fetch"}
	wrapped := NewCachedFetchTool(inner, true)
	wrapped.SetCacheDir(dir)

	_, err := wrapped.Execute(t.Context(), map[string]any{"url": "https://uncaptured.example"})
	if err == nil {
		t.Fatal("cache miss: want error, got nil")
	}
	if inner.calls != 0 {
		t.Fatal("inner tool executed on cache miss; must fail WITHOUT network")
	}
	if !strings.HasPrefix(err.Error(), "cache-miss:") || !strings.Contains(err.Error(), "not captured") {
		t.Errorf("error = %q, want prefix \"cache-miss:\" + \"not captured\"", err.Error())
	}
	if !errors.Is(err, ErrCacheMiss) {
		t.Errorf("error must wrap ErrCacheMiss sentinel for errors.Is")
	}
}

func TestCachedFetchGateOffPassesThrough(t *testing.T) {
	dir := t.TempDir()
	inner := &fakeTool{name: "web_fetch"}
	wrapped := NewCachedFetchTool(inner, false) // config off
	wrapped.SetCacheDir(dir)
	t.Setenv("MEEPT_DETERMINISTIC_TOOLS", "") // env off

	res, err := wrapped.Execute(t.Context(), map[string]any{"url": "https://anything.example"})
	if err != nil {
		t.Fatalf("gate off: Execute: %v", err)
	}
	if inner.calls != 1 {
		t.Errorf("inner calls = %d, want 1 (pass-through)", inner.calls)
	}
	if res != "LIVE" {
		t.Errorf("res = %v, want inner LIVE result", res)
	}
}

func TestCachedFetchEnvOverrideTurnsGateOn(t *testing.T) {
	dir := t.TempDir()
	inner := &fakeTool{name: "web_fetch"}
	wrapped := NewCachedFetchTool(inner, false) // config off...
	wrapped.SetCacheDir(dir)
	t.Setenv("MEEPT_DETERMINISTIC_TOOLS", "1") // ...but env on

	_, err := wrapped.Execute(t.Context(), map[string]any{"url": "https://uncaptured.example"})
	if err == nil || !errors.Is(err, ErrCacheMiss) {
		t.Errorf("env override: err = %v, want cache-miss", err)
	}
	if inner.calls != 0 {
		t.Error("inner executed under env override; gate leaked")
	}
}

func TestCachedFetchMissingArgDelegates(t *testing.T) {
	dir := t.TempDir()
	inner := &fakeTool{name: "web_fetch"}
	wrapped := NewCachedFetchTool(inner, true)
	wrapped.SetCacheDir(dir)

	// No "url" arg: gate has no opinion, inner tool reports the problem.
	_, _ = wrapped.Execute(t.Context(), map[string]any{})
	if inner.calls != 1 {
		t.Errorf("inner calls = %d, want 1 (missing arg delegates to inner)", inner.calls)
	}
}

func TestCachedFetchStreamingPathAlsoGated(t *testing.T) {
	dir := t.TempDir()
	inner := &fakeTool{name: "web_fetch"}
	wrapped := NewCachedFetchTool(inner, true)
	wrapped.SetCacheDir(dir)

	// Streaming path must serve from cache (executor prefers
	// ExecuteStreaming when a bus is present).
	_, err := wrapped.ExecuteStreaming(t.Context(), map[string]any{"url": "https://uncaptured.example"}, func(tools.ProgressUpdate) {})
	if err == nil || !errors.Is(err, ErrCacheMiss) {
		t.Fatalf("streaming miss: err = %v, want cache-miss", err)
	}
	if inner.calls != 0 {
		t.Error("streaming path reached the inner tool on a miss")
	}

	if _, err := SaveCacheEntry(dir, "https://captured.example", "via streaming", time.Now()); err != nil {
		t.Fatal(err)
	}
	res, err := wrapped.ExecuteStreaming(t.Context(), map[string]any{"url": "https://captured.example"}, func(tools.ProgressUpdate) {})
	if err != nil || inner.calls != 0 {
		t.Fatalf("streaming hit: res=%v err=%v inner.calls=%d", res, err, inner.calls)
	}
}

func TestCachedFetchSearchQueryKey(t *testing.T) {
	dir := t.TempDir()
	inner := &fakeTool{name: "websearch"}
	wrapped := NewCachedFetchTool(inner, true)
	wrapped.SetCacheDir(dir)

	// Search caches under the query string.
	if _, err := SaveCacheEntry(dir, "deterministic bench query", "search body", time.Now()); err != nil {
		t.Fatal(err)
	}
	res, err := wrapped.Execute(t.Context(), map[string]any{"query": "deterministic bench query"})
	if err != nil || inner.calls != 0 {
		t.Fatalf("search hit: res=%v err=%v inner.calls=%d", res, err, inner.calls)
	}
}

func TestFetchArgKey(t *testing.T) {
	if got := fetchArgKey("web_fetch"); got != "url" {
		t.Errorf("fetchArgKey(web_fetch) = %q, want url", got)
	}
	if got := fetchArgKey("websearch"); got != "query" {
		t.Errorf("fetchArgKey(websearch) = %q, want query", got)
	}
}

func TestDeterministicFetchNames(t *testing.T) {
	for _, name := range []string{"web_fetch", "websearch", "web_search"} {
		if !DeterministicFetchNames(name) {
			t.Errorf("DeterministicFetchNames(%q) = false, want true", name)
		}
	}
	if DeterministicFetchNames("file_read") {
		t.Error("file_read must not be gated")
	}
}
