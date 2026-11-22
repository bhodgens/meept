package agent

// 02-routing TEST-GATE T4 (W13; R7/A9; session-steering A6; master C3/C5,
// matrix F8).
//
// Two continuation pins:
//
//  1. TestDraftRefinementRoute - a non-command follow-up to a session
//     holding a PARKED PLANNING task with plan_draft metadata must refine
//     the SAME task's draft (SaveDraft), never mint a new task. Today
//     there is no SaveDraft branch in the dispatcher's pre-task-creation
//     region: the follow-up classifies as work and createTask mints a
//     second task while the parked draft goes stale.
//
//  2. TestThreadResolveOnce - the thread id is resolved TWICE per turn
//     (ClassifyAndRoute on the full input, RouteToAgent on
//     Intent.Summary), so a topic keyword past char 100 lands in a
//     different thread for memory than for the queue. The turn must
//     resolve ONCE and carry the id on DispatchResult.
//
// Both drive the REAL production entry points.

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

// continuationGateDispatcher builds a dispatcher over a real task
// registry and session store, with a canned classifier verdict.
func continuationGateDispatcher(t *testing.T, verdict string) (*Dispatcher, *task.Store, *session.MemoryStore) {
	t.Helper()
	logger := testLogger()

	reg, err := task.NewRegistry(filepath.Join(t.TempDir(), "tasks.db"), bus.New(nil, logger), logger)
	if err != nil {
		t.Fatalf("task registry: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := reg.Close(); closeErr != nil {
			t.Errorf("close registry: %v", closeErr)
		}
	})

	ss := session.NewMemoryStore(logger)
	t.Cleanup(func() {
		if closeErr := ss.Close(); closeErr != nil {
			t.Errorf("close session store: %v", closeErr)
		}
	})

	d := NewDispatcher(DispatcherConfig{
		Registry:         NewAgentRegistry(RegistryConfig{Logger: logger}),
		TaskStore:        reg.Store(),
		TaskRegistry:     reg,
		ClassifierClient: newClassifierJSONServer(t, verdict),
		Logger:           logger,
	})
	d.SetSessionStore(ss)
	return d, reg.Store(), ss
}

// seedParkedDraftTask creates a StatePlanning task with a seeded plan
// draft, linked to the session's conversation id (the live linkage).
func seedParkedDraftTask(t *testing.T, d *Dispatcher, store *task.Store, convID, request string) *task.Task {
	t.Helper()
	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore:      store,
		StepStore:      store.StepStore(),
		Logger:         testLogger(),
		TemplateLoader: newPlannerTemplateLoader(t.TempDir()),
	})

	tsk := newTestTask("parked planning task", request)
	tsk.SetState(task.StatePlanning)
	tsk.LinkSession(convID)
	if err := store.Create(tsk); err != nil {
		t.Fatalf("create parked task: %v", err)
	}
	if err := store.LinkSession(tsk.ID, convID); err != nil {
		t.Fatalf("link session: %v", err)
	}
	if err := sp.SaveDraft(tsk.ID, "# Plan: draft v1\n\nGoal: "+request+"\n"); err != nil {
		t.Fatalf("seed draft: %v", err)
	}
	return tsk
}

// draftVersionOf reads the parked task's draft version from metadata.
func draftVersionOf(t *testing.T, store *task.Store, taskID string) int {
	t.Helper()
	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore: store,
		StepStore: store.StepStore(),
		Logger:    testLogger(),
	})
	d, ok := sp.DraftFor(taskID)
	if !ok || d == nil {
		t.Fatalf("task %s has no plan_draft metadata", taskID)
	}
	return d.Version
}

