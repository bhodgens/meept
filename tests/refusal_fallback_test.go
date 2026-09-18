//go:build integration

package tests

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	appmetrics "github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Refusal-fallback tree leaf 05: end-to-end stub-endpoint acceptance tests.
//
// Unlike the loop-level tests in internal/agent (which short-circuit call 1
// with a hand-built *llm.RefusalError), these drive the REAL llm.Client
// against a scripted OpenAI-compatible httptest endpoint so the WHOLE chain
// is exercised over the wire: leaf 01 finish_reason detection, leaf 03
// one-hop re-dispatch with the pinned llm.WithModelOverride, and leaf 04
// reply disclosure. Three frozen scenarios plus leaf-04 contract pins:
//
//	A: fallback succeeds    — refusal on model-a (finish_reason
//	                        "content_filter"), model-b serves "done",
//	                        disclosure appended, escalation event carries
//	                        reason "refusal_fallback", exactly 2 wire calls.
//	B: fallback also refuses — exactly 2 calls, *llm.RefusalError surfaces,
//	                        no third attempt, no disclosure anywhere.
//	C: feature off          — single refusing call, the refusal surfaces,
//	                        no retry, no event, no disclosure.
//
// The loops are wired through the same seam chain the daemon uses
// (WithResolver defaults refusalResolver/servingRef/applier/clear; the bus
// publisher is installed with the loop.go:2187 closure shape).

// refusalStub is a scripted OpenAI-compatible /chat/completions endpoint.
// Each call pops the next scripted response; an exhausted script fails the
// test fast (the call-count assertions would be wrong anyway).
type refusalStub struct {
	t *testing.T

	mu       sync.Mutex
	entries  []string // scripted raw JSON bodies, in order
	requests []string // received "model" values, in arrival order
}

func newRefusalStub(t *testing.T, entries ...string) *refusalStub {
	t.Helper()
	return &refusalStub{t: t, entries: entries}
}

func (s *refusalStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model  string `json:"model"`
		Stream bool   `json:"stream"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		body.Model = ""
	}

	s.mu.Lock()
	s.requests = append(s.requests, body.Model)
	if len(s.entries) == 0 {
		s.mu.Unlock()
		s.t.Errorf("refusalStub: unexpected request for model %q — script exhausted", body.Model)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"script exhausted"}}`))
		return
	}
	next := s.entries[0]
	s.entries = s.entries[1:]
	s.mu.Unlock()

	// The agent loop's streaming path sends "stream": true and the client's
	// doStreamRequest parses SSE "data:" lines. Non-2xx stays plain JSON;
	// scripted 200 entries are SSE stream chunks (see openaiChunk).
	if body.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(next))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(next))
}

func (s *refusalStub) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

func (s *refusalStub) models() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.requests))
	copy(out, s.requests)
	return out
}

// openaiCompletion builds ONE scripted SSE exchange for the streaming path:
// a role delta, a content delta, a finish chunk carrying finishReason
// ("content_filter" = the leaf-01 refusal signal, "stop" = success), and a
// usage chunk, terminated by [DONE]. Matches the wire format the client's
// doStreamRequest scanner parses.
func openaiCompletion(id, model, content, finishReason string) string {
	chunk := func(choices map[string]any, usage map[string]any) string {
		body := map[string]any{
			"id":      id,
			"object":  "chat.completion.chunk",
			"created": 1,
			"model":   model,
			"choices": []map[string]any{choices},
		}
		if usage != nil {
			body["usage"] = usage
		}
		raw, err := json.Marshal(body)
		if err != nil {
			panic(err) // unreachable for map[string]any of scalars
		}
		return "data: " + string(raw) + "\n\n"
	}
	var b strings.Builder
	b.WriteString(chunk(map[string]any{
		"index": 0,
		"delta": map[string]any{"role": "assistant", "content": content},
	}, nil))
	b.WriteString(chunk(map[string]any{
		"index":         0,
		"delta":         map[string]any{},
		"finish_reason": finishReason,
	}, nil))
	b.WriteString(chunk(map[string]any{}, map[string]any{
		"prompt_tokens":     4,
		"completion_tokens": len(content),
		"total_tokens":      4 + len(content),
	}))
	b.WriteString("data: [DONE]\n\n")
	return b.String()
}

