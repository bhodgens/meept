package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

type fakeACPSession struct {
	state   acp.SessionState
	reply   string
	sendErr error
	lastMsg string
	events  chan acp.SessionEvent
}

func (s *fakeACPSession) State() acp.SessionState { return s.state }

func (s *fakeACPSession) Send(_ context.Context, msg string) (string, error) {
	s.lastMsg = msg
	if s.sendErr != nil {
		return "", s.sendErr
	}
	return s.reply, nil
}

func (s *fakeACPSession) Events() <-chan acp.SessionEvent { return s.events }

type fakeACPMgr struct {
	enabled bool
	sess    acpSession
	getErr  error
	stopErr error
	gets    int
	stops   int
	lastID  string
	lastWD  string
}

func (m *fakeACPMgr) Enabled() bool { return m.enabled }

func (m *fakeACPMgr) GetOrCreate(_ context.Context, agentID, workdir string) (acpSession, error) {
	m.gets++
	m.lastID = agentID
	m.lastWD = workdir
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.sess, nil
}

func (m *fakeACPMgr) Stop(agentID string) error {
	m.stops++
	m.lastID = agentID
	return m.stopErr
}

func newTestACPTool(mgr acpMgr) *ACPAgentTool {
	tool := NewACPAgentTool(nil)
	tool.mgr = mgr
	return tool
}

func readySession(reply string, events ...acp.SessionEvent) *fakeACPSession {
	ch := make(chan acp.SessionEvent, len(events)+1)
	for _, ev := range events {
		ch <- ev
	}
	return &fakeACPSession{
		state:  acp.StateReady,
		reply:  reply,
		events: ch,
	}
}

func acpExecErr(t *testing.T, res any, err error) string {
	t.Helper()
	if err != nil {
		return err.Error()
	}
	switch tr := res.(type) {
	case *tools.ToolResult:
		return tr.Error
	case tools.ToolResult:
		return tr.Error
	default:
		t.Fatalf("expected execute error, got %T %+v", res, res)
		return ""
	}
}

func acpResultMap(t *testing.T, res any, err error) map[string]any {
	t.Helper()
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	var tr tools.ToolResult
	switch v := res.(type) {
	case *tools.ToolResult:
		tr = *v
	case tools.ToolResult:
		tr = v
	default:
		t.Fatalf("result type %T", res)
	}
	if !tr.Success {
		t.Fatalf("success=false error=%s", tr.Error)
	}
	m, ok := tr.Result.(map[string]any)
	if !ok {
		t.Fatalf("result payload %T", tr.Result)
	}
	return m
}

func TestACPAgentTool_NameAndSchema(t *testing.T) {
	tool := NewACPAgentTool(nil)
	if got := tool.Name(); got != "acp_agent" {
		t.Fatalf("Name() = %q, want acp_agent", got)
	}
	params := tool.Parameters()
	if params.Type != schemaTypeObject {
		t.Fatalf("Parameters type = %q, want object", params.Type)
	}
	for _, key := range []string{"agent", "verb", "message", "session"} {
		if _, ok := params.Properties[key]; !ok {
			t.Fatalf("missing property %q", key)
		}
	}
	wantReq := map[string]bool{"agent": true, "verb": true}
	if len(params.Required) != 2 {
		t.Fatalf("Required = %v, want [agent verb]", params.Required)
	}
	for _, r := range params.Required {
		if !wantReq[r] {
			t.Fatalf("unexpected required %q", r)
		}
	}
	verb := params.Properties["verb"]
	wantEnum := []string{"launch", "send", "read", "stop"}
	if len(verb.Enum) != len(wantEnum) {
		t.Fatalf("verb enum = %v, want %v", verb.Enum, wantEnum)
	}
	for i, v := range wantEnum {
		if verb.Enum[i] != v {
			t.Fatalf("verb enum[%d] = %q, want %q", i, verb.Enum[i], v)
		}
	}
}

func TestACPAgentTool_Execute_Validation(t *testing.T) {
	mgr := &fakeACPMgr{enabled: true, sess: readySession("ok")}
	tool := newTestACPTool(mgr)

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{
			name: "missing agent",
			args: map[string]any{"verb": "launch"},
			want: "agent is required",
		},
		{
			name: "empty agent",
			args: map[string]any{"agent": "", "verb": "launch"},
			want: "agent is required",
		},
		{
			name: "bad verb",
			args: map[string]any{"agent": "codex", "verb": "dance"},
			want: "verb must be launch, send, read, or stop",
		},
		{
			name: "missing verb",
			args: map[string]any{"agent": "codex"},
			want: "verb must be launch, send, read, or stop",
		},
		{
			name: "send without message",
			args: map[string]any{"agent": "codex", "verb": "send"},
			want: "message is required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := tool.Execute(context.Background(), tt.args)
			got := acpExecErr(t, res, err)
			if got != tt.want {
				t.Fatalf("error = %q, want %q", got, tt.want)
			}
			if mgr.gets != 0 || mgr.stops != 0 {
				t.Fatalf("validation must not reach manager: gets=%d stops=%d", mgr.gets, mgr.stops)
			}
		})
	}
}

