package rpc

// voice_link_test.go - handler tests for the voice.link.* RPC surface
// (deadair-link plan leaf 03 + the monitor-tap extension). Seam-driven
// like voice_local_test.go: fake seams only, zero voice-package types
// (the rpc -> voice edge would close the voice -> activity -> rpc cycle).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// fakeLinkSeams builds a recording seam set. openOpts captures the
// options the last Open received; openResult is returned verbatim.
type fakeLinkSeams struct {
	openCalls  []string // "employeeA|employeeB" per call
	openOpts   []VoiceLinkOpenOptions
	closeCalls []string
	openResult VoiceLinkOpen
	openErr    error
	closeErr   error
	statusRes  VoiceLinkStatusResult
	statusErr  error
	employeeOK *bool // nil = seam unwired posture; else the answer
}

func (f *fakeLinkSeams) seams() VoiceLinkSeams {
	s := VoiceLinkSeams{
		Open: func(_ context.Context, a, b string, opts VoiceLinkOpenOptions) (VoiceLinkOpen, error) {
			f.openCalls = append(f.openCalls, a+"|"+b)
			f.openOpts = append(f.openOpts, opts)
			return f.openResult, f.openErr
		},
		Close: func(linkID, _ string) error {
			f.closeCalls = append(f.closeCalls, linkID)
			return f.closeErr
		},
		Status: func(_ context.Context, linkID string) (VoiceLinkStatusResult, error) {
			res := f.statusRes
			res.LinkID = linkID
			return res, f.statusErr
		},
	}
	if f.employeeOK != nil {
		ok := *f.employeeOK
		s.EmployeeExists = func(_ context.Context, _ string) (bool, error) { return ok, nil }
	}
	return s
}

func linkParams(t *testing.T, m map[string]any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return raw
}

func TestVoiceLinkOpenOK(t *testing.T) {
	f := &fakeLinkSeams{
		openResult: VoiceLinkOpen{LinkID: "link-abc", ACCID: "local-1", BCCID: "local-2"},
		employeeOK: new(true),
	}
	h := NewVoiceLinkHandler(f.seams())
	res, err := h.handleOpen(context.Background(), linkParams(t, map[string]any{
		"employee_a": "companion", "employee_b": "secretary",
	}))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	open, ok := res.(VoiceLinkOpen)
	if !ok {
		t.Fatalf("want VoiceLinkOpen, got %T", res)
	}
	if open.LinkID != "link-abc" || open.ACCID != "local-1" || open.BCCID != "local-2" {
		t.Fatalf("unexpected open result: %+v", open)
	}
	// Byte-compat: without monitor the new fields are EMPTY and absent
	// from the JSON envelope (old CLI/GUI callers unchanged).
	if open.MonitorAPath != "" || open.MonitorBPath != "" {
		t.Fatalf("monitor paths leaked without monitor:true: %+v", open)
	}
	b, err := json.Marshal(open)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"link_id"`, `"a_ccid"`, `"b_ccid"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("json missing %s: %s", key, b)
		}
	}
	for _, key := range []string{"a_mon_path", "b_mon_path"} {
		if strings.Contains(string(b), key) {
			t.Fatalf("json carries %s without monitor: %s", key, b)
		}
	}
	// Seam received the zero options.
	if len(f.openOpts) != 1 || f.openOpts[0].Monitor {
		t.Fatalf("seam opts = %v, want one zero-value entry", f.openOpts)
	}
}

func TestVoiceLinkOpenMonitor(t *testing.T) {
	f := &fakeLinkSeams{
		openResult: VoiceLinkOpen{
			LinkID: "link-mon", ACCID: "local-1", BCCID: "local-2",
			MonitorAPath: "/s/link-mon.mon-a", MonitorBPath: "/s/link-mon.mon-b",
		},
		employeeOK: new(true),
	}
	h := NewVoiceLinkHandler(f.seams())
	res, err := h.handleOpen(context.Background(), linkParams(t, map[string]any{
		"employee_a": "companion", "employee_b": "secretary", "monitor": true,
	}))
	if err != nil {
		t.Fatalf("open(monitor): %v", err)
	}
	open := res.(VoiceLinkOpen)
	if !f.openOpts[0].Monitor {
		t.Fatal("monitor:true did not reach the seam")
	}
	if open.MonitorAPath != "/s/link-mon.mon-a" || open.MonitorBPath != "/s/link-mon.mon-b" {
		t.Fatalf("monitor paths not echoed: %+v", open)
	}
	b, err := json.Marshal(open)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"a_mon_path"`, `"b_mon_path"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("json missing %s: %s", key, b)
		}
	}

	// monitor:false behaves exactly like an absent monitor.
	f2 := &fakeLinkSeams{openResult: VoiceLinkOpen{LinkID: "l"}, employeeOK: new(true)}
	h2 := NewVoiceLinkHandler(f2.seams())
	res2, err := h2.handleOpen(context.Background(), linkParams(t, map[string]any{
		"employee_a": "a", "employee_b": "b", "monitor": false,
	}))
	if err != nil {
		t.Fatalf("open(monitor:false): %v", err)
	}
	o2 := res2.(VoiceLinkOpen)
	if o2.MonitorAPath != "" || o2.MonitorBPath != "" || f2.openOpts[0].Monitor {
		t.Fatalf("monitor:false leaked: %+v opts=%v", o2, f2.openOpts)
	}
}

