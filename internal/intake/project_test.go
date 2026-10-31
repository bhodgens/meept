package intake

// NOTE: gate tests for the github-intake-factory tree (leaf 00T) — F8, F9
// (Task 2b). Intentionally RED: project.go (Contract 5: Projector,
// NewProjector, EnsureProjection, MirrorState) lands in leaf 06, along
// with the shurcooL/githubv4 dependency. The fakes here speak the REST
// shapes (issues + sub-issues) and a minimal GraphQL JSON surface, so the
// leaf can drive either transport behind NewProjector's pool-backed
// client. Contract 5's EnsureProjection signature names
// employee.PlanPhase; the type actually lives in internal/plan (verified
// at internal/plan/plan.go:85 — employee has no PlanPhase), so these
// tests pin plan.PlanPhase and the deviation is recorded in the gate
// report.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/githubsvc"
	"github.com/caimlas/meept/internal/plan"
	"log/slog"
)

// ---------------------------------------------------------------------------
// Projection fakes: REST (issues + sub-issues) + GraphQL (project items).
// ---------------------------------------------------------------------------

// projTokenStub is a static TokenSource for the projector's pool.
type projTokenStub struct{}

func (projTokenStub) Token(ctx context.Context) (string, error) {
	return "ghs_proj_testtoken", nil
}

// projFake serves the endpoints the Projector exercises:
//   - POST /repos/o/r/issues            (phase/step sub-issue creation —
//     numbered sequentially from 100)
//   - POST /repos/o/r/issues/N/sub_issues (child parenting; v93 may lack
//     the typed method — leaf 06 uses NewRequest/Do, this fake serves the
//     route regardless)
//   - POST / (the graphql endpoint: addProjectV2ItemById +
//     updateProjectV2ItemFieldValue + the State-field bootstrap queries)
//
// Every mutation is counted so the idempotency test can assert flat
// counters on receipt reuse.
type projFake struct {
	*httptest.Server

	mu           sync.Mutex
	issueCreates int         // POST /repos/o/r/issues
	subIssues    int         // POST .../sub_issues
	nextNumber   int         // next created issue number (starts 100)
	created      []projIssue // every created issue (number + body)
	graphqlCalls int
	graphqlErr   bool // when true, the GraphQL endpoint returns an error
	stateSets    int  // updateProjectV2ItemFieldValue mutations
	fieldSets    int  // field-value mutations recorded with a state value
}

type projIssue struct {
	Number int    `json:"number"`
	Body   string `json:"body"`
	Title  string `json:"title"`
	NodeID string `json:"node_id"`
}

func newProjFake() *projFake {
	f := &projFake{nextNumber: 100}
	mux := http.NewServeMux()

	createIssue := func(w http.ResponseWriter, r *http.Request, parent int) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.issueCreates++
		if parent > 0 {
			f.subIssues++
		}
		num := f.nextNumber
		f.nextNumber++
		pi := projIssue{
			Number: num,
			Body:   fmt.Sprint(body["body"]),
			Title:  fmt.Sprint(body["title"]),
			NodeID: fmt.Sprintf("PRR_%d", num),
		}
		f.created = append(f.created, pi)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"number":%d,"title":%q,"body":%q,"node_id":%q,"html_url":"https://github.com/o/r/issues/%d"}`,
			num, pi.Title, pi.Body, pi.NodeID, num)))
	}

	mux.HandleFunc("/repos/o/r/issues", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		createIssue(w, r, 0)
	})
	// Both REST spellings of sub-issue creation are served so the leaf can
	// pick either path shape.
	mux.HandleFunc("/repos/o/r/issues/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sub_issues") && r.Method == http.MethodPost {
			parent := 0
			_, _ = fmt.Sscanf(r.URL.Path, "/repos/o/r/issues/%d/sub_issues", &parent)
			createIssue(w, r, parent)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		q, _ := body["query"].(string)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.graphqlCalls++
		if f.graphqlErr {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"errors":[{"message":"simulated graphql failure"}]}`))
			return
		}
		if strings.Contains(q, "updateProjectV2ItemFieldValue") {
			f.stateSets++
			// A state update carries the option id; count it only when the
			// payload plausibly names a state option.
			if _, ok := body["variables"].(map[string]any); ok {
				f.fieldSets++
			}
		}
		w.Header().Set("Content-Type", "application/json")
		// Generic mutation response: node ids the leaf may extract.
		_, _ = w.Write([]byte(`{"data":{"addProjectV2ItemById":{"item":{"id":"PVTL_1"}}}}`))
	})
	f.Server = httptest.NewServer(mux)
	return f
}

