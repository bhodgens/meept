package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// fakeActivityClient implements ActivityRPCClient for testing (pattern:
// fakeReviewClient in memory_review_test.go).
type fakeActivityClient struct {
	connected bool

	queryResult []ActivityRecordJSON
	queryTotal  int
	queryErr    error
	queryCalls  int
	lastQuery   *ActivityQueryParams

	recentResult  []ActivityRecordJSON
	recentDropped int
	recentCalls   int

	findingsResult []FindingJSON
	findingsTotal  int
	findingsErr    error
	findingsCalls  int
	lastFindings   *FindingsParams

	reviewResult DriftJSON
	reviewErr    error
	reviewCalls  int
	lastReviewID string
}

func newFakeActivityClient() *fakeActivityClient {
	return &fakeActivityClient{connected: true}
}

func (f *fakeActivityClient) ActivityQuery(params ActivityQueryParams) ([]ActivityRecordJSON, int, error) {
	f.queryCalls++
	p := params
	f.lastQuery = &p
	if f.queryErr != nil {
		return nil, 0, f.queryErr
	}
	return f.queryResult, f.queryTotal, nil
}

func (f *fakeActivityClient) ActivityRecent(limit int) ([]ActivityRecordJSON, int, int, error) {
	f.recentCalls++
	if f.queryErr != nil {
		return nil, 0, 0, f.queryErr
	}
	return f.recentResult, f.recentDropped, len(f.recentResult) + f.recentDropped, nil
}

func (f *fakeActivityClient) ActivityFindings(params FindingsParams) ([]FindingJSON, int, error) {
	f.findingsCalls++
	p := params
	f.lastFindings = &p
	if f.findingsErr != nil {
		return nil, 0, f.findingsErr
	}
	return f.findingsResult, f.findingsTotal, nil
}

func (f *fakeActivityClient) AgentsReview(employeeID string) (DriftJSON, error) {
	f.reviewCalls++
	f.lastReviewID = employeeID
	if f.reviewErr != nil {
		return DriftJSON{}, f.reviewErr
	}
	return f.reviewResult, nil
}

func (f *fakeActivityClient) IsConnected() bool { return f.connected }

// rec builds an ActivityRecordJSON the way the C2 result decodes.
func rec(kind, at, agentID, taskID, planID, state, severity, summary string) ActivityRecordJSON {
	return ActivityRecordJSON{
		Kind: kind, At: at, AgentID: agentID, TaskID: taskID, PlanID: planID,
		State: state, Severity: severity, Summary: summary,
	}
}

func newTestActivityModel() (*ActivityModel, *fakeActivityClient) {
	fake := newFakeActivityClient()
	m := NewActivityModel(fake)
	m.SetSize(80, 24)
	return m, fake
}

func press(m *ActivityModel, key string) tea.Cmd {
	return m.Update(tea.KeyPressMsg{Code: rune(key[0]), Text: key})
}

// ---------------------------------------------------------------------------
// T1: contract JSON tags
// ---------------------------------------------------------------------------

func TestActivityModel_ContractJSONTags(t *testing.T) {
	// The models decode the daemon's result JSON byte-identically (C2/C4);
	// round-trip through the wire field names.
	now := time.Now().UTC().Format(time.RFC3339)
	raw := fmt.Sprintf(`{"records":[{"kind":"task_step","at":%q,"agent_id":"emp-1","session_id":"sess-1","task_id":"task-1","step_id":"step-1","plan_id":"plan-1","state":"running","severity":"warning","summary":"step running","detail":{"k":"v"}}],"total":7}`, now)
	var resp ActivityQueryResponse
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatalf("decode ActivityQueryResponse: %v", err)
	}
	if resp.Total != 7 || len(resp.Records) != 1 {
		t.Fatalf("unexpected decode: %+v", resp)
	}
	r := resp.Records[0]
	for _, c := range []struct{ name, got, want string }{
		{"kind", r.Kind, "task_step"},
		{"at", r.At, now},
		{"agent_id", r.AgentID, "emp-1"},
		{"session_id", r.SessionID, "sess-1"},
		{"task_id", r.TaskID, "task-1"},
		{"step_id", r.StepID, "step-1"},
		{"plan_id", r.PlanID, "plan-1"},
		{"state", r.State, "running"},
		{"severity", r.Severity, "warning"},
		{"summary", r.Summary, "step running"},
	} {
		if c.got != c.want {
			t.Errorf("field %s: got %q, want %q", c.name, c.got, c.want)
		}
	}
	if r.Detail["k"] != "v" {
		t.Errorf("detail map did not decode: %+v", r.Detail)
	}
}