func TestVoiceLinkOpenRefusals(t *testing.T) {
	ok := true
	f := &fakeLinkSeams{employeeOK: &ok}
	h := NewVoiceLinkHandler(f.seams())

	// Empty ids.
	if _, err := h.handleOpen(context.Background(), linkParams(t, map[string]any{"employee_b": "b"})); !errors.Is(err, ErrVoiceLinkUnknownEmployee) {
		t.Fatalf("missing employee_a = %v, want ErrVoiceLinkUnknownEmployee", err)
	}
	// Same employee (refused BEFORE the seam).
	if _, err := h.handleOpen(context.Background(), linkParams(t, map[string]any{"employee_a": "x", "employee_b": "x"})); !errors.Is(err, ErrVoiceLinkSameEmployee) {
		t.Fatalf("same employee = %v, want ErrVoiceLinkSameEmployee", err)
	}
	if len(f.openCalls) != 0 {
		t.Fatalf("refusals reached the seam: %v", f.openCalls)
	}
	// Unknown employee via the exists seam.
	no := false
	f2 := &fakeLinkSeams{employeeOK: &no}
	h2 := NewVoiceLinkHandler(f2.seams())
	if _, err := h2.handleOpen(context.Background(), linkParams(t, map[string]any{"employee_a": "a", "employee_b": "b"})); !errors.Is(err, ErrVoiceLinkUnknownEmployee) {
		t.Fatalf("unknown employee = %v, want ErrVoiceLinkUnknownEmployee", err)
	}
}

func TestVoiceLinkOpenDisabled(t *testing.T) {
	h := NewVoiceLinkHandler(VoiceLinkSeams{})
	if _, err := h.handleOpen(context.Background(), linkParams(t, map[string]any{"employee_a": "a", "employee_b": "b"})); !errors.Is(err, ErrVoiceLinkDisabled) {
		t.Fatalf("nil seams = %v, want ErrVoiceLinkDisabled", err)
	}
}

func TestVoiceLinkCloseAndStatus(t *testing.T) {
	f := &fakeLinkSeams{}
	h := NewVoiceLinkHandler(f.seams())
	res, err := h.handleClose(context.Background(), linkParams(t, map[string]any{"link_id": "link-1"}))
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if r := res.(VoiceLinkCloseResult); !r.OK {
		t.Fatalf("close result = %+v, want ok=true", r)
	}
	if _, err := h.handleClose(context.Background(), linkParams(t, map[string]any{})); !errors.Is(err, ErrVoiceLinkUnknownLink) {
		t.Fatalf("missing link_id = %v, want ErrVoiceLinkUnknownLink", err)
	}

	// Status: transcripts never null.
	f.statusRes = VoiceLinkStatusResult{Ended: false, A: VoiceLinkSideStatus{CallID: "local-1"}}
	sres, err := h.handleStatus(context.Background(), linkParams(t, map[string]any{"link_id": "link-1"}))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	st := sres.(VoiceLinkStatusResult)
	if st.A.Transcript == nil || st.B.Transcript == nil {
		t.Fatalf("status transcripts must never be nil: %+v", st)
	}
}

func TestVoiceLinkRegister(t *testing.T) {
	f := &fakeLinkSeams{
		openResult: VoiceLinkOpen{LinkID: "link-r"},
		employeeOK: new(true),
	}
	h := NewVoiceLinkHandler(f.seams())
	s := New(&Config{}, nil, nil)
	h.RegisterVoiceLinkMethods(s)
	if _, err := s.CallMethod(context.Background(), "voice.link.open", []byte(`{"employee_a":"eve","employee_b":"ada"}`)); err != nil {
		t.Fatalf("voice.link.open not registered: %v", err)
	}
	if _, err := s.CallMethod(context.Background(), "voice.link.close", []byte(`{"link_id":"link-r"}`)); err != nil {
		t.Fatalf("voice.link.close not registered: %v", err)
	}
	if _, err := s.CallMethod(context.Background(), "voice.link.status", []byte(`{"link_id":"link-r"}`)); err != nil {
		t.Fatalf("voice.link.status not registered: %v", err)
	}
}
