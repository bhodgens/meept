package builtin

import (
	"context"
	"errors"
	"testing"

	"github.com/caimlas/meept/internal/tools"
)

func TestTeamCreateTool_MissingRequiredParams(t *testing.T) {
	tool := NewTeamCreateTool()

	tests := []struct {
		name string
		args map[string]any
	}{
		{"no args", nil},
		{"missing lead_agent", map[string]any{
			"roster":           []any{"coder"},
			"task_description": "build feature",
		}},
		{"missing task_description", map[string]any{
			"lead_agent": "planner",
			"roster":     []any{"coder"},
		}},
		{"missing roster", map[string]any{
			"lead_agent":       "planner",
			"task_description": "build feature",
		}},
		{"empty roster", map[string]any{
			"lead_agent":       "planner",
			"roster":           []any{},
			"task_description": "build feature",
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tr, ok := result.(*tools.ToolResult)
			if !ok {
				t.Fatalf("expected *ToolResult, got %T", result)
			}
			if tr.Success {
				t.Error("expected failure for missing required params")
			}
			if tr.Error == "" {
				t.Error("expected error message")
			}
		})
	}
}

func TestTeamCreateTool_Success(t *testing.T) {
	tool := NewTeamCreateTool()
	tool.SetCallback(func(ctx context.Context, config TeamCreateConfig) (string, error) {
		if config.LeadAgent != "planner" {
			t.Errorf("lead_agent = %q, want %q", config.LeadAgent, "planner")
		}
		if len(config.Roster) != 2 {
			t.Errorf("roster len = %d, want 2", len(config.Roster))
		}
		if config.MaxConcurrent != 5 {
			t.Errorf("max_concurrent = %d, want 5", config.MaxConcurrent)
		}
		if config.Mode != "team_parallel" {
			t.Errorf("mode = %q, want %q", config.Mode, "team_parallel")
		}
		return "team-abc-123", nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"lead_agent":       "planner",
		"roster":           []any{"coder", "analyst"},
		"max_concurrent":   float64(5),
		"task_description": "review and implement auth module",
		"mode":             "team_parallel",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamCreateResult)
	if !ok {
		t.Fatalf("expected TeamCreateResult, got %T", result)
	}
	if !r.Success {
		t.Error("expected success")
	}
	if r.TeamID != "team-abc-123" {
		t.Errorf("team_id = %q, want %q", r.TeamID, "team-abc-123")
	}
	if len(r.Roster) != 2 {
		t.Errorf("roster len = %d, want 2", len(r.Roster))
	}
	if r.MaxConcurrent != 5 {
		t.Errorf("max_concurrent = %d, want 5", r.MaxConcurrent)
	}
	if r.Mode != "team_parallel" {
		t.Errorf("mode = %q, want %q", r.Mode, "team_parallel")
	}
}

func TestTeamCreateTool_Defaults(t *testing.T) {
	tool := NewTeamCreateTool()
	tool.SetCallback(func(ctx context.Context, config TeamCreateConfig) (string, error) {
		// Tool applies defaults before calling callback
		if config.MaxConcurrent != 3 {
			t.Errorf("max_concurrent = %d, want 3 (default)", config.MaxConcurrent)
		}
		if config.Mode != "team_parallel" {
			t.Errorf("mode = %q, want %q (default)", config.Mode, "team_parallel")
		}
		return "team-xyz", nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"lead_agent":       "planner",
		"roster":           []any{"coder"},
		"task_description": "task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamCreateResult)
	if !ok {
		t.Fatalf("expected TeamCreateResult, got %T", result)
	}
	if !r.Success {
		t.Error("expected success")
	}
	// Verify defaults are applied at tool level
	if r.MaxConcurrent != 3 {
		t.Errorf("max_concurrent default = %d, want 3", r.MaxConcurrent)
	}
	if r.Mode != "team_parallel" {
		t.Errorf("mode default = %q, want team_parallel", r.Mode)
	}
}

func TestTeamCreateTool_CallbackError(t *testing.T) {
	tool := NewTeamCreateTool()
	tool.SetCallback(func(ctx context.Context, config TeamCreateConfig) (string, error) {
		return "", errors.New("no available agents")
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"lead_agent":       "planner",
		"roster":           []any{"coder"},
		"task_description": "task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamCreateResult)
	if !ok {
		t.Fatalf("expected TeamCreateResult, got %T", result)
	}
	if r.Success {
		t.Error("expected failure on callback error")
	}
}

func TestTeamCreateTool_NoCallback(t *testing.T) {
	tool := NewTeamCreateTool() // no callback set

	result, err := tool.Execute(context.Background(), map[string]any{
		"lead_agent":       "planner",
		"roster":           []any{"coder"},
		"task_description": "task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamCreateResult)
	if !ok {
		t.Fatalf("expected TeamCreateResult, got %T", result)
	}
	if r.Success {
		t.Error("expected failure when no callback set")
	}
}

func TestTeamAssignTool_MissingRequiredParams(t *testing.T) {
	tool := NewTeamAssignTool()

	tests := []struct {
		name string
		args map[string]any
	}{
		{"no args", nil},
		{"missing team_id", map[string]any{
			"agent_id": "coder",
			"subtask":  "write tests",
		}},
		{"missing agent_id", map[string]any{
			"team_id": "team-123",
			"subtask": "write tests",
		}},
		{"missing subtask", map[string]any{
			"team_id":  "team-123",
			"agent_id": "coder",
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tr, ok := result.(*tools.ToolResult)
			if !ok {
				t.Fatalf("expected *ToolResult, got %T", result)
			}
			if tr.Success {
				t.Error("expected failure for missing required params")
			}
		})
	}
}

func TestTeamAssignTool_Success(t *testing.T) {
	tool := NewTeamAssignTool()
	var receivedTeamID, receivedAgentID, receivedSubtask string
	var receivedPriority string

	tool.SetCallback(func(ctx context.Context, teamID string, assignment TaskAssignment) error {
		receivedTeamID = teamID
		receivedAgentID = assignment.AgentID
		receivedSubtask = assignment.Subtask
		receivedPriority = assignment.Priority
		return nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id":  "team-abc",
		"agent_id": "coder",
		"subtask":  "implement login handler",
		"priority": "high",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamAssignResult)
	if !ok {
		t.Fatalf("expected TeamAssignResult, got %T", result)
	}
	if !r.Success {
		t.Error("expected success")
	}
	if r.TeamID != "team-abc" {
		t.Errorf("team_id = %q, want %q", r.TeamID, "team-abc")
	}
	if r.AgentID != "coder" {
		t.Errorf("agent_id = %q, want %q", r.AgentID, "coder")
	}
	if r.Priority != "high" {
		t.Errorf("priority = %q, want %q", r.Priority, "high")
	}

	if receivedTeamID != "team-abc" {
		t.Errorf("callback received team_id = %q", receivedTeamID)
	}
	if receivedAgentID != "coder" {
		t.Errorf("callback received agent_id = %q", receivedAgentID)
	}
	if receivedSubtask != "implement login handler" {
		t.Errorf("callback received subtask = %q", receivedSubtask)
	}
	if receivedPriority != "high" {
		t.Errorf("callback received priority = %q", receivedPriority)
	}
}

func TestTeamAssignTool_DefaultPriority(t *testing.T) {
	tool := NewTeamAssignTool()
	var receivedPriority string
	tool.SetCallback(func(ctx context.Context, teamID string, assignment TaskAssignment) error {
		receivedPriority = assignment.Priority
		return nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id":  "team-abc",
		"agent_id": "coder",
		"subtask":  "task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamAssignResult)
	if !ok {
		t.Fatalf("expected TeamAssignResult, got %T", result)
	}
	if r.Priority != "medium" {
		t.Errorf("default priority = %q, want %q", r.Priority, "medium")
	}
	if receivedPriority != "medium" {
		t.Errorf("callback received priority = %q, want %q", receivedPriority, "medium")
	}
}

func TestTeamAssignTool_CallbackError(t *testing.T) {
	tool := NewTeamAssignTool()
	tool.SetCallback(func(ctx context.Context, teamID string, assignment TaskAssignment) error {
		return errors.New("agent not found on team")
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id":  "team-abc",
		"agent_id": "unknown",
		"subtask":  "task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamAssignResult)
	if !ok {
		t.Fatalf("expected TeamAssignResult, got %T", result)
	}
	if r.Success {
		t.Error("expected failure on callback error")
	}
}

func TestTeamAssignTool_NoCallback(t *testing.T) {
	tool := NewTeamAssignTool()

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id":  "team-abc",
		"agent_id": "coder",
		"subtask":  "task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamAssignResult)
	if !ok {
		t.Fatalf("expected TeamAssignResult, got %T", result)
	}
	if r.Success {
		t.Error("expected failure when no callback set")
	}
}

func TestTeamStatusTool_MissingTeamID(t *testing.T) {
	tool := NewTeamStatusTool()

	tests := []struct {
		name string
		args map[string]any
	}{
		{"no args", nil},
		{"empty team_id", map[string]any{"team_id": ""}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tr, ok := result.(*tools.ToolResult)
			if !ok {
				t.Fatalf("expected *ToolResult, got %T", result)
			}
			if tr.Success {
				t.Error("expected failure for missing team_id")
			}
		})
	}
}

func TestTeamStatusTool_Success(t *testing.T) {
	tool := NewTeamStatusTool()
	tool.SetCallback(func(ctx context.Context, teamID string) (*TeamStatusResult, error) {
		if teamID != "team-abc" {
			t.Errorf("team_id = %q, want %q", teamID, "team-abc")
		}
		return &TeamStatusResult{
			SessionID: "team-abc",
			LeadAgent: "planner",
			Phase:     "running",
			MemberResults: map[string]*MemberStatusInfo{
				"coder":   {AgentID: "coder", Status: "done"},
				"analyst": {AgentID: "analyst", Status: "running"},
			},
		}, nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id": "team-abc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(*TeamStatusResult)
	if !ok {
		t.Fatalf("expected *TeamStatusResult, got %T", result)
	}
	if r.SessionID != "team-abc" {
		t.Errorf("session_id = %q, want %q", r.SessionID, "team-abc")
	}
	if r.LeadAgent != "planner" {
		t.Errorf("lead_agent = %q, want %q", r.LeadAgent, "planner")
	}
	if r.Phase != "running" {
		t.Errorf("phase = %q, want %q", r.Phase, "running")
	}
	if len(r.MemberResults) != 2 {
		t.Errorf("member_results len = %d, want 2", len(r.MemberResults))
	}
}

func TestTeamStatusTool_CallbackError(t *testing.T) {
	tool := NewTeamStatusTool()
	tool.SetCallback(func(ctx context.Context, teamID string) (*TeamStatusResult, error) {
		return nil, errors.New("team not found")
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id": "team-nonexistent",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("expected *ToolResult, got %T", result)
	}
	if tr.Success {
		t.Error("expected failure on callback error")
	}
}

func TestTeamStatusTool_NoCallback(t *testing.T) {
	tool := NewTeamStatusTool()

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id": "team-abc",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tr, ok := result.(*tools.ToolResult)
	if !ok {
		t.Fatalf("expected *ToolResult, got %T", result)
	}
	if tr.Success {
		t.Error("expected failure when no callback set")
	}
}

func TestTeamMessageTool_MissingRequiredParams(t *testing.T) {
	tool := NewTeamMessageTool()

	tests := []struct {
		name string
		args map[string]any
	}{
		{"no args", nil},
		{"missing team_id", map[string]any{
			"content": "hello team",
		}},
		{"missing content", map[string]any{
			"team_id": "team-abc",
		}},
		{"empty content", map[string]any{
			"team_id": "team-abc",
			"content": "",
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tr, ok := result.(*tools.ToolResult)
			if !ok {
				t.Fatalf("expected *ToolResult, got %T", result)
			}
			if tr.Success {
				t.Error("expected failure for missing required params")
			}
		})
	}
}

func TestTeamMessageTool_Broadcast(t *testing.T) {
	tool := NewTeamMessageTool()
	var receivedTargetAgent string

	tool.SetCallback(func(ctx context.Context, teamID string, msg TeamMessage) error {
		receivedTargetAgent = msg.TargetAgent
		return nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id":      "team-abc",
		"content":      "starting work now",
		"message_type": "info",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamMessageResult)
	if !ok {
		t.Fatalf("expected TeamMessageResult, got %T", result)
	}
	if !r.Success {
		t.Error("expected success")
	}
	if r.TargetAgent != "" {
		t.Errorf("broadcast should have empty target_agent, got %q", r.TargetAgent)
	}
	if receivedTargetAgent != "" {
		t.Errorf("callback should receive empty target_agent for broadcast, got %q", receivedTargetAgent)
	}
}

func TestTeamMessageTool_Targeted(t *testing.T) {
	tool := NewTeamMessageTool()
	var receivedTargetAgent string

	tool.SetCallback(func(ctx context.Context, teamID string, msg TeamMessage) error {
		receivedTargetAgent = msg.TargetAgent
		return nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id":      "team-abc",
		"content":      "please review my PR",
		"target_agent": "coder",
		"message_type": "request",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamMessageResult)
	if !ok {
		t.Fatalf("expected TeamMessageResult, got %T", result)
	}
	if !r.Success {
		t.Error("expected success")
	}
	if r.TargetAgent != "coder" {
		t.Errorf("target_agent = %q, want %q", r.TargetAgent, "coder")
	}
	if r.MessageType != "request" {
		t.Errorf("message_type = %q, want %q", r.MessageType, "request")
	}
	if receivedTargetAgent != "coder" {
		t.Errorf("callback received target_agent = %q", receivedTargetAgent)
	}
}

func TestTeamMessageTool_DefaultMessageType(t *testing.T) {
	tool := NewTeamMessageTool()
	var receivedMsgType string
	tool.SetCallback(func(ctx context.Context, teamID string, msg TeamMessage) error {
		receivedMsgType = msg.MessageType
		return nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id": "team-abc",
		"content": "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamMessageResult)
	if !ok {
		t.Fatalf("expected TeamMessageResult, got %T", result)
	}
	if r.MessageType != "info" {
		t.Errorf("default message_type = %q, want %q", r.MessageType, "info")
	}
	if receivedMsgType != "info" {
		t.Errorf("callback received message_type = %q", receivedMsgType)
	}
}

func TestTeamMessageTool_CallbackError(t *testing.T) {
	tool := NewTeamMessageTool()
	tool.SetCallback(func(ctx context.Context, teamID string, msg TeamMessage) error {
		return errors.New("team session expired")
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id": "team-abc",
		"content": "hello",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamMessageResult)
	if !ok {
		t.Fatalf("expected TeamMessageResult, got %T", result)
	}
	if r.Success {
		t.Error("expected failure on callback error")
	}
}

func TestTeamResultTool_MissingRequiredParams(t *testing.T) {
	tool := NewTeamResultTool()

	tests := []struct {
		name string
		args map[string]any
	}{
		{"no args", nil},
		{"missing team_id", map[string]any{
			"agent_id": "coder",
			"output":   "done",
		}},
		{"missing agent_id", map[string]any{
			"team_id": "team-abc",
			"output":  "done",
		}},
		{"missing output", map[string]any{
			"team_id":  "team-abc",
			"agent_id": "coder",
		}},
		{"empty output", map[string]any{
			"team_id":  "team-abc",
			"agent_id": "coder",
			"output":   "",
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tr, ok := result.(*tools.ToolResult)
			if !ok {
				t.Fatalf("expected *ToolResult, got %T", result)
			}
			if tr.Success {
				t.Error("expected failure for missing required params")
			}
		})
	}
}

func TestTeamResultTool_Success(t *testing.T) {
	tool := NewTeamResultTool()
	var receivedResult MemberResult

	tool.SetCallback(func(ctx context.Context, teamID string, result MemberResult) error {
		receivedResult = result
		return nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id":   "team-abc",
		"agent_id":  "coder",
		"output":    "implemented login handler with 5 tests",
		"status":    "completed",
		"artifacts": []any{"auth/login.go", "auth/login_test.go"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamResultSubmitResult)
	if !ok {
		t.Fatalf("expected TeamResultSubmitResult, got %T", result)
	}
	if !r.Success {
		t.Error("expected success")
	}
	if r.TeamID != "team-abc" {
		t.Errorf("team_id = %q, want %q", r.TeamID, "team-abc")
	}
	if r.AgentID != "coder" {
		t.Errorf("agent_id = %q, want %q", r.AgentID, "coder")
	}
	if r.Status != "completed" {
		t.Errorf("status = %q, want %q", r.Status, "completed")
	}

	if receivedResult.AgentID != "coder" {
		t.Errorf("callback received agent_id = %q", receivedResult.AgentID)
	}
	if receivedResult.Output != "implemented login handler with 5 tests" {
		t.Errorf("callback received output = %q", receivedResult.Output)
	}
	if receivedResult.Status != "completed" {
		t.Errorf("callback received status = %q", receivedResult.Status)
	}
	if len(receivedResult.Artifacts) != 2 {
		t.Errorf("callback received artifacts len = %d, want 2", len(receivedResult.Artifacts))
	}
}

func TestTeamResultTool_DefaultStatus(t *testing.T) {
	tool := NewTeamResultTool()
	var receivedStatus string
	tool.SetCallback(func(ctx context.Context, teamID string, result MemberResult) error {
		receivedStatus = result.Status
		return nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id":  "team-abc",
		"agent_id": "coder",
		"output":   "partial progress",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamResultSubmitResult)
	if !ok {
		t.Fatalf("expected TeamResultSubmitResult, got %T", result)
	}
	if r.Status != "completed" {
		t.Errorf("default status = %q, want %q", r.Status, "completed")
	}
	if receivedStatus != "completed" {
		t.Errorf("callback received status = %q", receivedStatus)
	}
}

func TestTeamResultTool_FailedStatus(t *testing.T) {
	tool := NewTeamResultTool()
	var receivedStatus string
	tool.SetCallback(func(ctx context.Context, teamID string, result MemberResult) error {
		receivedStatus = result.Status
		return nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id":  "team-abc",
		"agent_id": "coder",
		"output":   "unable to complete due to missing dependency",
		"status":   "failed",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamResultSubmitResult)
	if !ok {
		t.Fatalf("expected TeamResultSubmitResult, got %T", result)
	}
	if !r.Success {
		t.Error("submission with failed status should still succeed")
	}
	if r.Status != "failed" {
		t.Errorf("status = %q, want %q", r.Status, "failed")
	}
	if receivedStatus != "failed" {
		t.Errorf("callback received status = %q", receivedStatus)
	}
}

func TestTeamResultTool_CallbackError(t *testing.T) {
	tool := NewTeamResultTool()
	tool.SetCallback(func(ctx context.Context, teamID string, result MemberResult) error {
		return errors.New("submission rejected: team already completed")
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id":  "team-abc",
		"agent_id": "coder",
		"output":   "done",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamResultSubmitResult)
	if !ok {
		t.Fatalf("expected TeamResultSubmitResult, got %T", result)
	}
	if r.Success {
		t.Error("expected failure on callback error")
	}
}

func TestTeamResultTool_NoCallback(t *testing.T) {
	tool := NewTeamResultTool()

	result, err := tool.Execute(context.Background(), map[string]any{
		"team_id":  "team-abc",
		"agent_id": "coder",
		"output":   "done",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamResultSubmitResult)
	if !ok {
		t.Fatalf("expected TeamResultSubmitResult, got %T", result)
	}
	if r.Success {
		t.Error("expected failure when no callback set")
	}
}

func TestRegisterTeamTools_AllRegistered(t *testing.T) {
	registry := tools.NewRegistry(nil)
	callbacks := &TeamCallbacks{
		CreateTeam: func(ctx context.Context, config TeamCreateConfig) (string, error) {
			return "team-1", nil
		},
		CreatePresetTeam: func(ctx context.Context, presetName string, taskDescription string, maxConcurrentOverride int) (string, error) {
			return "team-preset-1", nil
		},
		AssignTask: func(ctx context.Context, teamID string, assignment TaskAssignment) error {
			return nil
		},
		GetStatus: func(ctx context.Context, teamID string) (*TeamStatusResult, error) {
			return &TeamStatusResult{}, nil
		},
		SendMessage: func(ctx context.Context, teamID string, msg TeamMessage) error {
			return nil
		},
		SubmitResult: func(ctx context.Context, teamID string, result MemberResult) error {
			return nil
		},
	}

	RegisterTeamTools(registry, callbacks)

	expected := []string{"platform_team_create", "team_preset_create", "team_assign", "team_message", "team_result", "team_status"}
	for _, name := range expected {
		if registry.Get(name) == nil {
			t.Errorf("tool %q not registered", name)
		}
	}

	if registry.Count() < len(expected) {
		t.Errorf("expected at least %d tools registered, got %d", len(expected), registry.Count())
	}
}

func TestRegisterTeamTools_NilCallbacks(t *testing.T) {
	registry := tools.NewRegistry(nil)

	RegisterTeamTools(registry, nil)

	expected := []string{"platform_team_create", "team_preset_create", "team_assign", "team_message", "team_result", "team_status"}
	for _, name := range expected {
		tool := registry.Get(name)
		if tool == nil {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestTeamToolNames(t *testing.T) {
	tests := []struct {
		tool     interface{ Name() string }
		expected string
	}{
		{NewTeamCreateTool(), "platform_team_create"},
		{NewTeamPresetCreateTool(), "team_preset_create"},
		{NewTeamAssignTool(), "team_assign"},
		{NewTeamStatusTool(), "team_status"},
		{NewTeamMessageTool(), "team_message"},
		{NewTeamResultTool(), "team_result"},
	}

	for _, tc := range tests {
		if got := tc.tool.Name(); got != tc.expected {
			t.Errorf("Name() = %q, want %q", got, tc.expected)
		}
	}
}

func TestTeamToolCategories(t *testing.T) {
	tests := []struct {
		tool     interface{ Category() string }
		expected string
	}{
		{NewTeamCreateTool(), "team"},
		{NewTeamPresetCreateTool(), "team"},
		{NewTeamAssignTool(), "team"},
		{NewTeamStatusTool(), "team"},
		{NewTeamMessageTool(), "team"},
		{NewTeamResultTool(), "team"},
	}

	for _, tc := range tests {
		if got := tc.tool.Category(); got != tc.expected {
			t.Errorf("Category() = %q, want %q", got, tc.expected)
		}
	}
}

func TestTeamPresetCreateTool_MissingRequiredParams(t *testing.T) {
	tool := NewTeamPresetCreateTool()

	tests := []struct {
		name string
		args map[string]any
	}{
		{"no args", nil},
		{"missing preset_name", map[string]any{
			"task_description": "review this plan",
		}},
		{"missing task_description", map[string]any{
			"preset_name": "hyperplan",
		}},
		{"empty preset_name", map[string]any{
			"preset_name":      "",
			"task_description": "review this plan",
		}},
		{"empty task_description", map[string]any{
			"preset_name":      "hyperplan",
			"task_description": "",
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := tool.Execute(context.Background(), tc.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			tr, ok := result.(*tools.ToolResult)
			if !ok {
				t.Fatalf("expected *ToolResult, got %T", result)
			}
			if tr.Success {
				t.Error("expected failure for missing required params")
			}
			if tr.Error == "" {
				t.Error("expected error message")
			}
		})
	}
}

func TestTeamPresetCreateTool_Success(t *testing.T) {
	tool := NewTeamPresetCreateTool()
	var receivedPreset, receivedTask string
	var receivedMaxConcurrent int

	tool.SetCallback(func(ctx context.Context, presetName string, taskDescription string, maxConcurrentOverride int) (string, error) {
		receivedPreset = presetName
		receivedTask = taskDescription
		receivedMaxConcurrent = maxConcurrentOverride
		return "team-hyperplan-123", nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"preset_name":      "hyperplan",
		"task_description": "review the authentication system design",
		"max_concurrent":   float64(3),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamPresetCreateResult)
	if !ok {
		t.Fatalf("expected TeamPresetCreateResult, got %T", result)
	}
	if !r.Success {
		t.Error("expected success")
	}
	if r.TeamID != "team-hyperplan-123" {
		t.Errorf("team_id = %q, want %q", r.TeamID, "team-hyperplan-123")
	}
	if r.PresetName != "hyperplan" {
		t.Errorf("preset_name = %q, want %q", r.PresetName, "hyperplan")
	}
	if receivedPreset != "hyperplan" {
		t.Errorf("callback received preset_name = %q", receivedPreset)
	}
	if receivedTask != "review the authentication system design" {
		t.Errorf("callback received task_description = %q", receivedTask)
	}
	if receivedMaxConcurrent != 3 {
		t.Errorf("callback received max_concurrent = %d, want 3", receivedMaxConcurrent)
	}
}

func TestTeamPresetCreateTool_NoMaxConcurrent(t *testing.T) {
	tool := NewTeamPresetCreateTool()
	var receivedMaxConcurrent int

	tool.SetCallback(func(ctx context.Context, presetName string, taskDescription string, maxConcurrentOverride int) (string, error) {
		receivedMaxConcurrent = maxConcurrentOverride
		return "team-sec-123", nil
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"preset_name":      "security_research",
		"task_description": "audit the login flow",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamPresetCreateResult)
	if !ok {
		t.Fatalf("expected TeamPresetCreateResult, got %T", result)
	}
	if !r.Success {
		t.Error("expected success")
	}
	if r.PresetName != "security_research" {
		t.Errorf("preset_name = %q, want %q", r.PresetName, "security_research")
	}
	if receivedMaxConcurrent != 0 {
		t.Errorf("default max_concurrent should be 0 (use preset default), got %d", receivedMaxConcurrent)
	}
}

func TestTeamPresetCreateTool_CallbackError(t *testing.T) {
	tool := NewTeamPresetCreateTool()
	tool.SetCallback(func(ctx context.Context, presetName string, taskDescription string, maxConcurrentOverride int) (string, error) {
		return "", errors.New("preset 'unknown_preset' not found")
	})

	result, err := tool.Execute(context.Background(), map[string]any{
		"preset_name":      "hyperplan",
		"task_description": "task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamPresetCreateResult)
	if !ok {
		t.Fatalf("expected TeamPresetCreateResult, got %T", result)
	}
	if r.Success {
		t.Error("expected failure on callback error")
	}
}

func TestTeamPresetCreateTool_NoCallback(t *testing.T) {
	tool := NewTeamPresetCreateTool() // no callback set

	result, err := tool.Execute(context.Background(), map[string]any{
		"preset_name":      "hyperplan",
		"task_description": "task",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r, ok := result.(TeamPresetCreateResult)
	if !ok {
		t.Fatalf("expected TeamPresetCreateResult, got %T", result)
	}
	if r.Success {
		t.Error("expected failure when no callback set")
	}
}
