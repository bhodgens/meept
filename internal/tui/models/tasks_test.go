package models

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/caimlas/meept/internal/tui/types"
)

// MockTasksRPCClient implements TasksRPCClient for testing.
type MockTasksRPCClient struct {
	connected             bool
	JobsResponse          *types.JobListResponse
	JobsError             error
	JobsCalls             int
	TasksExtendedResponse *types.TaskExtendedListResponse
	TasksExtendedError    error
	TasksExtendedCalls    int
	TaskStepsResponse     *types.TaskStepsResponse
	TaskStepsError        error
	TaskStepsCalls        int
}

func NewMockTasksRPCClient() *MockTasksRPCClient {
	return &MockTasksRPCClient{
		connected: true,
		JobsResponse: &types.JobListResponse{
			Jobs: []types.Job{
				{
					ID:          "job-1",
					Name:        "Daily Backup",
					Schedule:    "0 0 * * *",
					NextRunTime: "2026-02-19T00:00:00Z",
					Paused:      false,
					Action:      "backup.run",
					LastResult:  "success",
				},
				{
					ID:          "job-2",
					Name:        "Weekly Report",
					Schedule:    "0 9 * * MON",
					NextRunTime: "2026-02-24T09:00:00Z",
					Paused:      true,
					Action:      "report.generate",
					LastResult:  "",
				},
				{
					ID:          "job-3",
					Name:        "",
					Trigger:     "interval:1h",
					NextRunTime: "",
					Paused:      false,
				},
			},
		},
		TasksExtendedResponse: &types.TaskExtendedListResponse{
			Tasks: []types.TaskExtended{
				{
					Task: types.Task{
						ID:    "task-1",
						Name:  "Test Task",
						State: "pending",
					},
				},
			},
		},
	}
}

func (m *MockTasksRPCClient) ListJobs() (*types.JobListResponse, error) {
	m.JobsCalls++
	if m.JobsError != nil {
		return nil, m.JobsError
	}
	return m.JobsResponse, nil
}

func (m *MockTasksRPCClient) ListTasksExtended() (*types.TaskExtendedListResponse, error) {
	m.TasksExtendedCalls++
	if m.TasksExtendedError != nil {
		return nil, m.TasksExtendedError
	}
	return m.TasksExtendedResponse, nil
}

func (m *MockTasksRPCClient) ListTaskSteps(taskID string) (*types.TaskStepsResponse, error) {
	m.TaskStepsCalls++
	if m.TaskStepsError != nil {
		return nil, m.TaskStepsError
	}
	if m.TaskStepsResponse != nil {
		return m.TaskStepsResponse, nil
	}
	return &types.TaskStepsResponse{Steps: []types.TaskStepView{}}, nil
}

func (m *MockTasksRPCClient) IsConnected() bool {
	return m.connected
}

func TestTasksModel_NewTasksModel(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)

	if model == nil {
		t.Fatal("expected non-nil tasks model")
	}
	if model.selectedJob != nil {
		t.Error("expected no job selected initially")
	}
	if model.showingHelp {
		t.Error("expected help to be hidden initially")
	}
}

func TestTasksModel_SetSize(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)

	model.SetSize(100, 40)

	if model.width != 100 {
		t.Errorf("expected width 100, got %d", model.width)
	}
	if model.height != 40 {
		t.Errorf("expected height 40, got %d", model.height)
	}
}

func TestTasksModel_SetSizeMinimum(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)

	// Very small size should use minimum values
	model.SetSize(30, 10)

	// Should not panic and should set values
	if model.width != 30 {
		t.Errorf("expected width 30, got %d", model.width)
	}
}

func TestTasksModel_Init(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)

	cmd := model.Init()

	if cmd == nil {
		t.Error("expected Init to return a command")
	}
}

func TestTasksModel_FetchJobs(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)

	msg := model.fetchJobs()
	updateMsg, ok := msg.(JobsUpdateMsg)

	if !ok {
		t.Fatal("expected JobsUpdateMsg")
	}
	if updateMsg.Err != nil {
		t.Errorf("unexpected error: %v", updateMsg.Err)
	}
	if len(updateMsg.Jobs) != 3 {
		t.Errorf("expected 3 jobs, got %d", len(updateMsg.Jobs))
	}
}