// ---------------------------------------------------------------------------
// T1/T2: fetch + sort modes
// ---------------------------------------------------------------------------

func TestActivityModel_FetchChrono(t *testing.T) {
	m, fake := newTestActivityModel()
	fake.queryResult = []ActivityRecordJSON{rec("bus_event", "2026-10-30T10:00:00Z", "emp-1", "task-1", "", "running", "", "fresh row")}
	fake.queryTotal = 1
	cmd := m.fetch()
	if cmd == nil {
		t.Fatal("expected fetch cmd")
	}
	msg := cmd()
	m.Update(msg)
	if fake.lastQuery == nil || fake.lastQuery.Sort != SortChrono {
		t.Fatalf("expected default sort chrono, got %+v", fake.lastQuery)
	}
	am, ok := msg.(ActivityUpdateMsg)
	if !ok {
		t.Fatalf("expected ActivityUpdateMsg, got %T", msg)
	}
	if am.Err != nil {
		t.Fatalf("unexpected fetch error: %v", am.Err)
	}
	if len(m.records) != 1 || m.records[0].Summary != "fresh row" {
		t.Fatalf("records not applied: %+v", m.records)
	}
}

func TestActivityModel_TabCyclesSortMode(t *testing.T) {
	m, fake := newTestActivityModel()
	want := []string{SortTask, SortPlan, SortChrono}
	for i, w := range want {
		fetchCmd := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
		if m.SortMode() != w {
			t.Fatalf("press %d: mode = %q, want %q", i, m.SortMode(), w)
		}
		if fetchCmd == nil {
			t.Fatalf("press %d: expected refetch cmd", i)
		}
		fetchCmd()
		if fake.lastQuery == nil || fake.lastQuery.Sort != w {
			t.Fatalf("press %d: query sort = %+v, want %q", i, fake.lastQuery, w)
		}
		if !m.Loading() {
			t.Fatalf("press %d: expected loading during refetch", i)
		}
	}
}

func TestActivityModel_TaskSortGroupsByTaskID(t *testing.T) {
	m, fake := newTestActivityModel()
	// The daemon returns the canonical order for sort=task: grouped by
	// task id (empty last), time ascending within the group.
	base := time.Date(2026, 10, 30, 10, 0, 0, 0, time.UTC)
	fake.queryResult = []ActivityRecordJSON{
		rec("task_step", base.Format(time.RFC3339), "emp-1", "task-a", "", "running", "", "a first"),
		rec("task_step", base.Add(time.Minute).Format(time.RFC3339), "emp-2", "task-a", "", "completed", "", "a second"),
		rec("bus_event", base.Add(2*time.Minute).Format(time.RFC3339), "emp-1", "task-b", "", "started", "", "b first"),
		rec("session_message", base.Add(5*time.Minute).Format(time.RFC3339), "emp-1", "", "", "", "", "ungrouped last"),
	}
	m.SetSortMode(SortTask)
	m.Update(m.fetch()())

	rows := m.table.Rows()
	if len(rows) != 7 {
		t.Fatalf("expected 7 rows (3 headers + 4 records), got %d", len(rows))
	}
	if !strings.Contains(rows[0][0], "task-a") {
		t.Fatalf("expected task-a header row first, got %+v", rows[0])
	}
	if !strings.Contains(rows[3][0], "task-b") {
		t.Fatalf("expected task-b header at row 3, got %+v", rows[3])
	}
	if !strings.Contains(rows[5][0], "no task") {
		t.Fatalf("expected empty-task header last group, got %+v", rows[5])
	}
	if rows[1][2] != "a first" {
		t.Fatalf("expected first task-a row summary 'a first', got %+v", rows[1])
	}
}

func TestActivityModel_PlanSortGroupsByPlanID(t *testing.T) {
	m, fake := newTestActivityModel()
	fake.queryResult = []ActivityRecordJSON{
		rec("task_step", "2026-10-30T10:00:00Z", "emp-1", "task-1", "plan-a", "running", "", "plan a work"),
		rec("audit_record", "2026-10-30T10:01:00Z", "emp-1", "task-2", "plan-b", "", "info", "plan b work"),
	}
	m.SetSortMode(SortPlan)
	m.Update(m.fetch()())

	rows := m.table.Rows()
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows (2 headers + 2 records), got %d", len(rows))
	}
	if !strings.Contains(rows[0][0], "plan-a") || !strings.Contains(rows[2][0], "plan-b") {
		t.Fatalf("expected plan headers in canonical order, got %+v / %+v", rows[0], rows[2])
	}
}

