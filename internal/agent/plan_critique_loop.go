package agent

// TierComplex critique loop (tiered-iteration leaf 02).
//
// For TierComplex requests: draft the plan in the brainstorm dialect,
// critique it against PlanCritiqueInput evidence, refine, and self-seal
// when clean — no human required on the automatic path. Structure per the
// master: the CALLER composes evidence (PlanCritiqueInput, leaf 03), the
// critic judges, the planner writes. The critic runs on the same planner
// model/chatter for v1 (a critic_model slot is deliberately deferred —
// master open question 1, resolved).
//
// Degradation rules (never silent):
//   - Critic output malformed → fail-open (zero objections that round) +
//     critique_outcome{outcome=critic_fail} metric. A plan is never
//     blocked because the critic misformatted.
//   - Blocking objections prevent self-seal until fixed or rounds
//     exhaust; rounds exhausted with open blocking items → the draft
//     seals anyway with a ## Known Risks section (honest degradation).
//   - Advisory objections roll into ## Known Risks unconditionally.
//   - Planner draft/revise call fails (transport) → the flow reports
//     fallback and Plan() takes the legacy single-shot path with a Warn.
//     TierComplex never hard-fails the task because the fancy path is down.
//   - Compiler rejection of the sealed draft → the problems feed ONE
//     revise round, then the task fails honestly with the problems.
//
// Self-seal is gated by SetSelfSealEnabled (plans.self_seal_enabled,
// default FALSE — ships dark). Flag off: quick_plan TierComplex keeps
// leaf 01's single-shot fallback (Warn + tier_complex_fallback metric,
// gated in Plan()); plan mode runs the rounds but WAITS at the existing
// human seal step. Flag on: quick_plan self-seals with provenance
// "planner-self"; plan mode still presents the seal request — self-seal
// is the DEFAULT the user can override, not a bypass.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/id"
)

// sealProvenanceMetadataKey is the task.Metadata key stamping who sealed
// the draft: "planner-self" (autonomous critique-loop seal) or "user"
// (the human seal step). Audits use it to distinguish automatic from
// human plans (leaf 02: seal provenance).
const sealProvenanceMetadataKey = "sealed_by"

// sealProvenancePlannerSelf / sealProvenanceUser are the two provenance
// values (leaf 02: sealed_by: "planner-self" | "user").
const (
	sealProvenancePlannerSelf = "planner-self"
	sealProvenanceUser        = "user"
)

// critiqueRoundsUsedMetadataKey / knownRisksCountMetadataKey are the draft
// companions the leaf requires on task metadata next to the draft bag.
const (
	critiqueRoundsUsedMetadataKey = "critique_rounds_used"
	knownRisksCountMetadataKey    = "known_risks_count"
)

// critiqueOutcomeMetadataKey is the leaf-04 draft companion carrying the
// loop verdict ("clean" | "exhausted" | "critic_fail") so the plan-mode
// draft surface (plan.draft get / seal request) can present what the
// critique concluded, not just the counts.
const critiqueOutcomeMetadataKey = "critique_outcome"

// knownRisksHeading is the section the draft gains on rounds-exhausted
// sealing (and that advisory objections roll into unconditionally).
const knownRisksHeading = "## Known Risks"

// Critique flow actions (critiqueFlowResult.Action) and outcomes.
const (
	critiqueActionHandled   = "handled"    // flow completed the task end-to-end (self-seal + schedule); caller returns
	critiqueActionAwaitSeal = "await_seal" // rounds done; refined draft waits at the human seal step
	critiqueActionFallback  = "fallback"   // flow could not run; caller takes the legacy path

	critiqueOutcomeClean      = "clean"
	critiqueOutcomeExhausted  = "exhausted"
	critiqueOutcomeCriticFail = "critic_fail"
	// critiqueOutcomeHostileSkipped stamps a round whose hostile pass was
	// SKIPPED (contract C5: flag on but chatter nil or same model ref). A
	// skipped hostile pass is not a clean seal - the draft stays at the human
	// seal and the self-seal path treats it like an open blocking item.
	critiqueOutcomeHostileSkipped = "hostile_skipped"
)

// SetSelfSealEnabled toggles the planner's autonomous self-seal
// (plans.self_seal_enabled, default false — ships dark). Threaded from
// daemon.go next to the SetPlanCompilerEnabled site. Nil-guarded per the
// repo's setter convention.
func (sp *StrategicPlanner) SetSelfSealEnabled(enabled bool) {
	if sp == nil {
		return
	}
	sp.selfSealEnabled = enabled
}

// SetFreshCriticEnabled gates the fresh-context evidence critic
// (plans.fresh_critic_enabled, default false - ships dark). Flag off:
// round 1 stays on runCriticOnce / the planner loop, byte-identical.
// Flag on: round 1 runs through runIsolatedCritic (a new child AgentLoop
// with no tools and no transcript; plan_critic_child.go). Nil-guarded per
// the repo's setter convention.
func (sp *StrategicPlanner) SetFreshCriticEnabled(enabled bool) {
	if sp == nil {
		return
	}
	sp.freshCriticEnabled = enabled
}

// SetCriticChatter wires the optional round-1 critic model (the C2
// "critic_model" slot's chatter). Nil (the production default) = the
// planner chatter is allowed for round 1 ONLY. The hostile pass (leaf 03)
// must never fall back onto this. Nil-guarded.
func (sp *StrategicPlanner) SetCriticChatter(ch llm.Chatter) {
	if sp == nil {
		return
	}
	sp.criticChatter = ch
}

// freshCriticFlag reads the fresh-critic flag nil-safely.
func (sp *StrategicPlanner) freshCriticFlag() bool {
	if sp == nil {
		return false
	}
	return sp.freshCriticEnabled
}

