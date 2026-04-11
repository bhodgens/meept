package lite

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tui/types"
)

// mockRPCClient implements RPCClient for testing.
type mockRPCClient struct {
	connected      bool
	tasks          []types.Task
	extendedTasks  []types.TaskExtended
	taskByID       map[string]*types.Task
	stepsByTask    map[string][]types.TaskStepView
	listTasksErr   error
	getTaskErr     error
	listStepsErr   error
	extendedErr    error
}

func newMockRPCClient() *mockRPCClient {
	return &mockRPCClient{
		connected:   true,
		taskByID:    make(map[string]*types.Task),
		stepsByTask: make(map[string][]types.TaskStepView),
	}
}

func (m *mockRPCClient) IsConnected() bool {
	return m.connected
}

func (m *mockRPCClient) ListTasks(state string, limit int) (*types.TaskListResponse, error) {
	if m.listTasksErr != nil {
		return nil, m.listTasksErr
	}
	return &types.TaskListResponse{Tasks: m.tasks}, nil
}

func (m *mockRPCClient) ListTasksExtended() (*types.TaskExtendedListResponse, error) {
	if m.extendedErr != nil {
		return nil, m.extendedErr
	}
	return &types.TaskExtendedListResponse{Tasks: m.extendedTasks}, nil
}

func (m *mockRPCClient) GetTask(taskID string) (*types.Task, error) {
	if m.getTaskErr != nil {
		return nil, m.getTaskErr
	}
	if task, ok := m.taskByID[taskID]; ok {
		return task, nil
	}
	// Return first matching task from list
	for _, t := range m.tasks {
		if t.ID == taskID {
			return &t, nil
		}
	}
	return nil, nil
}

func (m *mockRPCClient) ListTaskSteps(taskID string) (*types.TaskStepsResponse, error) {
	if m.listStepsErr != nil {
		return nil, m.listStepsErr
	}
	steps := m.stepsByTask[taskID]
	return &types.TaskStepsResponse{Steps: steps}, nil
}

func TestNewTaskManager(t *testing.T) {
	rpc := newMockRPCClient()
	tm := NewTaskManager(rpc)

	if tm == nil {
		t.Fatal("expected non-nil TaskManager")
	}
	if tm.rpc != rpc {
		t.Error("expected rpc client to be set")
	}
}

func TestTaskManager_List(t *testing.T) {
	tests := []struct {
		name      string
		sessionID string
		tasks     []types.Task
		extended  []types.TaskExtended
		wantCount int
		wantErr   bool
	}{
		{
			name:      "empty list",
			sessionID: "",
			tasks:     []types.Task{},
			extended:  []types.TaskExtended{},
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "list all tasks",
			sessionID: "",
			extended: []types.TaskExtended{
				{Task: types.Task{ID: "task-1", Name: "task one"}},
				{Task: types.Task{ID: "task-2", Name: "task two"}},
			},
			wantCount: 2,
			wantErr:   false,
		},
		{
			name:      "filter by session",
			sessionID: "sess-1",
			extended: []types.TaskExtended{
				{Task: types.Task{ID: "task-1", Name: "task one", LinkedSessions: []string{"sess-1"}}},
				{Task: types.Task{ID: "task-2", Name: "task two", LinkedSessions: []string{"sess-2"}}},
				{Task: types.Task{ID: "task-3", Name: "task three", LinkedSessions: []string{"sess-1", "sess-2"}}},
			},
			wantCount: 2,
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rpc := newMockRPCClient()
			rpc.tasks = tt.tasks
			rpc.extendedTasks = tt.extended

			tm := NewTaskManager(rpc)
			tasks, err := tm.List(tt.sessionID)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if len(tasks) != tt.wantCount {
				t.Errorf("expected %d tasks, got %d", tt.wantCount, len(tasks))
			}
		})
	}
}

func TestTaskManager_List_NotConnected(t *testing.T) {
	rpc := newMockRPCClient()
	rpc.connected = false

	tm := NewTaskManager(rpc)
	_, err := tm.List("")

	if err == nil {
		t.Error("expected error when not connected")
	}
	if !strings.Contains(err.Error(), "not connected") {
		t.Errorf("expected 'not connected' error, got: %v", err)
	}
}

func TestTaskManager_Get(t *testing.T) {
	rpc := newMockRPCClient()
	rpc.tasks = []types.Task{
		{ID: "task-1", Name: "task one", State: "executing"},
	}

	tm := NewTaskManager(rpc)

	task, err := tm.Get("task-1")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if task == nil {
		t.Fatal("expected non-nil task")
	}
	if task.ID != "task-1" {
		t.Errorf("expected task ID 'task-1', got '%s'", task.ID)
	}
}

func TestTaskManager_Get_EmptyID(t *testing.T) {
	rpc := newMockRPCClient()
	tm := NewTaskManager(rpc)

	_, err := tm.Get("")
	if err == nil {
		t.Error("expected error for empty task ID")
	}
	if !strings.Contains(err.Error(), "required") {
		t.Errorf("expected 'required' error, got: %v", err)
	}
}

func TestTaskManager_Cancel(t *testing.T) {
	rpc := newMockRPCClient()
	tm := NewTaskManager(rpc)

	// Currently returns "not implemented"
	err := tm.Cancel("task-1")
	if err == nil {
		t.Error("expected error (not implemented)")
	}
}