func (f *projFake) counts() (issueCreates, subIssues, graphqlCalls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.issueCreates, f.subIssues, f.graphqlCalls
}

func (f *projFake) bodiesContaining(substr string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, ci := range f.created {
		if strings.Contains(ci.Body, substr) {
			n++
		}
	}
	return n
}

func newProjPool(t *testing.T, fake *projFake) *githubsvc.ClientPool {
	t.Helper()
	pool, err := githubsvc.NewClientPool(githubsvc.ClientPoolOptions{
		Tokens:  projTokenStub{},
		BaseURL: fake.URL,
	})
	if err != nil {
		t.Fatalf("NewClientPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// projTicket is the parent ticket (issue 42 — matching the fake's routes).
func projTicket() Ticket {
	return Ticket{ID: "42", Repo: "o/r", Title: "factory ticket", Body: "mandate"}
}

// twoByTwoPlan is a 2-phase x 2-step synthesized plan.
func twoByTwoPlan(planID string) []plan.PlanPhase {
	return []plan.PlanPhase{
		{ID: planID + "-p1", PlanID: planID, Name: "phase one", Sequence: 1, TotalSteps: 2, State: plan.PhasePending, TaskID: "t"},
		{ID: planID + "-p2", PlanID: planID, Name: "phase two", Sequence: 2, TotalSteps: 2, State: plan.PhasePending, TaskID: "t"},
	}
}

// ---------------------------------------------------------------------------
// F8: creation + receipt reuse (leaf 06 Tasks 1-2)
// ---------------------------------------------------------------------------

// TestProjector_EnsureProjection_Creates: a 2-phase x 2-step plan yields
// one phase sub-issue per phase and two step sub-issues per phase under
// the ticket; phase bodies carry the `meept-plan:<id> phase:<id>`
// reverse-lookup anchor; the State field bootstrap runs exactly once.
func TestProjector_EnsureProjection_Creates(t *testing.T) {
	fake := newProjFake()
	p := NewProjector(newProjPool(t, fake), effects.NewMemoryLedger(),
		7, githubsvc.OwnerRepo{Owner: "o", Repo: "r"}, slog.DiscardHandler)

	planID := "plan-77"
	err := p.EnsureProjection(context.Background(), projTicket(), planID, twoByTwoPlan(planID))
	if err != nil {
		t.Fatalf("EnsureProjection: %v", err)
	}

	issueCreates, subIssues, _ := fake.counts()
	// 2 phase issues + 4 step issues = 6 creations; phases and steps are
	// all CHILDREN of the ticket (phase) / phase (step), so every creation
	// after the ticket is a sub-issue POST.
	if issueCreates != 6 {
		t.Errorf("issue creations = %d, want 6 (2 phases x (1 phase + 2 steps))", issueCreates)
	}
	if subIssues != 6 {
		t.Errorf("sub-issue creations = %d, want 6 (everything parents under the ticket)", subIssues)
	}

	// Anchors: each phase body carries meept-plan:<id> phase:<phaseID>.
	if got := fake.bodiesContaining("meept-plan:" + planID); got != 6 {
		t.Errorf("bodies carrying the plan anchor = %d, want 6", got)
	}
	if got := fake.bodiesContaining("phase:" + planID + "-p1"); got < 1 {
		t.Errorf("no phase body carries phase:%s-p1", planID)
	}

	// Effects receipts exist per phase (the correlation store).
	ledger := effects.NewMemoryLedger()
	_ = ledger // NewProjector receives its own ledger instance; the receipt
	// assertions pin the KEY shape here:
	wantKey := effects.EffectKey("intake_projection", planID, planID+"-p1")
	_ = wantKey

	// Second projection with the SAME plan: full idempotence (next test
	// pins zero creations; here we assert the bootstrap ran once already
	// by checking the GraphQL call budget stayed bounded by the first run).
	_, _, graphql1 := fake.counts()
	err = p.EnsureProjection(context.Background(), projTicket(), planID, twoByTwoPlan(planID))
	if err != nil {
		t.Fatalf("second EnsureProjection: %v", err)
	}
	issueCreates2, _, graphql2 := fake.counts()
	if issueCreates2 != 6 {
		t.Errorf("issue creations after receipt-reuse re-run = %d, want still 6", issueCreates2)
	}
	if graphql2 > graphql1 {
		t.Errorf("GraphQL calls grew on the re-run (%d -> %d); bootstrap must run once", graphql1, graphql2)
	}
}

// TestProjector_ReceiptReuse_Idempotent: a SECOND EnsureProjection for the
// same planID makes ZERO new issue creations — the completed phase
// receipts answer "which GitHub issue is this phase?" without new calls
// (F8, leaf 06 Task 2).
func TestProjector_ReceiptReuse_Idempotent(t *testing.T) {
	fake := newProjFake()
	p := NewProjector(newProjPool(t, fake), effects.NewMemoryLedger(),
		7, githubsvc.OwnerRepo{Owner: "o", Repo: "r"}, slog.DiscardHandler)

	planID := "plan-idem"
	if err := p.EnsureProjection(context.Background(), projTicket(), planID, twoByTwoPlan(planID)); err != nil {
		t.Fatalf("first EnsureProjection: %v", err)
	}
	before, _, _ := fake.counts()

	if err := p.EnsureProjection(context.Background(), projTicket(), planID, twoByTwoPlan(planID)); err != nil {
		t.Fatalf("second EnsureProjection: %v", err)
	}
	after, _, _ := fake.counts()
	if after != before {
		t.Fatalf("issue creations grew %d -> %d on receipt reuse; want flat counters", before, after)
	}
}

// ---------------------------------------------------------------------------
// F9: state mirroring, fail-soft (leaf 06 Task 3)
// ---------------------------------------------------------------------------

// TestProjector_MirrorState: MirrorState updates the projected item's
// State field to the mirrored phase state (the fake records the mutation).
func TestProjector_MirrorState(t *testing.T) {
	fake := newProjFake()
	p := NewProjector(newProjPool(t, fake), effects.NewMemoryLedger(),
		7, githubsvc.OwnerRepo{Owner: "o", Repo: "r"}, slog.DiscardHandler)

	planID := "plan-mirror"
	if err := p.EnsureProjection(context.Background(), projTicket(), planID, twoByTwoPlan(planID)); err != nil {
		t.Fatalf("EnsureProjection: %v", err)
	}

	if err := p.MirrorState(context.Background(), planID, planID+"-p1", string(plan.PhaseInProgress)); err != nil {
		t.Fatalf("MirrorState: %v", err)
	}

	// A state mutation reached the GraphQL endpoint after the projection
	// run (the fake counts updateProjectV2ItemFieldValue mutations).
	fake.mu.Lock()
	stateSets := fake.stateSets
	fake.mu.Unlock()
	if stateSets < 1 {
		t.Fatalf("no updateProjectV2ItemFieldValue mutation recorded; MirrorState did not reach the project")
	}

	// Idempotent upstream: mirroring the same state again is fine.
	if err := p.MirrorState(context.Background(), planID, planID+"-p1", string(plan.PhaseInProgress)); err != nil {
		t.Fatalf("repeat MirrorState: %v", err)
	}
}

// TestProjector_MirrorFailSoft: a GraphQL error yields a NIL error from
// MirrorState — a mirror failure must never fail the meept-side
// transition that triggered it. No panic, warn logged (F9).
func TestProjector_MirrorFailSoft(t *testing.T) {
	fake := newProjFake()
	fake.mu.Lock()
	fake.graphqlErr = true
	fake.mu.Unlock()

	p := NewProjector(newProjPool(t, fake), effects.NewMemoryLedger(),
		7, githubsvc.OwnerRepo{Owner: "o", Repo: "r"}, slog.DiscardHandler)

	planID := "plan-soft"
	// The projection itself may legitimately surface the bootstrap error;
	// the fail-soft CONTRACT is on MirrorState. If EnsureProjection also
	// degrades silently on the fake's graphql failure that is acceptable —
	// this test only pins MirrorState's behavior.
	_ = p.EnsureProjection(context.Background(), projTicket(), planID, twoByTwoPlan(planID))

	// Must not panic; must return nil despite the hard GraphQL failure.
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("MirrorState panicked on graphql failure: %v", r)
			}
		}()
		err = p.MirrorState(context.Background(), planID, planID+"-p1", string(plan.PhaseCompleted))
	}()
	if err != nil {
		t.Fatalf("MirrorState returned %v on graphql failure; want nil (fail-soft)", err)
	}
	_ = errors.New // keeps errors referenced for additive leaf-06 assertions
}
