package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

// sessionEvidence reports whether the session holds quickplan-shaped
// state: an approved/executing/confirmed plan or an active tracked task.
// This is the missing signal the per-message classifier cannot see
// (campaign hard rule 3; teacher-mix gate 2026-09-21 proved the ceiling
// is state-bound, not capability-bound — two frontier models agreed on
// the wrong lane for 9/17 misses). Either evidence source suffices.
// C1 signature (docs/plans/quickplan-session-upgrade/master.md):
// do not change without updating the plan contract.
func (d *Dispatcher) sessionEvidence(ctx context.Context, sessionID string) (bool, string) {
	if d.planManager != nil {
		plans, err := d.planManager.GetPlansForSession(ctx, sessionID)
		if err != nil {
			// Evidence is advisory: a read failure is no evidence, not
			// a dispatch failure.
			d.logger.Debug("session evidence plan read failed", "error", err, "session", sessionID)
		} else {
			for _, p := range plans {
				if p == nil {
					continue
				}
				switch p.State {
				case plan.StateApproved:
					return true, "plan:approved"
				case plan.StateExecuting:
					return true, "plan:executing"
				case plan.StateConfirmed:
					return true, "plan:confirmed"
				}
			}
		}
	}
	if d.taskStore != nil {
		tasks, err := d.taskStore.GetTasksForSession(sessionID)
		if err != nil {
			d.logger.Debug("session evidence task read failed", "error", err, "session", sessionID)
			return false, ""
		}
		active := 0
		for _, tk := range tasks {
			if tk == nil {
				continue
			}
			switch tk.State {
			case task.StatePending, task.StatePlanning, task.StateAwaitingApproval,
				task.StateExecuting, task.StateTesting:
				active++
			}
		}
		if active > 0 {
			return true, fmt.Sprintf("%d active tasks", active)
		}
	}
	return false, ""
}

// sessionUpgradeBoundaryLanes: the lanes the adjudicated quickplan cases
// scatter to (campaign 20260918 phase 2 + teacher-mix 2026-09-21: the
// same scatter set, confirmed for cloud frontier models). Deliberately
// excludes chat/clarify (the ambiguity gate's question is a real scope
// question) and recall/schedule/report/search/platform (no measured
// scatter onto quickplan).
func sessionUpgradeBoundaryLanes(verdictType string) bool {
	switch IntentType(verdictType) {
	case IntentCode, IntentDebug, IntentReview, IntentPlan, IntentGit, IntentAnalyze:
		return true
	default:
		return false
	}
}

// sessionStateUpgradeApplies is the pure upgrade predicate (testable
// without a dispatcher). All conditions must hold:
//   - verdict is in the measured boundary lane set
//   - the input carries lexical quickplan evidence (QuickPlanCuePattern
//   - strong-form requirement — reuse quickPlanCueUpgradeApplies's cue
//     logic, not a second copy)
//   - session evidence exists and the config knob is on
//
// One-way (C4): a quickplan verdict returns false — the gate never
// downgrades, it only upgrades INTO quickplan.
func sessionStateUpgradeApplies(verdictType, input string, hasEvidence, knob bool) bool {
	if !knob || !hasEvidence {
		return false
	}
	if IntentType(verdictType) == IntentQuickPlan {
		return false // one-way: nothing to upgrade
	}
	if !sessionUpgradeBoundaryLanes(verdictType) {
		return false
	}
	// Lexical evidence stays mandatory (C6): session state alone must not
	// upgrade — "fix this typo" mid-plan-session is still code. Reuse the
	// adjudicated cue+strong-form check.
	return quickPlanCueUpgradeApplies(verdictType, input) || quickPlanCueInputMatches(input)
}

// quickPlanCueInputMatches reports plain QuickPlanCuePattern support for
// inputs whose strong-form verbs differ from the cue-upgrade's narrowed
// list but which still carry adjudicated orchestration cues (subagents,
// waves, plan.md). The session gate is allowed the BROADER cue set
// because it also requires session evidence — two independent signals
// instead of one.
func quickPlanCueInputMatches(input string) bool {
	return QuickPlanCuePattern.MatchString(strings.ToLower(input))
}

// maybeUpgradeSessionQuickplan applies the one-way session-state upgrade
// to an LLM verdict. Called from ClassifyAndRoute's LLM branch only.
// Returns the (possibly replaced) intent. knob off or no evidence = the
// original intent, untouched (C4/C6; default-off inertness contract).
func (d *Dispatcher) maybeUpgradeSessionQuickplan(ctx context.Context, intent *Intent, input, sessionID string, knob bool) *Intent {
	if intent == nil || !knob {
		return intent
	}
	if !sessionStateUpgradeApplies(intent.Type, input, false, true) {
		// Fast path: not a boundary lane or already quickplan — pay no
		// session-state read latency. (Re-invokes the pure predicate with
		// evidence=false to short-circuit; boundary lanes fall through.)
		if !sessionUpgradeBoundaryLanes(intent.Type) || IntentType(intent.Type) == IntentQuickPlan {
			return intent
		}
	}
	hasEv, reason := d.sessionEvidence(ctx, sessionID)
	if !sessionStateUpgradeApplies(intent.Type, input, hasEv, true) {
		return intent
	}
	d.logger.Info("LLM verdict upgraded to quickplan by session state evidence",
		"verdict", intent.Type,
		"session_reason", reason,
		"session", sessionID,
		"input_len", len(input),
	)
	d.recordClassificationMethod("quickplan_session_upgrade")
	return &Intent{
		Type:             string(IntentQuickPlan),
		Confidence:       0.85,
		AgentType:        "orchestrator",
		RequiresPlanning: true,
		Summary:          extractSummary(input),
		Method:           "quickplan_session_upgrade",
		Model:            intent.Model,
	}
}

// classifierConfig mirrors config.ClassifierConfig (session-state gate
// knob). A local struct keeps the config-import surface identical to the
// existing mirror pattern (PrefilterConfig) and avoids an
// internal/config cycle if the struct grows behavior.
type classifierGateConfig struct {
	SessionStateUpgrade bool
}
