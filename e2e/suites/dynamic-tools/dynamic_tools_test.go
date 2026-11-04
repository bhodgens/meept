//go:build e2e

// Capability-gate + flag-off RED gates for the as-needed tool-exposure design
// (branch 03 leaf 00T; design docs/research/dynamic-tools-design.md §5.2, §4.3).
//
// Every case boots the three-model capability fixture (00D §5.1) through the
// hand-rolled sandbox and drives the orchestrator's Integration Test Plan
// cases 1-4 through the EXISTING per-request model-override seam (the design
// §2.2 "mid-session switch" vehicle: chat.submit "model" ->
// agent.applyRequestModel -> loop.ApplyRequestModel -> executor body model).
// No implementation symbol (llm.CapFrontier, EffectiveExposureMode, the
// warn-once downgrade, the predicted-budget payload, tool_search) is
// referenced, so the suite compiles against CURRENT code and each capability-
// gate case fails RED for a missing-implementation reason.
package dynamictools

import (
	"strings"
	"testing"
	"time"
)

// agentToolsSearchOn is the [agent.tools] overlay that would arm the as-needed
// exposure engine in "search" mode. The keys are UNKNOWN to the current
// non-strict JSON5 loader, so they are dropped at boot and the boot stays
// clean — the RED is behavioral (the gate / warn / predicted payload never
// fire), not a config-validation failure.
const agentToolsSearchOn = `{
  "dynamic_tool_exposure": true,
  "tool_exposure": "search",
  "predicted_budget": 25
}`

// agentToolsOff pins the master switch explicitly off (the safe default the
// byte-identical case guards: with the flag off the payload is the stable
// legacy enumeration, never reshaped mid-turn).
const agentToolsOff = `{ "dynamic_tool_exposure": false }`

const turnTimeout = 45 * time.Second

// TestDynamicToolsFlagOffByteIdentical guards the safe default (00T Goal 1,
// 00D §3): with the dynamic knob off, the tool definitions offered on every
// turn are the STATIC legacy payload — byte-identical across consecutive
// turns, never append/reshape-spliced. This is the GREEN control both before
// and after the 01..07 leaves land; the dynamic modes must not regress it.
// (Today it PASSES trivially: the [agent.tools] overlay keys are dropped by
// the loader, so "off" and "on" are indistinguishable and the legacy payload
// is stable — exactly the invariant this case pins.)
func TestDynamicToolsFlagOffByteIdentical(t *testing.T) {
	s := newSandbox(t,
		withAgentTools(agentToolsOff),
		withDefaultModel("fake/frontier-flagship"),
	)
	session := s.createSession("flagoff")

	before1 := s.Fake.RequestCount()
	s.submitTurn(session, "write the gate probe file", "")
	payload1 := s.awaitNewToolRequest("", before1, turnTimeout)

	before2 := s.Fake.RequestCount()
	s.submitTurn(session, "write the gate probe file again", "")
	payload2 := s.awaitNewToolRequest("", before2, turnTimeout)

	// The byte-identical invariant: two consecutive flag-off turns offer the
	// SAME static tool payload (no dynamic append/splice churn mid-episode).
	if !payloadEqual(payload1, payload2) {
		t.Errorf("flag-off payload is NOT byte-identical across consecutive turns; the legacy default reshapes mid-episode.\nfirst=%v\nsecond=%v",
			toolNamesFromBody(payload1), toolNamesFromBody(payload2))
	}
}

// TestCapabilityGate_SearchDowngradedForNonFrontier is Integration Test Plan
// case 1 (00D §5.2.1): configured `search` + a NON-frontier active model ->
// effective mode `predicted` + EXACTLY ONE warn log line naming the model;
// repeat downgrades for the same model are not re-logged (warn-once).
//
// RED today: the "not frontier-class; search downgraded to predicted" marker
// does not exist in current code, so the expected exactly-one warn never
// appears. The active non-frontier model is pinned via the per-request
// override seam so the frag match is deterministic.
func TestCapabilityGate_SearchDowngradedForNonFrontier(t *testing.T) {
	s := newSandbox(t,
		withAgentTools(agentToolsSearchOn),
		withDefaultModel("fake/frontier-flagship"),
	)
	session := s.createSession("downgrade")

	// Two consecutive turns riding the NON-frontier model.
	before := s.Fake.RequestCount()
	s.submitTurn(session, "write the gate probe file", "fake/local-mini")
	s.awaitNewToolRequest("local-mini", before, turnTimeout)

	before2 := s.Fake.RequestCount()
	s.submitTurn(session, "write the gate probe file again", "fake/local-mini")
	s.awaitNewToolRequest("local-mini", before2, turnTimeout)

	// Exactly one downgrade warn naming the model, no re-log on repeat.
	if n := s.logLineCount(warnNotFrontierPhrase); n != 1 {
		t.Errorf("case 1: expected exactly ONE 'not frontier-class; search downgraded to predicted' warn line for the non-frontier model; got %d (RED: the capability-gate downgrade seam is not implemented yet).\nlines=%v",
			n, s.logLinesContaining(warnNotFrontierPhrase))
	}
	if !s.warnNamesModel("local-mini") {
		t.Errorf("case 1: the downgrade warn (if any) must NAME the downgraded model 'local-mini'; the warn-once-per-active-model contract is not met. (RED: seam not implemented.)\nlines=%v",
			s.logLinesContaining(warnNotFrontierPhrase))
	}
}

