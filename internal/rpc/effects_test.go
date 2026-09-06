package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/effects"
)

// callEffects invokes an effects RPC handler directly with JSON params,
// bypassing the socket server. internal/rpc has no existing *_test.go
// server fixture, so the leaf spec sanctions direct handler invocation;
// RegisterEffectsMethods is additionally exercised by the registration
// test below against a real rpc.Server instance.
func callEffects(t *testing.T, h *EffectsRPCHandler, method, params string) (any, error) {
	t.Helper()
	var handler Handler
	switch method {
	case "effects.list":
		handler = h.handleList
	case "effects.reconcile":
		handler = h.handleReconcile
	default:
		t.Fatalf("unknown effects method %q", method)
	}
	return handler(context.Background(), json.RawMessage(params))
}

// seedClaim claims key on ledger as a granted first claim.
func seedClaim(t *testing.T, ledger effects.Ledger, key string, meta effects.EffectMeta) {
	t.Helper()
	granted, _, err := ledger.Claim(context.Background(), key, meta)
	if err != nil {
		t.Fatalf("seed claim %s: %v", key, err)
	}
	if !granted {
		t.Fatalf("seed claim %s: expected grant", key)
	}
}

func TestEffectsRPCHandlerRegistration(t *testing.T) {
	server := New(&Config{SocketPath: ""}, nil, nil)
	h := NewEffectsRPCHandler(effects.NewMemoryLedger())
	h.RegisterEffectsMethods(server)

	for _, method := range []string{"effects.list", "effects.reconcile"} {
		_, err := server.CallMethod(context.Background(), method, json.RawMessage(`{}`))
		if err != nil && strings.Contains(err.Error(), "method not found") {
			t.Errorf("CallMethod(%s): method not registered: %v", method, err)
		}
	}
}

