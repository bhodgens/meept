package llm_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// --- leaf-06 teardown test doubles (session-affinity, master Contract 6) ---

// teardownLogger keeps adapter Debug logs out of test output (the no-op
// cloud adapter logs per call by design).
func teardownLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(
		&discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError}))
}

// discardWriter swallows log output.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// recordingAdapter is a TeardownAdapter double that records calls.
type recordingAdapter struct {
	mu      sync.Mutex
	calls   []string // "sessionID|endpointKey"
	handles func(endpointKey string) bool
	err     error
	delay   time.Duration
}

func (a *recordingAdapter) Handles(endpointKey string) bool {
	if a.handles != nil {
		return a.handles(endpointKey)
	}
	return true
}

func (a *recordingAdapter) Teardown(ctx context.Context, sessionID, endpointKey string) error {
	if a.delay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(a.delay):
		}
	}
	a.mu.Lock()
	a.calls = append(a.calls, sessionID+"|"+endpointKey)
	a.mu.Unlock()
	return a.err
}

// SweepTeardown mirrors Teardown: the double records the same way for both
// paths (callers that care distinguish by which coordinator entry ran).
func (a *recordingAdapter) SweepTeardown(ctx context.Context, sessionID, endpointKey string) error {
	return a.Teardown(ctx, sessionID, endpointKey)
}

func (a *recordingAdapter) recorded() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.calls...)
}

// compile-time assertions: the production adapters satisfy the contract.
var _ llm.TeardownAdapter = (*llm.LlamaServerTeardown)(nil)
var _ llm.TeardownAdapter = (*llm.CloudTeardown)(nil)

// TestLlamaTeardownErases pins the erase wire shape: the adapter POSTs the
// verified llama-server slot-erase route to the endpoint's base URL, skips
// (nil, no call) when another session still maps to the endpoint, and
// bounds the erase by its timeout (deadline error, no panic).
func TestLlamaTeardownErases(t *testing.T) {
	type eraseRecord struct {
		Method   string `json:"method"`
		Path     string `json:"path"`
		RawQuery string `json:"query"`
	}
	var mu sync.Mutex
	var erases []eraseRecord
	// Hold the response until released so the timeout path is
	// deterministic.
	release := make(chan struct{})
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		erases = append(erases, eraseRecord{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery})
		mu.Unlock()
		<-release
	}))
	defer stub.Close()
	defer close(release) // unblock any in-flight handler on test exit

	mgr := llm.NewRuntimeManager(teardownLogger())
	endpointKey := "llama-cpp:" + hostPortOf(t, stub.URL)
	registerTeardownRuntime(t, mgr, endpointKey, stub.URL)

	var snapshotMu sync.Mutex
	snapshot := map[string]string{"sess-a": endpointKey}
	adapter := llm.NewLlamaServerTeardown(mgr, func() map[string]string {
		snapshotMu.Lock()
		defer snapshotMu.Unlock()
		cp := make(map[string]string, len(snapshot))
		for k, v := range snapshot {
			cp[k] = v
		}
		return cp
	}, 2*time.Second, teardownLogger())

	// SHARED-RUNTIME SKIP (leaf task 1, third case): sess-b still maps to
	// the endpoint, so erasing sess-a's view would pull the cache out from
	// under sess-b. The adapter must skip with NO HTTP call.
	snapshot["sess-b"] = endpointKey
	if err := adapter.Teardown(context.Background(), "sess-a", endpointKey); err != nil {
		t.Fatalf("shared-runtime teardown must skip with nil, got %v", err)
	}
	mu.Lock()
	if n := len(erases); n != 0 {
		mu.Unlock()
		t.Fatalf("shared runtime must NOT be erased (refcount > 0), got %d erases", n)
	}
	mu.Unlock()

	// LAST SESSION: with only sess-b remaining, sess-b's teardown erases.
	snapshotMu.Lock()
	delete(snapshot, "sess-a")
	snapshotMu.Unlock()

	// Timeout path runs against the held stub: bounded error, no panic.
	// Run it before the final erase so the handler's release channel is
	// still open for the closing stub.
	timeoutAdapter := llm.NewLlamaServerTeardown(mgr, func() map[string]string {
		return map[string]string{"sess-b": endpointKey}
	}, 50*time.Millisecond, teardownLogger())
	errCh := make(chan error, 1)
	go func() {
		errCh <- timeoutAdapter.Teardown(context.Background(), "sess-b", endpointKey)
	}()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("held erase response must produce a timeout error, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout-path teardown did not return within 5s (unbounded?)")
	}

	// Now the real erase: sess-b is the last session, endpoint live. The
	// stub above holds responses for the timeout probe, so use a fresh
	// instant-response stub for the positive path.
	var mu2 sync.Mutex
	var okErase eraseRecord
	okStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu2.Lock()
		okErase = eraseRecord{Method: r.Method, Path: r.URL.Path, RawQuery: r.URL.RawQuery}
		mu2.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer okStub.Close()
	okMgr := llm.NewRuntimeManager(teardownLogger())
	okKey := "llama-cpp:" + hostPortOf(t, okStub.URL)
	registerTeardownRuntime(t, okMgr, okKey, okStub.URL)
	okAdapter := llm.NewLlamaServerTeardown(okMgr, func() map[string]string {
		return map[string]string{"sess-b": okKey}
	}, 2*time.Second, teardownLogger())
	if err := okAdapter.Teardown(context.Background(), "sess-b", okKey); err != nil {
		t.Fatalf("last-session teardown: %v", err)
	}
	mu2.Lock()
	defer mu2.Unlock()
	last := okErase
	if last.Method == "" {
		t.Fatal("expected an erase request recorded")
	}
	if last.Method != http.MethodPost {
		t.Errorf("erase method = %s, want POST", last.Method)
	}
	if last.Path != "/slots/0" {
		t.Errorf("erase path = %q, want /slots/0 (VERIFIED route: POST /slots/0?action=erase, pinned llama.cpp tools/server/server.cpp:286 + server-context.cpp:4796)", last.Path)
	}
	if last.RawQuery != "action=erase" {
		t.Errorf("erase query = %q, want action=erase", last.RawQuery)
	}
}