// TestCapabilityGate_MidSessionSwitchRevertsToD is Integration Test Plan case
// 2 (00D §5.2.2): a mid-session frontier->lesser model switch with `search`
// configured -> the effective mode REVERTS to D (the predicted append) ON the
// switch, not at the next episode.
//
// RED today: the "one warn naming the model" on the switch does not exist.
func TestCapabilityGate_MidSessionSwitchRevertsToD(t *testing.T) {
	// Frontier default with search active: the frontier turn is fine (no
	// downgrade), then a mid-session switch to the lesser model.
	s := newSandbox(t,
		withAgentTools(agentToolsSearchOn),
		withDefaultModel("fake/frontier-flagship"),
	)
	session := s.createSession("switch")

	// Turn 1: the frontier model, search active — no downgrade.
	before := s.Fake.RequestCount()
	s.submitTurn(session, "write the gate probe file", "fake/frontier-flagship")
	s.awaitNewToolRequest("frontier-flagship", before, turnTimeout)

	// Turn 2 (mid-session switch to the lesser model via the override seam).
	before2 := s.Fake.RequestCount()
	s.submitTurn(session, "write the gate probe file again", "fake/local-mini")
	body := s.awaitNewToolRequest("local-mini", before2, turnTimeout)

	if m, _ := body["model"].(string); !strings.Contains(m, "local-mini") {
		t.Errorf("case 2: the switched executor request did not ride the lesser model; recorded model=%q (the per-request model override did not reach the executor body)", m)
	}
	// On the switch the effective mode must have reverted to D and logged
	// exactly ONE downgrade warn naming the lesser model.
	if n := s.logLineCount(warnNotFrontierPhrase); n != 1 {
		t.Errorf("case 2: the frontier->lesser switch must revert effective mode to D and log exactly ONE downgrade warn naming 'local-mini'; got %d (RED: the on-switch revert seam is not implemented yet).\nlines=%v",
			n, s.logLinesContaining(warnNotFrontierPhrase))
	}
	if !s.warnNamesModel("local-mini") {
		t.Errorf("case 2: the on-switch downgrade warn must name the lesser model 'local-mini'. (RED: seam not implemented.)\nlines=%v",
			s.logLinesContaining(warnNotFrontierPhrase))
	}
}

// TestCapabilityGate_RestoreOnlyAtEpisodeBoundary is Integration Test Plan
// case 3 (00D §5.2.3) — THE FLAP REGRESSION this branch must never ship: a
// lesser->frontier switch back restores `search` ONLY at the next episode
// boundary, not mid-episode. Repeated flapping switches must NOT churn the
// payload or re-log.
//
// RED today: there is no exposure engine, so the "stays reverted mid-episode
// then restores at the boundary" behavior is not observable; the pinned
// boundary-restore marker is absent.
func TestCapabilityGate_RestoreOnlyAtEpisodeBoundary(t *testing.T) {
	s := newSandbox(t,
		withAgentTools(agentToolsSearchOn),
		withDefaultModel("fake/frontier-flagship"),
	)
	episode1 := s.createSession("flap-episode-1")

	// frontier -> lesser (revert to D; case-2 behavior).
	b1 := s.Fake.RequestCount()
	s.submitTurn(episode1, "write the gate probe file", "fake/local-mini")
	s.awaitNewToolRequest("local-mini", b1, turnTimeout)

	// lesser -> frontier switch back WITHIN the same episode: search must stay
	// reverted (no mid-episode restore, no flap, no re-log).
	b2 := s.Fake.RequestCount()
	s.submitTurn(episode1, "write the gate probe file again", "fake/frontier-flagship")
	s.awaitNewToolRequest("frontier-flagship", b2, turnTimeout)

	// The flap regression: the frontier->lesser downgrade logged exactly ONCE
	// for local-mini; the switch-back within the episode must NOT add a second
	// warn or churn the payload.
	if n := s.logLineCount(warnNotFrontierPhrase); n != 1 {
		t.Errorf("case 3 (flap regression): the frontier->lesser downgrade must log exactly ONCE and the switch-back must NOT re-log or flap; got %d (RED: the episode-boundary restore + no-flap seam is not implemented yet).\nlines=%v",
			n, s.logLinesContaining(warnNotFrontierPhrase))
	}

	// Next episode boundary: a fresh session on the frontier default resumes
	// search (no new downgrade warn fires for the frontier model). The
	// boundary-restore marker is absent today -> the whole case REDs on the
	// count above regardless.
	episode2 := s.createSession("flap-episode-2")
	b3 := s.Fake.RequestCount()
	s.submitTurn(episode2, "write the gate probe file", "fake/frontier-flagship")
	s.awaitNewToolRequest("frontier-flagship", b3, turnTimeout)
}

