package agent

// M4 pin (bughunt 2026-09-29): the /plan SLASH route must link the plan
// (and, on the quickplan arm, its task) with the SESSION-level conversation
// id.
//
// The thread router resolves sessionID → conv-<hex>-thread-<topic> for
// LLM-continuity lane isolation, but CreatePlan links plan_sessions (and
// createTask → LinkSession writes session_tasks) with the id it is handed;
// GetPlansForSession / GetTasksForSession JOIN on that key. The detected
// compound/plan routes pass the session-level id since 90eb0f45; the slash
// route still passed the thread-resolved id, so work created via /plan
// inside a non-general thread was invisible to every digest/recall lookup.
//
// The pin drives the REAL ClassifyAndRoute with a thread router wired (the
// same seam TestDispatcher_UsesThreadRouter uses) and asserts on what the
// plan store received: the plan links to the session-level id and never to
// the thread-scoped one.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

func TestPlanSlashRoute_LinksSessionLevelConversationID(t *testing.T) {
	logger := digestTestLogger()
	ctx := context.Background()

	planStore, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "plans.db"), logger)
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { _ = planStore.Close() })

	// Real task registry so the quickplan arm's createTask → LinkSession
	// also lands in a real session_tasks table (asserted below).
	reg, err := task.NewRegistry(filepath.Join(t.TempDir(), "tasks.db"), bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("task registry: %v", err)
	}
	t.Cleanup(func() { _ = reg.Close() })

	// Thread router over a store holding ONE session whose conversation id
	// the router will resolve into a NON-general thread (the classifier
	// input carries a work keyword, so the topic detector labels it "work"
	// and GetThreadConversationID returns conv-...-thread-work-XXXX, not
	// the session-level id the general/migration thread carries).
	threadStore := newMockThreadStore()
	const sessionID = "sess-plan-slash"
	const sessionConvID = "conv-sessplanslash"
	threadStore.addSession(&session.Session{
		ID:             sessionID,
		ConversationID: sessionConvID,
		Threads:        nil, // triggers migration on first router access
	})
	router := NewThreadRouter(WithThreadRouterSessionStore(threadStore))

	// Point plan markdown storage at a temp dir: resolvePlanDir with an
	// empty project path joins "docs/plans" onto the PROCESS cwd, which
	// inside the test binary is the package directory — the test must not
	// write into the repo.
	planCfg := config.PlansConfig{}
	planCfg.Storage.ExternalPath = filepath.Join(t.TempDir(), "plans-md")

	d := NewDispatcher(DispatcherConfig{
		TaskStore:    reg.Store(),
		TaskRegistry: reg,
		Logger:       logger,
	})
	d.SetPlanManager(plan.NewPlanManager(planStore, nil, planCfg, nil, logger))
	d.SetThreadRouter(router)

	// The slash input names a work keyword ("build") so the router resolves
	// a NON-general thread — exactly the shape that used to leak the
	// thread-scoped id into routeToPlan.
	const input = "/plan build the new billing service end to end"
	res, err := d.ClassifyAndRoute(ctx, input, sessionID, nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute(/plan …): %v", err)
	}
	if res == nil || res.Plan == nil {
		t.Fatalf("/plan slash route produced no plan: %+v", res)
	}

	// Sanity: the router actually resolved a thread-scoped id for this
	// input (otherwise the pin cannot discriminate the two ids).
	resolved, err := router.GetThreadConversationID(ctx, sessionID, input)
	if err != nil {
		t.Fatalf("thread router lookup: %v", err)
	}
	if resolved == sessionID || resolved == sessionConvID {
		t.Fatalf("precondition failed: router resolved %q, want a thread-scoped id distinct from both %q and %q",
			resolved, sessionID, sessionConvID)
	}
	threadConvID := resolved

	// The plan's SourceSession and its plan_sessions link must carry the
	// SESSION-level id. GetPlansForSession is the exact JOIN the digest /
	// recall lookups use downstream.
	got, err := planStore.GetPlan(ctx, res.Plan.ID)
	if err != nil {
		t.Fatalf("get plan %s: %v", res.Plan.ID, err)
	}
	if got.SourceSession != sessionID {
		t.Errorf("plan SourceSession = %q, want session-level id %q (thread id %q leaked into the link)",
			got.SourceSession, sessionID, threadConvID)
	}
	linked, err := planStore.GetPlansForSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("get plans for session %s: %v", sessionID, err)
	}
	foundSessionLinked := false
	for _, lp := range linked {
		if lp.ID == res.Plan.ID {
			foundSessionLinked = true
			break
		}
	}
	if !foundSessionLinked {
		t.Errorf("plan %s not linked to session-level id %s; digest/recall lookups would find nothing (linked: %d plans)",
			res.Plan.ID, sessionID, len(linked))
	}
	linkedThread, err := planStore.GetPlansForSession(ctx, threadConvID)
	if err != nil {
		t.Fatalf("get plans for thread id: %v", err)
	}
	if len(linkedThread) != 0 {
		t.Errorf("plan linked under the thread-scoped id %q: %d plans (the exact M4 regression)",
			threadConvID, len(linkedThread))
	}
}

// TestPlanSlashRoute_LinksSessionLevelIDWithoutRouter documents the legacy
// shape stays byte-identical: with no thread router wired, the slash route
// behaves exactly as before (the session-level id was already the only id
// in play).
func TestPlanSlashRoute_LinksSessionLevelIDWithoutRouter(t *testing.T) {
	logger := digestTestLogger()
	ctx := context.Background()

	planStore, err := plan.NewSQLiteStore(filepath.Join(t.TempDir(), "plans.db"), logger)
	if err != nil {
		t.Fatalf("plan store: %v", err)
	}
	t.Cleanup(func() { _ = planStore.Close() })

	d := NewDispatcher(DispatcherConfig{Logger: logger})
	planCfg := config.PlansConfig{}
	planCfg.Storage.ExternalPath = filepath.Join(t.TempDir(), "plans-md")
	d.SetPlanManager(plan.NewPlanManager(planStore, nil, planCfg, nil, logger))

	const sessionID = "sess-plan-slash-legacy"
	res, err := d.ClassifyAndRoute(ctx, "/plan write the migration guide", sessionID, nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute(/plan …): %v", err)
	}
	if res == nil || res.Plan == nil {
		t.Fatalf("/plan slash route produced no plan: %+v", res)
	}
	linked, err := planStore.GetPlansForSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("get plans for session: %v", err)
	}
	if len(linked) == 0 || !strings.Contains(linked[0].ID, "plan") {
		t.Errorf("legacy path: plan not linked to session %s", sessionID)
	}
}
