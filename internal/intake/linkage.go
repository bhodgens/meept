package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/employee"
)

// TriggerRefPrefix marks goals spawned by the intake factory. The linkage
// convention (Contract 2): a ticket maps to exactly one live goal via
// TriggerRef == intake:<repo>#<ticketID> — no schema change.
const TriggerRefPrefix = "intake:"

// TriggerRefFor composes the TriggerRef identifying the goal linked to a
// ticket.
func TriggerRefFor(repo, ticketID string) string {
	return fmt.Sprintf("%s%s#%s", TriggerRefPrefix, repo, ticketID)
}

// IntakeKey is the effects-ledger identity of a ticket->goal creation:
// stable for identical (repo, ticketID, body), different when any part
// differs. The body hash makes a changed mandate a new claim while the
// (repo, ticketID) pair keeps the crash-window double-create impossible.
func IntakeKey(repo, ticketID, body string) string {
	return effects.EffectKey("intake_ticket_goal", repo, ticketID, ticketBodySHA256(body))
}

// ticketBodySHA256 returns the lowercase hex SHA256 of s (the
// prBodySHA256 pattern from internal/tools/builtin/github_pr.go, copied
// rather than imported — intake must not depend on builtin).
func ticketBodySHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// GoalCreator is the minimal manager surface the Linker needs (satisfied
// by *employee.Manager; interface kept local so unit tests can stub it —
// same pattern as employee.Scheduler in scheduler_jobs.go). UpdateGoal is
// the manager's goal-update seam for the re-dispatch path (leaf 02: the
// real Manager delegates to GoalStore.Update).
type GoalCreator interface {
	CreateGoal(ctx context.Context, spec employee.GoalSpec) (*employee.Goal, error)
	ListGoals(ctx context.Context, employeeID string) ([]*employee.Goal, error)
	UpdateGoal(ctx context.Context, goal *employee.Goal) error
}

// Linker implements the ticket->goal identity layer: create on first
// arrival, no-op on identical re-arrival, in-place re-dispatch on a
// changed mandate.
type Linker struct {
	goals  GoalCreator
	ledger effects.Ledger
	log    *slog.Logger
}

// NewLinker builds a Linker over the manager seam and the effects ledger.
// The ledger must be non-nil: the create path claims the IntakeKey before
// any CreateGoal call (fail-closed).
func NewLinker(goals GoalCreator, ledger effects.Ledger, handler slog.Handler) *Linker {
	return &Linker{
		goals:  goals,
		ledger: ledger,
		log:    slog.New(handler),
	}
}

// EnsureGoal maps a ticket to exactly one live goal for employeeID:
//
//   - no linked goal -> claim IntakeKey in the ledger, then create a
//     trigger-source goal (the goal row doubles as the receipt; on a
//     reused claim the goal comes from a fresh TriggerRef scan);
//   - linked goal with an identical body hash -> returned unchanged, zero
//     store writes, no new claim;
//   - linked goal with a changed body -> re-dispatched in place (mandate
//     updated, state reset active, health reset unknown, UpdateGoal
//     persisted). An update to an existing internal row is not an
//     external effect, so no ledger claim rides it.
func (l *Linker) EnsureGoal(ctx context.Context, t Ticket, employeeID string, gate *employee.GoalGateSpec) (*employee.Goal, error) {
	if l.goals == nil {
		return nil, fmt.Errorf("intake: EnsureGoal: nil goal manager")
	}
	if l.ledger == nil {
		return nil, fmt.Errorf("intake: EnsureGoal: nil effects ledger (fail-closed: the create path claims before CreateGoal)")
	}

	goal, err := l.findLinkedGoal(ctx, employeeID, t.Repo, t.ID)
	if err != nil {
		return nil, fmt.Errorf("intake: scan linked goals for %s: %w", TriggerRefFor(t.Repo, t.ID), err)
	}

	if goal != nil {
		if ticketBodySHA256(t.Body) == ticketBodySHA256(goal.Mandate) {
			// Dedup: identical re-arrival. The hash comparison is the
			// cheap gate; the IntakeKey claim already guards the
			// crash-window double-create. No store writes.
			return goal, nil
		}
		return l.redispatch(ctx, goal, t)
	}

	return l.create(ctx, t, employeeID, gate)
}