// ---------------------------------------------------------------------------
// T3: live tail
// ---------------------------------------------------------------------------

func TestActivityModel_TailToggleAndTick(t *testing.T) {
	m, fake := newTestActivityModel()

	// Tail off by default: a tick must not poll.
	m.Update(ActivityTickMsg{})
	if fake.recentCalls != 0 {
		t.Fatal("tick polled while tail is off")
	}

	if cmd := press(m, "t"); cmd == nil {
		t.Fatal("tail toggle returned no cmd")
	}
	if !m.TailOn() {
		t.Fatal("tail not on after toggle")
	}

	// The toggle arms the tick; the first poll happens on the tick, not
	// the toggle (one poll path). The tick returns the poll cmd; running
	// it is the runtime's job (mirrored here).
	fake.recentCalls = 0
	fake.recentResult = []ActivityRecordJSON{
		rec("bus_event", "2026-10-30T10:02:00Z", "emp-1", "task-new", "", "started", "", "newest event"),
		rec("bus_event", "2026-10-30T10:01:00Z", "emp-1", "task-old", "", "started", "", "older event"),
	}
	fake.recentDropped = 3
	poll := m.Update(ActivityTickMsg{})
	if poll == nil {
		t.Fatal("tick while tail on returned no poll cmd")
	}
	if fake.recentCalls != 0 {
		t.Fatalf("tick must not poll inline, got %d polls", fake.recentCalls)
	}
	m.Update(poll())
	if fake.recentCalls != 1 {
		t.Fatalf("expected exactly one poll per tick, got %d", fake.recentCalls)
	}
	if m.Dropped() != 3 {
		t.Fatalf("dropped counter = %d, want 3", m.Dropped())
	}
	rows := m.table.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 tail rows, got %d", len(rows))
	}
	if !strings.Contains(rows[0][2], "newest event") {
		t.Fatalf("newest record must render at the top, got %+v", rows[0])
	}
	if !strings.Contains(m.View(), "dropped 3") {
		t.Fatalf("status line missing dropped counter: %s", m.View())
	}

	// Re-toggling stops the tail: the armed tick becomes a no-op.
	press(m, "t")
	if m.TailOn() {
		t.Fatal("tail still on after second toggle")
	}
	fake.recentCalls = 0
	m.Update(ActivityTickMsg{})
	if fake.recentCalls != 0 {
		t.Fatal("tick polled after tail stopped")
	}
}

func TestActivityModel_TailMergeNewestFirst(t *testing.T) {
	m, fake := newTestActivityModel()
	press(m, "t")

	fake.recentResult = []ActivityRecordJSON{rec("bus_event", "2026-10-30T10:00:00Z", "e", "t1", "", "", "", "one")}
	m.Update(m.Update(ActivityTickMsg{})())

	fake.recentResult = append([]ActivityRecordJSON{
		rec("bus_event", "2026-10-30T10:03:00Z", "e", "t2", "", "", "", "three"),
	}, fake.recentResult...)
	fake.recentResult = append(fake.recentResult, rec("bus_event", "2026-10-30T09:00:00Z", "e", "t0", "", "", "", "zero"))
	m.Update(m.Update(ActivityTickMsg{})())

	rows := m.table.Rows()
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows after merge, got %d", len(rows))
	}
	if !strings.Contains(rows[0][2], "three") {
		t.Fatalf("expected newest row first, got %+v", rows[0])
	}
	if !strings.Contains(rows[2][2], "zero") {
		t.Fatalf("expected oldest row last, got %+v", rows[2])
	}
}

func TestActivityModel_HideStopsTailTick(t *testing.T) {
	m, fake := newTestActivityModel()
	press(m, "t")
	if !m.TailOn() {
		t.Fatal("tail not armed")
	}
	if cmd := m.Update(hideActivityViewMsg{}); cmd != nil {
		t.Fatal("hide returned an unexpected cmd")
	}
	if m.TailOn() {
		t.Fatal("tail still on after hide")
	}
	// The tick that was armed before the hide fires but must not poll.
	if poll := m.Update(ActivityTickMsg{}); poll != nil {
		t.Fatal("tick while tail off returned a poll cmd")
	}
	if fake.recentCalls != 0 {
		t.Fatalf("tick polled after hide: %d", fake.recentCalls)
	}
}

// ---------------------------------------------------------------------------
// T4: findings + drift panel
// ---------------------------------------------------------------------------