// refusalFallbackLoop wires a loop against the stub with the daemon seam
// chain: WithResolver defaults refusalResolver + servingRef + applier +
// clear; WithAgentSpec carries the per-agent RefusalModel ("" = feature
// off); the bus publisher uses the production closure shape. Uses the
// GLOBAL slot when useGlobalSlot is set (exercises the second config level),
// otherwise the per-agent spec.
func refusalFallbackLoop(
	t *testing.T,
	endpoint string,
	primaryModel string,
	refusalModelRef string,
	useGlobalSlot bool,
	msgBus *bus.MessageBus,
	extraClients ...**llm.Client,
) *agent.AgentLoop {
	t.Helper()

	resolver := llm.NewResolver(&llm.ProvidersConfig{
		ModelAliases: map[string]llm.ModelAliasEntry{
			"primary": {Models: []string{"local/" + primaryModel}},
		},
		Providers: map[string]llm.ProviderConfig{
			"local": {
				API:     "openai",
				Options: llm.ProviderOptionsConfig{BaseURL: endpoint},
				Models: map[string]llm.ModelDef{
					primaryModel:   {Name: primaryModel},
					"stub-model-b": {Name: "stub-model-b"},
				},
			},
		},
	}, nil)

	client := llm.NewClient(&llm.ModelConfig{
		BaseURL:    endpoint,
		ProviderID: "local",
		ModelID:    primaryModel,
	})
	for _, out := range extraClients {
		if out != nil {
			*out = client
		}
	}

	opts := []agent.LoopOption{
		agent.WithLoopLogger(slog.New(slog.NewTextHandler(io.Discard, nil))),
		agent.WithResolver(resolver),
		agent.WithModelRef("primary"),
		agent.WithLLMClient(client),
		// No tool registry: the stub replies with prose, the loop must not
		// offer tool schemas the scripted endpoint knows nothing about.
	}
	if useGlobalSlot {
		// Scenario A exercises the GLOBAL slot path: the per-agent spec
		// stays empty and SetGlobalRefusalModel mirrors the models.json5
		// refusal_model value, exactly like daemon wiring (components.go).
		opts = append(opts, agent.WithAgentSpec(&agent.AgentSpec{}))
	} else {
		opts = append(opts, agent.WithAgentSpec(&agent.AgentSpec{RefusalModel: refusalModelRef}))
	}
	loop := agent.NewAgentLoop("sess-refusal-e2e", t.TempDir(), opts...)

	if useGlobalSlot {
		loop.SetGlobalRefusalModel(refusalModelRef)
	}
	// Bus-backed escalation publisher with the SAME closure shape the
	// daemon installs for verification escalation (loop.go:2186), so the
	// event assertion proves the FULL bus path, not just a seam call.
	// refusalEventPublisher is an unexported AgentLoop field, so this
	// external-package test injects it through the exported
	// ExportedRefusalSeams accessor (loop_refusal_export_test.go seam)
	// rather than guessing at setter names.
	if msgBus != nil {
		seams := agent.ExportedRefusalSeams(loop)
		seams.EventPublisher = func(topic string, payload map[string]any) {
			if msg, err := models.NewBusMessage(models.MessageTypeEvent, "agent", payload); err == nil {
				msgBus.Publish(topic, msg)
			}
		}
		seams.Apply()
	}
	return loop
}

const refusalEscalationTopic = "agent.model_escalated"

// escalationRecorder captures agent.model_escalated messages through a REAL
// bus subscription (not the seam), proving publish → bus → subscriber.
type escalationRecorder struct {
	mu   sync.Mutex
	msgs []*models.BusMessage
}

func subscribeEscalations(t *testing.T, msgBus *bus.MessageBus) *escalationRecorder {
	t.Helper()
	rec := &escalationRecorder{}
	sub := msgBus.Subscribe("tests-refusal-e2e", refusalEscalationTopic)
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })
	go func() {
		for msg := range sub.Channel {
			rec.mu.Lock()
			rec.msgs = append(rec.msgs, msg)
			rec.mu.Unlock()
		}
	}()
	return rec
}