// TestCloudTeardownNoOp pins the explicit cloud no-op: Handles matches
// non-llama keys, Teardown returns nil with no observable effect.
func TestCloudTeardownNoOp(t *testing.T) {
	cloud := llm.NewCloudTeardown(teardownLogger())
	if !cloud.Handles("openai:api.openai.com|openai:key:abc") {
		t.Error("cloud adapter must handle cloud endpoint keys")
	}
	if cloud.Handles("llama-cpp:127.0.0.1:8080") {
		t.Error("cloud adapter must NOT handle llama.cpp endpoints")
	}
	if err := cloud.Teardown(context.Background(), "sess-1", "openai:api.openai.com|openai:key:abc"); err != nil {
		t.Errorf("cloud teardown must be a nil no-op, got %v", err)
	}
}

// TestCoordinatorFansOut pins the fan-out contract: an archive event for a
// session mapped to endpoint A reaches ONLY the A-matching adapters; the
// cloud adapter is called with the cloud endpoint key and returns nil; an
// unknown session fans out to nobody.
func TestCoordinatorFansOut(t *testing.T) {
	llamaKey := "llama-cpp:127.0.0.1:8080"
	cloudKey := "openai:api.openai.example|openai:key:k"

	llamaAdapter := &recordingAdapter{handles: func(key string) bool {
		return key == llamaKey
	}}
	cloudAdapter := &recordingAdapter{handles: func(key string) bool {
		return key == cloudKey
	}}
	neverAdapter := &recordingAdapter{handles: func(string) bool { return false }}

	snapshot := map[string]string{
		"sess-llama": llamaKey,
		"sess-cloud": cloudKey,
	}
	coordinator := llm.NewTeardownCoordinator(
		[]llm.TeardownAdapter{llamaAdapter, cloudAdapter, neverAdapter},
		func() map[string]string { return snapshot },
		time.Second, teardownLogger())

	// Archive the llama session: only the llama-matching adapter fires,
	// and it carries the endpoint key the sticky map remembers.
	coordinator.OnSessionEvent(context.Background(), "sess-llama", "archive")
	waitForCalls(t, llamaAdapter, 1, "llama adapter")
	if calls := llamaAdapter.recorded(); len(calls) != 1 || calls[0] != "sess-llama|"+llamaKey {
		t.Fatalf("llama adapter calls = %v, want exactly [sess-llama|<llamaKey>]", calls)
	}
	if calls := cloudAdapter.recorded(); len(calls) != 0 {
		t.Fatalf("cloud adapter must not fire for a llama endpoint, got %v", calls)
	}
	if calls := neverAdapter.recorded(); len(calls) != 0 {
		t.Fatalf("non-matching adapter must never fire, got %v", calls)
	}

	// The cloud session fans out to the cloud adapter; its Teardown is a
	// nil no-op (asserted directly above in TestCloudTeardownNoOp; here we
	// assert the routing).
	coordinator.OnSessionEvent(context.Background(), "sess-cloud", "idle")
	waitForCalls(t, cloudAdapter, 1, "cloud adapter")
	if calls := cloudAdapter.recorded(); len(calls) != 1 || calls[0] != "sess-cloud|"+cloudKey {
		t.Fatalf("cloud adapter calls = %v, want exactly [sess-cloud|<cloudKey>]", calls)
	}
	if calls := llamaAdapter.recorded(); len(calls) != 1 {
		t.Fatalf("llama adapter must stay at 1 call, got %v", calls)
	}

	// Unknown session: no endpoint remembered -> nobody fires.
	coordinator.OnSessionEvent(context.Background(), "sess-unknown", "delete")
	time.Sleep(50 * time.Millisecond)
	if calls := llamaAdapter.recorded(); len(calls) != 1 {
		t.Errorf("unknown session must fan out to nobody (llama), got %v", calls)
	}
	if calls := cloudAdapter.recorded(); len(calls) != 1 {
		t.Errorf("unknown session must fan out to nobody (cloud), got %v", calls)
	}
}

