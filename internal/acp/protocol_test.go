package acp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRequestMarshalsJSONRPCEnvelope(t *testing.T) {
	t.Parallel()

	req := Request{
		JSONRPC: "2.0",
		ID:      7,
		Method:  MethodInitialize,
		Params: InitializeParams{
			ProtocolVersion: ProtocolVersion,
			ClientInfo: ImplementationInfo{
				Name:    "meept",
				Title:   "Meept",
				Version: "0.0.0",
			},
		},
	}

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, `"jsonrpc":"2.0"`) {
		t.Fatalf("missing jsonrpc 2.0 in %s", got)
	}
	if !strings.Contains(got, `"id":7`) {
		t.Fatalf("id must be numeric, got %s", got)
	}
	if !strings.Contains(got, `"method":"initialize"`) {
		t.Fatalf("method missing in %s", got)
	}
	if strings.Contains(got, `"protocolVersion":"1"`) {
		t.Fatalf("protocolVersion marshaled as string: %s", got)
	}
	if !strings.Contains(got, `"protocolVersion":1`) {
		t.Fatalf("protocolVersion must be JSON number 1, got %s", got)
	}

	var round Request
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	if round.JSONRPC != "2.0" || round.ID != 7 || round.Method != MethodInitialize {
		t.Fatalf("round-trip request = %+v", round)
	}
}

func TestResponseRoundTrip(t *testing.T) {
	t.Parallel()

	result, err := json.Marshal(InitializeResult{
		ProtocolVersion: ProtocolVersion,
		AgentInfo: ImplementationInfo{
			Name:    "codex",
			Version: "1.0.0",
		},
		AuthMethods: []AuthMethod{},
	})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}

	resp := Response{
		JSONRPC: "2.0",
		ID:      0,
		Result:  result,
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	if strings.Contains(string(raw), `"protocolVersion":"1"`) {
		t.Fatalf("protocolVersion marshaled as string: %s", raw)
	}

	var round Response
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if round.JSONRPC != "2.0" || round.ID != 0 || round.Error != nil {
		t.Fatalf("round-trip response = %+v", round)
	}
	var init InitializeResult
	if err := json.Unmarshal(round.Result, &init); err != nil {
		t.Fatalf("unmarshal initialize result: %v", err)
	}
	if init.ProtocolVersion != 1 {
		t.Fatalf("protocolVersion = %d, want 1", init.ProtocolVersion)
	}
}

func TestNotificationRoundTrip(t *testing.T) {
	t.Parallel()

	params, err := json.Marshal(SessionUpdateParams{
		SessionID: "sess_abc",
		Update: SessionUpdate{
			SessionUpdate: "agent_message_chunk",
			Content:       json.RawMessage(`{"type":"text","text":"hi"}`),
		},
	})
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}

	n := Notification{
		JSONRPC: "2.0",
		Method:  MethodSessionUpdate,
		Params:  params,
	}
	raw, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal notification: %v", err)
	}
	if strings.Contains(string(raw), `"id"`) {
		t.Fatalf("notification must not include id: %s", raw)
	}

	var round Notification
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal notification: %v", err)
	}
	if round.Method != MethodSessionUpdate {
		t.Fatalf("method = %q, want %q", round.Method, MethodSessionUpdate)
	}
	var up SessionUpdateParams
	if err := json.Unmarshal(round.Params, &up); err != nil {
		t.Fatalf("unmarshal session update: %v", err)
	}
	if up.SessionID != "sess_abc" || up.Update.SessionUpdate != "agent_message_chunk" {
		t.Fatalf("update params = %+v", up)
	}
}

func TestRPCErrorErrorMessage(t *testing.T) {
	t.Parallel()

	err := &RPCError{Code: -32601, Message: "method not found"}
	got := err.Error()
	if !strings.Contains(got, "-32601") {
		t.Fatalf("error %q missing code", got)
	}
	if !strings.Contains(got, "method not found") {
		t.Fatalf("error %q missing message", got)
	}

	withData := &RPCError{Code: -32000, Message: "failed", Data: "detail"}
	if !strings.Contains(withData.Error(), "detail") {
		t.Fatalf("error %q missing data", withData.Error())
	}
}

func TestSessionPromptRoundTrip(t *testing.T) {
	t.Parallel()

	params := SessionPromptParams{
		SessionID: "sess_1",
		Prompt: []ContentBlock{
			{Type: "text", Text: "hello"},
		},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round SessionPromptParams
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.SessionID != "sess_1" || len(round.Prompt) != 1 || round.Prompt[0].Text != "hello" {
		t.Fatalf("round-trip = %+v", round)
	}

	result := SessionPromptResult{StopReason: "end_turn"}
	rraw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if !strings.Contains(string(rraw), `"stopReason":"end_turn"`) {
		t.Fatalf("stopReason missing: %s", rraw)
	}
}

func TestSessionNewRoundTrip(t *testing.T) {
	t.Parallel()

	params := SessionNewParams{
		Cwd:        "/tmp/project",
		MCPServers: []MCPServer{},
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round SessionNewParams
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round.Cwd != "/tmp/project" {
		t.Fatalf("cwd = %q", round.Cwd)
	}

	res := SessionNewResult{SessionID: "sess_xyz"}
	rraw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if !strings.Contains(string(rraw), `"sessionId":"sess_xyz"`) {
		t.Fatalf("sessionId missing: %s", rraw)
	}
}

func TestMethodConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		got, want string
	}{
		{MethodInitialize, "initialize"},
		{MethodSessionNew, "session/new"},
		{MethodSessionPrompt, "session/prompt"},
		{MethodSessionCancel, "session/cancel"},
		{MethodSessionUpdate, "session/update"},
		{MethodSessionRequestPermission, "session/requestPermission"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("got %q, want %q", tt.got, tt.want)
		}
	}
	if ProtocolVersion != 1 {
		t.Fatalf("ProtocolVersion = %d, want 1", ProtocolVersion)
	}
}
