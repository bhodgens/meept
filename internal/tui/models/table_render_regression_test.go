package models

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// Regression: the table viewport width stayed at 0 because SetSize only called
// SetHeight. A zero-width viewport renders nothing, so these views showed their
// headers and border with an empty body while cursor navigation worked.

func TestTasksModel_TasksViewRendersRows(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(100, 30)

	model.Update(TasksUpdateMsg{Tasks: mock.TasksExtendedResponse.Tasks})

	out := model.View()
	if !strings.Contains(out, "Test Task") {
		t.Errorf("tasks view body is empty; want the task name in the rendered table\n%s", out)
	}
}

func TestTasksModel_JobsViewRendersRows(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(100, 30)

	model.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // tasks -> jobs
	model.Update(JobsUpdateMsg{Jobs: mock.JobsResponse.Jobs})

	out := model.View()
	if !strings.Contains(out, "Daily Backup") {
		t.Errorf("jobs view body is empty; want the job name in the rendered table\n%s", out)
	}
	if !strings.Contains(out, "schedule") {
		t.Errorf("jobs view is missing the 'schedule' column header\n%s", out)
	}
	if strings.Contains(out, "memory") {
		t.Errorf("jobs view shows the tasks column set\n%s", out)
	}
}

func TestQueueModel_ViewRendersRows(t *testing.T) {
	model := NewQueueModel(&mockQueueRPC{jobs: []types.QueueJob{
		{ID: "queue-job-1", Type: "one_off", Priority: 2, State: "pending", TaskID: "task-9"},
	}})
	model.SetSize(100, 30)

	model.Update(QueueUpdateMsg{Jobs: []types.QueueJob{
		{ID: "queue-job-1", Type: "one_off", Priority: 2, State: "pending", TaskID: "task-9"},
	}})

	out := model.View()
	if !strings.Contains(out, "queue-job-1") {
		t.Errorf("queue view body is empty; want the job id in the rendered table\n%s", out)
	}
}

func TestPlansModel_ViewRendersRows(t *testing.T) {
	model := NewPlansModel(&mockPlansRPC{})
	model.SetSize(100, 30)

	model.Update(PlansUpdateMsg{Plans: []types.PlanExtended{
		{ID: "plan-1", Title: "Ship The Thing", State: "draft", TotalSteps: 3},
	}})

	out := model.View()
	if !strings.Contains(out, "Ship The Thing") {
		t.Errorf("plans view body is empty; want the plan title in the rendered table\n%s", out)
	}
}

// Regression: a tasks fetch in flight when the user switched to the jobs view
// wrote 7-cell task rows against the 4 job columns. bubbles/table indexed the
// column slice with the row-cell index and panicked:
//
//	panic: runtime error: index out of range [4] with length 4
//	charm.land/bubbles/v2/table.(*Model).renderRow
func TestTasksModel_LateTasksMsgInJobsViewDoesNotPanic(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(100, 30)

	model.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // tasks -> jobs
	model.Update(JobsUpdateMsg{Jobs: mock.JobsResponse.Jobs})

	// Late TasksUpdateMsg from the fetch started before the switch.
	model.Update(TasksUpdateMsg{Tasks: mock.TasksExtendedResponse.Tasks})

	rows := model.table.Rows()
	if got, want := len(rows), len(mock.JobsResponse.Jobs); got != want {
		t.Fatalf("jobs rows after late tasks update = %d, want %d", got, want)
	}
	assertRowCellCounts(t, model)
}

// Regression: SetViewMode (used by the app when it deep-links into a task)
// changed the view mode without installing that mode's columns.
func TestTasksModel_SetViewModeKeepsColumnsAndRowsInSync(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(100, 30)
	model.Update(TasksUpdateMsg{Tasks: mock.TasksExtendedResponse.Tasks})

	model.SetViewMode(ViewModeJobs)
	if got := len(model.table.Columns()); got != 4 {
		t.Fatalf("jobs view columns = %d, want 4", got)
	}
	assertRowCellCounts(t, model)

	model.SetViewMode(ViewModeTasks)
	model.Update(TasksUpdateMsg{Tasks: mock.TasksExtendedResponse.Tasks})
	if got := len(model.table.Columns()); got != 7 {
		t.Fatalf("tasks view columns = %d, want 7", got)
	}
	assertRowCellCounts(t, model)
}

// Regression: changing columns cleared every row and nothing repopulated them,
// so a terminal resize blanked the table until the next fetch.
func TestTasksModel_ResizeKeepsRowsAndSelection(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(100, 30)

	model.Update(tea.KeyPressMsg{Code: tea.KeyTab}) // tasks -> jobs
	model.Update(JobsUpdateMsg{Jobs: mock.JobsResponse.Jobs})
	model.Update(tea.KeyPressMsg{Code: tea.KeyDown})

	if got := model.table.Cursor(); got != 1 {
		t.Fatalf("cursor after one down = %d, want 1", got)
	}

	model.SetSize(140, 40)

	if got, want := len(model.table.Rows()), len(mock.JobsResponse.Jobs); got != want {
		t.Fatalf("rows after resize = %d, want %d (resize must not blank the table)", got, want)
	}
	if got := model.table.Cursor(); got != 1 {
		t.Errorf("cursor after resize = %d, want 1 (resize must not move the selection)", got)
	}
}

// assertRowCellCounts fails when any row's cell count differs from the table's
// column count — the shape that panics inside bubbles/table.
func assertRowCellCounts(t *testing.T, model *TasksModel) {
	t.Helper()
	cols := len(model.table.Columns())
	for i, row := range model.table.Rows() {
		if len(row) != cols {
			t.Errorf("row %d has %d cells, table has %d columns", i, len(row), cols)
		}
	}
}

type mockQueueRPC struct {
	jobs []types.QueueJob
}

func (m *mockQueueRPC) GetQueueStats() (*types.QueueStatsResponse, error) {
	return &types.QueueStatsResponse{}, nil
}

func (m *mockQueueRPC) ListQueueJobs(state string, limit int) (*types.QueueJobListResponse, error) {
	return &types.QueueJobListResponse{Jobs: m.jobs}, nil
}

func (m *mockQueueRPC) RetryQueueJob(jobID string) error { return nil }

func (m *mockQueueRPC) IsConnected() bool { return true }

type mockPlansRPC struct{}

func (m *mockPlansRPC) Call(method string, params any) (json.RawMessage, error) {
	return nil, errors.New("no rpc in tests")
}

func (m *mockPlansRPC) IsConnected() bool { return true }
