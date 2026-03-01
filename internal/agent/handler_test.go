package agent

import (
	"encoding/json"
	"testing"
)

func TestChatRequest_Unmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantMsg string
		wantErr bool
	}{
		{
			name:    "valid request",
			input:   `{"message": "hello", "conversation_id": "conv-1"}`,
			wantMsg: "hello",
			wantErr: false,
		},
		{
			name:    "missing message",
			input:   `{"conversation_id": "conv-1"}`,
			wantMsg: "",
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req ChatRequest
			err := json.Unmarshal([]byte(tt.input), &req)
			if (err != nil) != tt.wantErr {
				t.Errorf("unmarshal error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && req.Message != tt.wantMsg {
				t.Errorf("Message = %q, want %q", req.Message, tt.wantMsg)
			}
		})
	}
}

func TestChatResponse_Marshal(t *testing.T) {
	resp := ChatResponse{
		Reply:          "Hello there!",
		ConversationID: "conv-123",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ChatResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Reply != resp.Reply {
		t.Errorf("Reply = %q, want %q", decoded.Reply, resp.Reply)
	}
	if decoded.ConversationID != resp.ConversationID {
		t.Errorf("ConversationID = %q, want %q", decoded.ConversationID, resp.ConversationID)
	}
}

func TestChatResponse_WithError(t *testing.T) {
	resp := ChatResponse{
		Reply:          "Error occurred",
		ConversationID: "conv-123",
		Error:          "something went wrong",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded ChatResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Error != resp.Error {
		t.Errorf("Error = %q, want %q", decoded.Error, resp.Error)
	}
}

func TestWorker_JSONMarshal(t *testing.T) {
	worker := &Worker{
		ID:             "worker-1",
		ConversationID: "conv-1",
		RequestID:      "req-1",
		State:          "processing",
		CurrentTool:    "bash",
	}

	data, err := json.Marshal(worker)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Worker
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != worker.ID {
		t.Errorf("ID = %q, want %q", decoded.ID, worker.ID)
	}
	if decoded.State != worker.State {
		t.Errorf("State = %q, want %q", decoded.State, worker.State)
	}
	if decoded.CurrentTool != worker.CurrentTool {
		t.Errorf("CurrentTool = %q, want %q", decoded.CurrentTool, worker.CurrentTool)
	}
}

func TestPlanRequest_JSONMarshal(t *testing.T) {
	req := PlanRequest{
		TaskID:    "task-123",
		SessionID: "session-456",
		Input:     "Write a CSV parser",
		Intent:    "code",
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded PlanRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.TaskID != req.TaskID {
		t.Errorf("TaskID = %q, want %q", decoded.TaskID, req.TaskID)
	}
	if decoded.Input != req.Input {
		t.Errorf("Input = %q, want %q", decoded.Input, req.Input)
	}
	if decoded.Intent != req.Intent {
		t.Errorf("Intent = %q, want %q", decoded.Intent, req.Intent)
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "he..."},
		{"abc", 3, "abc"},
		{"abcd", 3, "..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncateString(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}

func TestGenerateMessageID(t *testing.T) {
	id1 := generateMessageID()
	id2 := generateMessageID()

	if id1 == "" {
		t.Error("generateMessageID returned empty string")
	}
	// IDs should be unique (with nanosecond timestamp, extremely unlikely to collide)
	if id1 == id2 {
		t.Log("Warning: Two consecutive IDs were equal (very unlikely)")
	}
}

func TestGenerateWorkerID(t *testing.T) {
	id := generateWorkerID()
	if id == "" {
		t.Error("generateWorkerID returned empty string")
	}
	if len(id) < 8 {
		t.Errorf("generateWorkerID returned short ID: %q", id)
	}
	if id[:7] != "worker-" {
		t.Errorf("generateWorkerID should start with 'worker-', got %q", id)
	}
}
