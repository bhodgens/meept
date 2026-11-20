package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/table"
	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/tableutil"
)

type stubAgentsRPC struct{}

func (stubAgentsRPC) Call(method string, params any) (json.RawMessage, error) {
	return nil, nil
}

func (stubAgentsRPC) IsConnected() bool { return true }

// Regression: the agents table viewport was never given a width, so the panel
// rendered its header and border with an empty body while navigation worked.
func TestAgentsPanel_ViewRendersRows(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)

	p.agents = []AgentSummary{{
		ID:             "agent-alpha",
		Name:           "Alpha",
		Status:         "running",
		Tier:           "tier_1_reactive",
		LastInvocation: time.Now(),
	}}
	p.updateAgentsTable()

	out := p.View()
	if !strings.Contains(out, "agent-alpha") {
		t.Errorf("agents panel body is empty; want the agent id in the rendered table\n%s", out)
	}
	if !strings.Contains(out, "last run") {
		t.Errorf("agents panel is missing the 'last run' column header\n%s", out)
	}
}

// Regression: resizeColumns cleared the rows and nothing repopulated them, so a
// terminal resize blanked the table until the next data event.
func TestAgentsPanel_ResizeKeepsRows(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)
	p.agents = []AgentSummary{{ID: "agent-alpha", Status: "running"}}
	p.updateAgentsTable()

	p.SetSize(140, 40)

	if got := len(p.table.Rows()); got != 1 {
		t.Fatalf("rows after resize = %d, want 1 (resize must not blank the table)", got)
	}
	if !strings.Contains(p.View(), "agent-alpha") {
		t.Error("agents panel body is empty after a resize")
	}
}

// Row cells must always match the column count; bubbles/table panics otherwise.
func TestAgentsPanel_RowWidthMatchesColumns(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)
	p.agents = []AgentSummary{{ID: "agent-alpha", Status: "running"}}
	p.updateAgentsTable()

	cols := len(p.table.Columns())
	for i, row := range p.table.Rows() {
		if len(row) != cols {
			t.Errorf("row %d has %d cells, table has %d columns", i, len(row), cols)
		}
	}
}

// The goals sub-view (key 4) renders the cached goals through the
// tableutil-sized goals table, with lowercase health labels.
func TestAgentsPanel_GoalsViewRendersRows(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)
	p.selectedID = "agent-alpha"
	p.subView = agentsViewGoals
	p.goals = []AgentGoal{
		{ID: "g1", Title: "keep main shippable", Health: "healthy", State: "active"},
		{ID: "g2", Title: "nightly report", Health: "at_risk", State: "active"},
	}
	p.updateGoalsTable()

	out := p.View()
	if !strings.Contains(out, "keep main shippable") {
		t.Errorf("goals view body is empty; want the goal title in the rendered table\n%s", out)
	}
	if !strings.Contains(out, "healthy") || !strings.Contains(out, "at_risk") {
		t.Errorf("goals view missing health labels\n%s", out)
	}
	for _, banned := range []string{"Healthy", "At_Risk", "Broken"} {
		if strings.Contains(out, banned) {
			t.Errorf("goals view renders non-lowercase label %q", banned)
		}
	}

	// Goal rows must match the goals-table column count.
	cols := len(p.goalsTable.Columns())
	for i, row := range p.goalsTable.Rows() {
		if len(row) != cols {
			t.Errorf("goal row %d has %d cells, table has %d columns", i, len(row), cols)
		}
	}
}

// A resize must not blank the goals table (same regression class as the
// agents list table).
func TestAgentsPanel_GoalsViewResizeKeepsRows(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)
	p.selectedID = "agent-alpha"
	p.subView = agentsViewGoals
	p.goals = []AgentGoal{{ID: "g1", Title: "keep main shippable", Health: "healthy"}}
	p.updateGoalsTable()

	p.SetSize(140, 40)

	if got := len(p.goalsTable.Rows()); got != 1 {
		t.Fatalf("goal rows after resize = %d, want 1 (resize must not blank the table)", got)
	}
	if !strings.Contains(p.View(), "keep main shippable") {
		t.Error("goals view body is empty after a resize")
	}
}

// columnWidthSum returns the summed cell-text widths of a table and the
// content budget its viewport leaves for that many cells.
func columnWidthSum(tbl table.Model) (sum, budget int) {
	cols := tbl.Columns()
	for _, c := range cols {
		sum += c.Width
	}
	return sum, tableutil.ContentBudget(tbl.Width(), len(cols))
}

