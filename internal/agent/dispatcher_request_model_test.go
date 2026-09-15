package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// Pins for the per-request model threading (chat.request "model" →
// ChatService payload → ChatHandler → ClassifyAndRoute → DispatchResult.
// RequestModel → RouteToAgent → AgentLoop.ApplyRequestModel → the loop's
// one-shot SetModelOverride seam).
//
// The dispatcher carries the client's per-request model on DispatchResult
// WITHOUT reworking its parsed-directive machinery: a request model coexists
// with a parsed user directive (both land in the same precedence slot — the
// loop's one-shot override), and an absent request model leaves every
// DispatchResult field byte-identical to before.

func requestModelTestDispatcher(t *testing.T) (*Dispatcher, *task.Registry) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "tasks.db")
	logger := slog.New(slog.DiscardHandler)
	reg, err := task.NewRegistry(dbPath, bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("create task registry: %v", err)
	}
	t.Cleanup(func() {
		if err := reg.Close(); err != nil {
			t.Errorf("close task registry: %v", err)
		}
	})

	d := NewDispatcher(DispatcherConfig{
		TaskStore:    reg.Store(),
		TaskRegistry: reg,
		Logger:       logger,
	})
	return d, reg
}

// The request model must survive ClassifyAndRoute onto DispatchResult.
func TestClassifyAndRoute_CarriesRequestModel(t *testing.T) {
	d, _ := requestModelTestDispatcher(t)

	res, err := d.ClassifyAndRoute(context.Background(),
		"please write some code for me", "sess-req-model-1", nil, "", "local/user-b")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res == nil {
		t.Fatal("nil DispatchResult")
	}
	if res.RequestModel != "local/user-b" {
		t.Errorf("RequestModel = %q, want %q (must ride the dispatch result)", res.RequestModel, "local/user-b")
	}
}

// An absent request model must leave the result untouched (empty field),
// preserving the pre-field wire shape for every existing consumer.
func TestClassifyAndRoute_AbsentRequestModelUnchanged(t *testing.T) {
	d, _ := requestModelTestDispatcher(t)

	res, err := d.ClassifyAndRoute(context.Background(),
		"please write some code for me", "sess-req-model-2", nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res == nil {
		t.Fatal("nil DispatchResult")
	}
	if res.RequestModel != "" {
		t.Errorf("RequestModel = %q, want empty (absent field = unchanged behavior)", res.RequestModel)
	}

	// And it must not leak into user-facing JSON (operational metadata).
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal DispatchResult: %v", err)
	}
	if strings.Contains(string(raw), "request_model") {
		t.Errorf("DispatchResult JSON leaks request_model: %s", raw)
	}
}

// ApplyRequestModel contract: a ref the resolver resolves arms the loop's
// one-shot override (cleared by the turn that consumes it); an alias name is
// a legal request model; an unresolvable ref is dropped without arming.
func TestApplyRequestModel_SeamContract(t *testing.T) {
	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	defer server.Close()

	resolver := overrideResolver(t, server.URL)
	pm := llm.NewProviderManager(llm.ProviderManagerConfig{
		Providers: []*llm.ModelConfig{
			{ProviderID: "local", ModelID: "manager-default", BaseURL: server.URL},
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	loop := newOverrideLoop(t, resolver, pm)

	// Alias-named request model resolves through the same resolver as
	// provider refs: the "classifier" alias name is a legal request model
	// and arms its first member as a provider/model ref (the loop's
	// in-cycle override branch resolves refs, not alias names; the armed
	// ref uses the member's resolved ModelID).
	loop.ApplyRequestModel(testClassifierAlias)
	if got := loop.GetModelOverride(); got != "local/alias-model-a" {
		t.Fatalf("alias-named request model not armed to its member ref: %q", got)
	}
	if loop.IsModelOverridePersistent() {
		t.Error("ApplyRequestModel must arm a one-shot override")
	}
	loop.ClearModelOverride()

	// Unresolvable ref: dropped, nothing armed.
	loop.ApplyRequestModel("local/does-not-exist")
	if got := loop.GetModelOverride(); got != "" {
		t.Fatalf("unresolvable ref armed the override: %q", got)
	}
}

// The ChatHandler entry point arms the request model only when present and
// is nil/empty-safe (mirrors handleRequest's direct-mode call).
func TestChatHandler_ApplyRequestModel_NilAndEmptySafe(t *testing.T) {
	h := &ChatHandler{logger: slog.New(slog.DiscardHandler)}

	// Empty ref: no call into the loop, no panic.
	h.applyRequestModel(nil, "", "conv-nil-safe")

	resolver := overrideResolver(t, "http://127.0.0.1:1") // unreachable is fine — no turn runs
	pm := llm.NewProviderManager(llm.ProviderManagerConfig{
		Providers: []*llm.ModelConfig{
			{ProviderID: "local", ModelID: "manager-default", BaseURL: "http://127.0.0.1:1"},
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	loop := newOverrideLoop(t, resolver, pm)

	h.applyRequestModel(loop, "local/user-b", "conv-arm")
	if got := loop.GetModelOverride(); got != "local/user-b" {
		t.Fatalf("handler did not arm the request model: %q", got)
	}
	loop.ClearModelOverride()
	if got := loop.GetModelOverride(); got != "" {
		t.Fatalf("override not cleared: %q", got)
	}
}

// One-shot semantics through the chat API entry: the override is consumed by
// the first turn (verified end-to-end at the wire in
// TestAgentLoop_RequestModel_NoLeakToNextTurn); here we pin that a SECOND
// ApplyRequestModel before a turn replaces (not stacks) the armed ref.
func TestApplyRequestModel_ReplacementNotStacking(t *testing.T) {
	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	defer server.Close()

	resolver := overrideResolver(t, server.URL)
	pm := llm.NewProviderManager(llm.ProviderManagerConfig{
		Providers: []*llm.ModelConfig{
			{ProviderID: "local", ModelID: "manager-default", BaseURL: server.URL},
		},
		Logger: slog.New(slog.DiscardHandler),
	})
	loop := newOverrideLoop(t, resolver, pm)

	loop.ApplyRequestModel("local/user-b")
	loop.ApplyRequestModel(testClassifierAlias) // alias name resolves too
	if got := loop.GetModelOverride(); got != "local/alias-model-a" {
		t.Fatalf("second request model should replace the first: %q", got)
	}

	_, err := loop.RunOnce(context.Background(), "single turn", "conv-replace")
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if got := cap.models[0]; got != "alias-model-a" {
		t.Errorf("wire model = %q, want the replaced request model's resolution %q", got, "alias-model-a")
	}
	if got := loop.GetModelOverride(); got != "" {
		t.Errorf("override leaked past its turn: %q", got)
	}
}
