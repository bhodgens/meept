package rpc

// voice_local_test.go - handler tests for the voice.local.* RPC surface
// (local-leg leaf 08, T1). The handler is seam-based: every test drives a
// fake seam set, no voice-package types (the rpc -> voice edge would close
// the voice -> activity -> rpc import cycle).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeSeams builds a seam set with recording fakes.
type fakeSeams struct {
	openCalls   []string // "employee|label" per call
	endCalls    []string
	openResult  VoiceLocalOpen
	openErr     error
	endErr      error
	statusRows  []VoiceCallStatus
	employeeOK  bool
	employeeErr error
	// voice.local.events fakes (operator telemetry).
	eventsCalls     int
	eventsRows      []VoiceCallEvent
	eventsTruncated bool
	eventsHasMore   bool
	eventsFound     bool
	// voice.local.transcript fakes (end-of-call transcript read).
	transcriptCalls int
	transcriptEmp   string
	transcriptSess  string
	transcriptLines []VoiceCallTranscriptLine
	transcriptFound bool
}

func (f *fakeSeams) seams() VoiceLocalSeams {
	return VoiceLocalSeams{
		Open: func(_ context.Context, employeeID, label string) (VoiceLocalOpen, error) {
			f.openCalls = append(f.openCalls, employeeID+"|"+label)
			return f.openResult, f.openErr
		},
		End: func(callID, _ string) error {
			f.endCalls = append(f.endCalls, callID)
			return f.endErr
		},
		Status: func() []VoiceCallStatus {
			return f.statusRows
		},
		EmployeeExists: func(_ context.Context, _ string) (bool, error) {
			return f.employeeOK, f.employeeErr
		},
		Events: func(_ string, _ uint64, _ int) ([]VoiceCallEvent, bool, bool, bool) {
			f.eventsCalls++
			return f.eventsRows, f.eventsTruncated, f.eventsHasMore, f.eventsFound
		},
		Transcript: func(_ context.Context, _ string) (string, string, []VoiceCallTranscriptLine, bool) {
			f.transcriptCalls++
			return f.transcriptEmp, f.transcriptSess, f.transcriptLines, f.transcriptFound
		},
	}
}

