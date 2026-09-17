package http

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/comm/wsclass"
	"github.com/caimlas/meept/pkg/models"
)

// The schema contract lives at schemas/ws_events.schema.json (repo root).
// It is the single cross-language artifact binding agent.TurnTerminalEvent's
// json tags and the relay's chat_message normalization to the Flutter
// client's generated models (ui/flutter_ui/lib/models/ws_events.dart).
// This file is the Go-side drift gate: a struct-tag edit that is not
// mirrored in the schema (or vice versa) fails here and in CI via
// `go test ./internal/comm/http -run TestWSSchema`.
const wsEventsSchemaPath = "../../../schemas/ws_events.schema.json"

var update = flag.Bool("update", false, "rewrite the golden fixture at ui/flutter_ui/test/fixtures/turn_terminal_event.json")

// goStructJSONTags extracts the json tags of agent.TurnTerminalEvent via
// reflect, so the drift check compares against the real source of truth
// (the struct itself), not a hand-copied key list.
func goStructJSONTags(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	typ := reflect.TypeOf(agent.TurnTerminalEvent{})
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		out[name] = tag
	}
	return out
}

// validateAgainstDefinition structurally checks a wire map against a schema
// definition: every emitted key must be declared (drift = rename/new key
// without a schema edit), emitted scalar types must match the declaration,
// and required keys must be present unless the Go tag carries omitempty.
func validateAgainstDefinition(t *testing.T, schema map[string]any, def string, wire map[string]any, structTags map[string]string) error {
	t.Helper()
	defs, _ := schema["definitions"].(map[string]any)
	d, ok := defs[def].(map[string]any)
	if !ok {
		return fmt.Errorf("schema definitions missing %q", def)
	}
	props, _ := d["properties"].(map[string]any)
	for k, v := range wire {
		decl, declared := props[k]
		if !declared {
			return fmt.Errorf("wire key %q (def %s) is NOT declared in schemas/ws_events.schema.json — struct tag drift", k, def)
		}
		pmap, _ := decl.(map[string]any)
		wantType, _ := pmap["type"].(string)
		switch wantType {
		case "string":
			if _, ok := v.(string); !ok {
				return fmt.Errorf("wire key %q: schema says string, Go emitted %T", k, v)
			}
		case "integer":
			if _, ok := v.(float64); !ok {
				return fmt.Errorf("wire key %q: schema says integer, Go emitted %T", k, v)
			}
		}
	}
	for _, req := range schemaRequired(t, schema, def) {
		if _, ok := wire[req]; !ok {
			// Only an error if the Go struct does not mark it omitempty
			// (a required schema key Go may legally omit means the schema
			// drifted, not the struct). Keys absent from structTags are
			// relay-injected (type/source_topic/timestamp) and guaranteed.
			if tag, ok := structTags[req]; ok && !strings.Contains(tag, "omitempty") {
				return fmt.Errorf("schema requires key %q but the Go struct omits it without omitempty — schema drift", req)
			}
		}
	}
	return nil
}

// schemaRequired returns the required-key list of a definition.
func schemaRequired(t *testing.T, schema map[string]any, def string) []string {
	t.Helper()
	defs := schema["definitions"].(map[string]any)
	d, ok := defs[def].(map[string]any)
	if !ok {
		t.Fatalf("schema definitions missing %q", def)
	}
	raw, ok := d["required"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		s, isStr := r.(string)
		if !isStr {
			t.Fatalf("schema %s.required has non-string entry %v", def, r)
		}
		out = append(out, s)
	}
	return out
}