// TestCapabilityGate_UnknownModelIsNonFrontier is Integration Test Plan case
// 4 (00D §5.1/§5.2.4): an unknown / unclassified model (no capability tags)
// is treated as NON-frontier, so `search` downgrades for it too.
//
// RED today: the "not frontier-class; search downgraded to predicted" warn
// for the unclassified model does not exist.
func TestCapabilityGate_UnknownModelIsNonFrontier(t *testing.T) {
	s := newSandbox(t,
		withAgentTools(agentToolsSearchOn),
		withDefaultModel("fake/frontier-flagship"),
	)
	session := s.createSession("unknown-model")

	// Pin the executor to the unclassified model via the override seam.
	before := s.Fake.RequestCount()
	s.submitTurn(session, "write the gate probe file", "fake/mystery-lab")
	s.awaitNewToolRequest("mystery-lab", before, turnTimeout)

	// An unclassified model is non-frontier: the downgrade warn must fire for
	// it, naming it.
	if n := s.logLineCount(warnNotFrontierPhrase); n != 1 {
		t.Errorf("case 4: an unknown/unclassified model must be treated as NON-frontier and downgrade `search` with exactly ONE warn naming 'mystery-lab'; got %d (RED: the unknown-model-is-non-frontier seam is not implemented yet).\nlines=%v",
			n, s.logLinesContaining(warnNotFrontierPhrase))
	}
	if !s.warnNamesModel("mystery-lab") {
		t.Errorf("case 4: the downgrade warn must NAME the unclassified model 'mystery-lab'. (RED: seam not implemented.)\nlines=%v",
			s.logLinesContaining(warnNotFrontierPhrase))
	}
}

// TestStarvationCanary_SearchViewPromotion (00T Goal 3, 00D §6) drives an e2e
// chat that needs a NON-core tool (one outside DefaultAlwaysFullTools) and
// must recover it via the always-full meta-tool pair tool_search ->
// tool_view promotion.
//
// TODO(dynamic-tools 01/04/05 impl seam): tool_search does NOT exist in the
// registry today (confirmed: no `tool_search` in internal/tools/ or
// internal/config/), and the predicted-budget promotion mechanism
// (NewExposurePlan/Promote, 00D §3.1) lands in leaves 01/04/05. This canary
// therefore CANNOT be written against a real seam yet — it is todo-marked
// and will turn into a runnable RED once those leaves land. The assertion
// contract (a non-core tool reached through tool_search -> tool_view
// promotion still completes the turn; the meta pair is always present and
// always-full in dynamic modes) is pinned here so the implementation leaves
// know the shape the canary will take.
func TestStarvationCanary_SearchViewPromotion(t *testing.T) {
	t.Skip("TODO(dynamic-tools 01/04/05): the starvation canary needs the " +
		"always-full meta-tool pair (tool_search) and the predicted-budget " +
		"promotion seam (NewExposurePlan/Promote, 00D §3.1/§6), none of which " +
		"exist yet. Contract pinned: an e2e chat that requires a NON-core tool " +
		"(one not in config.DefaultAlwaysFullTools) must still complete by " +
		"recovering it via tool_search -> tool_view promotion; the meta pair " +
		"is always present and always-full in dynamic modes. Re-arm as a real " +
		"RED once the tool_search registration + promotion leaves land.")
}

// TestCacheContinuity_NoInvalidationWithinEpisode (00T Goal 4, 00D §4.3)
// asserts that three consecutive turns of one episode emit NO "prefix cache
// invalidated" line in the daemon log (the provider-agnostic absence
// assertion: no re-cache churn mid-episode; only a promotion or a model
// switch re-caches, once each).
//
// TODO(dynamic-tools 05 impl seam): the exposure engine's prefix re-cache /
// splice mechanism (SetSchemaMode swap + append-only promotion, 00D §3.2/
// §4.3) is the seam that makes this case meaningful — without it the "no
// invalidation within an episode" invariant is vacuous (there is nothing to
// churn). The pinned assertion is the ABSENCE of the "prefix cache
// invalidated" DEBUG line across three same-episode turns, which only becomes
// a real gate once the engine can legitimately re-cache on a promotion
// (exactly one event); this case verifies it does NOT re-cache on ordinary
// consecutive turns.
func TestCacheContinuity_NoInvalidationWithinEpisode(t *testing.T) {
	t.Skip("TODO(dynamic-tools 05): the cache-continuity gate needs the " +
		"exposure engine's bounded one-time prefix re-cache seam (00D §4.3: " +
		"a promotion splices into the sorted payload and fires 'prefix cache " +
		"invalidated' exactly once; a model switch is free). Without that seam " +
		"the 'three consecutive turns emit no prefix-cache-invalidation' " +
		"invariant is vacuous. Pinned contract: across three same-episode " +
		"turns the daemon log contains ZERO 'prefix cache invalidated' lines; " +
		"a single promotion is the only allowed re-cache event (exactly one). " +
		"Re-arm as a real RED once the SetSchemaMode-swap + append-only " +
		"promotion leaves land.")
}
