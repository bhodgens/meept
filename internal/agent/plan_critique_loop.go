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

// draftToolHintRe extracts tool hints from draft step lines
// ("1. Do the thing [code]"). Same hint vocabulary as the compiler's
// step-line grammar.
var draftToolHintRe = regexp.MustCompile(`^\s*[0-9]+\.\s+.+?\[([a-z_]+)\]`)

// draftToolHints collects the tool hints named in a draft's step lines,
// preserving first-seen order and deduplicating case-insensitively.
func draftToolHints(markdown string) []string {
	seen := make(map[string]bool)
	var hints []string
	for _, line := range strings.Split(markdown, "\n") {
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
	var blocking, advisory []criticObjection

	for round := 1; round <= maxRounds; round++ {
		// --- Critique (master step 2) ---
		critiquePrompt := sp.buildCriticPrompt(req, draft, input)
		objections, criticErr := sp.runCriticOnce(ctx, plannerLoop, critiquePrompt)
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

		if len(blocking) == 0 {
			// Clean round. Advisory items still roll into Known Risks
			// (unconditional per the leaf), then the loop exits.
			break
		}

		// --- Refine (master step 3), unless this was the last round ---
		if round == maxRounds {
			break
		}
		revised, reviseErr := sp.reviseDraft(ctx, plannerLoop, req, draft, append(append([]criticObjection{}, blocking...), advisory...), "")
		if reviseErr != nil {
			sp.recordMetric("strategic_planner.tier_complex_fallback", 1, map[string]string{"reason": "revise_transport"})
			sp.logger.Warn("Critique loop revise failed; falling back to legacy path",
				"task_id", req.TaskID,
				"error", reviseErr,
			)
			return &critiqueFlowResult{Action: critiqueActionFallback}, nil
		}
		draft = revised
	}

	// Rounds exhausted with open blocking items → the Known Risks section
	// carries them (honest degradation, never silent), then the draft
	// seals anyway.
	outcome := critiqueOutcomeClean
	if len(blocking) > 0 {
		outcome = critiqueOutcomeExhausted
	} else if criticFailed {
		outcome = critiqueOutcomeCriticFail
	}
	known := append(append([]criticObjection{}, blocking...), advisory...)
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
		revised, reviseErr := sp.reviseDraft(ctx, plannerLoop, req, draft, nil, problems)
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
		if sp.selfSealFlag() {
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

// reviseDraft asks the planner to produce a revised draft addressing the
// objection list (and, for the compile-fix path, the compiler's problems).
func (sp *StrategicPlanner) reviseDraft(ctx context.Context, plannerLoop *AgentLoop, req PlanRequest, draft *PlanDraft, objections []criticObjection, compileProblems string) (*PlanDraft, error) {
	var sb strings.Builder
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
		return nil, err
	}
	md := strings.TrimSpace(output)
	if md == "" || !strings.Contains(md, "## Phases") {
		return nil, fmt.Errorf("planner revise output missing Phases section")
	}
	return &PlanDraft{Markdown: md, Version: draft.Version, UpdatedAt: time.Now().UTC()}, nil
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
	ce, ok := err.(*plan.CompileError)
	if ok {
		*target = ce
	}
	return ok
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