func TestTaskManager_FormatTaskList(t *testing.T) {
	rpc := newMockRPCClient()
	tm := NewTaskManager(rpc)

	tests := []struct {
		name   string
		tasks  []types.Task
		checks []string
	}{
		{
			name:   "empty list",
			tasks:  []types.Task{},
			checks: []string{"no tasks found"},
		},
		{
			name: "single task",
			tasks: []types.Task{
				{ID: "task-1", Name: "build feature", State: "executing", TotalJobs: 5, CompletedJobs: 2},
			},
			checks: []string{"task-1", "build feature", "exec", "2/5"},
		},
		{
			name: "multiple tasks",
			tasks: []types.Task{
				{ID: "task-1", Name: "task one", State: "completed"},
				{ID: "task-2", Name: "task two", State: "pending"},
			},
			checks: []string{"task one", "task two", "done", "pend", "2 task(s) total"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tm.FormatTaskList(tt.tasks)
			for _, check := range tt.checks {
				if !strings.Contains(result, check) {
					t.Errorf("expected output to contain '%s', got:\n%s", check, result)
				}
			}
		})
	}
}

func TestTaskManager_FormatTaskDetail(t *testing.T) {
	rpc := newMockRPCClient()
	rpc.stepsByTask["task-1"] = []types.TaskStepView{
		{ID: "step-1", Sequence: 1, Description: "setup environment", State: "completed"},
		{ID: "step-2", Sequence: 2, Description: "run tests", State: "running"},
	}

	tm := NewTaskManager(rpc)

	task := &types.Task{
		ID:            "task-1",
		Name:          "build feature",
		Description:   "implement the new feature",
		State:         "executing",
		TotalJobs:     5,
		CompletedJobs: 2,
		FailedJobs:    1,
		ProjectDir:    "/home/user/project",
		CreatedAt:     "2026-04-10T10:00:00Z",
		UpdatedAt:     "2026-04-10T10:30:00Z",
	}

	result := tm.FormatTaskDetail(task)

	checks := []string{
		"task: build feature",
		"id:",
		"task-1",
		"state:",
		"exec",
		"description:",
		"implement the new feature",
		"progress",
		"2/5",
		"completed: 2",
		"failed: 1",
		"project:",
		"/home/user/project",
		"steps",
		"setup environment",
		"run tests",
	}

	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("expected output to contain '%s', got:\n%s", check, result)
		}
	}
}

func TestTaskManager_FormatTaskDetail_Nil(t *testing.T) {
	rpc := newMockRPCClient()
	tm := NewTaskManager(rpc)

	result := tm.FormatTaskDetail(nil)
	if !strings.Contains(result, "not found") {
		t.Errorf("expected 'not found', got: %s", result)
	}
}

func TestTaskManager_FormatTaskExtendedDetail(t *testing.T) {
	rpc := newMockRPCClient()
	tm := NewTaskManager(rpc)

	task := &types.TaskExtended{
		Task: types.Task{
			ID:            "task-1",
			Name:          "build feature",
			State:         "executing",
			TotalJobs:     5,
			CompletedJobs: 2,
		},
		AssignedAgent:   "coder",
		MemoryRefs:      []string{"mem-1", "mem-2"},
		ContextQuery:    "feature implementation",
		InheritedFrom:   "task-0",
		CreatedMemories: []string{"mem-3"},
		Steps: []types.TaskStepView{
			{Sequence: 1, Description: "step one", State: "completed", AgentID: "planner"},
			{Sequence: 2, Description: "step two", State: "pending", DependsOn: []string{"step-1"}},
		},
	}

	result := tm.FormatTaskExtendedDetail(task)

	checks := []string{
		"task: build feature",
		"agent:",
		"coder",
		"memory context",
		"inherited from: task-0",
		"memory refs:",
		"2",
		"context query:",
		"feature implementation",
		"created:",
		"1 memories",
		"steps",
		"step one",
		"step two",
		"blocked by",
	}

	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("expected output to contain '%s', got:\n%s", check, result)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string", 10, "this is..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "abc"},
		{"", 5, ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestTaskManager_getStateIcon(t *testing.T) {
	rpc := newMockRPCClient()
	tm := NewTaskManager(rpc)

	tests := []struct {
		state string
		want  string
	}{
		{"pending", "o pend"},
		{"planning", "* plan"},
		{"executing", "> exec"},
		{"testing", "? test"},
		{"completed", "+ done"},
		{"failed", "x fail"},
		{"cancelled", "- stop"},
		{"unknown", "? u..."},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			got := tm.getStateIcon(tt.state)
			if got != tt.want {
				t.Errorf("getStateIcon(%q) = %q, want %q", tt.state, got, tt.want)
			}
		})
	}
}

func TestTaskManager_formatProgress(t *testing.T) {
	rpc := newMockRPCClient()
	tm := NewTaskManager(rpc)

	tests := []struct {
		completed int
		total     int
		want      string
	}{
		{0, 0, "-/-"},
		{0, 10, "0/10 (0%)"},
		{5, 10, "5/10 (50%)"},
		{10, 10, "10/10 (100%)"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tm.formatProgress(tt.completed, tt.total)
			if got != tt.want {
				t.Errorf("formatProgress(%d, %d) = %q, want %q", tt.completed, tt.total, got, tt.want)
			}
		})
	}
}

func TestTaskManager_formatProgressBar(t *testing.T) {
	rpc := newMockRPCClient()
	tm := NewTaskManager(rpc)

	// Test zero total
	result := tm.formatProgressBar(0, 0, 10)
	if !strings.Contains(result, "-/-") {
		t.Errorf("expected -/- for zero total, got: %s", result)
	}

	// Test 50%
	result = tm.formatProgressBar(5, 10, 10)
	if !strings.Contains(result, "#####-----") {
		t.Errorf("expected 5 filled and 5 empty, got: %s", result)
	}
	if !strings.Contains(result, "50%") {
		t.Errorf("expected 50%%, got: %s", result)
	}

	// Test 100%
	result = tm.formatProgressBar(10, 10, 10)
	if !strings.Contains(result, "##########") {
		t.Errorf("expected all filled, got: %s", result)
	}
}