func TestVoiceLocalOpenOK(t *testing.T) {
	f := &fakeSeams{
		openResult: VoiceLocalOpen{
			CallID: "local-abc", InPath: "/x/a.in", OutPath: "/x/a.out", SessionID: "sess-1",
		},
		employeeOK: true,
	}
	h := NewVoiceLocalHandler(f.seams())
	raw, err := json.Marshal(map[string]any{"employee_id": "eve", "label": "desk"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := h.handleOpen(context.Background(), raw)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	open, ok := res.(VoiceLocalOpen)
	if !ok {
		t.Fatalf("want VoiceLocalOpen, got %T", res)
	}
	if open.CallID != "local-abc" || open.InPath != "/x/a.in" || open.OutPath != "/x/a.out" || open.SessionID != "sess-1" {
		t.Fatalf("unexpected open result: %+v", open)
	}
	if len(f.openCalls) != 1 || f.openCalls[0] != "eve|desk" {
		t.Fatalf("seam args mismatch: %v", f.openCalls)
	}

	// CL8 verbatim JSON tags.
	b, err := json.Marshal(open)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"call_id", "in_path", "out_path", "session_id"} {
		if !strings.Contains(string(b), `"`+key+`"`) {
			t.Fatalf("json missing %s: %s", key, b)
		}
	}
}

func TestVoiceLocalOpenDisabled(t *testing.T) {
	// nil seams = unwired leg: the named disabled refusal.
	h := NewVoiceLocalHandler(VoiceLocalSeams{})
	raw, _ := json.Marshal(map[string]any{"employee_id": "eve"})
	_, err := h.handleOpen(context.Background(), raw)
	if !errors.Is(err, ErrVoiceLocalDisabled) {
		t.Fatalf("want ErrVoiceLocalDisabled, got %v", err)
	}
	_, err = h.handleEnd(context.Background(), []byte(`{"call_id":"local-x"}`))
	if !errors.Is(err, ErrVoiceLocalDisabled) {
		t.Fatalf("end: want ErrVoiceLocalDisabled, got %v", err)
	}
}

func TestVoiceLocalOpenUnknownEmployee(t *testing.T) {
	f := &fakeSeams{employeeOK: false}
	h := NewVoiceLocalHandler(f.seams())
	raw, _ := json.Marshal(map[string]any{"employee_id": "ghost"})
	_, err := h.handleOpen(context.Background(), raw)
	if !errors.Is(err, ErrVoiceLocalUnknownEmployee) {
		t.Fatalf("want ErrVoiceLocalUnknownEmployee, got %v", err)
	}
	// empty employee id: same named refusal before any seam call.
	raw, _ = json.Marshal(map[string]any{"employee_id": ""})
	_, err = h.handleOpen(context.Background(), raw)
	if !errors.Is(err, ErrVoiceLocalUnknownEmployee) {
		t.Fatalf("empty id: want ErrVoiceLocalUnknownEmployee, got %v", err)
	}
	if len(f.openCalls) != 0 {
		t.Fatalf("open seam must not fire on unknown employee: %v", f.openCalls)
	}
}

func TestVoiceLocalOpenBusyAndDuplicate(t *testing.T) {
	f := &fakeSeams{openErr: ErrVoiceLocalBusy, employeeOK: true}
	h := NewVoiceLocalHandler(f.seams())
	raw, _ := json.Marshal(map[string]any{"employee_id": "eve"})
	if _, err := h.handleOpen(context.Background(), raw); !errors.Is(err, ErrVoiceLocalBusy) {
		t.Fatalf("want ErrVoiceLocalBusy, got %v", err)
	}
	f.openErr = ErrVoiceLocalDuplicate
	if _, err := h.handleOpen(context.Background(), raw); !errors.Is(err, ErrVoiceLocalDuplicate) {
		t.Fatalf("want ErrVoiceLocalDuplicate, got %v", err)
	}
}

func TestVoiceLocalEndIdempotent(t *testing.T) {
	f := &fakeSeams{employeeOK: true} // End seam returns nil both times
	h := NewVoiceLocalHandler(f.seams())
	raw, _ := json.Marshal(map[string]any{"call_id": "local-abc"})
	res, err := h.handleEnd(context.Background(), raw)
	if err != nil {
		t.Fatalf("end 1: %v", err)
	}
	if res.(VoiceLocalEndResult).OK != true {
		t.Fatalf("end 1: want ok=true")
	}
	res, err = h.handleEnd(context.Background(), raw) // double end: still ok
	if err != nil {
		t.Fatalf("end 2 (idempotent): %v", err)
	}
	if res.(VoiceLocalEndResult).OK != true {
		t.Fatalf("end 2: want ok=true")
	}
	if len(f.endCalls) != 2 || f.endCalls[0] != "local-abc" {
		t.Fatalf("end seam calls mismatch: %v", f.endCalls)
	}
	// missing call_id: clean refusal, seam untouched
	if _, err := h.handleEnd(context.Background(), []byte(`{}`)); err == nil {
		t.Fatalf("want error for empty call_id")
	}
	if len(f.endCalls) != 2 {
		t.Fatalf("empty call_id must not reach the seam: %v", f.endCalls)
	}
}

func TestVoiceLocalEndForeignCall(t *testing.T) {
	f := &fakeSeams{endErr: ErrVoiceLocalUnknownCall}
	h := NewVoiceLocalHandler(f.seams())
	raw, _ := json.Marshal(map[string]any{"call_id": "not-ours"})
	_, err := h.handleEnd(context.Background(), raw)
	if !errors.Is(err, ErrVoiceLocalUnknownCall) {
		t.Fatalf("want ErrVoiceLocalUnknownCall, got %v", err)
	}
}

func TestVoiceLocalStatusListsLegs(t *testing.T) {
	f := &fakeSeams{statusRows: []VoiceCallStatus{
		{CallID: "cc-phone", EmployeeID: "eve", Target: "+15551234", SinceRFC3339: "2026-11-07T10:00:00Z", Leg: "phone"},
		{CallID: "local-abc", EmployeeID: "eve", Target: "local:desk", SinceRFC3339: "2026-11-07T10:05:00Z", Leg: "local"},
	}}
	h := NewVoiceLocalHandler(f.seams())
	res, err := h.handleStatus(context.Background(), nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	st, ok := res.(VoiceLocalStatusResult)
	if !ok {
		t.Fatalf("want VoiceLocalStatusResult, got %T", res)
	}
	if len(st.Calls) != 2 {
		t.Fatalf("want 2 rows (both legs), got %d", len(st.Calls))
	}
	legs := map[string]bool{}
	for _, c := range st.Calls {
		legs[c.Leg] = true
	}
	if !legs["phone"] || !legs["local"] {
		t.Fatalf("want both legs, got %v", st.Calls)
	}
	// CL8 verbatim JSON tags.
	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"calls", "call_id", "employee_id", "target", "since_rfc3339", "leg"} {
		if !strings.Contains(string(b), `"`+key+`"`) {
			t.Fatalf("json missing %s: %s", key, b)
		}
	}
}

func TestVoiceLocalStatusEmpty(t *testing.T) {
	// nil rows (no voice wiring) still answer with an empty list envelope.
	f := &fakeSeams{statusRows: nil}
	h := NewVoiceLocalHandler(f.seams())
	res, err := h.handleStatus(context.Background(), nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"calls":[]}` {
		t.Fatalf("want empty-list envelope, got %s", b)
	}
}

func TestVoiceLocalRegister(t *testing.T) {
	f := &fakeSeams{openResult: VoiceLocalOpen{CallID: "local-r"}, employeeOK: true}
	h := NewVoiceLocalHandler(f.seams())
	s := New(&Config{}, nil, nil)
	h.RegisterVoiceLocalMethods(s)
	if _, err := s.CallMethod(context.Background(), "voice.local.open", []byte(`{"employee_id":"eve"}`)); err != nil {
		t.Fatalf("voice.local.open not registered: %v", err)
	}
	if _, err := s.CallMethod(context.Background(), "voice.local.end", []byte(`{"call_id":"local-r"}`)); err != nil {
		t.Fatalf("voice.local.end not registered: %v", err)
	}
	if _, err := s.CallMethod(context.Background(), "voice.local.status", nil); err != nil {
		t.Fatalf("voice.local.status not registered: %v", err)
	}
}

// --- voice.local.transcript (end-of-call transcript read) ---

func TestVoiceLocalTranscriptOK(t *testing.T) {
	f := &fakeSeams{
		transcriptEmp:  "companion",
		transcriptSess: "session-1",
		transcriptLines: []VoiceCallTranscriptLine{
			{Role: "user", Text: "hi", At: "2026-11-11T21:39:08Z"},
			{Role: "assistant", Text: "hello", At: "2026-11-11T21:39:13Z"},
		},
		transcriptFound: true,
	}
	h := NewVoiceLocalHandler(f.seams())
	raw, err := json.Marshal(map[string]any{"call_id": "local-x"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := h.handleTranscript(context.Background(), raw)
	if err != nil {
		t.Fatalf("transcript: %v", err)
	}
	out, ok := res.(VoiceLocalTranscriptResult)
	if !ok || !out.Found || len(out.Lines) != 2 || out.EmployeeID != "companion" || out.SessionID != "session-1" {
		t.Fatalf("unexpected result: %+v", out)
	}
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"call_id", "employee_id", "session_id", "lines", "found", "role", "text", "at"} {
		if !strings.Contains(string(b), `"`+key+`"`) {
			t.Fatalf("json missing %s: %s", key, b)
		}
	}
}

func TestVoiceLocalTranscriptMissingCallID(t *testing.T) {
	f := &fakeSeams{}
	h := NewVoiceLocalHandler(f.seams())
	_, err := h.handleTranscript(context.Background(), []byte(`{}`))
	if !errors.Is(err, ErrVoiceLocalUnknownCall) {
		t.Fatalf("want ErrVoiceLocalUnknownCall, got %v", err)
	}
	if f.transcriptCalls != 0 {
		t.Fatalf("seam must not fire without call_id: %d", f.transcriptCalls)
	}
}

func TestVoiceLocalTranscriptUnwiredIsEmptyEnvelope(t *testing.T) {
	// Zero seams (no store wired): the empty envelope, never an error -
	// the CLI's end-of-call print is best-effort against old/unwired
	// daemons.
	h := NewVoiceLocalHandler(VoiceLocalSeams{})
	res, err := h.handleTranscript(context.Background(), []byte(`{"call_id":"local-x"}`))
	if err != nil {
		t.Fatalf("unwired transcript must not error: %v", err)
	}
	out := res.(VoiceLocalTranscriptResult)
	if out.Found || out.Lines == nil || len(out.Lines) != 0 {
		t.Fatalf("want empty non-nil envelope, got %+v", out)
	}
}