func TestACPAgentTool_Execute_Launch(t *testing.T) {
	sess := readySession("")
	mgr := &fakeACPMgr{enabled: true, sess: sess}
	tool := newTestACPTool(mgr)
	ctx := tools.ContextWithWorkingDir(context.Background(), "/tmp/proj")

	res, err := tool.Execute(ctx, map[string]any{
		"agent": "codex",
		"verb":  "launch",
	})
	got := acpResultMap(t, res, err)
	if mgr.gets != 1 {
		t.Fatalf("GetOrCreate calls = %d, want 1", mgr.gets)
	}
	if mgr.lastID != "codex" {
		t.Fatalf("agent id = %q, want codex", mgr.lastID)
	}
	if mgr.lastWD != "/tmp/proj" {
		t.Fatalf("workdir = %q, want /tmp/proj", mgr.lastWD)
	}
	if got["agent"] != "codex" {
		t.Fatalf("result agent = %v", got["agent"])
	}
	if got["state"] != "ready" {
		t.Fatalf("result state = %v, want ready", got["state"])
	}
	if _, ok := got["elapsed_ms"]; !ok {
		t.Fatal("missing elapsed_ms")
	}
}

func TestACPAgentTool_Execute_EmptyWorkdirPassthrough(t *testing.T) {
	mgr := &fakeACPMgr{enabled: true, sess: readySession("")}
	tool := newTestACPTool(mgr)
	_, err := tool.Execute(context.Background(), map[string]any{
		"agent": "codex",
		"verb":  "launch",
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if mgr.lastWD != "" {
		t.Fatalf("workdir = %q, want empty passthrough", mgr.lastWD)
	}
}

func TestACPAgentTool_Execute_Send(t *testing.T) {
	sess := readySession("pong")
	mgr := &fakeACPMgr{enabled: true, sess: sess}
	tool := newTestACPTool(mgr)

	res, err := tool.Execute(context.Background(), map[string]any{
		"agent":   "codex",
		"verb":    "send",
		"message": "ping",
	})
	got := acpResultMap(t, res, err)
	if sess.lastMsg != "ping" {
		t.Fatalf("sent message = %q, want ping", sess.lastMsg)
	}
	if got["reply"] != "pong" {
		t.Fatalf("reply = %v, want pong", got["reply"])
	}
	if got["agent"] != "codex" {
		t.Fatalf("agent = %v", got["agent"])
	}
}

func TestACPAgentTool_Execute_Read(t *testing.T) {
	sess := readySession("", acp.SessionEvent{Kind: "chunk", Text: "hello"}, acp.SessionEvent{Kind: "tool", Text: "grep"})
	mgr := &fakeACPMgr{enabled: true, sess: sess}
	tool := newTestACPTool(mgr)

	res, err := tool.Execute(context.Background(), map[string]any{
		"agent": "codex",
		"verb":  "read",
	})
	got := acpResultMap(t, res, err)
	events, ok := got["events"].([]map[string]string)
	if !ok {
		if raw, ok := got["events"].([]any); ok {
			if len(raw) != 2 {
				t.Fatalf("events len = %d, want 2 (%T)", len(raw), got["events"])
			}
			return
		}
		t.Fatalf("events type %T", got["events"])
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
}

func TestACPAgentTool_Execute_Stop(t *testing.T) {
	mgr := &fakeACPMgr{enabled: true, sess: readySession("")}
	tool := newTestACPTool(mgr)

	res, err := tool.Execute(context.Background(), map[string]any{
		"agent": "codex",
		"verb":  "stop",
	})
	got := acpResultMap(t, res, err)
	if mgr.stops != 1 {
		t.Fatalf("Stop calls = %d, want 1", mgr.stops)
	}
	if mgr.gets != 0 {
		t.Fatalf("stop must not GetOrCreate, gets=%d", mgr.gets)
	}
	if mgr.lastID != "codex" {
		t.Fatalf("stopped id = %q", mgr.lastID)
	}
	if got["state"] != "closed" {
		t.Fatalf("state = %v, want closed", got["state"])
	}
}

func TestACPAgentTool_Execute_Disabled(t *testing.T) {
	tests := []struct {
		name string
		tool *ACPAgentTool
		mgr  *fakeACPMgr
	}{
		{
			name: "nil manager",
			tool: NewACPAgentTool(nil),
		},
		{
			name: "enabled false",
			mgr:  &fakeACPMgr{enabled: false, sess: readySession("")},
		},
	}
	verbs := []string{"launch", "send", "read", "stop"}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := tt.tool
			if tool == nil {
				tool = newTestACPTool(tt.mgr)
			} else if tt.mgr != nil {
				tool.mgr = tt.mgr
			}
			for _, verb := range verbs {
				args := map[string]any{"agent": "codex", "verb": verb}
				if verb == "send" {
					args["message"] = "hi"
				}
				res, err := tool.Execute(context.Background(), args)
				got := acpExecErr(t, res, err)
				if got != "acp disabled" {
					t.Fatalf("verb %s: error = %q, want acp disabled", verb, got)
				}
			}
			if tt.mgr != nil && (tt.mgr.gets != 0 || tt.mgr.stops != 0) {
				t.Fatalf("disabled path reached manager: gets=%d stops=%d", tt.mgr.gets, tt.mgr.stops)
			}
		})
	}
}