func inList(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestWSSchemaDrift pins the TurnTerminalEvent <-> schema contract:
//
//  1. every json key the Go struct can emit is declared in the schema
//     definition with a compatible type, and
//  2. every non-omitempty struct key is in the schema's required list, and
//  3. a marshaled representative event structurally validates, and
//  4. the relay's flattened frame (type/source_topic/timestamp merged at
//     top level — the exact shape the Flutter client reads) still validates
//     against the same definition and classifies as agent_progress.
func TestWSSchemaDrift(t *testing.T) {
	raw, err := os.ReadFile(filepath.Clean(wsEventsSchemaPath))
	if err != nil {
		t.Fatalf("ws event schema missing/unreadable at %s: %v", wsEventsSchemaPath, err)
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}

	tags := goStructJSONTags(t)
	defs, _ := schema["definitions"].(map[string]any)
	turnDef, ok := defs["turn_terminal"].(map[string]any)
	if !ok {
		t.Fatalf("schema definitions missing turn_terminal")
	}
	turnProps := turnDef["properties"].(map[string]any)
	required := schemaRequired(t, schema, "turn_terminal")

	// (1)+(2) key-by-key agreement between the struct tags and the schema.
	for key, tag := range tags {
		decl, declared := turnProps[key].(map[string]any)
		if !declared {
			t.Errorf("TurnTerminalEvent json key %q not declared in schema turn_terminal — add it to schemas/ws_events.schema.json", key)
			continue
		}
		want := "string"
		if key == "duration_ms" {
			want = "integer"
		}
		if got, _ := decl["type"].(string); got != want && got != "" {
			t.Errorf("TurnTerminalEvent key %q: Go emits %s, schema declares %s", key, want, got)
		}
		hasOmit := strings.Contains(tag, "omitempty")
		if inList(required, key) && hasOmit {
			t.Errorf("schema requires %q but the Go tag is omitempty — make the contract match", key)
		}
	}
	for _, req := range required {
		if req == "type" || req == "source_topic" || req == "timestamp" {
			continue // relay-injected, not part of the payload struct
		}
		tag, ok := tags[req]
		if !ok {
			t.Errorf("schema turn_terminal requires %q but agent.TurnTerminalEvent has no such json key — schema drift", req)
			continue
		}
		if strings.Contains(tag, "omitempty") {
			t.Errorf("schema requires %q but the Go tag carries omitempty — contract mismatch", req)
		}
	}

	// (3) a representative marshaled struct validates.
	ev := agent.TurnTerminalEvent{
		ConversationID: "conv-drift-1",
		SessionID:      "session-drift-1",
		TurnID:         "turn-drift-1",
		TaskID:         "task-drift-1",
		IntentType:     "coding",
		AgentID:        "agent-1",
		HandlerCase:    "sync_dispatch",
		Status:         "completed",
		Reply:          "drift gate reply",
		DurationMS:     4321,
		ClassifiedBy:   "heuristic",
		Model:          "local/test-model",
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal TurnTerminalEvent: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(payload, &wire); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if err := validateAgainstDefinition(t, schema, "turn_terminal", wire, tags); err != nil {
		t.Fatalf("TurnTerminalEvent payload does not match schema: %v", err)
	}

	// (4) the flattened relay frame — what the Flutter client decodes.
	frame := transformBusEventToWS(&models.BusMessage{
		Topic:   agent.TopicTurnTerminal.Name,
		Payload: payload,
	})
	if frame == nil {
		t.Fatal("relay dropped the turn.terminal frame")
	}
	if frame["type"] != wsclass.WSProgress.String() {
		t.Fatalf("relay classified turn.terminal as %v, want agent_progress", frame["type"])
	}
	if err := validateAgainstDefinition(t, schema, "turn_terminal", frame, tags); err != nil {
		t.Fatalf("flattened relay frame does not match schema: %v", err)
	}

	// chat_message definition must require the keys the relay guarantees.
	chatDef, ok := defs["chat_message"].(map[string]any)
	if !ok {
		t.Fatalf("schema definitions missing chat_message")
	}
	chatProps := chatDef["properties"].(map[string]any)
	for _, key := range []string{"session_id", "role", "content", "type", "source_topic"} {
		if _, ok := chatProps[key]; !ok {
			t.Errorf("chat_message schema missing guaranteed key %q", key)
		}
	}
	for _, req := range []string{"session_id", "role", "content"} {
		if !inList(schemaRequired(t, schema, "chat_message"), req) {
			t.Errorf("chat_message schema must require %q (relay normalization guarantees it)", req)
		}
	}
}

// TestWSSchemaFixtureGolden pins the canonical fixture consumed by the Dart
// golden round-trip test (ui/flutter_ui/test/models/ws_events_test.dart via
// test/fixtures/turn_terminal_event.json). The fixture is the relay's
// flattened output — regenerated through the real relay so it stays
// wire-true. Refresh after a deliberate schema change:
//
//	go test ./internal/comm/http -run TestWSSchemaFixtureGolden -update
func TestWSSchemaFixtureGolden(t *testing.T) {
	ev := agent.TurnTerminalEvent{
		ConversationID: "conv-golden-1",
		SessionID:      "session-golden-1",
		TurnID:         "turn-golden-1",
		TaskID:         "task-golden-1",
		IntentType:     "coding",
		AgentID:        "agent-golden",
		HandlerCase:    "sync_dispatch",
		Status:         "completed",
		Reply:          "golden round-trip reply",
		DurationMS:     4321,
		ClassifiedBy:   "heuristic",
		Model:          "local/test-model",
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	frame := transformBusEventToWS(&models.BusMessage{
		Topic:   agent.TopicTurnTerminal.Name,
		Payload: payload,
	})
	if frame == nil {
		t.Fatal("relay dropped the frame")
	}
	fixture, err := json.MarshalIndent(frame, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	fixture = append(fixture, '\n')

	dest := filepath.Clean("../../../ui/flutter_ui/test/fixtures/turn_terminal_event.json")
	if *update {
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			t.Fatalf("mkdir fixture dir: %v", err)
		}
		if err := os.WriteFile(dest, fixture, 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		t.Logf("fixture updated: %s", dest)
		return
	}
	existing, err := os.ReadFile(dest)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("fixture not yet written; run: go test ./internal/comm/http -run TestWSSchemaFixtureGolden -update (%s)", dest)
		return
	}
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// Key-set + value comparison (timestamp is relay-injected wall time and
	// legitimately differs between refresh and check; the schema pins its
	// TYPE, checked below via validateAgainstDefinition).
	var want, got map[string]any
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatalf("fixture marshal produced invalid JSON: %v", err)
	}
	if err := json.Unmarshal(existing, &got); err != nil {
		t.Fatalf("existing fixture is not valid JSON: %v", err)
	}
	for k, v := range want {
		if k == "timestamp" {
			if _, ok := got[k].(string); !ok {
				t.Errorf("fixture timestamp: want RFC3339 string, got %T", got[k])
			}
			continue
		}
		if fmt.Sprint(got[k]) != fmt.Sprint(v) {
			t.Errorf("fixture drift at %q: golden=%v existing=%v — rerun with -update after a deliberate change", k, v, got[k])
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("fixture has key %q not present in the current Go payload — rerun with -update", k)
		}
	}
	// Pin the fixture's schema conformance so the Dart test can trust it.
	var schema map[string]any
	schemaRaw, err := os.ReadFile(filepath.Clean(wsEventsSchemaPath))
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	if err := json.Unmarshal(schemaRaw, &schema); err != nil {
		t.Fatalf("schema invalid: %v", err)
	}
	if err := validateAgainstDefinition(t, schema, "turn_terminal", got, goStructJSONTags(t)); err != nil {
		t.Fatalf("fixture does not satisfy schema: %v", err)
	}
}