// M12 pin: the agents + goals tables must size their columns through
// tableutil (FitWidthsMin/ContentBudget) so the summed widths never exceed
// the viewport's cell-text budget. The pre-fix hand-computed widths summed
// to 72 against a 39 budget at the 80x24 default (panel width 55 = 80 minus
// the 25-column sidebar) and rendered a bordered box wider than its
// container. Checked at the default layout and at a narrow width.
func TestAgentsPanel_ColumnWidthsFitContentBudget(t *testing.T) {
	for _, width := range []int{55, 30, 120} {
		p := NewAgentsPanel(stubAgentsRPC{})
		p.SetSize(width, 24)
		if sum, budget := columnWidthSum(p.table); sum > budget {
			t.Errorf("agents table at width %d: columns sum %d > content budget %d (viewport %d)",
				width, sum, budget, p.table.Width())
		}

		p.subView = agentsViewGoals
		p.SetSize(width, 24)
		if sum, budget := columnWidthSum(p.goalsTable); sum > budget {
			t.Errorf("goals table at width %d: columns sum %d > content budget %d (viewport %d)",
				width, sum, budget, p.goalsTable.Width())
		}
	}
}

// acp-employees phase 3 display parity: the exec column cell is "acp"
// when the employee references an acp-backed executor agent, "-" for the
// default local executor (pure formatting, lowercase per the UI rule).
func TestExecutorListMarker(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"set renders acp", "hermes", "acp"},
		{"mixed-case id still acp", "Hermes-1", "acp"},
		{"empty renders dash", "", "-"},
		{"whitespace renders dash", "   ", "-"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := executorListMarker(tc.in); got != tc.want {
				t.Errorf("executorListMarker(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The detail line renders "executor: <id> (acp)" with the id lowercased;
// an empty reference renders no line (matching the CLI's omission).
func TestExecutorDetailLine(t *testing.T) {
	if got := executorDetailLine("Hermes"); got != "executor: hermes (acp)" {
		t.Errorf("executorDetailLine(\"Hermes\") = %q, want %q", got, "executor: hermes (acp)")
	}
	if got := executorDetailLine(""); got != "" {
		t.Errorf("executorDetailLine(\"\") = %q, want empty", got)
	}
	if got := executorDetailLine("  bob "); got != "executor: bob (acp)" {
		t.Errorf("executorDetailLine(\"  bob \") = %q, want %q", got, "executor: bob (acp)")
	}
}

// The agents list table must render the acp marker for an executor-backed
// employee and the dash for a plain one, with row widths matching the
// eight columns (tableutil discipline).
func TestAgentsPanel_ListRendersAcpMarker(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(140, 30)

	p.agents = []AgentSummary{
		{ID: "emp-acp", Status: "running", Tier: "tier_2_propose", ExecutorAgent: "hermes"},
		{ID: "emp-local", Status: "running", Tier: "tier_1_reactive"},
	}
	p.updateAgentsTable()

	cols := len(p.table.Columns())
	for i, row := range p.table.Rows() {
		if len(row) != cols {
			t.Fatalf("row %d has %d cells, table has %d columns", i, len(row), cols)
		}
	}
	if got := p.table.Rows()[0][cols-1]; got != "acp" {
		t.Errorf("exec cell for acp-backed employee = %q, want %q", got, "acp")
	}
	if got := p.table.Rows()[1][cols-1]; got != "-" {
		t.Errorf("exec cell for local executor = %q, want %q", got, "-")
	}
	if !strings.Contains(p.View(), "exec") {
		t.Errorf("agents list is missing the 'exec' column header\n%s", p.View())
	}
}

// The detail view renders the executor line when the drill-in carried
// an executor_agent, and omits it otherwise.
func TestAgentsPanel_DetailRendersExecutorLine(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(140, 30)
	p.detail = &AgentDetail{Agent: AgentSummary{ID: "emp-acp", Name: "Emp Acp", Tier: "tier_2_propose", ExecutorAgent: "Hermes"}}
	p.subView = agentsViewDetail

	out := p.View()
	if !strings.Contains(out, "executor:") || !strings.Contains(out, "hermes (acp)") {
		t.Errorf("detail view missing executor line\n%s", out)
	}

	p.detail = &AgentDetail{Agent: AgentSummary{ID: "emp-local", Name: "Emp Local", Tier: "tier_1_reactive"}}
	out = p.View()
	if strings.Contains(out, "executor:") {
		t.Errorf("detail view renders an executor line for a local-executor employee\n%s", out)
	}
}

// detailErrRPC answers the roster fetch but fails agents.get the way the
// daemon does for bots/registry agents (employee-only RPC).
type detailErrRPC struct{}

func (detailErrRPC) Call(method string, _ any) (json.RawMessage, error) {
	if method == "agents.get" {
		return nil, fmt.Errorf("employee not found")
	}
	return json.RawMessage(rosterPayloadJSON), nil
}

func (detailErrRPC) IsConnected() bool { return true }

// M10 pin: Enter on a non-employee roster row fails agents.get. The error
// path must surface the failure inside the detail view (not an endless
// "loading agents...") and esc must return to the list.
func TestAgentsPanel_DetailFetchErrorRendersErrorNotLoading(t *testing.T) {
	p := NewAgentsPanel(detailErrRPC{})
	p.SetSize(120, 30)

	// Load the roster through the real fetch chain.
	if msg := p.fetchAgents(); msg != nil {
		p.Update(msg)
	}
	if len(p.agents) == 0 {
		t.Fatal("roster did not load")
	}

	// Enter drills into the first row (a registry agent, type "agent").
	cmd := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no fetch command")
	}
	p.Update(cmd()) // agentsDetailMsg{err}

	if p.subView != agentsViewDetail {
		t.Fatalf("subView = %v, want detail after enter", p.subView)
	}
	out := p.View()
	if strings.Contains(out, "loading agents...") {
		t.Errorf("failed drill-in renders the endless loading state\n%s", out)
	}
	if !strings.Contains(out, "employee not found") {
		t.Errorf("failed drill-in does not surface the RPC error\n%s", out)
	}
	if !strings.Contains(out, "esc: back to list") {
		t.Errorf("failed drill-in does not advertise the esc affordance\n%s", out)
	}

	// esc keeps its behavior: back to the list with the rows intact.
	p.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if p.subView != agentsViewList {
		t.Fatalf("esc left subView = %v, want list", p.subView)
	}
	if p.detail != nil {
		t.Error("esc did not clear the detail snapshot")
	}
	if !strings.Contains(p.View(), "a-1") {
		t.Errorf("list view lost its roster rows after the failed drill-in\n%s", p.View())
	}
}

// M10 variant: a stale detail snapshot from a previous successful drill-in
// must not mask a later failed one — the error path clears the snapshot.
func TestAgentsPanel_DetailFetchErrorClearsStaleDetail(t *testing.T) {
	p := NewAgentsPanel(detailErrRPC{})
	p.SetSize(120, 30)
	p.detail = &AgentDetail{Agent: AgentSummary{ID: "old-employee"}}
	p.subView = agentsViewDetail
	p.selectedID = "b-1"

	p.Update(agentsDetailMsg{err: fmt.Errorf("employee not found")})
	if p.detail != nil {
		t.Fatal("error path kept the stale detail snapshot")
	}
	out := p.View()
	if strings.Contains(out, "old-employee") {
		t.Errorf("stale snapshot rendered over the error\n%s", out)
	}
	if !strings.Contains(out, "employee not found") {
		t.Errorf("error not rendered in the detail view\n%s", out)
	}
}

// L17 pin: below width 30 the panel must not keep the 98-wide default
// 9-column set installed. The narrow path installs a minimal column set
// whose widths fit the viewport's content budget, and cached rows are
// normalized to it without a panic.
func TestAgentsPanel_NarrowWidthColumnsFit(t *testing.T) {
	for _, width := range []int{20, 29} {
		p := NewAgentsPanel(stubAgentsRPC{})
		p.SetSize(width, 24)

		cols := p.table.Columns()
		if len(cols) >= 9 {
			t.Errorf("width %d kept the full %d-column set", width, len(cols))
		}
		if sum, budget := columnWidthSum(p.table); sum > budget {
			t.Errorf("width %d: columns sum %d > content budget %d", width, sum, budget)
		}

		// Cached roster rows repopulate through tableutil.SetRows and
		// must match the narrow column count.
		p.agents = []AgentSummary{{ID: "agent-alpha", Status: "running"}}
		p.updateAgentsTable()
		for i, row := range p.table.Rows() {
			if len(row) != len(cols) {
				t.Errorf("width %d: row %d has %d cells, table has %d columns", width, i, len(row), len(cols))
			}
		}
	}
}

// Regression (P3): a live result that reaches the TUI just before the server
// poll can be newer than the snapshot the RPC returns (projector lag). A
// wholesale p.runs = msg.runs made it vanish for one poll and reappear on the
// next. The reconciler must carry rows newer than the server max timestamp.
func TestExecutionRunsMsg_PreservesLiveRowsNewerThanServer(t *testing.T) {
	base := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)

	// A live frame arrives after the last poll, stamped newer than anything
	// the server page will contain.
	live := AgentRun{
		Topic:     "employee.result",
		Tier:      "employee",
		AgentID:   "live-agent",
		Status:    "ok",
		Output:    "live result",
		Timestamp: base.Add(2 * time.Minute),
	}
	p.Update(executionResultMsg{Run: live})
	if len(p.runs) != 1 {
		t.Fatalf("after live frame: runs = %d, want 1", len(p.runs))
	}

	// The server page misses the live row: its newest row is the older one.
	server := []AgentRun{{
		Topic:     "employee.result",
		Tier:      "employee",
		AgentID:   "server-agent",
		Status:    "ok",
		Output:    "persisted result",
		Timestamp: base,
	}}
	p.Update(executionRunsMsg{runs: server})

	found := false
	for _, r := range p.runs {
		if runKey(r) == runKey(live) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("live row newer than the server snapshot was dropped: runs = %+v", p.runs)
	}
	if len(p.runs) != 2 {
		t.Fatalf("runs = %d, want 2 (live row preserved ahead of server page)", len(p.runs))
	}
	if runKey(p.runs[0]) != runKey(live) {
		t.Fatalf("runs[0] = %+v, want the live row first", p.runs[0])
	}
}

// H1 pin (issue #160 audit): the SAME execution arrives twice - once as a
// live bus frame stamped with nanosecond publish time, once in the server
// page whose created_at the store truncates to whole seconds. The composite
// ns-vs-second key never matched, so every poll re-preserved the live copy
// alongside the persisted row and the runs view doubled every execution
// permanently. After the merge exactly ONE row must remain, and repeated
// polls must not accumulate copies.
func TestExecutionRunsMsg_DedupsLiveRowAgainstServerPage(t *testing.T) {
	base := time.Date(2026, 11, 19, 12, 0, 5, 0, time.UTC)
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)

	live := AgentRun{
		Topic:     "employee.result",
		Tier:      "employee",
		AgentID:   "emp-1",
		Status:    "completed",
		Output:    "same execution",
		Timestamp: base.Add(500 * time.Millisecond), // publish-time ns precision
	}
	p.Update(executionResultMsg{Run: live})

	// The store persisted the same execution with a second-truncated stamp.
	server := []AgentRun{{
		Topic:     "employee.result",
		Tier:      "employee",
		AgentID:   "emp-1",
		Status:    "completed",
		Output:    "same execution",
		Timestamp: base,
	}}
	p.Update(executionRunsMsg{runs: server})
	if len(p.runs) != 1 {
		t.Fatalf("runs = %d, want 1 (live copy of a persisted execution must dedup)\n%+v", len(p.runs), p.runs)
	}

	// A second poll of the same page must not re-introduce the live copy.
	p.Update(executionRunsMsg{runs: server})
	if len(p.runs) != 1 {
		t.Fatalf("runs after second poll = %d, want 1 (dedup must be stable across polls)", len(p.runs))
	}
}

