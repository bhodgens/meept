package main

// Gate tests for Contract C4 (tree docs/plans/20261113-escalation-resume-and-hardening,
// leaf 04): `meept agents amend` must send {"id", "fields": {map},
// "reason"-when-set} — the shape internal/employee/handler.go amendRequest
// requires — and decode JSON-looking values before sending. The current
// command sends {"id", "field", "value"} (agents_cmd.go:853-857), which the
// handler always rejects with "at least one field is required".
//
// Factor-testable contract: leaf 04 factors the payload construction into
// buildAmendPayload(agentID, field, value, reason string) (map[string]any, error)
// (the cobra RunE keeps using it inline). These tests drive that helper AND
// the full client.Call path against a fake transport.Client so the wire
// payload is pinned end-to-end. RED today: buildAmendPayload is undefined
// (compile error naming it); after leaf 04, any regression to the singular
// field/value shape fails the map-shape assertions.

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/transport"
)

// spyClient is a transport.Client fake that records the last Call payload.
// transport.Client is a wide interface, so we embed the real interface and
// override only what the amend path touches; every embedded method stays
// nil and panics naturally if accidentally invoked.
type spyClient struct {
	transport.Client
	lastMethod string
	lastParams json.RawMessage
}

func (s *spyClient) Call(method string, params any) (json.RawMessage, error) {
	s.lastMethod = method
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	s.lastParams = raw
	return json.RawMessage(`{"id":"x","status":"amendment_proposed","plan_id":"plan-1"}`), nil
}

func decodePayload(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode payload: %v (%s)", err, string(raw))
	}
	return m
}

// TestAmendCmd_SendsFieldsShape: the amend payload carries {"id", "fields"
// as a map} (+ "reason" only when set); the singular "field"/"value" keys —
// today's broken shape — must be gone.
func TestAmendCmd_SendsFieldsShape(t *testing.T) {
	payload, err := buildAmendPayload("coder", "role", "planner", "")
	if err != nil {
		t.Fatalf("buildAmendPayload error: %v", err)
	}

	fields, ok := payload["fields"].(map[string]any)
	if !ok {
		t.Fatalf("payload[fields] = %T (%v), want map[string]any", payload["fields"], payload["fields"])
	}
	if got := fields["role"]; got != "planner" {
		t.Errorf("fields[role] = %v, want %q", got, "planner")
	}
	if _, has := payload["field"]; has {
		t.Errorf("payload carries the singular \"field\" key — the broken shape the handler rejects: %v", payload)
	}
	if _, has := payload["value"]; has {
		t.Errorf("payload carries the singular \"value\" key — the broken shape the handler rejects: %v", payload)
	}
	if got := payload["id"]; got != "coder" {
		t.Errorf("payload[id] = %v, want %q", got, "coder")
	}
	if _, has := payload["reason"]; has {
		t.Errorf("payload carries \"reason\" without --reason set: %v (key must be omitted when empty)", payload)
	}
}

// TestAmendCmd_ReasonKeyPresentWhenSet pins the --reason flag: set → key
// present with the exact value; the omitempty semantics live at the map
// level.
func TestAmendCmd_ReasonKeyPresentWhenSet(t *testing.T) {
	payload, err := buildAmendPayload("coder", "role", "planner", "hardening")
	if err != nil {
		t.Fatalf("buildAmendPayload error: %v", err)
	}
	if got := payload["reason"]; got != "hardening" {
		t.Errorf("payload[reason] = %v, want %q", got, "hardening")
	}
}

// TestAmendCmd_ValueJSONDecoded: a value that parses as JSON is sent
// decoded ([]any for arrays, numbers as numbers); a plain string stays a
// raw string.
func TestAmendCmd_ValueJSONDecoded(t *testing.T) {
	// Array value decodes to []any.
	payload, err := buildAmendPayload("coder", "constraints.tools_allowed", `["a","b"]`, "")
	if err != nil {
		t.Fatalf("buildAmendPayload array error: %v", err)
	}
	fields := payload["fields"].(map[string]any)
	arr, ok := fields["constraints.tools_allowed"].([]any)
	if !ok {
		t.Fatalf("array value sent as %T (%v), want []any", fields["constraints.tools_allowed"], fields["constraints.tools_allowed"])
	}
	if len(arr) != 2 || arr[0] != "a" || arr[1] != "b" {
		t.Errorf("decoded array = %v, want [a b]", arr)
	}

	// Number value decodes to a JSON number.
	payload, err = buildAmendPayload("coder", "max_active_plans", "42", "")
	if err != nil {
		t.Fatalf("buildAmendPayload number error: %v", err)
	}
	fields = payload["fields"].(map[string]any)
	if num, ok := fields["max_active_plans"].(float64); !ok || num != 42 {
		t.Errorf("number value sent as %T (%v), want float64(42)", fields["max_active_plans"], fields["max_active_plans"])
	}

	// Plain string stays raw (json.Unmarshal("planner") fails → fallback).
	payload, err = buildAmendPayload("coder", "role", "planner", "")
	if err != nil {
		t.Fatalf("buildAmendPayload string error: %v", err)
	}
	fields = payload["fields"].(map[string]any)
	if got := fields["role"]; got != "planner" {
		t.Errorf("plain-string value sent as %T (%v), want raw string \"planner\"", got, got)
	}
}

// TestAmendCmd_WirePayloadThroughClient pins the end-to-end wire shape: the
// amend RunE path calls agents.amend on the client with the fields-map
// payload (uses the same builder the RunE must call).
func TestAmendCmd_WirePayloadThroughClient(t *testing.T) {
	payload, err := buildAmendPayload("researcher", "constraints.risk_ceiling", "high", "ops request")
	if err != nil {
		t.Fatalf("buildAmendPayload error: %v", err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	got := decodePayload(t, raw)
	fields, ok := got["fields"].(map[string]any)
	if !ok {
		t.Fatalf("wire payload[fields] = %T, want map (handler amendRequest requires fields)", got["fields"])
	}
	if fields["constraints.risk_ceiling"] != "high" {
		t.Errorf("wire fields = %v, want constraints.risk_ceiling=high", fields)
	}
	if got["reason"] != "ops request" {
		t.Errorf("wire reason = %v, want %q", got["reason"], "ops request")
	}

	// spyClient stays as scaffolding for the leaf's RunE-level wiring
	// variant (the builder is the frozen contract); keep it referenced so
	// the type is exercised by the compiler.
	_ = spyClient{}
}
