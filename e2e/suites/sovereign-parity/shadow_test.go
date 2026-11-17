//go:build e2e

// Gate leaf 00T — R4 shadow routing (T10, T11; Contract C6, leaf 04).
//
// T10: ResolveShadow returns the same model the live path picks with a
// populated Candidates list. T11: shadow writes ZERO llm_calls rows,
// consumes no budget, mutates no alias health, creates no sticky pin.
//
// RED today: on main the llm.route.shadow RPC does not exist — the RPC
// call returns method-not-found at runtime; the ResolveShadow Go symbol
// lives in contracts_test.go (compile-RED until leaf 04 lands).
package sovereignparity

import (
	"encoding/json"
	"testing"
	"time"
)

// shadowConfig: one balanced-sticky alias over two members so T11 can
// prove no sticky pin was created by the shadow path (a live resolve on
// a pinned alias would serve the pinned member; an unpinned one rotates).
func shadowConfig(t *testing.T) parityConfig {
	t.Helper()
	stubA := newParityStub(t)
	stubB := newParityStub(t)
	return parityConfig{
		providers: []parityProvider{
			{
				id:     "provsh1",
				stub:   stubA,
				apiKey: "key-sh1",
				models: []parityModel{
					{name: "model-s1", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 1.0, outCost: 1.0, ctxLimit: 32768, maxOutput: 4096},
				},
			},
			{
				id:     "provsh2",
				stub:   stubB,
				apiKey: "key-sh2",
				models: []parityModel{
					{name: "model-s2", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 2.0, outCost: 2.0, ctxLimit: 32768, maxOutput: 4096},
				},
			},
		},
		aliases: []parityAlias{
			{name: "general", members: []string{"provsh1/model-s1", "provsh2/model-s2"}, timeout: 5, maxFails: 3, balanced: true},
		},
		chatAlias: "general",
	}
}

// callShadowRoute invokes the llm.route.shadow RPC and returns
// (status, decoded result map). Non-200 decodes the error envelope too.
func callShadowRoute(t *testing.T, s *paritySandbox, alias, prompt string) (int, map[string]any) {
	t.Helper()
	status, body := s.busCall(t, "llm.route.shadow", map[string]any{"alias": alias, "prompt": prompt})
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("llm.route.shadow reply decode (status %d): %v (%s)", status, err, body)
	}
	return status, out
}

// TestShadow_ParityWithLivePath (T10): the shadow decision names the same
// model the live ResolveForAlias path picks, with a populated Candidates
// list carrying skip reasons.
func TestShadow_ParityWithLivePath(t *testing.T) {
	cfg := shadowConfig(t)
	s := newParitySandbox(t, cfg)

	// One live turn first: the routing log names the model the live path
	// picked.
	sessID := s.createSession(t, "shadow-live")
	if reply := s.chatTurn(t, sessID, "live shadow parity", 120*time.Second); reply == "" {
		t.Fatal("live turn returned empty reply")
	}
	liveRows := s.routingRowsForAlias(t, "general")
	if len(liveRows) == 0 {
		t.Fatal("live turn wrote no routing_decisions rows; cannot establish the live pick")
	}
	live := liveRows[len(liveRows)-1]

	// Shadow-resolve the same alias.
	status, result := callShadowRoute(t, s, "general", "live shadow parity")
	if status != 200 {
		t.Fatalf("T10 R4 RED (expected): llm.route.shadow status %d (%v) — the RPC does not exist on main; leaf 04 lands it", status, result)
	}
	res, _ := result["result"].(map[string]any)
	if res == nil {
		t.Fatalf("T10: shadow reply carries no result envelope: %+v", result)
	}
	modelID, _ := res["model_id"].(string)
	providerID, _ := res["provider_id"].(string)
	if modelID == "" || providerID == "" {
		t.Fatalf("T10: shadow decision missing model/provider identity: %+v", res)
	}
	if providerID != live.Provider || modelID != live.Model {
		t.Fatalf("T10: shadow decision %s/%s diverges from the live pick %s/%s", providerID, modelID, live.Provider, live.Model)
	}
	cands, _ := res["candidates"].([]any)
	if len(cands) == 0 {
		t.Fatal("T10: shadow decision carries an empty Candidates list; every candidate needs its skip reason (C6)")
	}
	sel := 0
	for _, c := range cands {
		cm, _ := c.(map[string]any)
		if cm == nil {
			continue
		}
		if chosen, _ := cm["selected"].(bool); chosen {
			sel++
		} else if skip, _ := cm["skip_reason"].(string); skip == "" {
			t.Fatalf("T10: unselected shadow candidate carries no skip_reason: %+v", cm)
		}
	}
	if sel != 1 {
		t.Fatalf("T10: shadow Candidates mark %d selected; want exactly one", sel)
	}
}

// TestShadow_NoSideEffects (T11): two shadow resolves write ZERO llm_calls
// rows and ZERO routing_decisions rows, and a subsequent live resolve
// picks the rotation head — proving no sticky pin was created.
func TestShadow_NoSideEffects(t *testing.T) {
	cfg := shadowConfig(t)
	s := newParitySandbox(t, cfg)

	sessID := s.createSession(t, "shadow-sidefx")
	_ = sessID

	callsBefore := s.llmCallsCount(t)
	rowsBefore := s.routingRowCount(t)

	for range 2 {
		status, result := callShadowRoute(t, s, "general", "no side effects probe")
		if status != 200 {
			t.Fatalf("T11 R4 RED (expected): llm.route.shadow status %d (%v) — the RPC does not exist on main", status, result)
		}
	}

	if got := s.llmCallsCount(t); got != callsBefore {
		t.Fatalf("T11: shadow resolves wrote %d llm_calls rows (before %d, after %d); shadow must never write the ledger", got-callsBefore, callsBefore, got)
	}
	if got := s.routingRowCount(t); got != rowsBefore {
		t.Fatalf("T11: shadow resolves wrote %d routing_decisions rows (before %d, after %d); shadow must never write the routing log", got-rowsBefore, rowsBefore, got)
	}

	// Sticky-pin proof: the alias is balanced_sticky. A live resolve in a
	// NEW session must land on the rotation head (provsh1/model-s1). Had
	// the shadow path created a pin for this caller, resolveStickyCaller
	// would serve the pinned member instead of advancing the rotation.
	sess2 := s.createSession(t, "shadow-after")
	if reply := s.chatTurn(t, sess2, "post shadow live turn", 120*time.Second); reply == "" {
		t.Fatal("post-shadow live turn returned empty reply")
	}
	rows := s.routingRowsForAlias(t, "general")
	if len(rows) == 0 {
		t.Fatal("post-shadow live turn wrote no routing rows")
	}
	last := rows[len(rows)-1]
	if last.Reason == "sticky_request" || last.Reason == "sticky_request_new" {
		t.Fatalf("T11: post-shadow live resolve served a sticky reason %q — the shadow path created a pin; C6 forbids sticky mutation", last.Reason)
	}
	if last.Provider != "provsh1" || last.Model != "model-s1" {
		t.Fatalf("T11: post-shadow live resolve served %s/%s (reason %q); want the untouched rotation head provsh1/model-s1 — evidence the shadow consumed rotation state", last.Provider, last.Model, last.Reason)
	}
}