// H1 companion pin: dedup must not collapse genuinely different executions.
// Two persisted runs of the SAME agent one second apart, their live copies,
// plus one live run genuinely newer than the page: exactly three rows.
func TestExecutionRunsMsg_DistinctExecutionsSurvive(t *testing.T) {
	base := time.Date(2026, 11, 19, 12, 0, 5, 0, time.UTC)
	server := []AgentRun{
		{Topic: "employee.result", Tier: "employee", AgentID: "emp-1", Status: "completed", Timestamp: base.Add(time.Second)},
		{Topic: "employee.result", Tier: "employee", AgentID: "emp-1", Status: "completed", Timestamp: base},
	}
	live := []AgentRun{
		{Topic: "employee.result", Tier: "employee", AgentID: "emp-1", Status: "completed", Timestamp: base.Add(2*time.Second + 250*time.Millisecond)},
		{Topic: "employee.result", Tier: "employee", AgentID: "emp-1", Status: "completed", Timestamp: base.Add(time.Second + 500*time.Millisecond)},
		{Topic: "employee.result", Tier: "employee", AgentID: "emp-1", Status: "completed", Timestamp: base.Add(500 * time.Millisecond)},
	}
	merged := mergeLiveRuns(server, live)
	if len(merged) != 3 {
		t.Fatalf("merged = %d rows, want 3 (two persisted executions + one genuinely newer live row)\n%+v", len(merged), merged)
	}
}