// TestSharedRuntimeSkip pins the refcount rule end to end at the
// COORDINATOR level (leaf task 1): two sessions on one endpoint; archiving
// the first fires NO erase HTTP call (the real llama adapter's refcount
// skip runs inside the fan-out); archiving the second erases exactly once.
// (The coordinator routes by Handles; the skip decision itself lives in
// the llama adapter, so the observable is the stub's erase count.)
func TestSharedRuntimeSkip(t *testing.T) {
	var mu sync.Mutex
	var erases int
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		erases++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer stub.Close()

	mgr := llm.NewRuntimeManager(teardownLogger())
	endpointKey := "llama-cpp:" + hostPortOf(t, stub.URL)
	registerTeardownRuntime(t, mgr, endpointKey, stub.URL)

	var snapshotMu sync.Mutex
	snapshot := map[string]string{
		"sess-1": endpointKey,
		"sess-2": endpointKey,
	}
	snapshotFn := func() map[string]string {
		snapshotMu.Lock()
		defer snapshotMu.Unlock()
		cp := make(map[string]string, len(snapshot))
		for k, v := range snapshot {
			cp[k] = v
		}
		return cp
	}
	realLlama := llm.NewLlamaServerTeardown(mgr, snapshotFn, 2*time.Second, teardownLogger())
	coordinator := llm.NewTeardownCoordinator(
		[]llm.TeardownAdapter{realLlama},
		snapshotFn,
		2*time.Second, teardownLogger())

	// First archive: sess-2 still maps to the endpoint — NO erase.
	coordinator.OnSessionEvent(context.Background(), "sess-1", "archive")
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	if erases != 0 {
		n := erases
		mu.Unlock()
		t.Fatalf("refcount > 0 must erase NOTHING, got %d erases", n)
	}
	mu.Unlock()

	// Resolver semantics after an archive (Contract 4): the remembered
	// preference is dropped with the session, so sess-1 leaves the
	// snapshot before sess-2's event fires.
	snapshotMu.Lock()
	delete(snapshot, "sess-1")
	snapshotMu.Unlock()

	// Second archive: sess-2 was the last session — exactly one erase.
	coordinator.OnSessionEvent(context.Background(), "sess-2", "archive")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := erases
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if erases != 1 {
		t.Fatalf("last-session archive must erase EXACTLY once, got %d", erases)
	}
}