func TestTasksModel_FetchJobsError(t *testing.T) {
	mock := NewMockTasksRPCClient()
	mock.JobsError = errors.New("connection failed")
	model := NewTasksModel(mock)

	msg := model.fetchJobs()
	updateMsg, ok := msg.(JobsUpdateMsg)

	if !ok {
		t.Fatal("expected JobsUpdateMsg")
	}
	if updateMsg.Err == nil {
		t.Error("expected error to be set")
	}
}

func TestTasksModel_UpdateWithJobsUpdateMsg(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.loading = true

	updateMsg := JobsUpdateMsg{
		Jobs: mock.JobsResponse.Jobs,
		Err:  nil,
	}
	model.Update(updateMsg)

	if model.loading {
		t.Error("expected loading to be false")
	}
	if model.err != nil {
		t.Errorf("unexpected error: %v", model.err)
	}
	if len(model.jobs) != 3 {
		t.Errorf("expected 3 jobs, got %d", len(model.jobs))
	}
}

func TestTasksModel_UpdateWithError(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.loading = true

	testErr := errors.New("test error")
	updateMsg := JobsUpdateMsg{
		Err: testErr,
	}
	model.Update(updateMsg)

	if model.loading {
		t.Error("expected loading to be false")
	}
	if !errors.Is(model.err, testErr) {
		t.Errorf("expected error to be set, got %v", model.err)
	}
}

func TestTasksModel_Refresh(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)

	msg := tea.KeyPressMsg{Code: 'r', Text: "r"}
	cmd := model.Update(msg)

	if !model.loading {
		t.Error("expected loading to be true after refresh")
	}
	if cmd == nil {
		t.Error("expected command to be returned for fetch")
	}
}

func TestTasksModel_ToggleHelp(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)

	// Show help
	msg := tea.KeyPressMsg{Code: '?', Text: "?"}
	model.Update(msg)

	if !model.showingHelp {
		t.Error("expected help to be showing")
	}

	// Hide help (any key)
	anyKey := tea.KeyPressMsg{Code: 'x', Text: "x"}
	model.Update(anyKey)

	if model.showingHelp {
		t.Error("expected help to be hidden")
	}
}

func TestTasksModel_SelectJob(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.viewMode = ViewModeJobs // Switch to jobs mode
	model.jobs = mock.JobsResponse.Jobs
	model.updateTable()

	// Press enter to select
	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	model.Update(msg)

	if model.selectedJob == nil {
		t.Error("expected job to be selected")
	}
}

func TestTasksModel_SelectJobEmpty(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.jobs = []types.Job{} // Empty

	// Press enter to select
	msg := tea.KeyPressMsg{Code: tea.KeyEnter}
	model.Update(msg)

	if model.selectedJob != nil {
		t.Error("expected no job selected when list is empty")
	}
}

func TestTasksModel_ClearSelection(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.jobs = mock.JobsResponse.Jobs
	model.selectedJob = &model.jobs[0]

	// Press escape
	msg := tea.KeyPressMsg{Code: tea.KeyEscape}
	model.Update(msg)

	if model.selectedJob != nil {
		t.Error("expected selection to be cleared")
	}
}

func TestTasksModel_Navigation(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.jobs = mock.JobsResponse.Jobs
	model.updateTable()

	// Navigate down
	downKeys := []string{"down", "j"}
	for _, key := range downKeys {
		msg := tea.KeyPressMsg{Code: rune(key[0]), Text: key}
		if key == "down" {
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		}
		model.Update(msg)
	}

	// Navigate up
	upKeys := []string{"up", "k"}
	for _, key := range upKeys {
		msg := tea.KeyPressMsg{Code: rune(key[0]), Text: key}
		if key == "up" {
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		}
		model.Update(msg)
	}

	// No assertions needed - just checking no panics
}

func TestTasksModel_UpdateTable(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.jobs = mock.JobsResponse.Jobs

	model.updateTable()

	// Table should have 3 rows
	rows := model.table.Rows()
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(rows))
	}
}