// M2 pin (partial, pre-seam): re-pressing '5' while already in the runs
// sub-view must not arm another fetch/tick chain.
func TestAgentsPanel_RunsReentryDoesNotRearm(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)
	p.subView = agentsViewRuns

	if cmd := p.Update(tea.KeyPressMsg{Code: '5', Text: "5"}); cmd != nil {
		t.Fatalf("re-pressing '5' inside the runs view returned a command (%T); it must not re-arm the poll chain", cmd)
	}
}

// L2 pin: an executionRunsMsg error must surface a lowercase error line in
// the runs view (GUI parity: state.error). Empty cache keeps the empty-state
// text plus the error hint; a cached page is kept, not blanked.
func TestAgentsPanel_RunsErrorLine(t *testing.T) {
	p := NewAgentsPanel(stubAgentsRPC{})
	p.SetSize(120, 30)
	p.subView = agentsViewRuns

	// Empty cache + degraded store: empty state AND the error hint.
	p.Update(executionRunsMsg{err: fmt.Errorf("execution store unavailable")})
	out := p.View()
	if !strings.Contains(out, "no executions recorded yet") {
		t.Errorf("empty runs view lost its empty-state text on error\n%s", out)
	}
	if !strings.Contains(out, "error:") || !strings.Contains(out, "execution store unavailable") {
		t.Errorf("runs view does not surface the RPC error (GUI shows state.error)\n%s", out)
	}

	// A good page renders rows and clears the error line.
	base := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	p.Update(executionRunsMsg{runs: []AgentRun{{
		Topic: "bot.result", Tier: "bot", AgentID: "bot-1", Status: "completed", Timestamp: base,
	}}})
	out = p.View()
	if strings.Contains(out, "error:") {
		t.Errorf("error line survived a successful poll\n%s", out)
	}
	if !strings.Contains(out, "bot-1") {
		t.Errorf("successful page did not render\n%s", out)
	}

	// A later failure keeps the last page and shows the error line again.
	p.Update(executionRunsMsg{err: fmt.Errorf("rpc timeout")})
	out = p.View()
	if !strings.Contains(out, "bot-1") {
		t.Errorf("error path blanked the last good page\n%s", out)
	}
	if !strings.Contains(out, "error:") || !strings.Contains(out, "rpc timeout") {
		t.Errorf("error line missing while the last page is shown\n%s", out)
	}
}

