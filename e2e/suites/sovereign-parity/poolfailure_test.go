//go:build e2e

// Gate leaf 00T — R5 pool-scoped failure (T12, T13, T14; Contract C5,
// leaf 06).
//
// T12: a 401 on model A blocks the ACCOUNT — sibling model B on the same
// provider is skipped. T13: a 400 (request fault) blocks nothing. T14:
// the frozen pools.py:176-206 classification table, verbatim.
//
// RED today: on main a 401 arms only the endpoint/alias cooldown and the
// sibling model keeps serving (account scope does not exist), and the
// llm.ClassifyFailure symbol lives in contracts_test.go (compile-RED
// until leaf 06 lands). T12/T13 run the classification through the LIVE
// failure path: inject the status via the stub, then observe what the
// next resolve picks.
package sovereignparity

import (
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// poolFailureConfig: ONE provider (provacct) carrying two sibling models,
// plus a second provider (provother) whose model is the cheaper rotation
// head — the escape candidate a scoped block must steer the resolver to.
func poolFailureConfig(t *testing.T) parityConfig {
	t.Helper()
	stubAcct := newParityStub(t)
	stubOther := newParityStub(t)
	return parityConfig{
		providers: []parityProvider{
			{
				id:     "provother",
				stub:   stubOther,
				apiKey: "key-other",
				models: []parityModel{
					{name: "model-other", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 1.0, outCost: 1.0, ctxLimit: 32768, maxOutput: 4096},
				},
			},
			{
				id:     "provacct",
				stub:   stubAcct,
				apiKey: "key-acct",
				models: []parityModel{
					{name: "model-a1", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 2.0, outCost: 2.0, ctxLimit: 32768, maxOutput: 4096},
					{name: "model-a2", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 2.0, outCost: 2.0, ctxLimit: 32768, maxOutput: 4096},
				},
			},
		},
		aliases: []parityAlias{
			// Rotation head = provother/model-other; a healthy resolve
			// never touches provacct. The scenario injects a failure on
			// provacct FIRST (direct hit), then asks what the next resolve
			// of the full alias does.
			{name: "general", members: []string{"provother/model-other", "provacct/model-a1", "provacct/model-a2"}, timeout: 5, maxFails: 3},
		},
		chatAlias: "general",
	}
}

// driveFailure turns the provider into a constant <status> responder,
// drives one turn onto it (the failure is recorded via RecordAliasFailure
// by the loop's failure path), then restores it and returns the routing
// rows observed across the drive.
func driveFailure(t *testing.T, s *paritySandbox, sessID string, failMode func(), restore func(), needle string) {
	t.Helper()
	failMode()
	// The turn may fail/park (the failure is the point); submit and wait
	// for any terminal.
	ack := s.submitChat(t, sessID, "failure injection probe "+needle)
	turnID, _ := ack["turn_id"].(string)
	if turnID != "" {
		sess, subID := subscribeTopic(t, s, "turn.terminal")
		func() {
			defer sess.Close()
			waitTerminal(t, sess, subID, turnID, 90*time.Second)
		}()
	}
	restore()
}

// TestPoolFailure_AccountScopeBlocksSiblings (T12): after a 401 on
// provacct/model-a1, the next resolve skips BOTH provacct models — the
// ACCOUNT is blocked, not just the model. RED on main: only the endpoint/
// alias cooldown arms; the sibling model-a2 stays eligible and will serve.
func TestPoolFailure_AccountScopeBlocksSiblings(t *testing.T) {
	cfg := poolFailureConfig(t)
	stubAcct := cfg.providers[1].stub
	s := newParitySandbox(t, cfg)

	sessID := s.createSession(t, "pool-acct")

	// Walk rotation onto provacct/model-a1 (cheap head serves turn 1),
	// then poison provacct with 401s and drive until a provacct model
	// actually fails.
	if reply := s.chatTurn(t, sessID, "warm the rotation", 120*time.Second); reply == "" {
		t.Fatal("warmup turn returned empty reply")
	}

	driveFailure(t, s, sessID,
		func() { stubAcct.setMode("model-a1", "status:401"); stubAcct.setMode("model-a2", "ok") },
		func() { stubAcct.setMode("model-a1", "ok") },
		"acct401")

	// Recovery turn: provacct is healthy again at the HTTP layer. The
	// scoped block must still hold at resolve time (within the cooldown).
	// Every subsequent routing row must avoid provacct entirely while the
	// account block is live.
	time.Sleep(250 * time.Millisecond) // failure-record settle
	if reply := s.chatTurn(t, sessID, "post 401 recovery probe", 120*time.Second); reply == "" {
		t.Fatal("recovery turn returned empty reply")
	}

	rows := s.routingRowsForAlias(t, "general")
	if len(rows) < 2 {
		t.Fatalf("expected >=2 routing rows, got %d", len(rows))
	}
	post := rows[len(rows)-1]
	if post.Provider == "provacct" {
		t.Fatalf("T12 R5 RED (expected): after a 401, the resolver served %s/%s — the account scope does not exist on main; sibling model-a2 (same provider, same key) must be skipped too", post.Provider, post.Model)
	}
}

// TestPoolFailure_RequestFaultBlocksNothing (T13): a 400 is the caller's
// fault; the model that returned it stays eligible. The observable RED on
// main: a 400 classifies FailureFatal — which arms NO cooldown and NO
// endpoint block, but ALSO arms the alias-level backoff cooldown
// (RecordAliasFailure's unconditional CooldownUntil advance), so the next
// resolve rotates AWAY from provacct/model-a1 even though nothing is
// wrong with it. Leaf 06's request-fault scope forbids arming anything.
func TestPoolFailure_RequestFaultBlocksNothing(t *testing.T) {
	cfg := poolFailureConfig(t)
	stubAcct := cfg.providers[1].stub
	// Pin the classifier to chat (admission/posture pattern) so the
	// rotation turn stays on the chat agent pinned to the general alias.
	// Without the pin, "rotate onto the account provider" reads as an
	// imperative work request and routes to quickplan; with the cheap head
	// 503ing, the classifier retries (~90s) plus planner endpoint-block
	// backoff then overrun the turn timeout before rotation can be observed.
	for _, p := range cfg.providers {
		p.stub.setClassifierOut(`{"intent":"chat","confidence":0.9,"reasoning":"pool request-fault drive"}`)
	}
	s := newParitySandbox(t, cfg)

	sessID := s.createSession(t, "pool-400")

	// Force rotation onto provacct (fail the cheap head briefly).
	stubOther := cfg.providers[0].stub
	stubOther.setMode("model-other", "status:503")
	if reply := s.chatTurn(t, sessID, "what model answers this turn?", 180*time.Second); reply == "" {
		t.Fatal("rotation turn returned empty reply")
	}
	stubOther.setMode("model-other", "ok")

	// Inject ONE 400 on provacct/model-a1 and drive it.
	driveFailure(t, s, sessID,
		func() { stubAcct.setFaultOnce("model-a1", 400) },
		func() {},
		"req400")

	time.Sleep(250 * time.Millisecond) // failure-record settle
	// The eligibility probe: provacct is the ONLY healthy candidate now
	// (provother 503s). A 400 must not have armed anything, so the
	// resolver returns to provacct/model-a1 and the turn completes.
	// RED on main: the alias-level cooldown advances on ANY recorded
	// failure including 400s, so the resolve rotates onto the failing
	// provother and the turn cannot complete.
	stubOther.setMode("model-other", "status:503")
	defer stubOther.setMode("model-other", "ok")

	if reply := s.chatTurn(t, sessID, "post 400 eligibility probe", 180*time.Second); reply == "" {
		t.Fatalf("T13 R5 RED (expected): after a single 400, provacct/model-a1 was not eligible to serve — the request fault armed a block (no request-fault scope exists on main). daemon tail:\n%s", tailRunes(s.daemonLog(t), 2000))
	}
}

// TestPoolFailure_ClassifyTable (T14): the frozen pools.py:176-206 table,
// asserted verbatim through the exported classifier. Compile-RED until
// leaf 06 declares llm.ClassifyFailure (the symbol reference lives here).
func TestPoolFailure_ClassifyTable(t *testing.T) {
	rows := []struct {
		name     string
		provider string
		model    string
		pool     string
		status   int
		body     string
		cls      string
		scope    string
		target   string
	}{
		{"transport-status0", "p", "m", "", 0, "", "transient", "model", "m"},
		{"400", "p", "m", "", 400, `{"error":"bad"}`, "request_fault", "request", "-"},
		{"413", "p", "m", "", 413, "payload too large", "request_fault", "request", "-"},
		{"422", "p", "m", "", 422, "unprocessable", "request_fault", "request", "-"},
		{"404", "p", "m", "", 404, "not found", "entry_missing", "model", "m"},
		{"401", "p", "m", "", 401, "unauthorized", "account_dead", "account", "p"},
		{"403", "p", "m", "", 403, "forbidden", "account_dead", "account", "p"},
		{"402", "p", "m", "", 402, "payment required", "account_dead", "account", "p"},
		{"429-pool-cloudflare", "p", "m", "poolcf", 429, `{"error":{"code":4006,"message":"rate limit exceeded for pool"}}`, "congested", "pool", "poolcf"},
		{"429-pool-openrouter", "p", "m", "poolor", 429, `{"error":{"message":"Rate limit exceeded: free-models-per-day"}}`, "congested", "pool", "poolor"},
		{"429-model", "p", "m", "poolcf", 429, `{"error":{"message":"too many requests for this model"}}`, "congested", "model", "m"},
		{"500", "p", "m", "", 500, "internal error", "transient", "model", "m"},
		{"503", "p", "m", "", 503, "unavailable", "transient", "model", "m"},
		{"unmapped-418", "p", "m", "", 418, "teapot", "transient", "model", "m"},
	}
	for _, r := range rows {
		cls, scope, target := llmClassifyFailureForGate(r.provider, r.model, r.pool, r.status, r.body)
		if cls != r.cls || scope != r.scope || target != r.target {
			t.Fatalf("T14 classification table drift at %s: got (%s, %s, %s), want (%s, %s, %s) — pools.py:176-206 verbatim",
				r.name, cls, scope, target, r.cls, r.scope, r.target)
		}
	}
}

// tailRunes returns at most n trailing runes of s.
func tailRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// llmClassifyFailureForGate is the single symbol seam to Contract C5.
// Leaf 06 landed: this delegates to the real llm.ClassifyFailure, keeping
// the raw strings here so the frozen table stays verbatim in this file
// (contracts_test.go carries the typed go-identifiers).
var llmClassifyFailureForGate = func(providerID, modelID, poolID string, status int, bodyExcerpt string) (cls, scope, target string) {
	c, s, t := llm.ClassifyFailure(providerID, modelID, poolID, status, bodyExcerpt)
	return string(c), string(s), t
}

// ensure the table rows all exercise string forms (compile-time pin that
// scope/class names stay the frozen lowercase vocabulary).
var _ = strings.ToLower