// TestDraftRefinementRoute_FollowupRefinesSameTask pins the continuation
// contract: given a parked planning task with a draft, a non-command
// refinement follow-up must SaveDraft on the SAME task (version bumps,
// no new task row). RED today: the follow-up mints a second task.
func TestDraftRefinementRoute_FollowupRefinesSameTask(t *testing.T) {
	// The canned verdict mirrors the live misroute: the refinement reads
	// as work to the classifier (code lane), which is exactly why the
	// pre-task-creation continuation branch must intercept it.
	d, store, ss := continuationGateDispatcher(t, `{"intent":"code","confidence":0.9}`)

	sess, err := ss.Create("draft-refinement")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.ProjectPath = t.TempDir()

	const request = "build a translator script covering three languages"
	parked := seedParkedDraftTask(t, d, store, sess.ConversationID, request)
	versionBefore := draftVersionOf(t, store, parked.ID)

	const refinement = "make it five languages instead"
	res, err := d.ClassifyAndRoute(context.Background(), refinement, sess.ID, nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute(follow-up): %v", err)
	}

	// No new task: the refinement belongs to the parked draft.
	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	for _, tsk := range active {
		if tsk.ID != parked.ID {
			t.Errorf("follow-up minted a new task %s (%q); a refinement of a parked planning draft must continue the SAME task (SaveDraft), not create work", tsk.ID, tsk.Name)
		}
	}
	if res != nil && res.Task != nil && res.Task.ID != parked.ID {
		t.Errorf("DispatchResult.Task = %s, want the parked task %s or nil", res.Task.ID, parked.ID)
	}

	// The draft advanced on the SAME task.
	versionAfter := draftVersionOf(t, store, parked.ID)
	if versionAfter <= versionBefore {
		t.Errorf("parked draft version stayed at %d after the refinement follow-up; SaveDraft must run on task %s with the FULL follow-up text (C3: never Intent.Summary)",
			versionBefore, parked.ID)
	}
}

// TestDraftRefinementRoute_NewWorkStillCreatesTask is the discrimination
// guard: an explicit NEW-WORK command on the same session must still
// create a task - the continuation branch must not swallow fresh work.
func TestDraftRefinementRoute_NewWorkStillCreatesTask(t *testing.T) {
	d, store, ss := continuationGateDispatcher(t, `{"intent":"code","confidence":0.9}`)

	sess, err := ss.Create("draft-new-work")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.ProjectPath = t.TempDir()

	parked := seedParkedDraftTask(t, d, store, sess.ConversationID, "build a translator script")
	versionBefore := draftVersionOf(t, store, parked.ID)

	const newWork = "Create a file named notes.txt in the project directory with the text 'fresh work'."
	if _, err := d.ClassifyAndRoute(context.Background(), newWork, sess.ID, nil, "", ""); err != nil {
		t.Fatalf("ClassifyAndRoute(new work): %v", err)
	}

	active, err := store.ListActive()
	if err != nil {
		t.Fatalf("ListActive: %v", err)
	}
	var foundNew bool
	for _, tsk := range active {
		if tsk.ID != parked.ID {
			foundNew = true
		}
	}
	if !foundNew {
		t.Error("explicit new-work command created no task: the continuation branch must only intercept refinements of the parked draft, never fresh work")
	}
	if v := draftVersionOf(t, store, parked.ID); v != versionBefore {
		t.Errorf("new-work command touched the parked draft (version %d -> %d)", versionBefore, v)
	}
}

// countingThreadStore wraps the mock thread store and counts
// GetThreadConversationID-visible resolutions via Get/GetByConversationID
// hits from the router. The router consults the store on EVERY
// GetThreadConversationID call, so per-turn lookup counts expose the
// double resolution.
type countingThreadStore struct {
	*mockThreadStore
	lookups int
}

func (c *countingThreadStore) Get(id string) *session.Session {
	c.lookups++
	return c.mockThreadStore.Get(id)
}

func (c *countingThreadStore) GetByConversationID(conversationID string) *session.Session {
	c.lookups++
	return c.mockThreadStore.GetByConversationID(conversationID)
}