// criticChatterFor reads the round-1 chatter seam nil-safely.
func (sp *StrategicPlanner) criticChatterFor() llm.Chatter {
	if sp == nil {
		return nil
	}
	return sp.criticChatter
}

// SetCritiqueMaxRounds threads plans.complex_max_critique_rounds into the
// planner. Values <= 0 are ignored (the config load boundary already
// clamped to the default 2 via NormalizePlansDefaults). Nil-guarded.
func (sp *StrategicPlanner) SetCritiqueMaxRounds(n int) {
	if sp == nil || n <= 0 {
		return
	}
	sp.maxCritiqueRounds = n
}

// selfSealFlag reads the autonomy flag nil-safely.
func (sp *StrategicPlanner) selfSealFlag() bool {
	if sp == nil {
		return false
	}
	return sp.selfSealEnabled
}

// critiqueRoundsCap reads the round cap nil-safely (default 2).
func (sp *StrategicPlanner) critiqueRoundsCap() int {
	if sp == nil || sp.maxCritiqueRounds <= 0 {
		return 2
	}
	return sp.maxCritiqueRounds
}

// criticObjection is one item of the critic's output contract: a JSON
// array of `{section, objection, severity}` objects referencing draft
// sections, severity ∈ {blocking, advisory}. Unknown severities downgrade
// to advisory so a critic typo can never block a seal; an unparsable
// payload is fail-open (zero objections + metric) at the call site.
type criticObjection struct {
	Section   string `json:"section"`
	Objection string `json:"objection"`
	Severity  string `json:"severity"`
}

// blocking reports whether the objection carries blocking severity.
func (o criticObjection) blocking() bool {
	return strings.EqualFold(strings.TrimSpace(o.Severity), "blocking")
}

// render renders the objection as a single prompt line.
func (o criticObjection) render() string {
	severity := strings.TrimSpace(o.Severity)
	if severity == "" {
		if o.blocking() {
			severity = "blocking"
		} else {
			severity = "advisory"
		}
	}
	section := strings.TrimSpace(o.Section)
	if section != "" {
		return fmt.Sprintf("- [%s] section %q: %s", severity, section, o.Objection)
	}
	return fmt.Sprintf("- [%s] %s", severity, o.Objection)
}