// runsCountRPC counts agents.executionRuns calls and serves one persisted
// row carrying the store id (the wire shape the pre-fix decoder dropped).
type runsCountRPC struct {
	calls int
}

func (r *runsCountRPC) Call(method string, _ any) (json.RawMessage, error) {
	if method == "agents.executionRuns" {
		r.calls++
		return json.RawMessage(`{"runs":[{"id":7,"tier":"bot","agent_id":"bot-1","status":"completed","created_at":"2026-11-19T12:00:05Z"}],"total":1}`), nil
	}
	return json.RawMessage(`{}`), nil
}

func (r *runsCountRPC) IsConnected() bool { return true }

// deliverRunsCmd executes one command returned by the panel and feeds every
// message it produces back through Update, returning the follow-up commands
// Update produced (tick arming). tea.Tick cmds are one-shot timers: each is
// invoked exactly once.
func deliverRunsCmd(t *testing.T, p *AgentsPanel, cmd tea.Cmd) []tea.Cmd {
	t.Helper()
	if cmd == nil {
		return nil
	}
	return deliverRunsMsg(t, p, cmd())
}

func deliverRunsMsg(t *testing.T, p *AgentsPanel, msg tea.Msg) []tea.Cmd {
	t.Helper()
	var follow []tea.Cmd
	switch m := msg.(type) {
	case tea.BatchMsg:
		for _, c := range m {
			follow = append(follow, deliverRunsCmd(t, p, c)...)
		}
	case nil:
	default:
		if c := p.Update(m); c != nil {
			follow = append(follow, c)
		}
	}
	return follow
}