func TestACPAgentTool_Execute_SentinelTranslation(t *testing.T) {
	tests := []struct {
		name   string
		getErr error
		want   string
	}{
		{name: "not found", getErr: acp.ErrAgentNotFound, want: "agent not found: codex"},
		{name: "agent disabled", getErr: acp.ErrAgentDisabled, want: "agent disabled: codex"},
		{name: "max agents", getErr: acp.ErrMaxAgents, want: "max agents: codex"},
		{name: "disabled sentinel", getErr: acp.ErrDisabled, want: "acp disabled"},
		{
			name:   "wrapped not found",
			getErr: errors.Join(acp.ErrAgentNotFound),
			want:   "agent not found: codex",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := &fakeACPMgr{enabled: true, getErr: tt.getErr}
			tool := newTestACPTool(mgr)
			res, err := tool.Execute(context.Background(), map[string]any{
				"agent": "codex",
				"verb":  "launch",
			})
			got := acpExecErr(t, res, err)
			if got != tt.want {
				t.Fatalf("error = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestACPAgentTool_IsReadOnly(t *testing.T) {
	tool := NewACPAgentTool(nil)
	if !tool.IsReadOnly(map[string]any{"verb": "read"}) {
		t.Fatal("read should be read-only")
	}
	for _, verb := range []string{"launch", "send", "stop", ""} {
		if tool.IsReadOnly(map[string]any{"verb": verb}) {
			t.Fatalf("verb %q should not be read-only", verb)
		}
	}
	if tool.IsReadOnly(nil) {
		t.Fatal("nil input should not be read-only")
	}
}

func TestACPAgentTool_SettersNilSafe(t *testing.T) {
	var tool *ACPAgentTool
	tool.SetManager(nil)
	tool.SetEnabled(false)

	tool = NewACPAgentTool(nil)
	tool.SetManager(nil)
	tool.SetEnabled(false)
	if tool.mgr != nil {
		t.Fatal("SetManager(nil) must not store a manager")
	}
}

func TestACPAgentTool_SchemaModes(t *testing.T) {
	tool := NewACPAgentTool(nil)
	def := llm.NewToolDefinition(tool.Name(), tool.Description(), tool.Parameters())
	raw, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("marshal full definition: %v", err)
	}
	if !strings.Contains(string(raw), "acp_agent") {
		t.Fatalf("marshalled definition missing name: %s", raw)
	}

	reg := tools.NewRegistry(nil)
	reg.Register(tool)
	full := reg.ToLLMDefinitions()
	if len(full) != 1 {
		t.Fatalf("full defs = %d, want 1", len(full))
	}
	reg.SetSchemaMode(tools.SchemaModeIndexed, nil)
	indexed := reg.ToLLMDefinitions()
	if len(indexed) != 1 {
		t.Fatalf("indexed defs = %d, want 1", len(indexed))
	}
	reg.SetSchemaMode(tools.SchemaModeFull, nil)
	roundTrip := reg.ToLLMDefinitions()
	if len(roundTrip) != 1 || roundTrip[0].Function.Name != "acp_agent" {
		t.Fatalf("full-mode round trip lost acp_agent: %+v", roundTrip)
	}
	if _, err := json.Marshal(indexed); err != nil {
		t.Fatalf("marshal indexed: %v", err)
	}
}