func TestEffectsRPC(t *testing.T) {
	t.Run("list empty", func(t *testing.T) {
		h := NewEffectsRPCHandler(effects.NewMemoryLedger())
		out, err := callEffects(t, h, "effects.list", `{}`)
		if err != nil {
			t.Fatalf("effects.list: %v", err)
		}
		m, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("result type %T, want map[string]any", out)
		}
		items, ok := m["effects"].([]effects.EffectRecord)
		if !ok || len(items) != 0 {
			t.Errorf("effects = %#v, want empty []effects.EffectRecord", m["effects"])
		}
	})

	t.Run("list filters by state", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		oldMeta := effects.EffectMeta{Tool: "backup.git_push", ProviderIdempotent: true}
		newMeta := effects.EffectMeta{Tool: "push.notify", ProviderIdempotent: false}
		seedClaim(t, ledger, "old", oldMeta)
		time.Sleep(2 * time.Millisecond)
		seedClaim(t, ledger, "new", newMeta)
		if err := ledger.Complete(context.Background(), "old"); err != nil {
			t.Fatalf("complete old: %v", err)
		}

		h := NewEffectsRPCHandler(ledger)
		out, err := callEffects(t, h, "effects.list", `{"state":"claimed"}`)
		if err != nil {
			t.Fatalf("effects.list: %v", err)
		}
		items := out.(map[string]any)["effects"].([]effects.EffectRecord)
		if len(items) != 1 {
			t.Fatalf("got %d records, want 1 claimed", len(items))
		}
		if items[0].Key != "new" || items[0].State != effects.StateClaimed {
			t.Errorf("record = %s/%s, want new/claimed", items[0].Key, items[0].State)
		}

		// Explicit state filter uses the same ordering. Terminal states
		// are NOT enumerable over the pinned Ledger surface (Contract 1:
		// only ReconcilePending + Get-by-key exist), so a terminal-state
		// filter resolves only in combination with a key; without one the
		// result is an empty list. This is the documented behavior choice.
		seedClaim(t, ledger, "older", oldMeta)
		out, err = callEffects(t, h, "effects.list", `{"state":"completed","key":"old"}`)
		if err != nil {
			t.Fatalf("effects.list completed: %v", err)
		}
		items = out.(map[string]any)["effects"].([]effects.EffectRecord)
		if len(items) != 1 || items[0].Key != "old" {
			t.Errorf("completed filter = %#v, want only 'old'", items)
		}
		out, err = callEffects(t, h, "effects.list", `{"state":"completed"}`)
		if err != nil {
			t.Fatalf("effects.list completed no-key: %v", err)
		}
		items = out.(map[string]any)["effects"].([]effects.EffectRecord)
		if len(items) != 0 {
			t.Errorf("completed filter without key = %#v, want empty (not enumerable)", items)
		}
	})

	t.Run("list all", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		seedClaim(t, ledger, "k1", effects.EffectMeta{Tool: "push.notify"})
		time.Sleep(2 * time.Millisecond)
		seedClaim(t, ledger, "k2", effects.EffectMeta{Tool: "push.notify"})
		if err := ledger.Complete(context.Background(), "k1"); err != nil {
			t.Fatalf("complete k1: %v", err)
		}

		h := NewEffectsRPCHandler(ledger)
		out, err := callEffects(t, h, "effects.list", `{}`)
		if err != nil {
			t.Fatalf("effects.list: %v", err)
		}
		items := out.(map[string]any)["effects"].([]effects.EffectRecord)
		if len(items) != 1 || items[0].Key != "k2" {
			t.Errorf("default list = %#v, want pending only (k2)", items)
		}
	})

	t.Run("list by key", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		seedClaim(t, ledger, "key-a", effects.EffectMeta{Tool: "push.notify"})
		seedClaim(t, ledger, "key-b", effects.EffectMeta{Tool: "push.notify"})

		h := NewEffectsRPCHandler(ledger)
		out, err := callEffects(t, h, "effects.list", `{"key":"key-a"}`)
		if err != nil {
			t.Fatalf("effects.list: %v", err)
		}
		items := out.(map[string]any)["effects"].([]effects.EffectRecord)
		if len(items) != 1 || items[0].Key != "key-a" {
			t.Errorf("key filter = %#v, want only key-a", items)
		}
	})

	t.Run("reconcile complete", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		seedClaim(t, ledger, "k", effects.EffectMeta{Tool: "push.notify"})

		h := NewEffectsRPCHandler(ledger)
		out, err := callEffects(t, h, "effects.reconcile", `{"key":"k","action":"complete"}`)
		if err != nil {
			t.Fatalf("effects.reconcile: %v", err)
		}
		rec := out.(map[string]any)["record"].(*effects.EffectRecord)
		if rec.Key != "k" || rec.State != effects.StateCompleted {
			t.Errorf("record = %s/%s, want k/completed", rec.Key, rec.State)
		}

		got, err := ledger.Get(context.Background(), "k")
		if err != nil || got.State != effects.StateCompleted {
			t.Errorf("ledger state after reconcile = %v (err %v), want completed", got, err)
		}
	})

	t.Run("reconcile complete with receipt over receipted refused", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		seedClaim(t, ledger, "k", effects.EffectMeta{Tool: "push.notify"})
		if err := ledger.RecordReceipt(context.Background(), "k", json.RawMessage(`{"v":1}`)); err != nil {
			t.Fatalf("seed receipt: %v", err)
		}

		h := NewEffectsRPCHandler(ledger)
		_, err := callEffects(t, h, "effects.reconcile", `{"key":"k","action":"complete","receipt":{"v":2}}`)
		if !errors.Is(err, effects.ErrInvalidTransition) {
			t.Errorf("error = %v, want ErrInvalidTransition (receipt overwrite refused)", err)
		}
	})

	t.Run("reconcile complete with receipt over claimed", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		seedClaim(t, ledger, "k", effects.EffectMeta{Tool: "push.notify"})

		h := NewEffectsRPCHandler(ledger)
		out, err := callEffects(t, h, "effects.reconcile", `{"key":"k","action":"complete","receipt":{"hand":"verified"}}`)
		if err := err; err != nil {
			t.Fatalf("effects.reconcile: %v", err)
		}
		rec := out.(map[string]any)["record"].(*effects.EffectRecord)
		if rec.State != effects.StateCompleted {
			t.Errorf("record state = %s, want completed", rec.State)
		}
		if !bytesEqualJSONBytes(rec.Receipt, json.RawMessage(`{"hand":"verified"}`)) {
			t.Errorf("receipt = %s, want {\"hand\":\"verified\"}", rec.Receipt)
		}
	})

	t.Run("reconcile abandon", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		seedClaim(t, ledger, "k", effects.EffectMeta{Tool: "push.notify"})

		h := NewEffectsRPCHandler(ledger)
		out, err := callEffects(t, h, "effects.reconcile", `{"key":"k","action":"abandon","reason":"provider says message never sent"}`)
		if err != nil {
			t.Fatalf("effects.reconcile: %v", err)
		}
		rec := out.(map[string]any)["record"].(*effects.EffectRecord)
		if rec.Key != "k" || rec.State != effects.StateAbandoned {
			t.Errorf("record = %s/%s, want k/abandoned", rec.Key, rec.State)
		}
	})

	t.Run("reconcile abandon no reason", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		seedClaim(t, ledger, "k", effects.EffectMeta{Tool: "push.notify"})

		h := NewEffectsRPCHandler(ledger)
		_, err := callEffects(t, h, "effects.reconcile", `{"key":"k","action":"abandon"}`)
		if err == nil || err.Error() != "reason required for abandon" {
			t.Errorf("error = %v, want 'reason required for abandon'", err)
		}
		if _, getErr := ledger.Get(context.Background(), "k"); getErr != nil {
			t.Fatalf("record should still exist: %v", getErr)
		}
		got, _ := ledger.Get(context.Background(), "k")
		if got.State != effects.StateClaimed {
			t.Errorf("state after refused abandon = %s, want claimed", got.State)
		}
	})

	t.Run("reconcile abandon empty reason", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		seedClaim(t, ledger, "k", effects.EffectMeta{Tool: "push.notify"})

		h := NewEffectsRPCHandler(ledger)
		_, err := callEffects(t, h, "effects.reconcile", `{"key":"k","action":"abandon","reason":"   "}`)
		if err == nil || err.Error() != "reason required for abandon" {
			t.Errorf("error = %v, want 'reason required for abandon'", err)
		}
	})

	t.Run("reconcile bad action", func(t *testing.T) {
		ledger := effects.NewMemoryLedger()
		seedClaim(t, ledger, "k", effects.EffectMeta{Tool: "push.notify"})

		h := NewEffectsRPCHandler(ledger)
		_, err := callEffects(t, h, "effects.reconcile", `{"key":"k","action":"retry"}`)
		if err == nil || err.Error() != "invalid action" {
			t.Errorf("error = %v, want 'invalid action'", err)
		}
		_, err = callEffects(t, h, "effects.reconcile", `{"key":"k"}`)
		if err == nil || err.Error() != "invalid action" {
			t.Errorf("missing action error = %v, want 'invalid action'", err)
		}
	})

	t.Run("reconcile unknown key", func(t *testing.T) {
		h := NewEffectsRPCHandler(effects.NewMemoryLedger())
		_, err := callEffects(t, h, "effects.reconcile", `{"key":"nope","action":"complete"}`)
		if err == nil || !errors.Is(err, effects.ErrUnknownKey) {
			t.Fatalf("error = %v, want ErrUnknownKey", err)
		}
		if !strings.Contains(err.Error(), "effects: unknown effect key") {
			t.Errorf("error text = %q, want it to contain 'effects: unknown effect key'", err.Error())
		}
	})

	t.Run("reconcile missing key", func(t *testing.T) {
		h := NewEffectsRPCHandler(effects.NewMemoryLedger())
		_, err := callEffects(t, h, "effects.reconcile", `{"action":"complete"}`)
		if err == nil || err.Error() != "key is required" {
			t.Errorf("error = %v, want 'key is required'", err)
		}
	})

	t.Run("no ledger", func(t *testing.T) {
		h := NewEffectsRPCHandler(nil)
		// Parity with the ParkStore degradation posture: list answers
		// with an EMPTY list on an unwired ledger, never an error.
		out, err := callEffects(t, h, "effects.list", `{}`)
		if err != nil {
			t.Fatalf("list error = %v, want nil (empty-list degradation)", err)
		}
		items := out.(map[string]any)["effects"].([]effects.EffectRecord)
		if len(items) != 0 {
			t.Errorf("effects = %#v, want empty", items)
		}
		_, err = callEffects(t, h, "effects.reconcile", `{"key":"k","action":"complete"}`)
		if err == nil || err.Error() != "effects service not available" {
			t.Errorf("reconcile error = %v, want 'effects service not available'", err)
		}
	})
}

func bytesEqualJSONBytes(a, b json.RawMessage) bool {
	return string(a) == string(b)
}
