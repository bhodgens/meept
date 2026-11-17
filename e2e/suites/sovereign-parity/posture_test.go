//go:build e2e

// Gate leaf 00T — R3 data posture (T7, T8, T9; Contract C4, leaf 05).
//
// T7: a prompt carrying an AWS-shaped access key never routes to a
// public_only or non_sensitive model — a private model serves.
// T8: posture absent everywhere = private default = routing byte-identical
// to pre-change behavior. T9: the scanner's MATCHED STRING never appears
// in any log line or routing_decisions row (only counts/rules may).
//
// RED today: on main the data_posture keys round-trip inertly and the
// sensitive prompt routes by plain rotation — T7's routing assertion
// fails at runtime; the llm.Posture* symbols live in contracts_test.go
// (compile-RED until leaf 05 lands).
package sovereignparity

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// sensitivePrompt is a REAL scanner match: AKIA + 16 [0-9A-Z] hits the
// aws_access_key rule in internal/security/secret_scanner.go. It is the
// canonical AWS documentation EXAMPLE key (AKIAIOSFODNN7EXAMPLE) — a
// well-known synthetic shape, never a real credential.
const sensitivePrompt = "here is my key AKIAIOSFODNN7EXAMPLE please remember it"

// extra is a per-model raw config-key map (data_posture etc.) that
// round-trips through the JSON5 loader untouched.
type extra map[string]any

// postureConfig builds the two-provider world. first/second carry raw
// per-model config keys for each provider's model.
func postureConfig(t *testing.T, first extra, second extra) parityConfig {
	t.Helper()
	stubA := newParityStub(t)
	stubB := newParityStub(t)
	return parityConfig{
		providers: []parityProvider{
			{
				id:     "provpub",
				stub:   stubA,
				apiKey: "key-pub",
				models: []parityModel{
					{name: "model-public", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 1.0, outCost: 1.0, ctxLimit: 32768, maxOutput: 4096, extra: map[string]any(first)},
				},
			},
			{
				id:     "provpriv",
				stub:   stubB,
				apiKey: "key-priv",
				models: []parityModel{
					{name: "model-private", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 2.0, outCost: 2.0, ctxLimit: 32768, maxOutput: 4096, extra: map[string]any(second)},
				},
			},
		},
		aliases: []parityAlias{
			{name: "general", members: []string{"provpub/model-public", "provpriv/model-private"}, timeout: 5, maxFails: 3},
		},
		// RULING (orchestrator, 2026-11-17): the aux aliases (classifier,
		// summarizer, planner, ...) MUST ride the PRIVATE provider here.
		// The default (first provider's first model) would send the
		// classifier request itself to the public_only stub, and T7's
		// zero-hit assertion counts ALL requests, not just chat-lane
		// dispatches. The routing-row scan already proves the CHAT lane
		// never lands on provpub; isolating the classifier makes the
		// stronger zero-hit assertion meaningful.
		secondaryRef: "provpriv/model-private",
		chatAlias: "general",
	}
}

// TestPosture_SensitiveNeverRoutesPublic (T7): the sensitive prompt lands
// ONLY on the private model even though the public_only model is the
// rotation head.
func TestPosture_SensitiveNeverRoutesPublic(t *testing.T) {
	cfg := postureConfig(t,
		extra{"data_posture": "public_only"},
		extra{"data_posture": "private"},
	)
	stubPublic := cfg.providers[0].stub
	// Pin the classifier to intent chat so the turn routes to the chat
	// agent (whose loop is pinned to the alias under test) instead of
	// escalating to a planner task.
	cfg.providers[0].stub.setClassifierOut(`{"intent":"chat","confidence":0.9,"reasoning":"gate posture drive"}`)
	cfg.providers[1].stub.setClassifierOut(`{"intent":"chat","confidence":0.9,"reasoning":"gate posture drive"}`)
	s := newParitySandbox(t, cfg)

	sessID := s.createSession(t, "posture-sensitive")
	if reply := s.chatTurn(t, sessID, sensitivePrompt, 120*time.Second); reply == "" {
		t.Fatal("sensitive turn returned empty reply")
	}

	rows := s.routingRowsForAlias(t, "general")
	if len(rows) == 0 {
		t.Fatal("sensitive turn wrote no routing_decisions rows")
	}
	for i, r := range rows {
		if r.Model == "model-public" || r.Provider == "provpub" {
			t.Fatalf("T7 R3 RED (expected): sensitive prompt routed to public_only model (row %d: %s/%s reason %q) — no posture gate exists on main", i, r.Provider, r.Model, r.Reason)
		}
	}
	if stubPublic.hitCount() != 0 {
		t.Fatalf("T7: the public_only upstream saw %d requests; a sensitive turn must never reach it", stubPublic.hitCount())
	}
	t.Logf("T7: sensitive turn served by %s (rows: %d), public stub untouched", rows[len(rows)-1].Model, len(rows))
}

// TestPosture_AbsentIsPrivate (T8): no data_posture key anywhere gives
// byte-identical routing — the sensitive prompt resolves to the rotation
// head exactly as a plain turn would.
func TestPosture_AbsentIsPrivate(t *testing.T) {
	cfg := postureConfig(t, nil, nil)
	// Pin intent chat (see TestPosture_SensitiveNeverRoutesPublic).
	cfg.providers[0].stub.setClassifierOut(`{"intent":"chat","confidence":0.9,"reasoning":"gate absent posture"}`)
	cfg.providers[1].stub.setClassifierOut(`{"intent":"chat","confidence":0.9,"reasoning":"gate absent posture"}`)
	s := newParitySandbox(t, cfg)

	sessID := s.createSession(t, "posture-absent")
	if reply := s.chatTurn(t, sessID, sensitivePrompt, 120*time.Second); reply == "" {
		t.Fatal("absent-posture sensitive turn returned empty reply")
	}

	rows := s.routingRowsForAlias(t, "general")
	if len(rows) == 0 {
		t.Fatal("absent-posture turn wrote no routing_decisions rows")
	}
	// Byte-identical to pre-change behavior: the first alias member
	// (provpub/model-public) serves, unconditionally.
	last := rows[len(rows)-1]
	if last.Provider != "provpub" || last.Model != "model-public" {
		t.Fatalf("T8: posture-absent routing changed behavior: served %s/%s reason %q; want provpub/model-public (the unguarded rotation head, byte-identical to pre-change)", last.Provider, last.Model, last.Reason)
	}
}

// TestPosture_NoMatchInLogs (T9): the matched secret string never appears
// in the daemon log or any routing_decisions row after a sensitive turn.
func TestPosture_NoMatchInLogs(t *testing.T) {
	cfg := postureConfig(t, nil, nil)
	s := newParitySandbox(t, cfg)

	sessID := s.createSession(t, "posture-leak")
	if reply := s.chatTurn(t, sessID, sensitivePrompt, 120*time.Second); reply == "" {
		t.Fatal("sensitive turn returned empty reply")
	}

	secret := "AKIAIOSFODNN7EXAMPLE"
	log := s.daemonLog(t)
	if strings.Contains(log, secret) {
		t.Fatal("T9: the matched secret string appears in the daemon log — C4 log discipline violated (only counts/rule names may be logged)")
	}
	rows := s.routingRowsForAlias(t, "general")
	for _, r := range rows {
		blob := fmt.Sprintf("%s|%s|%s|%s|%s", r.Alias, r.Provider, r.Model, r.Reason, r.Candidates)
		if strings.Contains(blob, secret) {
			t.Fatalf("T9: routing_decisions row carries the secret: %+v", r)
		}
	}
}