// next returns the first recorded message not yet consumed, waiting up to
// `wait` for it. nil on timeout (caller asserts the negative cases).
func (r *escalationRecorder) next(wait time.Duration) *models.BusMessage {
	deadline := time.Now().Add(wait)
	for {
		r.mu.Lock()
		if len(r.msgs) > 0 {
			msg := r.msgs[0]
			r.msgs = r.msgs[1:]
			r.mu.Unlock()
			return msg
		}
		r.mu.Unlock()
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *escalationRecorder) payload(msg *models.BusMessage) map[string]any {
	var payload map[string]any
	if msg != nil && len(msg.Payload) > 0 {
		_ = json.Unmarshal(msg.Payload, &payload)
	}
	return payload
}

// --- Scenario A: fallback succeeds (frozen contract) ---

func TestRefusalFallbackE2E_FallbackSucceeds(t *testing.T) {
	stub := newRefusalStub(t,
		// call 1: model-a refuses (leaf-01 finish_reason signal)
		openaiCompletion("cmpl-refuse", "stub-model-a", "", "content_filter"),
		// call 2: the configured refusal model serves normally
		openaiCompletion("cmpl-done", "stub-model-b", "done", "stop"),
	)
	server := httptest.NewServer(stub)
	defer server.Close()

	msgBus := bus.New(nil, nil)
	escalations := subscribeEscalations(t, msgBus)
	// RefusalModel points at the GLOBAL slot level here; spec stays empty.
	loop := refusalFallbackLoop(t, server.URL, "stub-model-a", "local/stub-model-b", true, msgBus)

	reply, err := loop.RunOnceWithParts(context.Background(), "hello", nil, "conv-refusal-a")
	require.NoError(t, err, "Scenario A: turn must succeed after the fallback retry")

	// Reply disclosure: leaf-04 contract, the CODE-appended line naming the
	// fallback model must terminate the user-visible reply.
	assert.True(t, strings.HasSuffix(reply, "\n\n[answered by local/stub-model-b after refusal]"),
		"Scenario A: reply must end with the fallback disclosure naming the fallback model; got %q", reply)

	// Exactly 2 wire calls; the SECOND must carry the refusal model id —
	// the pinned llm.WithModelOverride flows as a real model override.
	assert.Equal(t, 2, stub.calls(), "Scenario A: exactly 2 wire calls (refusal + fallback retry)")
	assert.Equal(t, []string{"stub-model-a", "stub-model-b"}, stub.models(),
		"Scenario A: second wire call must carry the refusal model id stub-model-b")

	// Bus event with reason refusal_fallback, through the real bus.
	event := escalations.next(2 * time.Second)
	require.NotNil(t, event, "Scenario A: no %q event observed on the bus within 2s", refusalEscalationTopic)
	payload := escalations.payload(event)
	assert.Equal(t, "refusal_fallback", payload["reason"], "Scenario A: event reason must be refusal_fallback")
	assert.Equal(t, "local/stub-model-b", payload["to_model"], "Scenario A: event to_model must name the fallback")
	assert.Equal(t, float64(0), payload["fix_loops"], "Scenario A: refusal fallback grants no fix loops")
}

// --- Scenario B: fallback also refuses (frozen contract) ---

func TestRefusalFallbackE2E_FallbackAlsoRefuses(t *testing.T) {
	stub := newRefusalStub(t,
		openaiCompletion("cmpl-r1", "stub-model-a", "", "content_filter"),
		openaiCompletion("cmpl-r2", "stub-model-b", "", "content_filter"),
	)
	server := httptest.NewServer(stub)
	defer server.Close()

	msgBus := bus.New(nil, nil)
	escalations := subscribeEscalations(t, msgBus)
	loop := refusalFallbackLoop(t, server.URL, "stub-model-a", "local/stub-model-b", false, msgBus)

	reply, err := loop.RunOnceWithParts(context.Background(), "hello", nil, "conv-refusal-b")
	require.Error(t, err, "Scenario B: double refusal must surface an error, not a reply")

	var refusal *llm.RefusalError
	require.True(t, errors.As(err, &refusal),
		"Scenario B: error must satisfy errors.As for *llm.RefusalError; got %T: %v", err, err)
	assert.Equal(t, "content_filter", refusal.FinishReason,
		"Scenario B: the surfaced refusal is the one-hop failure, finish_reason preserved")

	assert.Equal(t, 2, stub.calls(), "Scenario B: exactly 2 wire calls — no third attempt")
	assert.Equal(t, []string{"stub-model-a", "stub-model-b"}, stub.models(),
		"Scenario B: both calls must have hit the wire (primary, then fallback)")

	assert.NotContains(t, reply, "answered by",
		"Scenario B: nothing returned may carry the fallback disclosure")
	// Exactly ONE event: the ARM event (primary refused, fallback armed).
	// The give-up itself must not fire a second escalation.
	arm := escalations.next(500 * time.Millisecond)
	require.NotNil(t, arm, "Scenario B: the fallback ARM event must be published")
	assert.Equal(t, "refusal_fallback", escalations.payload(arm)["reason"])
	assert.Nil(t, escalations.next(500*time.Millisecond),
		"Scenario B: no SECOND escalation event may fire on the give-up path")
}

// --- Scenario C: feature off (frozen contract) ---

func TestRefusalFallbackE2E_FeatureOff(t *testing.T) {
	stub := newRefusalStub(t,
		openaiCompletion("cmpl-off", "stub-model-a", "", "content_filter"),
	)
	server := httptest.NewServer(stub)
	defer server.Close()

	msgBus := bus.New(nil, nil)
	escalations := subscribeEscalations(t, msgBus)
	// Empty spec RefusalModel AND empty global slot => feature off.
	loop := refusalFallbackLoop(t, server.URL, "stub-model-a", "", false, msgBus)

	reply, err := loop.RunOnceWithParts(context.Background(), "hello", nil, "conv-refusal-c")
	require.Error(t, err, "Scenario C: the refusal must surface with the feature off")

	var refusal *llm.RefusalError
	require.True(t, errors.As(err, &refusal),
		"Scenario C: error must satisfy errors.As for *llm.RefusalError; got %T: %v", err, err)

	assert.Equal(t, 1, stub.calls(), "Scenario C: exactly 1 wire call — no retry with the feature off")
	assert.NotContains(t, reply, "answered by",
		"Scenario C: nothing returned may carry the fallback disclosure")
	assert.Nil(t, escalations.next(500*time.Millisecond),
		"Scenario C: no escalation event may fire with the feature off")
}

// --- Task-path disclosure pin (leaf 04, second assembly site) ---

func TestRefusalFallbackE2E_TaskPathDisclosure(t *testing.T) {
	stub := newRefusalStub(t,
		openaiCompletion("cmpl-task-refuse", "stub-model-a", "", "content_filter"),
		openaiCompletion("cmpl-task-done", "stub-model-b", "done", "stop"),
	)
	server := httptest.NewServer(stub)
	defer server.Close()

	loop := refusalFallbackLoop(t, server.URL, "stub-model-a", "local/stub-model-b", false, nil)

	tk := task.NewTask("refusal task path", "prove the task-path disclosure")
	tk = tk.WithProjectDir(t.TempDir())

	reply, err := loop.RunWithTask(context.Background(), tk)
	require.NoError(t, err, "Task path: turn must succeed after the fallback retry")

	assert.True(t, strings.HasSuffix(reply, "\n\n[answered by local/stub-model-b after refusal]"),
		"Task path: reply must end with the fallback disclosure naming the fallback model; got %q", reply)
	assert.Equal(t, 2, stub.calls(), "Task path: exactly 2 wire calls")
	assert.Equal(t, "stub-model-b", stub.models()[1],
		"Task path: second wire call must carry the refusal model id")
}

// --- Ledger identity pin (leaf 04 Task 3, over the real wire) ---

func TestRefusalFallbackE2E_LedgerNamesFallbackModel(t *testing.T) {
	stub := newRefusalStub(t,
		openaiCompletion("cmpl-ledger-refuse", "stub-model-a", "", "content_filter"),
		openaiCompletion("cmpl-ledger-done", "stub-model-b", "done", "stop"),
	)
	server := httptest.NewServer(stub)
	defer server.Close()

	store, err := appmetrics.NewStore(&appmetrics.StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		FlushInterval: time.Hour, // disable background flush; llm_calls writes are direct
	})
	require.NoError(t, err)
	defer store.Close()

	var ledgerClient *llm.Client
	loop := refusalFallbackLoop(t, server.URL, "stub-model-a", "local/stub-model-b", false, nil, &ledgerClient)
	// Daemon wiring shape (daemon.go:668): the usage store attaches to the
	// CLIENT so the production recordUsageStore path writes the ledger.
	ledgerClient.SetUsageStore(store)

	_, err = loop.RunOnceWithParts(context.Background(), "hello", nil, "conv-refusal-ledger")
	require.NoError(t, err)

	// recordUsageStore writes asynchronously; poll briefly for the row.
	deadline := time.Now().Add(5 * time.Second)
	var fbCalls int
	for time.Now().Before(deadline) {
		fbCalls = 0
		require.NoError(t, store.DB().Get(&fbCalls,
			`SELECT COUNT(*) FROM llm_calls WHERE provider='local' AND model_id='stub-model-b'`))
		if fbCalls >= 1 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, fbCalls, 1,
		"expected an llm_calls row for local/stub-model-b (the fallback retry)")

	var primaryRows int
	require.NoError(t, store.DB().Get(&primaryRows,
		`SELECT COUNT(*) FROM llm_calls WHERE model_id='stub-model-a'`))
	// Bughunt F12: this assertion previously demanded ZERO primary rows,
	// enshrining the usage-discarding bug — a refused call consumed tokens
	// and the ledger silently dropped them. The refusal now carries
	// RefusalError.Usage and recordUsageStore writes the row before the
	// refusal surfaces, so exactly one primary row (an error row) is the
	// contract.
	assert.Equal(t, 1, primaryRows,
		"the refusing primary call must ledger exactly one llm_calls row carrying its consumed usage (was wrongly asserted 0 pre-F12)")
}
