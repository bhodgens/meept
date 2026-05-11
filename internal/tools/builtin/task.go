// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/tools"
)

// TaskCreateTool creates a new task.
type TaskCreateTool struct {
	store *task.Store
}

// NewTaskCreateTool creates a new task creation tool.
func NewTaskCreateTool(store *task.Store) *TaskCreateTool {
	return &TaskCreateTool{store: store}
}

func (t *TaskCreateTool) Name() string { return "task_create" }

func (t *TaskCreateTool) Description() string {
	return "Create a new task for tracking work. Tasks are used for multi-step workflows, coordinating between agents, and maintaining context across conversations."
}

func (t *TaskCreateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"name": {
				Type:        "string",
				Description: "Short descriptive name for the task.",
			},
			"description": {
				Type:        "string",
				Description: "Detailed description of what the task involves.",
			},
			"project_dir": {
				Type:        "string",
				Description: "Optional project directory path for context.",
			},
			"context_query": {
				Type:        "string",
				Description: "Optional query for auto-searching relevant memory context.",
			},
			"memory_refs": {
				Type:        "array",
				Description: "Optional array of memory IDs to attach as context.",
			},
		},
		Required: []string{"name"},
	}
}

func (t *TaskCreateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.store == nil {
		return nil, fmt.Errorf("task store not configured")
	}

	name, _ := args["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	description, _ := args["description"].(string)

	newTask := task.NewTask(name, description)

	// Apply optional fields
	if projectDir, ok := args["project_dir"].(string); ok && projectDir != "" {
		newTask.WithProjectDir(projectDir)
	}

	if contextQuery, ok := args["context_query"].(string); ok && contextQuery != "" {
		newTask.WithContextQuery(contextQuery)
	}

	if memRefs, ok := args["memory_refs"].([]any); ok {
		refs := make([]string, 0, len(memRefs))
		for _, r := range memRefs {
			if ref, ok := r.(string); ok {
				refs = append(refs, ref)
			}
		}
		if len(refs) > 0 {
			newTask.WithMemoryRefs(refs)
		}
	}

	if err := t.store.Create(newTask); err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	return map[string]any{
		"success":     true,
		"task_id":     newTask.ID,
		"name":        newTask.Name,
		"state":       string(newTask.State),
		"description": newTask.Description,
	}, nil
}

// TaskGetTool retrieves a task by ID.
type TaskGetTool struct {
	store *task.Store
}

// NewTaskGetTool creates a new task retrieval tool.
func NewTaskGetTool(store *task.Store) *TaskGetTool {
	return &TaskGetTool{store: store}
}

func (t *TaskGetTool) Name() string { return "task_get" }

func (t *TaskGetTool) Description() string {
	return "Get detailed information about a specific task by its ID."
}

func (t *TaskGetTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"id": {
				Type:        "string",
				Description: "The task ID to retrieve.",
			},
		},
		Required: []string{"id"},
	}
}