func TestActivityModel_FindingsPanel(t *testing.T) {
	m, fake := newTestActivityModel()
	fake.findingsResult = []FindingJSON{
		{ID: "f-1", EmployeeID: "emp-1", Severity: "critical", Checkpoint: "post_turn", ViolatedRule: "no destructive commands", Evidence: "rm -rf /tmp/x", DetectedAt: "2026-10-30T10:00:00Z"},
		{ID: "f-2", EmployeeID: "emp-2", Severity: "info", Checkpoint: "periodic", ViolatedRule: "late report", DetectedAt: "2026-10-30T09:00:00Z", ResolvedAt: "2026-10-30T09:30:00Z"},
	}
	fake.findingsTotal = 2
	fake.reviewResult = DriftJSON{EmployeeID: "emp-1", DriftScore: 0.4}
	// fetchDrift reviews every agent seen in the current records page.
	m.records = []ActivityRecordJSON{rec("bus_event", "2026-10-30T10:00:00Z", "emp-1", "t", "", "", "", "s")}

	if cmd := press(m, "p"); cmd == nil {
		t.Fatal("panel toggle returned no cmd")
	}
	if !m.PanelOpen() {
		t.Fatal("panel not open after toggle")
	}

	m.Update(m.fetchFindings()())
	if len(m.findings) != 2 {
		t.Fatalf("findings not applied: %+v", m.findings)
	}

	m.Update(m.fetchDrift()())
	if len(m.drift) != 1 || m.drift[0].DriftScore != 0.4 {
		t.Fatalf("drift not applied: %+v", m.drift)
	}
	if fake.lastReviewID != "emp-1" {
		t.Fatalf("AgentsReview called with %q, want emp-1", fake.lastReviewID)
	}

	if !strings.Contains(m.View(), "0.40") {
		t.Fatalf("drift 0.4 not rendered: %s", m.View())
	}
	if !strings.Contains(m.View(), "no destructive commands") {
		t.Fatalf("finding evidence not rendered: %s", m.View())
	}
	if strings.Contains(m.View(), "rm -rf /tmp/x") {
		t.Fatal("raw evidence must not leak into the panel render")
	}

	// The panel does not disturb the activity table's sort state.
	if m.SortMode() != SortChrono {
		t.Fatalf("sort mode changed by panel open: %q", m.SortMode())
	}

	press(m, "p")
	if m.PanelOpen() {
		t.Fatal("panel still open after second toggle")
	}
}

func TestActivityModel_FindingsFetchError(t *testing.T) {
	m, fake := newTestActivityModel()
	fake.findingsErr = errors.New("boom")
	m.panelOpen = true
	m.Update(m.fetchFindings()())
	if m.err == nil {
		t.Fatal("expected findings fetch error to surface")
	}
	if !strings.Contains(m.View(), "boom") {
		t.Fatalf("error not rendered: %s", m.View())
	}
}

// ---------------------------------------------------------------------------
// errors + window sizing
// ---------------------------------------------------------------------------

func TestActivityModel_FetchError(t *testing.T) {
	m, fake := newTestActivityModel()
	fake.queryErr = errors.New("rpc down")
	m.Update(m.fetch()())
	if m.err == nil || !strings.Contains(m.err.Error(), "rpc down") {
		t.Fatalf("expected rpc error surfaced, got %v", m.err)
	}
	if !strings.Contains(m.View(), "rpc down") {
		t.Fatalf("error not rendered: %s", m.View())
	}
}

func TestActivityModel_WindowSizeResizesTable(t *testing.T) {
	m, _ := newTestActivityModel()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if m.width != 120 || m.height != 40 {
		t.Fatalf("size not stored: %dx%d", m.width, m.height)
	}
	// The table viewport must be sized on BOTH axes (width-0 viewport
	// renders no rows).
	if m.table.Width() <= 0 {
		t.Fatalf("table width not set: %d", m.table.Width())
	}
	// A row that is longer than the column list must not panic on render.
	m.records = []ActivityRecordJSON{rec("bus_event", "2026-10-30T10:00:00Z", "e", "t", "", "", "", "s")}
	m.refreshRows()
	_ = m.View()
}

func TestActivityModel_NotConnectedStillRenders(t *testing.T) {
	fake := newFakeActivityClient()
	fake.connected = false
	m := NewActivityModel(fake)
	m.SetSize(80, 24)
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init must return the initial fetch cmd")
	}
	m.Update(m.fetch()())
	if m.err == nil {
		t.Fatal("expected not-connected error")
	}
}
