package agent

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scopeHeaderServer observes the ${session_id} sentinel substitution of a
// real llm.Client: WithTaskScope's session id lands in the ExtraHeaders
// header (same channel loop_session_scope_test.go uses), so a scope option
// reaching the wire is directly observable.
func scopeHeaderServer(t *testing.T) (*httptest.Server, *llm.Client, *scopeHeaderLog) {
	t.Helper()
	const headerName = "X-Meept-Scope-Session"
	log := &scopeHeaderLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r.Header.Get(headerName))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	client := llm.NewClient(&llm.ModelConfig{
		BaseURL:      srv.URL,
		ModelID:      "scope-model",
		APIKey:       "test-" + "value",
		ProviderID:   llm.ProviderIDOpenAI,
		ExtraHeaders: map[string]string{headerName: "${session_id}"},
	})
	return srv, client, log
}

type scopeHeaderLog struct {
	mu      sync.Mutex
	headers []string
}

func (l *scopeHeaderLog) record(h string) {
	l.mu.Lock()
	l.headers = append(l.headers, h)
	l.mu.Unlock()
}

func (l *scopeHeaderLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.headers...)
}

// scopedLoop builds a loop whose scopeOptions() yield WithTaskScope("",
// sess) — the capture a caller threads via WithScopeOptions.
func scopedLoop(t *testing.T, sess string) *AgentLoop {
	t.Helper()
	l := NewAgentLoop("scope-source", t.TempDir())
	require.NotNil(t, l)
	l.mu.Lock()
	l.turnAccountingSessionID = sess
	l.mu.Unlock()
	return l
}

// TestReflectionEngine_PerCallScopeReachesChatter pins item 9(a): the
// ReflectionEngine's fix-request LLM call carries the caller-threaded
// scope. The engine is a daemon singleton with no owning loop, so
// SetScopeProvider was dead wiring; resolveCallScope reads the per-call
// scope the caller attached via WithScopeOptions.
func TestReflectionEngine_PerCallScopeReachesChatter(t *testing.T) {
	_, client, log := scopeHeaderServer(t)
	re := NewReflectionEngine(slog.Default(), nil, nil, client)
	require.NotNil(t, re)

	loop := scopedLoop(t, "sess-reflection-scope")
	ctx := WithScopeOptions(context.Background(), loop.scopeOptions())

	_, err := re.requestFix(ctx, "fix the build", nil)
	require.NoError(t, err)

	headers := log.all()
	require.NotEmpty(t, headers, "fix request must hit the LLM endpoint")
	assert.Equal(t, "sess-reflection-scope", headers[0],
		"reflection engine fix call must carry the caller-threaded scope session id")
}

// TestReflectionEngine_NoScopeNoHeader pins the negative side: without a
// threaded scope (and with no wired provider) the call stays unscoped — no
// placeholder IDs invented, header omitted entirely.
func TestReflectionEngine_NoScopeNoHeader(t *testing.T) {
	_, client, log := scopeHeaderServer(t)
	re := NewReflectionEngine(slog.Default(), nil, nil, client)

	_, err := re.requestFix(context.Background(), "fix the build", nil)
	require.NoError(t, err)

	headers := log.all()
	require.NotEmpty(t, headers)
	assert.Equal(t, "", headers[0], "unscoped call must send no affinity header value")
}

// TestRLMAnalyzer_PerCallScopeReachesChatter pins item 9(b): the RLM
// analyzer's LLM call honors the caller-threaded scope via
// resolveCallScope, and SetScopeProvider (new setter, established shape)
// works as the fallback for single-owner use.
func TestRLMAnalyzer_PerCallScopeReachesChatter(t *testing.T) {
	_, client, log := scopeHeaderServer(t)
	a := NewRLMAnalyzer(RLMAnalyzerConfig{}, &emptyTraceStoreReader{}, client, nil)
	require.NotNil(t, a)

	loop := scopedLoop(t, "sess-rlm-scope")
	ctx := WithScopeOptions(context.Background(), loop.scopeOptions())

	_, err := a.invokeLLM(ctx, &analyzerRun{}, "analyze these traces")
	require.NoError(t, err)

	headers := log.all()
	require.NotEmpty(t, headers)
	assert.Equal(t, "sess-rlm-scope", headers[0],
		"RLM analyzer call must carry the caller-threaded scope session id")
}

// TestRLMAnalyzer_SetScopeProviderFallback pins the wired-provider
// fallback: with no per-call scope on the context, SetScopeProvider's
// provider supplies the scope (single-owner use).
func TestRLMAnalyzer_SetScopeProviderFallback(t *testing.T) {
	_, client, log := scopeHeaderServer(t)
	a := NewRLMAnalyzer(RLMAnalyzerConfig{}, &emptyTraceStoreReader{}, client, nil)
	loop := scopedLoop(t, "sess-rlm-provider")
	a.SetScopeProvider(loop)
	a.SetScopeProvider(nil) // nil guard: must not panic, must not clear
	a.SetScopeProvider(loop)

	_, err := a.invokeLLM(context.Background(), &analyzerRun{}, "analyze")
	require.NoError(t, err)

	headers := log.all()
	require.NotEmpty(t, headers)
	assert.Equal(t, "sess-rlm-provider", headers[0])
}

// emptyTraceStoreReader satisfies TraceStoreReader for invokeLLM, which
// does not read the store.
type emptyTraceStoreReader struct{}

func (emptyTraceStoreReader) ListTraceIDs() ([]string, error)            { return nil, nil }
func (emptyTraceStoreReader) GetSpansForTrace(string) ([]string, error)  { return nil, nil }
func (emptyTraceStoreReader) ListSpans([]string) ([]spanViewData, error) { return nil, nil }