func (t *TaskGetTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.store == nil {
		return nil, fmt.Errorf("task store not configured")
	}

	id, _ := args["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("task id is required")
	}

	taskObj, err := t.store.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	if taskObj == nil {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	return map[string]any{
		"id":              taskObj.ID,
		"name":            taskObj.Name,
		"description":     taskObj.Description,
		"state":           string(taskObj.State),
		"project_dir":     taskObj.ProjectDir,
		"total_jobs":      taskObj.TotalJobs,
		"completed_jobs":  taskObj.CompletedJobs,
		"failed_jobs":     taskObj.FailedJobs,
		"progress":        taskObj.Progress(),
		"linked_sessions": taskObj.LinkedSessions,
		"memory_refs":     taskObj.MemoryRefs,
		"context_query":   taskObj.ContextQuery,
		"assigned_agent":  taskObj.AssignedAgent,
		"created_at":      taskObj.CreatedAt.Format("2006-01-02T15:04:05Z"),
		"updated_at":      taskObj.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

// TaskListTool lists tasks.
type TaskListTool struct {
	store *task.Store
}

// NewTaskListTool creates a new task list tool.
func NewTaskListTool(store *task.Store) *TaskListTool {
	return &TaskListTool{store: store}
}

func (t *TaskListTool) Name() string { return "task_list" }

func (t *TaskListTool) Description() string {
	return "List tasks, optionally filtered by state. Returns task summaries ordered by most recently updated."
}

func (t *TaskListTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"state": {
				Type:        "string",
				Description: "Optional state filter: pending, planning, executing, testing, completed, failed, cancelled.",
				Enum:        []string{"pending", "planning", "executing", "testing", "completed", "failed", "cancelled", ""},
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of tasks to return (default 20, max 100).",
			},
			"active_only": {
				Type:        "boolean",
				Description: "If true, only return active (non-terminal) tasks.",
			},
		},
		Required: []string{},
	}
}

func (t *TaskListTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.store == nil {
		return nil, fmt.Errorf("task store not configured")
	}

	limit := 20
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = min(int(l), 100)
	}

	activeOnly, _ := args["active_only"].(bool)

	var tasks []*task.Task
	var err error

	if activeOnly {
		tasks, err = t.store.ListActive()
	} else if stateStr, ok := args["state"].(string); ok && stateStr != "" {
		state := task.TaskState(stateStr)
		tasks, err = t.store.List(&state, limit)
	} else {
		tasks, err = t.store.List(nil, limit)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	// Format results
	formatted := make([]map[string]any, 0, len(tasks))
	for _, taskObj := range tasks {
		formatted = append(formatted, map[string]any{
			"id":         taskObj.ID,
			"name":       taskObj.Name,
			"state":      string(taskObj.State),
			"progress":   taskObj.Progress(),
			"updated_at": taskObj.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	return map[string]any{
		"tasks": formatted,
		"count": len(formatted),
	}, nil
}

// TaskUpdateTool updates an existing task.
type TaskUpdateTool struct {
	store *task.Store
}

// NewTaskUpdateTool creates a new task update tool.
func NewTaskUpdateTool(store *task.Store) *TaskUpdateTool {
	return &TaskUpdateTool{store: store}
}

func (t *TaskUpdateTool) Name() string { return "task_update" }

func (t *TaskUpdateTool) Description() string {
	return "Update fields of an existing task. Use this to change task state, description, or add memory references."
}

func (t *TaskUpdateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"id": {
				Type:        "string",
				Description: "The task ID to update.",
			},
			"name": {
				Type:        "string",
				Description: "New name for the task.",
			},
			"description": {
				Type:        "string",
				Description: "New description for the task.",
			},
			"state": {
				Type:        "string",
				Description: "New state: pending, planning, executing, testing, completed, failed, cancelled.",
				Enum:        []string{"pending", "planning", "executing", "testing", "completed", "failed", "cancelled"},
			},
			"add_memory_ref": {
				Type:        "string",
				Description: "Add a memory ID reference to the task.",
			},
			"context_query": {
				Type:        "string",
				Description: "Set the context query for auto-search.",
			},
			"assigned_agent": {
				Type:        "string",
				Description: "Assign the task to a specific agent.",
			},
		},
		Required: []string{"id"},
	}
}

func (t *TaskUpdateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.store == nil {
		return nil, fmt.Errorf("task store not configured")
	}

	id, _ := args["id"].(string)
	if id == "" {
		return nil, fmt.Errorf("task id is required")
	}

	taskObj, err := t.store.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("failed to get task: %w", err)
	}
	if taskObj == nil {
		return nil, fmt.Errorf("task not found: %s", id)
	}

	// Apply updates
	if name, ok := args["name"].(string); ok && name != "" {
		taskObj.Name = name
	}

	if description, ok := args["description"].(string); ok {
		taskObj.Description = description
	}

	if stateStr, ok := args["state"].(string); ok && stateStr != "" {
		taskObj.SetState(task.TaskState(stateStr))
	}

	if memRef, ok := args["add_memory_ref"].(string); ok && memRef != "" {
		taskObj.AddMemoryRef(memRef)
	}

	if contextQuery, ok := args["context_query"].(string); ok {
		taskObj.ContextQuery = contextQuery
	}

	if assignedAgent, ok := args["assigned_agent"].(string); ok {
		taskObj.AssignedAgent = assignedAgent
	}

	if err := t.store.Update(taskObj); err != nil {
		return nil, fmt.Errorf("failed to update task: %w", err)
	}

	return map[string]any{
		"success": true,
		"task_id": taskObj.ID,
		"name":    taskObj.Name,
		"state":   string(taskObj.State),
	}, nil
}

// Ensure tools implement the Tool interface
var (
	_ tools.Tool = (*TaskCreateTool)(nil)
	_ tools.Tool = (*TaskGetTool)(nil)
	_ tools.Tool = (*TaskListTool)(nil)
	_ tools.Tool = (*TaskUpdateTool)(nil)
)