func TestTasksModel_UpdateTableWithMissingFields(t *testing.T) {
	mock := NewMockTasksRPCClient()
	mock.JobsResponse = &types.JobListResponse{
		Jobs: []types.Job{
			{
				ID:     "job-1",
				Name:   "", // No name - should use ID
				Paused: false,
			},
		},
	}
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.jobs = mock.JobsResponse.Jobs

	model.updateTable()

	rows := model.table.Rows()
	if len(rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(rows))
	}
	// First column should be ID when name is empty
	if !strings.Contains(rows[0][0], "job-1") {
		t.Error("expected ID to be used when name is empty")
	}
}

func TestTasksModel_ViewLoading(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.loading = true

	view := model.View()

	if !strings.Contains(view, "Loading") {
		t.Error("expected 'Loading' in view")
	}
}

func TestTasksModel_ViewLoadingWithExistingJobs(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.viewMode = ViewModeJobs // Switch to jobs mode
	model.loading = true
	model.jobs = mock.JobsResponse.Jobs

	view := model.View()

	// Should show jobs, not loading screen
	if strings.Contains(view, "Loading jobs...") {
		t.Error("expected jobs view, not loading screen, when jobs exist")
	}
}

func TestTasksModel_ViewError(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.err = errors.New("test error")

	view := model.View()

	if !strings.Contains(view, "Error") {
		t.Error("expected 'Error' in view")
	}
	if !strings.Contains(view, "test error") {
		t.Error("expected error message in view")
	}
}

func TestTasksModel_ViewErrorWithExistingJobs(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.viewMode = ViewModeJobs // Switch to jobs mode
	model.err = errors.New("test error")
	model.jobs = mock.JobsResponse.Jobs

	view := model.View()

	// Should show jobs, not error screen, when jobs exist
	if strings.Contains(view, "test error") {
		t.Error("expected jobs view, not error screen, when jobs exist")
	}
}

func TestTasksModel_ViewHelp(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.showingHelp = true

	view := model.View()

	if !strings.Contains(view, "Tasks View Help") {
		t.Error("expected help title in view")
	}
	if !strings.Contains(view, "Move cursor up") {
		t.Error("expected help content in view")
	}
}

func TestTasksModel_ViewWithJobs(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.viewMode = ViewModeJobs // Switch to jobs mode
	model.jobs = mock.JobsResponse.Jobs
	model.updateTable()

	view := model.View()

	if !strings.Contains(view, "Scheduled Jobs") {
		t.Error("expected 'Scheduled Jobs' title")
	}
	if !strings.Contains(view, "refresh") {
		t.Error("expected refresh hint")
	}
}

func TestTasksModel_ViewWithSelectedJob(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.viewMode = ViewModeJobs // Switch to jobs mode
	model.jobs = mock.JobsResponse.Jobs
	model.selectedJob = &model.jobs[0]
	model.updateTable()

	view := model.View()

	if !strings.Contains(view, "Job Detail") {
		t.Error("expected 'Job Detail' panel")
	}
	if !strings.Contains(view, "Daily Backup") {
		t.Error("expected job name in detail")
	}
	if !strings.Contains(view, "job-1") {
		t.Error("expected job ID in detail")
	}
}

func TestTasksModel_ViewJobDetailPaused(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.jobs = mock.JobsResponse.Jobs
	model.selectedJob = &model.jobs[1] // Paused job

	detail := model.renderJobDetail()

	if !strings.Contains(detail, "paused") {
		t.Error("expected 'paused' status in detail")
	}
}

func TestTasksModel_ViewJobDetailMissingFields(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.selectedJob = &types.Job{
		ID:     "job-x",
		Name:   "", // Should use ID
		Paused: false,
	}

	detail := model.renderJobDetail()

	if !strings.Contains(detail, "job-x") {
		t.Error("expected ID when name is empty")
	}
	if !strings.Contains(detail, "n/a") {
		t.Error("expected n/a for missing fields")
	}
}

func TestTasksModel_ViewEmptyDetail(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.jobs = mock.JobsResponse.Jobs
	model.selectedJob = nil

	view := model.View()

	if !strings.Contains(view, "Select a job") {
		t.Error("expected empty detail hint")
	}
}