// TestCoordinatorSweepSynchronous pins the shutdown sweep: every session
// with a remembered endpoint is torn down SYNCHRONOUSLY (the call returns
// only after the adapters saw every session) under the
// TeardownTimeout * min(8, len(sessions)) budget.
func TestCoordinatorSweepSynchronous(t *testing.T) {
	keys := []string{
		"llama-cpp:127.0.0.1:8080",
		"openai:api.openai.example|openai:key:k",
	}
	snapshot := map[string]string{
		"sess-1": keys[0],
		"sess-2": keys[1],
		"sess-3": keys[0],
	}
	adapter := &recordingAdapter{}
	coordinator := llm.NewTeardownCoordinator(
		[]llm.TeardownAdapter{adapter},
		func() map[string]string { return snapshot },
		time.Second, teardownLogger())

	done := make(chan struct{})
	go func() {
		coordinator.ShutdownSweep(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ShutdownSweep must run synchronously and return, still running after 5s")
	}
	calls := adapter.recorded()
	if len(calls) != 3 {
		t.Fatalf("sweep must tear down all 3 remembered sessions, got %d: %v", len(calls), calls)
	}
	seen := map[string]bool{}
	for _, c := range calls {
		seen[c] = true
	}
	for _, want := range []string{"sess-1|" + keys[0], "sess-2|" + keys[1], "sess-3|" + keys[0]} {
		if !seen[want] {
			t.Errorf("sweep missing teardown for %q; got %v", want, calls)
		}
	}
}

// TestLlamaTeardownUnknownEndpointSkips pins the defensive skip: a llama
// key with NO live runtime registration erases nothing and returns nil
// (never guess a base URL).
func TestLlamaTeardownUnknownEndpointSkips(t *testing.T) {
	mgr := llm.NewRuntimeManager(teardownLogger())
	adapter := llm.NewLlamaServerTeardown(mgr, nil, time.Second, teardownLogger())
	if err := adapter.Teardown(context.Background(), "sess-x", "llama-cpp:127.0.0.1:9999"); err != nil {
		t.Fatalf("unknown endpoint must skip with nil, got %v", err)
	}
}

// --- helpers ---

// registerTeardownRuntime registers a llama-cpp runtime config under
// endpointKey pointing at baseURL, so TeardownBaseURL resolves it. No
// process is spawned (AutoStart false; health checker is constructed but
// never started).
func registerTeardownRuntime(t *testing.T, mgr *llm.RuntimeManager, endpointKey, baseURL string) {
	t.Helper()
	cfg := &llm.RuntimeConfig{
		Type:           llm.RuntimeLlamaCpp,
		EndpointKey:    endpointKey,
		AutoStart:      false,
		AutoStop:       true,
		SpawnCommand:   []string{"sleep", "1"},
		SpawnTimeout:   2 * time.Second,
		HealthEndpoint: "/health",
		HealthInterval: time.Hour,
		HealthTimeout:  time.Second,
	}
	if err := mgr.RegisterConfig("teardown-test-provider", cfg, baseURL); err != nil {
		t.Fatalf("RegisterConfig: %v", err)
	}
}

// hostPortOf extracts "host:port" from a test server URL (mirrors
// ComputeEndpointKey's host:port suffix so the key matches a registered
// endpoint).
func hostPortOf(t *testing.T, baseURL string) string {
	t.Helper()
	key := llm.ComputeEndpointKey("llama-cpp", baseURL)
	// key = "llama-cpp:<host>:<port>"; strip the runtime prefix.
	for i := 0; i < len(key); i++ {
		if key[i] == ':' {
			return key[i+1:]
		}
	}
	t.Fatalf("unexpected endpoint key shape: %q", key)
	return ""
}

// waitForCalls polls until the adapter recorded n calls (fan-out is async
// per event by contract) or fails after a bounded wait.
func waitForCalls(t *testing.T, a *recordingAdapter, n int, label string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(a.recorded()) >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s did not reach %d calls in time: %v", label, n, a.recorded())
}