// parseCriticObjections extracts the objections array from critic output.
// The critic is prompted to wrap its array as {"objections": [...]}
// (object-shaped, like every other planner contract); a bare JSON array
// is also accepted. Returns an error when neither shape parses — the
// caller treats that as fail-open.
func parseCriticObjections(raw string) ([]criticObjection, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, fmt.Errorf("critic output empty")
	}
	if jsonStr := ExtractJSON(raw); jsonStr != "" {
		var envelope struct {
			Objections *[]criticObjection `json:"objections"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &envelope); err == nil && envelope.Objections != nil {
			return *envelope.Objections, nil
		}
	}
	var direct []criticObjection
	if err := json.Unmarshal([]byte(trimmed), &direct); err != nil {
		return nil, fmt.Errorf("critic output failed to parse: %w", err)
	}
	return direct, nil
}

// splitObjections partitions objections into blocking and advisory lists.
func splitObjections(objections []criticObjection) (blocking, advisory []criticObjection) {
	for _, o := range objections {
		if o.blocking() {
			blocking = append(blocking, o)
		} else {
			advisory = append(advisory, o)
		}
	}
	return blocking, advisory
}

// Contest record tags (fresh-context-critic leaf 04, contract C6). The
// tag is visible text on the Known Risks line, not a side-channel: a
// human reviewing the draft sees exactly which objections were settled
// by a quoted contest and which contests failed the quote check.
const (
	contestTagSettled  = "contested-settled"
	contestTagUnproven = "contested-unproven"
)

// criticRebuttal is contract C6 (leaf 04): one rebuttal the planner MAY
// attach to its revise output. Verdict is accept | contest. A contest
// counts only when Evidence is a verbatim substring of the
// PlanCritiqueInput.Render() text the critic actually saw; anything else
// is contested-unproven - never a silent accept, never an answered
// objection.
type criticRebuttal struct {
	Section  string `json:"section"`
	Targets  string `json:"targets"`  // objection text being contested
	Evidence string `json:"evidence"` // must quote a PlanCritiqueInput section
	Verdict  string `json:"verdict"`  // accept | contest
}

// parseCriticRebuttals parses the optional C6 rebuttal array from the
// planner's revise output. The reply is markdown-first: rebuttals ride
// an optional JSON block the planner may emit before or after the
// markdown (a fenced ```json block with {"rebuttals": [...]} or a bare
// single-rebuttal object). Parse DEFENSIVELY: absent or unparsable =
// nil rebuttals, which keeps the no-rebuttal path byte-identical to
// today's "address every objection" behavior.
func parseCriticRebuttals(raw string) []criticRebuttal {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	jsonStr := ExtractJSON(raw)
	if jsonStr == "" {
		return nil
	}
	var envelope struct {
		Rebuttals []criticRebuttal `json:"rebuttals"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &envelope); err == nil && len(envelope.Rebuttals) > 0 {
		return envelope.Rebuttals
	}
	// Single-rebuttal shape (e.g. an inline <!-- contest: {...} -->
	// comment). Evidence must be present for it to be a rebuttal at all;
	// anything else in the markdown that happens to parse as JSON with
	// no evidence is ignored.
	var single criticRebuttal
	if err := json.Unmarshal([]byte(jsonStr), &single); err == nil && strings.TrimSpace(single.Evidence) != "" {
		return []criticRebuttal{single}
	}
	return nil
}

// objectionKey is the cross-round identity of an objection for the
// contest ledger (section + objection text, case-insensitive).
func objectionKey(o criticObjection) string {
	return strings.ToLower(strings.TrimSpace(o.Section)) + "|" + strings.ToLower(strings.TrimSpace(o.Objection))
}

// rebuttalTargetKey is the ledger key a rebuttal aims at: its Targets
// text under objectionKey's normalization.
func rebuttalTargetKey(rb criticRebuttal) string {
	return objectionKey(criticObjection{Section: rb.Section, Objection: rb.Targets})
}

// tagObjection returns a copy of the objection annotated with a contest
// tag for the Known Risks record.
func tagObjection(o criticObjection, tag string) criticObjection {
	o.Objection = strings.TrimSpace(o.Objection) + " [" + tag + "]"
	return o
}

// contestEvidenceHolds is the C6 quote check: a contest counts only when
// its Evidence string is a verbatim substring of the rendered critique
// input the critic actually saw. A wish to contest is not evidence; this
// is a substring check, never a model judgment.
func contestEvidenceHolds(rb criticRebuttal, evidenceText string) bool {
	evidence := strings.TrimSpace(rb.Evidence)
	if evidence == "" || evidenceText == "" {
		return false
	}
	return strings.Contains(evidenceText, evidence)
}

// matchContestedObjection finds the objection a rebuttal targets in the
// list that drove its revise round. ok=false when no objection matches
// (the contest aims at nothing currently open).
func matchContestedObjection(objections []criticObjection, rb criticRebuttal) (criticObjection, bool) {
	target := rebuttalTargetKey(rb)
	for _, o := range objections {
		if objectionKey(o) == target {
			return o, true
		}
	}
	return criticObjection{}, false
}

// dropContestedSettled removes objections already settled by a valid
// contest earlier in this flow (C6: they do NOT re-block in later
// rounds; they stay recorded as tagged ledger entries).
func dropContestedSettled(objections []criticObjection, settled map[string]bool) []criticObjection {
	if len(objections) == 0 || len(settled) == 0 {
		return objections
	}
	kept := make([]criticObjection, 0, len(objections))
	for _, o := range objections {
		if settled[objectionKey(o)] {
			continue
		}
		kept = append(kept, o)
	}
	return kept
}

// applyContestRebuttals decides the C6 contests parsed from one revise
// output against the objections that drove that revise. A VALID contest
// (Evidence is a verbatim substring of evidenceText) settles its target:
// recorded tagged contested-settled, and it does not re-block in later
// rounds of this flow. An INVALID contest is recorded tagged
// contested-unproven and the objection stays blocking - never silently
// accepted, never counted as answered. verdict=accept rebuttals are the
// planner choosing to fix the objection; nothing to record.
func applyContestRebuttals(rebuttals []criticRebuttal, objections []criticObjection, evidenceText string, settled map[string]bool, records *[]criticObjection, recordKeys map[string]bool) {
	for _, rb := range rebuttals {
		if !strings.EqualFold(strings.TrimSpace(rb.Verdict), "contest") {
			continue
		}
		record, ok := matchContestedObjection(objections, rb)
		if !ok {
			record = criticObjection{Section: rb.Section, Objection: rb.Targets, Severity: "blocking"}
		}
		if contestEvidenceHolds(rb, evidenceText) {
			settled[rebuttalTargetKey(rb)] = true
			*records = append(*records, tagObjection(record, contestTagSettled))
		} else {
			*records = append(*records, tagObjection(record, contestTagUnproven))
		}
		recordKeys[objectionKey(record)] = true
	}
}

// mergeContestRecords folds the ledger's tagged contest records into the
// Known Risks list: a record REPLACES its untagged duplicate so the tag
// (contested-settled | contested-unproven) is what survives on the
// draft, and the objection is never silently dropped.
func mergeContestRecords(known, records []criticObjection, recordKeys map[string]bool) []criticObjection {
	if len(records) == 0 {
		return known
	}
	kept := make([]criticObjection, 0, len(known)+len(records))
	for _, o := range known {
		if recordKeys[objectionKey(o)] {
			continue
		}
		kept = append(kept, o)
	}
	return append(kept, records...)
}

// draftToolHintRe extracts tool hints from draft step lines
// ("1. Do the thing [code]"). Same hint vocabulary as the compiler's
// step-line grammar.
var draftToolHintRe = regexp.MustCompile(`^\s*[0-9]+\.\s+.+?\[([a-z_]+)\]`)

// draftToolHints collects the tool hints named in a draft's step lines,
// preserving first-seen order and deduplicating case-insensitively.
func draftToolHints(markdown string) []string {
	seen := make(map[string]bool)
	var hints []string
	for line := range strings.SplitSeq(markdown, "\n") {
		m := draftToolHintRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		lower := strings.ToLower(m[1])
		if lower == "" || seen[lower] {
			continue
		}
		seen[lower] = true
		hints = append(hints, lower)
	}
	return hints
}

// syntheticObjectionsFromHints converts the leaf-03 assembly-time blocking
// objections into critic objections so both classes share one refine path.
func syntheticObjectionsFromHints(objections []critiqueObjection) []criticObjection {
	out := make([]criticObjection, 0, len(objections))
	for _, o := range objections {
		out = append(out, criticObjection{
			Section:   o.Phase,
			Objection: o.String(),
			Severity:  "blocking",
		})
	}
	return out
}

// appendKnownRisks appends (or extends) the ## Known Risks section with
// the given objections. Idempotent per loop run: a draft that already
// carries the section gets the items merged without duplicating the
// heading.
func appendKnownRisks(markdown string, objections []criticObjection) string {
	if len(objections) == 0 {
		return markdown
	}
	var sb strings.Builder
	sb.WriteString(strings.TrimRight(markdown, "\n"))
	sb.WriteString("\n\n")
	sb.WriteString(knownRisksHeading)
	sb.WriteString("\n\n")
	for _, o := range objections {
		sb.WriteString(o.render())
		sb.WriteByte('\n')
	}
	return sb.String()
}

// critiqueFlowResult reports what one critique-flow invocation decided.
// Plan() branches on Action:
//   - handled: the flow sealed and scheduled the task end-to-end
//     (quick_plan + self-seal). The caller returns without the shared
//     steps tail — everything is already persisted.
//   - await_seal: rounds finished; the refined draft + critique summary
//     wait at the existing human seal step (plan mode, any flag). The
//     caller returns; the task stays in planning.
//   - fallback: the flow could not run (draft/revise transport failure);
//     the caller takes leaf 01's legacy single-shot path with a Warn.
type critiqueFlowResult struct {
	Action          string
	CompiledPhases  []plan.PhaseSpec
	RoundsUsed      int
	KnownRisks      int
	CritiqueSummary string
}

// critiqueSummaryText renders the user-facing critique summary presented
// with the draft at the seal step (plan mode: the human reviews a refined
// draft plus the verdict, not a first attempt).
func critiqueSummaryText(outcome string, roundsUsed int, open []criticObjection) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Critique outcome: %s after %d round(s).", outcome, roundsUsed)
	if len(open) > 0 {
		sb.WriteString(" Open items (carried into Known Risks):\n")
		for _, o := range open {
			sb.WriteString(o.render())
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// PlanCritiqueFlow runs the TierComplex draft→critique→refine loop for a
// request (tiered-iteration leaf 02). See the file comment for the
// degradation rules and the mode/flag gating table.
func (sp *StrategicPlanner) PlanCritiqueFlow(ctx context.Context, req PlanRequest, input *PlanCritiqueInput) (*critiqueFlowResult, error) {
	if sp == nil || sp.taskStore == nil || sp.registry == nil {
		return nil, fmt.Errorf("critique flow not available")
	}
	t, err := sp.taskStore.GetByID(req.TaskID)
	if err != nil || t == nil {
		return nil, fmt.Errorf("task not found: %s", req.TaskID)
	}
	plannerLoop, err := sp.registry.Get(config.AgentIDPlanner)
	if err != nil {
		return nil, fmt.Errorf("planner agent not available: %w", err)
	}

	// Ensure a draft exists (plan mode's Plan() branch seeds one; the
	// quick_plan autonomy path enters here without one).
	draft, ok := sp.draftFromMetadata(t)
	if !ok {
		if seedErr := sp.seedDraftFromRequest(req.TaskID, req.Input); seedErr != nil {
			return nil, seedErr
		}
		if t, err = sp.taskStore.GetByID(req.TaskID); err != nil || t == nil {
			return nil, fmt.Errorf("task not found after seed: %s", req.TaskID)
		}
		if draft, ok = sp.draftFromMetadata(t); !ok {
			return nil, fmt.Errorf("no draft after seed for task %s", req.TaskID)
		}
	}

	// --- Draft (master step 1): the planner fills the scaffold in the
	// brainstorm dialect. A transport failure here falls back to the
	// legacy path (TierComplex never hard-fails the task because the
	// fancy path is down).
	filled, err := sp.draftWithPlanner(ctx, plannerLoop, req, draft)
	if err != nil {
		sp.recordMetric("strategic_planner.tier_complex_fallback", 1, map[string]string{"reason": "draft_transport"})
		sp.logger.Warn("Critique loop draft call failed; falling back to legacy path",
			"task_id", req.TaskID,
			"error", err,
		)
		return &critiqueFlowResult{Action: critiqueActionFallback}, nil
	}
	draft = filled

	maxRounds := sp.critiqueRoundsCap()
	roundsUsed := 0
	criticFailed := false
	hostileSkipped := false
	var blocking, advisory []criticObjection
	// Contest ledger (leaf 04, C6): objections settled by a VALID quoted
	// contest do not re-block in later rounds of this flow; every decided
	// contest (settled or unproven) lands as a tagged record that
	// replaces its untagged duplicate in Known Risks.
	settledContests := make(map[string]bool)
	var contestRecords []criticObjection
	recordKeys := make(map[string]bool)

	for round := 1; round <= maxRounds; round++ {
		// --- Critique (master step 2) ---
		// Fresh-context-critic leaf 02: the branch point. Flag off keeps
		// runCriticOnce on the planner loop, byte-identical to the
		// tiered-iteration loop. Flag on, round 1 runs through
		// runIsolatedCritic: a NEW child AgentLoop with an empty tool
		// registry and an empty conversation that sees only the draft
		// markdown plus PlanCritiqueInput.Render(), and replies under the
		// C4 paraphrase envelope (a missing/empty paraphrase on a
		// non-empty draft is ONE synthetic blocking objection).
		var objections []criticObjection
		var criticErr error
		if sp.freshCriticFlag() {
			objections, criticErr = sp.runFreshCriticRound(ctx, draft, input)
		} else {
			critiquePrompt := sp.buildCriticPrompt(req, draft, input)
			objections, criticErr = sp.runCriticOnce(ctx, plannerLoop, critiquePrompt)
		}
		roundsUsed = round
		if criticErr != nil {
			// Fail-open: never block a plan because the critic
			// misformatted. Zero objections this round; the outcome
			// metric records critic_fail at the end of the flow.
			criticFailed = true
			objections = nil
		}

		blocking, advisory = splitObjections(objections)

		// The leaf-03 tool-coverage pre-check rides every round: its
		// objections are decided at assembly time (no critic call spent
		// on that class) and are blocking.
		blocking = append(blocking, syntheticObjectionsFromHints(input.ValidateToolHints(draftToolHints(draft.Markdown)))...)

		// Hostile second pass (fresh-context-critic leaf 03, contract
		// C5): a SECOND isolated child reader on a DIFFERENT model. It
		// runs only when the hostile flag is on AND the C5 gate fully
		// holds (chatter wired AND hostile model ref != planner model
		// ref); otherwise it is skipped with the skip metric and the
		// planner/critic chatters are never touched with the hostile
		// prompt. It is a reader, not an extra revise round: its
		// blocking objections join this round's blocking list so the
		// same cycle's reviseDraft addresses them, and a clean seal
		// requires both readers blocking-free in the same cycle.
		hostileSkippedReason := ""
		if sp.hostileCriticFlag() {
			if skip := hostilePassSkipReason(sp.hostileCriticChatterFor(), sp.plannerModelRef(plannerLoop)); skip != "" {
				hostileSkippedReason = skip
				sp.recordMetric("strategic_planner.hostile_critic_skipped", 1, map[string]string{"reason": skip})
			} else {
				hostileObjections, hostileErr := sp.runHostileCriticRound(ctx, draft, input)
				if hostileErr != nil {
					// Fail-open like round 1: never block a plan
					// because a critic misformatted. The transport
					// failure is logged, not silent.
					sp.logger.Warn("Hostile critic call failed; continuing with round-1 objections only",
						"task_id", req.TaskID,
						"error", hostileErr,
					)
				} else {
					hBlocking, hAdvisory := splitObjections(hostileObjections)
					blocking = append(blocking, hBlocking...)
					advisory = append(advisory, hAdvisory...)
				}
			}
		}
		hostileSkipped = hostileSkippedReason != ""

		// Contest ledger: settled objections do not re-block in later
		// rounds of this flow (C6). Applied BEFORE the clean-round check
		// so a fully settled round seals clean - the objections stay
		// recorded in the ledger as tagged entries.
		blocking = dropContestedSettled(blocking, settledContests)
		advisory = dropContestedSettled(advisory, settledContests)

		if len(blocking) == 0 {
			// Clean round. Advisory items still roll into Known Risks
			// (unconditional per the leaf), then the loop exits.
			break
		}

		// --- Refine (master step 3), unless this was the last round ---
		if round == maxRounds {
			break
		}
		reviseObjections := append(append([]criticObjection{}, blocking...), advisory...)
		// C6 evidence text: the exact Render() the critic saw for THIS
		// round - the quote a contest cites must come from what the
		// critic could actually have read.
		evidenceText := input.Render()
		revised, rebuttals, reviseErr := sp.reviseDraft(ctx, plannerLoop, req, draft, reviseObjections, "")
		if reviseErr != nil {
			sp.recordMetric("strategic_planner.tier_complex_fallback", 1, map[string]string{"reason": "revise_transport"})
			sp.logger.Warn("Critique loop revise failed; falling back to legacy path",
				"task_id", req.TaskID,
				"error", reviseErr,
			)
			return &critiqueFlowResult{Action: critiqueActionFallback}, nil
		}
		draft = revised
		// Contract C6: rebuttals ride an optional JSON block in the
		// revise output. No rebuttals -> this is a no-op and the
		// "address every objection" behavior is unchanged.
		applyContestRebuttals(rebuttals, reviseObjections, evidenceText, settledContests, &contestRecords, recordKeys)
	}

	// Rounds exhausted with open blocking items → the Known Risks section
	// carries them (honest degradation, never silent), then the draft
	// seals anyway.
	outcome := critiqueOutcomeClean
	if hostileSkipped {
		// Contract C5: a skipped hostile pass is NOT a clean seal. The
		// outcome stamps hostile_skipped (without overwriting the
		// exhausted/critic_fail verdicts - those still win when they
		// fired) and the self-seal path below refuses planner-self on
		// it: the draft stays at the human seal.
		outcome = critiqueOutcomeHostileSkipped
	}
	if len(blocking) > 0 {
		outcome = critiqueOutcomeExhausted
	} else if criticFailed {
		outcome = critiqueOutcomeCriticFail
	}
	known := append(append([]criticObjection{}, blocking...), advisory...)
	// Leaf 04: decided contests replace their untagged duplicates so the
	// contested-settled / contested-unproven tag survives on the draft.
	known = mergeContestRecords(known, contestRecords, recordKeys)
	if len(known) > 0 {
		draft.Markdown = appendKnownRisks(draft.Markdown, known)
	}
	// Leaf-04 critique summary on the draft itself: the presented draft
	// carries the loop's verdict (rounds, risks, outcome) so the human
	// reviewer sees it wherever the draft markdown travels — plan.draft
	// get, the seal request, the CLI — without a separate metadata read.
	draft.CritiqueRoundsUsed = roundsUsed
	draft.KnownRisksCount = len(known)
	draft.CritiqueOutcome = outcome
	draft.UpdatedAt = time.Now().UTC()
	draft.Version++
	if err := sp.storeDraft(t, draft); err != nil {
		return nil, fmt.Errorf("persist critiqued draft: %w", err)
	}

	sp.recordMetric("strategic_planner.critique_rounds", float64(roundsUsed), map[string]string{"rounds": fmt.Sprintf("%d", roundsUsed)})
	sp.recordMetric("strategic_planner.critique_outcome", 1, map[string]string{"outcome": outcome})
	sp.recordMetric("strategic_planner.known_risks", float64(len(known)), nil)

	// Draft companions (leaf: draft metadata carries critique_rounds_used,
	// known_risks_count, and — leaf 04 — the outcome verdict).
	if err := sp.stampCritiqueMetadata(req.TaskID, roundsUsed, len(known), outcome); err != nil {
		sp.logger.Warn("Failed to stamp critique metadata",
			"task_id", req.TaskID, "error", err)
	}

	// --- Compile (master step 5): the zero-LLM compiler. Rejection feeds
	// ONE revise round, then the task fails honestly with the problems.
	compiled, compileErr := plan.CompileSealed(draft.Markdown, sp.MaxPhases())
	if compileErr != nil {
		problems := compileProblemText(compileErr)
		revised, _, reviseErr := sp.reviseDraft(ctx, plannerLoop, req, draft, nil, problems)
		if reviseErr != nil {
			msg := fmt.Sprintf("plan compile failed (%s) and the compile-fix revise round also failed: %v", problems, reviseErr)
			sp.failTaskWithReason(req.TaskID, msg)
			return nil, fmt.Errorf("%s", msg)
		}
		revised.UpdatedAt = time.Now().UTC()
		revised.Version++
		if err := sp.storeDraft(t, revised); err != nil {
			return nil, fmt.Errorf("persist compile-revised draft: %w", err)
		}
		draft = revised
		compiled, compileErr = plan.CompileSealed(draft.Markdown, sp.MaxPhases())
		if compileErr != nil {
			msg := fmt.Sprintf("plan compile failed after the critique loop and one compile-fix revise round: %s", compileProblemText(compileErr))
			sp.failTaskWithReason(req.TaskID, msg)
			return nil, fmt.Errorf("%s", msg)
		}
	}

	// --- Mode/flag gating ---
	// quick_plan + flag on  → self-seal (full autonomy).
	// plan mode (any flag)  → wait at the human seal step; with the flag
	//                         on, the presented seal request carries
	//                         planner-self provenance as its default.
	// quick_plan + flag off → this flow is never entered (Plan() keeps
	//                         leaf 01's single-shot fallback).
	if req.Mode != "quick_plan" {
		provenance := sealProvenanceUser
		if sp.selfSealFlag() && !hostileSkipped {
			// Contract C5: a skipped-hostile draft is never handed a
			// planner-self default - it is treated like a draft with an
			// open blocking item and stays at the human seal.
			provenance = sealProvenancePlannerSelf
		}
		if err := sp.stampSealProvenance(req.TaskID, provenance); err != nil {
			sp.logger.Warn("Failed to stamp seal provenance",
				"task_id", req.TaskID, "error", err)
		}
		sp.recordMetric("strategic_planner.seal_provenance", 1, map[string]string{"by": provenance, "stage": "proposed"})
		return &critiqueFlowResult{
			Action:          critiqueActionAwaitSeal,
			CompiledPhases:  compiled.Phases,
			RoundsUsed:      roundsUsed,
			KnownRisks:      len(known),
			CritiqueSummary: critiqueSummaryText(outcome, roundsUsed, known),
		}, nil
	}

	// Self-seal: stamp provenance planner-self, then seal through the
	// existing seal pipeline (SealDraft's sealed-hash pattern, then
	// SealPlan: persist steps → spec → executing → promote → schedule).
	// Contract C5: a skipped-hostile draft does NOT self-seal - it stays
	// at the human seal (plan mode path above), stamped hostile_skipped.
	if hostileSkipped {
		sp.recordMetric("strategic_planner.seal_provenance", 1, map[string]string{"by": sealProvenanceUser, "stage": "hostile_skipped"})
		return &critiqueFlowResult{
			Action:          critiqueActionAwaitSeal,
			CompiledPhases:  compiled.Phases,
			RoundsUsed:      roundsUsed,
			KnownRisks:      len(known),
			CritiqueSummary: critiqueSummaryText(outcome, roundsUsed, known),
		}, nil
	}
	if err := sp.stampSealProvenance(req.TaskID, sealProvenancePlannerSelf); err != nil {
		sp.logger.Warn("Failed to stamp self-seal provenance",
			"task_id", req.TaskID, "error", err)
	}
	if err := sp.SealDraft(req.TaskID, compiled.Hash); err != nil {
		return nil, fmt.Errorf("self-seal draft: %w", err)
	}
	if err := sp.SealPlan(ctx, req.TaskID, PhaseSpecsFromPlan(compiled.Phases), nil); err != nil {
		return nil, fmt.Errorf("self-seal plan: %w", err)
	}
	sp.recordMetric("strategic_planner.seal_provenance", 1, map[string]string{"by": sealProvenancePlannerSelf, "stage": "sealed"})
	sp.logger.Info("TierComplex plan self-sealed by critique loop",
		"task_id", req.TaskID,
		"rounds_used", roundsUsed,
		"known_risks", len(known),
		"outcome", outcome,
	)
	return &critiqueFlowResult{
		Action:          critiqueActionHandled,
		CompiledPhases:  compiled.Phases,
		RoundsUsed:      roundsUsed,
		KnownRisks:      len(known),
		CritiqueSummary: critiqueSummaryText(outcome, roundsUsed, known),
	}, nil
}

// runCritiqueFlowForPlan is Plan()'s integration seam: it assembles the
// leaf-03 evidence base from the planner's live handles and enters
// PlanCritiqueFlow. A nil ValidTools set (no registry wired) disables the
// tool-coverage pre-check rather than flagging every hint.
func (sp *StrategicPlanner) runCritiqueFlowForPlan(ctx context.Context, req PlanRequest) (*critiqueFlowResult, error) {
	sources := CritiqueInputSources{
		StepStore: sp.stepStore,
		Registry:  sp.critiqueToolRegistry,
		SessionID: req.SessionID,
	}
	input, err := BuildPlanCritiqueInput(sources)
	if err != nil {
		sp.logger.Warn("Critique evidence assembly failed; flow runs without evidence",
			"task_id", req.TaskID, "error", err)
		input = &PlanCritiqueInput{}
	}
	return sp.PlanCritiqueFlow(ctx, req, input)
}

// SetCritiqueToolRegistry wires the production tool registry the
// tool-coverage pre-check reads (internal/daemon/components.go beside the
// SetValidToolNames site — the same *tools.Registry instance). Nil-guarded.
func (sp *StrategicPlanner) SetCritiqueToolRegistry(reg *tools.Registry) {
	if sp == nil {
		return
	}
	sp.critiqueToolRegistry = reg
}

// SetCritiqueChatter wires the raw chatter the critique loop's
// draft/critic/revise calls run through. Nil (the production default) =
// the planner agent loop. Nil-guarded.
func (sp *StrategicPlanner) SetCritiqueChatter(ch llm.Chatter) {
	if sp == nil {
		return
	}
	sp.critiqueChatter = ch
}

// critiqueChatterFor reads the seam nil-safely.
func (sp *StrategicPlanner) critiqueChatterFor() llm.Chatter {
	if sp == nil {
		return nil
	}
	return sp.critiqueChatter
}

// draftWithPlanner makes the initial fill call: the planner renders the
// seeded scaffold into a full plan-dialect v1 document (goals/decisions/
// phases in prose) grounded in the request.
func (sp *StrategicPlanner) draftWithPlanner(ctx context.Context, plannerLoop *AgentLoop, req PlanRequest, scaffold *PlanDraft) (*PlanDraft, error) {
	var sb strings.Builder
	sb.WriteString("You are the planner. Fill the following plan draft scaffold as a complete plan-dialect v1 markdown document.\n")
	sb.WriteString("Keep the exact section skeleton (## Meta, ## Goal, ## Decisions, ## Open Questions (empty), ## Phases). ")
	sb.WriteString("Phase steps use the numbered form `1. description [hint]`.\n\n")
	sb.WriteString("## Request\n\n")
	sb.WriteString(req.Input)
	sb.WriteString("\n\n## Scaffold\n\n")
	sb.WriteString(scaffold.Markdown)
	sb.WriteString("\n\nRespond with ONLY the completed markdown document.")

	output, err := sp.runPlannerChat(ctx, plannerLoop, req, sb.String())
	if err != nil {
		return nil, err
	}
	md := strings.TrimSpace(output)
	if md == "" || !strings.Contains(md, "## Phases") {
		return nil, fmt.Errorf("planner draft output missing Phases section")
	}
	return &PlanDraft{Markdown: md, Version: scaffold.Version, UpdatedAt: time.Now().UTC()}, nil
}

// buildCriticPrompt renders the critic prompt: the draft plus the leaf-03
// evidence sections. The critic judges against ARTIFACT EVIDENCE, not
// vibes (master).
func (sp *StrategicPlanner) buildCriticPrompt(req PlanRequest, draft *PlanDraft, input *PlanCritiqueInput) string {
	var sb strings.Builder
	sb.WriteString("You are the plan critic. Evaluate the following plan draft against the evidence sections. ")
	sb.WriteString("Reply with ONLY a JSON object of the form {\"objections\": [{\"section\", \"objection\", \"severity\"}]} ")
	sb.WriteString("where section names a draft section, objection states the concrete problem, and severity is ")
	sb.WriteString("\"blocking\" (must be fixed before sealing) or \"advisory\" (worth noting). ")
	sb.WriteString("If the draft is sound, reply with {\"objections\": []}. No prose outside the JSON.\n\n")
	sb.WriteString("## Draft under critique\n\n")
	sb.WriteString(draft.Markdown)
	sb.WriteString("\n\n")
	if evidence := input.Render(); evidence != "" {
		sb.WriteString("## Evidence\n\n")
		sb.WriteString(evidence)
		sb.WriteString("\n\n")
	}
	sb.WriteString("## Request the draft must serve\n\n")
	sb.WriteString(req.Input)
	return sb.String()
}

// runCriticOnce runs one critic pass on the planner loop and parses the
// objections array. A transport error or unparsable output is surfaced to
// the caller, which fail-opens.
func (sp *StrategicPlanner) runCriticOnce(ctx context.Context, plannerLoop *AgentLoop, prompt string) ([]criticObjection, error) {
	output, err := sp.runPlannerChat(ctx, plannerLoop, PlanRequest{}, prompt)
	if err != nil {
		return nil, fmt.Errorf("critic call failed: %w", err)
	}
	return parseCriticObjections(output)
}

// revisePromptContestLine is the leaf-04 prompt line added to reviseDraft,
// after the existing objection list.
const revisePromptContestLine = "You may contest an objection only by quoting a sentence from the evidence sections. A wish to contest is not evidence. You wrote this draft."

// revisePromptRebuttalBlock documents the optional C6 rebuttal ride-along:
// the reply stays markdown-first; a planner that wants to contest may emit
// an extra fenced json block (before or after the markdown) shaped
// {"rebuttals": [{"section", "targets", "evidence", "verdict"}]}. Verdict
// is "accept" (the objection is being fixed) or "contest". A contest
// counts ONLY when its evidence quotes a sentence from the evidence
// sections rendered in the critic prompt; the loop verifies that quote
// mechanically and never takes a contest on faith.
const revisePromptRebuttalBlock = "To contest an objection, add ONE extra fenced ```json block containing {\"rebuttals\": [{\"section\": \"...\", \"targets\": \"the objection text\", \"evidence\": \"a verbatim sentence quoted from the evidence sections\", \"verdict\": \"contest\"}]}."

// reviseDraft asks the planner to produce a revised draft addressing the
// objection list (and, for the compile-fix path, the compiler's problems).
// The reply is markdown-first: rebuttals (leaf 04, contract C6) ride an
// optional JSON block and are stripped from the stored draft document.
func (sp *StrategicPlanner) reviseDraft(ctx context.Context, plannerLoop *AgentLoop, req PlanRequest, draft *PlanDraft, objections []criticObjection, compileProblems string) (*PlanDraft, []criticRebuttal, error) {
	var sb strings.Builder
	var rebuttals []criticRebuttal
	sb.WriteString("You are the planner. Revise the following plan draft to address every objection. ")
	sb.WriteString("Keep the plan-dialect v1 section skeleton (## Meta, ## Goal, ## Decisions, ## Open Questions (empty), ## Phases) ")
	sb.WriteString("and respond with ONLY the revised markdown document.\n\n")
	sb.WriteString("## Current draft\n\n")
	sb.WriteString(draft.Markdown)
	sb.WriteString("\n\n")
	if len(objections) > 0 {
		sb.WriteString("## Objections to address\n\n")
		for _, o := range objections {
			sb.WriteString(o.render())
			sb.WriteByte('\n')
		}
		sb.WriteString("\n")
		sb.WriteString(revisePromptContestLine)
		sb.WriteString("\n\n")
		sb.WriteString(revisePromptRebuttalBlock)
		sb.WriteString("\n\n")
	}
	if compileProblems != "" {
		sb.WriteString("## Compiler problems to fix\n\n")
		sb.WriteString(compileProblems)
		sb.WriteString("\n\n")
	}
	if req.Input != "" {
		sb.WriteString("## Request the draft must serve\n\n")
		sb.WriteString(req.Input)
		sb.WriteString("\n")
	}

	output, err := sp.runPlannerChat(ctx, plannerLoop, req, sb.String())
	if err != nil {
		return nil, nil, err
	}
	md := strings.TrimSpace(output)
	if md == "" || !strings.Contains(md, "## Phases") {
		return nil, nil, fmt.Errorf("planner revise output missing Phases section")
	}
	// Contract C6: parse the rebuttal ride-along from the RAW reply
	// (rebuttals may live in the fenced block), then strip that block
	// from the stored markdown-first document.
	rebuttals = parseCriticRebuttals(output)
	md = stripRebuttalJSON(md)
	return &PlanDraft{Markdown: md, Version: draft.Version, UpdatedAt: time.Now().UTC()}, rebuttals, nil
}

// stripRebuttalJSON removes the optional fenced ```json rebuttal block
// from a revise reply so it never lands inside the stored plan document.
// The markdown document itself is preserved verbatim; when no rebuttal
// block is present the input is returned unchanged (byte-identical
// no-rebuttal path).
func stripRebuttalJSON(markdown string) string {
	const fence = "```json"
	const closeFence = "```"
	idx := strings.Index(markdown, fence)
	if idx < 0 {
		return markdown
	}
	// Search for the closing fence AFTER the opening one (the opening
	// fence itself contains ``` and would otherwise match immediately).
	bodyStart := idx + len(fence)
	rel := strings.Index(markdown[bodyStart:], closeFence)
	if rel < 0 {
		// Unterminated fence: leave the reply untouched; the defensive
		// parse in parseCriticRebuttals handles it either way.
		return markdown
	}
	end := bodyStart + rel + len(closeFence)
	return strings.TrimRight(markdown[:idx]+markdown[end:], "\n")
}

// runPlannerChat runs one critique-loop LLM call: the raw chatter when a
// seam is wired (SetCritiqueChatter — machine-to-machine calls, the
// reasoning loop's reply guards are user-reply shaping and would misread
// the plan document's Meta line as a side-effect claim), otherwise the
// planner agent loop like every other planner call.
func (sp *StrategicPlanner) runPlannerChat(ctx context.Context, plannerLoop *AgentLoop, req PlanRequest, prompt string) (string, error) {
	planCtx, cancel := context.WithTimeout(ctx, sp.plannerTimeout)
	defer cancel()
	if ch := sp.critiqueChatterFor(); ch != nil {
		resp, err := ch.Chat(planCtx, []llm.ChatMessage{{Role: "user", Content: prompt}})
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}
	conversationID := fmt.Sprintf("plan-critique-%s-%s", req.TaskID, id.Generate(""))
	return plannerLoop.RunOnce(planCtx, prompt, conversationID)
}

// compileProblemText renders a CompileError's problems as one bounded
// string for the revise prompt and the honest-failure reason.
func compileProblemText(err error) string {
	var ce *plan.CompileError
	if !asAgentCompileError(err, &ce) {
		return truncateRunes(err.Error(), 500, "…")
	}
	msgs := make([]string, 0, len(ce.Problems))
	for _, p := range ce.Problems {
		msgs = append(msgs, fmt.Sprintf("line %d: %s", p.Line, p.Message))
	}
	return truncateRunes(strings.Join(msgs, "; "), 800, "…")
}

// asAgentCompileError is the package-local errors.As for *plan.CompileError
// (two-value type assertion convention).
func asAgentCompileError(err error, target **plan.CompileError) bool {
	if ce, ok := errors.AsType[*plan.CompileError](err); ok {
		*target = ce
		return true
	}
	return false
}

// stampCritiqueMetadata persists critique_rounds_used,
// known_risks_count, and critique_outcome on the task's metadata bag
// (leaf 02 rounds/risks; leaf 04 adds the outcome so the interactive
// draft surface can show the loop's verdict next to the counts).
func (sp *StrategicPlanner) stampCritiqueMetadata(taskID string, roundsUsed, knownRisks int, outcome string) error {
	t, err := sp.taskStore.GetByID(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	t.Metadata = mergeMetadata(t.Metadata, map[string]json.RawMessage{
		critiqueRoundsUsedMetadataKey: json.RawMessage(fmt.Sprintf("%d", roundsUsed)),
		knownRisksCountMetadataKey:    json.RawMessage(fmt.Sprintf("%d", knownRisks)),
		critiqueOutcomeMetadataKey:    json.RawMessage(fmt.Sprintf("%q", outcome)),
	})
	if err := sp.taskStore.Update(t); err != nil {
		return fmt.Errorf("persist critique metadata on task %s: %w", taskID, err)
	}
	return nil
}

// stampSealProvenance persists sealed_by ("planner-self" | "user") on the
// task's metadata — the seal record's provenance marker (leaf 02).
func (sp *StrategicPlanner) stampSealProvenance(taskID, provenance string) error {
	t, err := sp.taskStore.GetByID(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	t.Metadata = mergeMetadata(t.Metadata, map[string]json.RawMessage{
		sealProvenanceMetadataKey: json.RawMessage(fmt.Sprintf("%q", provenance)),
	})
	if err := sp.taskStore.Update(t); err != nil {
		return fmt.Errorf("persist seal provenance on task %s: %w", taskID, err)
	}
	return nil
}