// TestThreadResolveOnce_FullInputTopic pins single-resolution: the topic
// keyword sits PAST char 100, so extractSummary (first sentence / 100
// chars) drops it. ClassifyAndRoute resolves the thread on the FULL
// input; RouteToAgent re-resolves on Intent.Summary and lands in a
// DIFFERENT thread - memory key != queue key for the same turn.
// The contract: resolve once, carry DispatchResult.ThreadConversationID,
// and RouteToAgent reuses it (zero extra router consultations).
func TestThreadResolveOnce_FullInputTopic(t *testing.T) {
	logger := testLogger()
	store := &countingThreadStore{mockThreadStore: newMockThreadStore()}
	const sessionID = "sess-thread-once"
	const sessionConvID = "conv-threadonce"
	store.addSession(&session.Session{
		ID:             sessionID,
		ConversationID: sessionConvID,
	})
	router := NewThreadRouter(WithThreadRouterSessionStore(store))

	registry := NewAgentRegistry(RegistryConfig{Logger: logger})
	if err := registry.RegisterSpec(&AgentSpec{
		ID: config.AgentIDChat, Name: "chat", Role: RoleExecutor, Enabled: true,
	}); err != nil {
		t.Fatalf("register chat spec: %v", err)
	}

	d := NewDispatcher(DispatcherConfig{Registry: registry, Logger: logger})
	d.SetThreadRouter(router)

	// Work keyword ("deploy") past char 100: the full input detects the
	// work topic; the 100-char summary does not.
	input := "Please take a careful look at everything we discussed earlier about the release process. " +
		"After that, deploy the billing service to the staging environment."
	if len(input) <= 100 {
		t.Fatalf("fixture precondition: input must exceed 100 chars (len=%d)", len(input))
	}
	summary := extractSummary(input)
	if strings.Contains(strings.ToLower(summary), "deploy") {
		t.Fatalf("fixture precondition: summary must NOT carry the topic keyword; got %q", summary)
	}

	res, err := d.ClassifyAndRoute(context.Background(), input, sessionID, nil, "", "")
	if err != nil {
		t.Fatalf("ClassifyAndRoute: %v", err)
	}
	if res == nil || res.Intent == nil {
		t.Fatal("nil result/intent")
	}

	// The turn's thread id, resolved ONCE from the full input.
	wantThreadConvID, err := router.GetThreadConversationID(context.Background(), sessionID, input)
	if err != nil {
		t.Fatalf("reference resolution: %v", err)
	}

	lookupsAfterClassify := store.lookups

	// RouteToAgent must REUSE the turn's resolved id, not re-resolve on
	// the truncated summary (which detects a different topic).
	_, routeErr := d.RouteToAgent(context.Background(), res, sessionID)
	if routeErr != nil {
		// The chat loop has no LLM; a run error is fine - the pin is
		// about resolution count and the carried id, not the reply.
		t.Logf("RouteToAgent run error (expected without an LLM): %v", routeErr)
	}

	if store.lookups > lookupsAfterClassify {
		t.Errorf("thread router consulted %d extra time(s) inside RouteToAgent: the thread id must be resolved ONCE per turn (full input) and carried on DispatchResult, not re-derived from Intent.Summary",
			store.lookups-lookupsAfterClassify)
	}

	// The carried id must equal the full-input resolution. Reflection
	// keeps this compiling before the field lands; a missing field is
	// the RED state (no single source of truth exists yet).
	carried := threadConversationIDOnResult(res)
	if carried == "" {
		t.Error("DispatchResult carries no ThreadConversationID: the turn has no single-source thread id, so memory and queue keys can diverge (double resolution)")
	} else if carried != wantThreadConvID {
		t.Errorf("carried thread conversation id = %q, want the full-input resolution %q", carried, wantThreadConvID)
	}
}

// threadConversationIDOnResult reads the C-continuation single-source
// field via reflection so the gate compiles before the field exists.
func threadConversationIDOnResult(res *DispatchResult) string {
	if res == nil {
		return ""
	}
	v := reflect.ValueOf(res).Elem()
	f := v.FieldByName("ThreadConversationID")
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}
	return f.String()
}
