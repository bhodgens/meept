package agent

// L6 producer-lane verdict emission (owner ruling 2026-11-27): the
// pair_manager rounds and the adversarial verification hook are review
// producers that previously parsed ReviewResults / verdicts without ever
// publishing them, so their grades never reached model_review_verdicts.
// They now emit through the same step.review_completed /
// task.review_completed lane the metrics collector consumes
// (ReviewManager.PublishReviewVerdictFromProducer), and each lane's rows
// are distinguishable the way the evidence-gate lane's are — by the
// rubric ids its verdicts carry (mirrors withGateVerdicts in
// review_evidence_gate.go).
//
// Rubric selection: the internal/modelrating catalog (frozen C1) has no
// dedicated pair-review or adversarial-hook rubric, and the catalog is
// frozen — a new id requires re-freezing it (issue #168 scope). Both
// lanes therefore emit with the CATALOG-NEAREST id plus a detail string
// that names the producing lane verbatim, exactly the lenient-store
// fallback the task ruling allows ("emit with a detail string naming the
// hook"): the row is identifiable by rubric_id + the detail prefix, and
// the payload carries the machine-readable "review_source" key too.
//   - pair-review rejected round  -> MR-LC1 (requested change did not
//     land) — the reviewer says the work is not done; that is the
//     gate lane's own mapping for "work didn't land".
//   - pair-review approved round   -> no defect verdicts; passVerdictFor
//     supplies the pass row (same denominator rule as the step lane).
//   - hook VerdictFail / Partial   -> MR-LC1 (verification found the
//     work did not land / only partially landed).
//   - hook VerdictPass             -> pass row via passVerdictFor.

import (
	"fmt"

	"github.com/caimlas/meept/internal/modelrating"
)

// ReviewVerdictPublisher is the L6 emission seam the producer lanes
// (PairManager, VerificationAutoTrigger) publish through. It exists so
// those producers never hold the whole *ReviewManager — the daemon wires
// the ReviewManager itself (its PublishReviewVerdictFromProducer method
// satisfies this interface) via SetReviewVerdictEmitter on each producer.
// Nil (unset) = the producer stays non-emitting, byte-identical to the
// pre-L6 posture.
type ReviewVerdictPublisher interface {
	PublishReviewVerdictFromProducer(stepID, taskID string, result *ReviewResult, revisionCount int, src ReviewVerdictSource)
}

// Producer-lane source labels for the payload's "review_source" key.
const (
	// ReviewSourceStep is the step-review lane (the pre-L6 emitter; its
	// payloads carry the empty string for backward byte-compat of
	// existing consumers — the label exists for documentation and the
	// gate's zero-value assertion).
	ReviewSourceStep = ""
	// ReviewSourcePair names pair_manager's actor→reviewer rounds.
	ReviewSourcePair = "pair-review"
	// ReviewSourceVerificationHook names the adversarial verification
	// hook's SpawnVerifier verdicts.
	ReviewSourceVerificationHook = "verification-hook"
)

// pairReviewVerdicts tags a pair round's parsed ReviewResult with the
// lane-distinguishing verdicts. A rejected round gets exactly one
// model-scope defect row (the pair reviewer's Issues feed the detail);
// an approved round gets none — passVerdictFor appends the pass row at
// payload build time (issue #168 AC1 denominator rule). JSON-parsed
// outputs that already carry verdicts keep theirs verbatim (a reviewer
// that emitted real rubric grades needs no synthesized row).
func pairReviewVerdicts(result *ReviewResult) []modelrating.Verdict {
	if result == nil || result.Status != ReviewRejected || len(result.Verdicts) > 0 {
		return nil
	}
	detail := "pair-review rejection"
	if len(result.Issues) > 0 {
		detail = fmt.Sprintf("pair-review rejection: %s", result.Issues[0])
	}
	return []modelrating.Verdict{{
		RubricID:   modelrating.RubricID("MR-LC1"),
		Scope:      modelrating.ScopeModel,
		Category:   modelrating.CategoryIncompleteImplementation,
		Severity:   modelrating.SeverityFunctional,
		Detail:     detail,
		Confidence: result.Confidence,
	}}
}

// adversarialReviewVerdicts maps a parsed adversarial verdict onto the
// lane-tagged verdict rows. PASS yields none (passVerdictFor appends the
// pass row — the hook result below is constructed with Status
// ReviewApproved so the denominator rule fires unchanged); FAIL/PARTIAL
// yield one model-scope defect row whose detail names the hook and the
// failed-check count. UNKNOWN emits nothing — an unparseable verifier
// output grades nothing (honesty over invented rows).
func adversarialReviewVerdicts(verdict Verdict, checks []CheckResult, confidence float64) []modelrating.Verdict {
	if verdict != VerdictFail && verdict != VerdictPartial {
		return nil
	}
	failed := 0
	for _, c := range checks {
		if !c.Passed {
			failed++
		}
	}
	severity := modelrating.SeverityBlocking
	category := modelrating.CategoryIncompleteImplementation
	detail := fmt.Sprintf("adversarial verification hook: verdict FAIL (%d checks failed of %d)", failed, len(checks))
	if verdict == VerdictPartial {
		severity = modelrating.SeverityFunctional
		detail = fmt.Sprintf("adversarial verification hook: verdict PARTIAL (%d checks failed of %d)", failed, len(checks))
	}
	return []modelrating.Verdict{{
		RubricID:   modelrating.RubricID("MR-LC1"),
		Scope:      modelrating.ScopeModel,
		Category:   category,
		Severity:   severity,
		Detail:     detail,
		Confidence: confidence,
	}}
}