// H1 decode pin: fetchExecutionRuns must decode the store id the RPC
// returns (execution_store.go ExecutionRun.ID json:"id") onto AgentRun.ID.
func TestFetchExecutionRuns_DecodesStoreID(t *testing.T) {
	rpc := &runsCountRPC{}
	p := NewAgentsPanel(rpc)
	p.SetSize(120, 30)

	var page *executionRunsMsg
	var collect func(tea.Msg)
	collect = func(msg tea.Msg) {
		switch m := msg.(type) {
		case tea.BatchMsg:
			for _, c := range m {
				collect(c())
			}
		case executionRunsMsg:
			page = &m
		}
	}
	collect(p.fetchExecutionRuns())
	if page == nil {
		t.Fatal("fetch produced no executionRunsMsg")
	}
	if len(page.runs) != 1 || page.runs[0].ID != 7 {
		t.Fatalf("decoded runs = %+v, want one row with store ID 7", page.runs)
	}
}

// H1 ID-match pin: when both rows carry a store id, identity is the id -
// even when the timestamps differ (a re-stamped or skewed copy).
func TestMergeLiveRuns_MatchesByStoreID(t *testing.T) {
	base := time.Date(2026, 11, 19, 12, 0, 5, 0, time.UTC)
	server := []AgentRun{{ID: 7, Topic: "bot.result", Tier: "bot", AgentID: "bot-1", Status: "completed", Timestamp: base}}
	live := []AgentRun{{ID: 7, Topic: "bot.result", Tier: "bot", AgentID: "bot-1", Status: "completed", Timestamp: base.Add(900 * time.Millisecond)}}
	if merged := mergeLiveRuns(server, live); len(merged) != 1 {
		t.Fatalf("merged = %d rows, want 1 (same store id is the same execution)", len(merged))
	}
	// A different store id on the same agent/second is a different execution.
	live2 := []AgentRun{{ID: 8, Topic: "bot.result", Tier: "bot", AgentID: "bot-1", Status: "completed", Timestamp: base.Add(900 * time.Millisecond)}}
	if merged := mergeLiveRuns(server, live2); len(merged) != 2 {
		t.Fatalf("merged = %d rows, want 2 (different store ids must both survive)", len(merged))
	}
}

// M2 pin: entry + response + re-entry must keep exactly ONE fetch/tick
// chain. Counts real RPC fetches through the panel's Update path. The
// armed tick cmd is invoked once and its message HELD (not delivered)
// while the pending-guard assertions run - a fired timer is no longer a
// pending one.
func TestAgentsPanel_RunsPollChainSingleArm(t *testing.T) {
	rpc := &runsCountRPC{}
	p := NewAgentsPanel(rpc)
	p.SetSize(120, 30)
	p.runsPollInterval = time.Millisecond

	// Entry: one immediate fetch + one armed tick.
	entry := p.Update(tea.KeyPressMsg{Code: '5', Text: "5"})
	if entry == nil {
		t.Fatal("entry produced no command")
	}
	batch, ok := entry().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("entry cmd returned %T, want a 2-cmd batch (fetch + tick)", entry())
	}
	// Run the fetch half; hold the tick half.
	follow := deliverRunsCmd(t, p, batch[0])
	if rpc.calls != 1 {
		t.Fatalf("entry fetches = %d, want 1", rpc.calls)
	}
	if len(follow) != 0 {
		t.Fatalf("fetch response armed %d commands while a tick was pending, want 0", len(follow))
	}
	tickMsg := batch[1]() // invoke the one-shot timer exactly once; hold the msg
	if _, ok := tickMsg.(executionRunsTickMsg); !ok {
		t.Fatalf("tick cmd returned %T, want executionRunsTickMsg", tickMsg)
	}
	// While the tick is still armed, the scheduler must not arm another.
	if c := p.scheduleExecutionRunsTick(); c != nil {
		t.Fatal("tick scheduler armed a second tick while one was pending")
	}

	// Re-press '5' while in the runs view: no new fetch, no new tick.
	if c := p.Update(tea.KeyPressMsg{Code: '5', Text: "5"}); c != nil {
		t.Fatalf("re-entry returned a command (%T); chain would multiply", c)
	}
	if rpc.calls != 1 {
		t.Fatalf("fetches after re-entry = %d, want 1", rpc.calls)
	}

	// Deliver the held tick: the handler returns the fetch CMD (it does
	// not run it); execute that cmd to perform the fetch and deliver its
	// response, which arms exactly one successor tick. Repeat 3 cycles.
	for cycle := 0; cycle < 3; cycle++ {
		fetchCmds := deliverRunsMsg(t, p, tickMsg)
		if len(fetchCmds) != 1 {
			t.Fatalf("cycle %d: tick returned %d commands, want exactly 1 fetch cmd", cycle, len(fetchCmds))
		}
		next := deliverRunsCmd(t, p, fetchCmds[0])
		if rpc.calls != 2+cycle {
			t.Fatalf("cycle %d: fetches = %d, want %d (one fetch per tick)", cycle, rpc.calls, 2+cycle)
		}
		if len(next) != 1 {
			t.Fatalf("cycle %d: %d successor commands, want exactly 1 tick", cycle, len(next))
		}
		tickMsg = next[0]()
		if _, ok := tickMsg.(executionRunsTickMsg); !ok {
			t.Fatalf("cycle %d: successor returned %T, want executionRunsTickMsg", cycle, tickMsg)
		}
		if c := p.scheduleExecutionRunsTick(); c != nil {
			t.Fatalf("cycle %d: scheduler armed a competing tick", cycle)
		}
	}
}