// findLinkedGoal scans the employee's goals for the one carrying this
// ticket's TriggerRef. Returns nil when none is linked yet.
func (l *Linker) findLinkedGoal(ctx context.Context, employeeID, repo, ticketID string) (*employee.Goal, error) {
	wantRef := TriggerRefFor(repo, ticketID)
	goals, err := l.goals.ListGoals(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	for _, g := range goals {
		if g != nil && g.TriggerRef == wantRef {
			return g, nil
		}
	}
	return nil, nil
}

// create claims the IntakeKey and creates the trigger-source goal. The
// marshaled ticket rides the claim payload; the receipt carries the goal
// ID so reconcile re-executors can find the row.
func (l *Linker) create(ctx context.Context, t Ticket, employeeID string, gate *employee.GoalGateSpec) (*employee.Goal, error) {
	payload, err := json.Marshal(t)
	if err != nil {
		return nil, fmt.Errorf("intake: marshal ticket %s#%s: %w", t.Repo, t.ID, err)
	}

	spec := employee.GoalSpec{
		EmployeeID: employeeID,
		Title:      t.Title,
		Mandate:    t.Body,
		Source:     string(employee.SourceTrigger),
		TriggerRef: TriggerRefFor(t.Repo, t.ID),
		Gate:       gate,
	}

	receipt, reused, err := effects.Run(ctx, l.ledger, IntakeKey(t.Repo, t.ID, t.Body), effects.EffectMeta{
		Tool:               "intake_ticket_goal",
		Payload:            payload,
		ProviderIdempotent: false,
	}, func(ctx context.Context) (json.RawMessage, error) {
		goal, err := l.goals.CreateGoal(ctx, spec)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"goal_id": goal.ID})
	})
	if err != nil {
		return nil, fmt.Errorf("intake: create goal for %s#%s: %w", t.Repo, t.ID, err)
	}
	if err := l.ledger.Complete(ctx, IntakeKey(t.Repo, t.ID, t.Body)); err != nil {
		return nil, fmt.Errorf("intake: complete claim for %s#%s: %w", t.Repo, t.ID, err)
	}

	if reused {
		// A prior completed claim exists: the goal row is the receipt.
		// Scan fresh rather than trusting the receipt body.
		goal, err := l.findLinkedGoal(ctx, employeeID, t.Repo, t.ID)
		if err != nil {
			return nil, fmt.Errorf("intake: rescan linked goal for %s#%s after reused claim: %w", t.Repo, t.ID, err)
		}
		if goal == nil {
			return nil, fmt.Errorf("intake: reused claim %s but no goal linked via %s",
				effects.EffectKey("intake_ticket_goal", t.Repo, t.ID, ticketBodySHA256(t.Body)), TriggerRefFor(t.Repo, t.ID))
		}
		return goal, nil
	}
	_ = receipt

	goal, err := l.findLinkedGoal(ctx, employeeID, t.Repo, t.ID)
	if err != nil {
		return nil, fmt.Errorf("intake: scan created goal for %s#%s: %w", t.Repo, t.ID, err)
	}
	if goal == nil {
		return nil, fmt.Errorf("intake: CreateGoal succeeded for %s#%s but no goal linked via %s",
			t.Repo, t.ID, TriggerRefFor(t.Repo, t.ID))
	}
	return goal, nil
}

// redispatch updates a linked goal in place for a changed mandate: new
// body, state reset to active, health reset to unknown (a fresh
// assessment must start from scratch). No effects claim — an update to an
// existing internal row is not an external effect. Logs IDs and refs
// only, never body content.
func (l *Linker) redispatch(ctx context.Context, goal *employee.Goal, t Ticket) (*employee.Goal, error) {
	goal.Mandate = t.Body
	goal.State = employee.GoalActive
	goal.Assess(employee.GoalUnknown, time.Now())

	if err := l.goals.UpdateGoal(ctx, goal); err != nil {
		return nil, fmt.Errorf("intake: persist re-dispatch for %s#%s (goal %s): %w", t.Repo, t.ID, goal.ID, err)
	}

	l.log.InfoContext(ctx, "intake: re-dispatched goal on changed ticket body",
		"goal_id", goal.ID,
		"trigger_ref", TriggerRefFor(t.Repo, t.ID),
		"repo", t.Repo,
		"ticket_id", t.ID,
	)
	return goal, nil
}