// Note: teatest integration tests for sub-models are skipped because they don't
// implement the full tea.Model interface (missing quit command handling).
// The App-level teatest tests provide full integration testing.
// Sub-models are thoroughly tested via unit tests above.

func TestTasksModel_LineageViewToggle(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)

	// Default to tasks view
	if model.viewMode != ViewModeTasks {
		t.Error("expected tasks view by default")
	}

	// Press 't' to toggle lineage
	msg := tea.KeyPressMsg{Code: 't', Text: "t"}
	cmd := model.Update(msg)

	if model.viewMode != ViewModeLineage {
		t.Error("expected lineage view after pressing 't'")
	}
	if cmd == nil {
		t.Error("expected fetch command on lineage toggle")
	}
}

func TestTasksModel_LineageViewToggleOff(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)

	// Toggle to lineage
	model.viewMode = ViewModeLineage

	// Press 't' again to toggle back
	msg := tea.KeyPressMsg{Code: 't', Text: "t"}
	model.Update(msg)

	if model.viewMode != ViewModeTasks {
		t.Error("expected tasks view after toggling lineage off")
	}
}

func TestTasksModel_LineageViewRendering(t *testing.T) {
	mock := NewMockTasksRPCClient()
	// Set up tasks with parent-child relationships
	mock.TasksExtendedResponse = &types.TaskExtendedListResponse{
		Tasks: []types.TaskExtended{
			{
				Task: types.Task{
					ID:    "parent-1",
					Name:  "Build auth system",
					State: "executing",
				},
				InheritedFrom: "",
				MemoryRefs:    []string{"mem-1", "mem-2"},
			},
			{
				Task: types.Task{
					ID:            "child-1",
					Name:          "Plan architecture",
					State:         "completed",
					CompletedJobs: 1,
					TotalJobs:     1,
				},
				InheritedFrom:   "parent-1",
				CreatedMemories: []string{"mem-3"},
			},
			{
				Task: types.Task{
					ID:    "child-2",
					Name:  "Implement handlers",
					State: "pending",
				},
				InheritedFrom: "parent-1",
			},
		},
	}

	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.viewMode = ViewModeLineage

	// Load data
	msg := model.fetchTasks()
	model.Update(msg)

	view := model.View()

	// Check that lineage view shows parent and children
	if !strings.Contains(view, "Task Lineage") {
		t.Error("expected 'Task Lineage' header")
	}
	if !strings.Contains(view, "Build auth system") {
		t.Error("expected parent task name")
	}
	if !strings.Contains(view, "Plan architecture") {
		t.Error("expected child task name")
	}
	if !strings.Contains(view, "Implement handlers") {
		t.Error("expected child task name")
	}
}

func TestTasksModel_LineageViewEmpty(t *testing.T) {
	mock := NewMockTasksRPCClient()
	mock.TasksExtendedResponse = &types.TaskExtendedListResponse{
		Tasks: []types.TaskExtended{},
	}

	model := NewTasksModel(mock)
	model.SetSize(80, 24)
	model.viewMode = ViewModeLineage

	// Load data
	msg := model.fetchTasks()
	model.Update(msg)

	// With empty tasks, the lineage view falls back to renderLoading or renderError
	// since tasks is empty and loading was cleared. The key thing is it doesn't panic.
	view := model.View()

	// Verify it renders something (either loading, empty state, or the lineage header)
	if view == "" {
		t.Error("expected non-empty view")
	}
}

func TestTasksModel_TabCycleThroughViews(t *testing.T) {
	mock := NewMockTasksRPCClient()
	model := NewTasksModel(mock)
	model.SetSize(80, 24)

	// Start at tasks view
	if model.viewMode != ViewModeTasks {
		t.Error("expected tasks view initially")
	}

	// Tab to jobs
	msg := tea.KeyPressMsg{Code: tea.KeyTab, Text: "tab"}
	model.Update(msg)
	if model.viewMode != ViewModeJobs {
		t.Error("expected jobs view after first tab")
	}

	// Tab to lineage
	model.Update(msg)
	if model.viewMode != ViewModeLineage {
		t.Error("expected lineage view after second tab")
	}

	// Tab back to tasks
	model.Update(msg)
	if model.viewMode != ViewModeTasks {
		t.Error("expected tasks view after third tab")
	}
}