// M2 inflight pin: a second fetch landing while one RPC is in flight is
// dropped; the done message released by the first fetch re-opens the gate.
func TestAgentsPanel_RunsFetchInflightDedup(t *testing.T) {
	rpc := &runsCountRPC{}
	p := NewAgentsPanel(rpc)
	p.SetSize(120, 30)

	first := p.fetchExecutionRuns() // inflight stays set until the done msg lands
	if first == nil {
		t.Fatal("first fetch returned nil")
	}
	if second := p.fetchExecutionRuns(); second != nil {
		t.Fatalf("second fetch while inflight returned %T, want nil", second)
	}
	if rpc.calls != 1 {
		t.Fatalf("rpc calls = %d, want 1 (inflight fetch must not double-poll)", rpc.calls)
	}
	deliverRunsMsg(t, p, first) // delivers page + done; clears inflight
	if third := p.fetchExecutionRuns(); third == nil {
		t.Fatal("fetch after done returned nil; inflight flag stuck")
	}
	if rpc.calls != 2 {
		t.Fatalf("rpc calls after done = %d, want 2", rpc.calls)
	}
}

// M2 generation pin: exit and re-entry bump the runs generation; a tick
// armed by the previous visit must be dropped, not fetch. Exiting the
// runs view also stops the chain.
func TestAgentsPanel_RunsStaleGenerationTickDropped(t *testing.T) {
	rpc := &runsCountRPC{}
	p := NewAgentsPanel(rpc)
	p.SetSize(120, 30)
	p.runsPollInterval = time.Millisecond

	entry := p.Update(tea.KeyPressMsg{Code: '5', Text: "5"})
	batch, ok := entry().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("entry cmd returned %T, want a 2-cmd batch (fetch + tick)", entry())
	}
	deliverRunsCmd(t, p, batch[0]) // run the fetch half only
	staleMsg := batch[1]()         // invoke the one-shot timer exactly once; hold it
	stale, ok := staleMsg.(executionRunsTickMsg)
	if !ok {
		t.Fatalf("tick cmd returned %T, want executionRunsTickMsg", staleMsg)
	}

	// Exit to the list, then re-enter: the held tick is now stale.
	p.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if c := p.Update(stale); c != nil {
		t.Fatalf("stale tick after exit returned %T, want nil", c)
	}
	reentry := p.Update(tea.KeyPressMsg{Code: '5', Text: "5"})
	rebatch, ok := reentry().(tea.BatchMsg)
	if !ok || len(rebatch) != 2 {
		t.Fatalf("re-entry cmd returned %T, want a 2-cmd batch", reentry())
	}
	deliverRunsCmd(t, p, rebatch[0])
	calls := rpc.calls
	if c := p.Update(stale); c != nil {
		t.Fatalf("stale-generation tick after re-entry returned %T, want nil (second chain)", c)
	}
	if rpc.calls != calls {
		t.Fatalf("stale tick fetched: calls %d -> %d", calls, rpc.calls)
	}

	// A current-generation tick still fetches (normal polling intact).
	if c := p.Update(executionRunsTickMsg{generation: p.runsGeneration}); c == nil {
		t.Fatal("current-generation tick was dropped; polling broke")
	}
}
