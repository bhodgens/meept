package agent

// 04-clutter TEST-GATE T5 (F11b): trivial fast lane pin.
//
// Frozen defect: F-ORCH-5 (MAJOR, live-verified). "What is 2+2? Answer
// briefly." takes the FULL task machinery: intent=quickplan conf 0.8575 ->
// orchestrator task -> planner loop -> reply guard fires TWICE on planner
// JSON -> repair retry -> step job -> review -> task.completed relay. The
// answer "4" is buried in job-ID prose; 5s wall and ~15k tokens for a
// one-token answer; the TUI double-renders card + block (probes/
// tui-2plus2.json).
//
// Contract (02-trivial-lane-dedupe T1, Q2 ratified default-on): a
// config-gated trivial-question fast lane - orchestrator.trivial_fast_lane
// (default true) routes simple interrogatives straight to the chat lane:
// no task row, one brief reply. Artifact verbs still task-route; flag off
// = byte-identical legacy; the lane never bypasses security sanitization
// or session binding (it rides the SAME classify -> RouteToAgent chain).
//
// RED today: the fixture classifies quickplan and creates a task. GREEN
// after 04-clutter/02 (config field + dispatcher fast lane).

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// trivialFixture is the VERBATIM user input from the frozen rig probe
// tui-2plus2.json (the double-rendered 2+2 incident).
const trivialFixture = "What is 2+2? Answer briefly."

// trivialClassifierServer answers the analyzer, multi-intent detector, and
// single-intent classifier. The classifier verdict mirrors the live rig:
// quickplan @0.8575 for the trivial fixture (the over-classification the
// fast lane must pre-empt); code for artifact requests.
func trivialClassifierServer(t *testing.T) *llm.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []llm.ChatMessage `json:"messages"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("classifier request decode: %v", err)
		}
		var prompt strings.Builder
		for _, m := range req.Messages {
			prompt.WriteString(m.Content)
			prompt.WriteString("\n")
		}
		var content string
		// The classifier prompt embeds the user input; detect the artifact
		// fixture by its distinctive artifact name (the prompt scaffolding
		// itself contains "create a file" examples, so a bare verb match
		// would misroute the trivial fixture too).
		isArtifact := strings.Contains(prompt.String(), "notes.txt")
		switch {
		case strings.Contains(prompt.String(), "identify ALL distinct intents"):
			content = `[]`
		case strings.Contains(prompt.String(), "Classify this user input"):
			if isArtifact {
				content = `{"intent":"code","confidence":0.92,"reasoning":"artifact work"}`
			} else {
				// The live over-classification: a trivial arithmetic
				// question scored quickplan @0.8575.
				content = `{"intent":"quickplan","confidence":0.8575,"reasoning":"direct execution"}`
			}
		default:
			if isArtifact {
				content = `{"goal":"create notes.txt","ambiguity":0.1,"scope":"narrow","category":"code","suggested_questions":[],"confidence":0.9}`
			} else {
				content = `{"goal":"answer the question","ambiguity":0.1,"scope":"narrow","category":"quickplan","suggested_questions":[],"confidence":0.9}`
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":` +
			strconv.Quote(content) + `}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
	}))
	t.Cleanup(srv.Close)
	return llm.NewClient(&llm.ModelConfig{BaseURL: srv.URL, ModelID: "trivial-classifier"})
}

// newTrivialLaneDispatcher wires the production classify chain with a real
// task store so task-row creation is observable.
func newTrivialLaneDispatcher(t *testing.T) (*Dispatcher, *task.Store) {
	t.Helper()
	logger := slogDiscardLogger()
	reg, err := task.NewRegistry(filepath.Join(t.TempDir(), "tasks.db"), bus.New(nil, nil), logger)
	if err != nil {
		t.Fatalf("task registry: %v", err)
	}
	t.Cleanup(func() { reg.Close() })
	d := NewDispatcher(DispatcherConfig{
		Registry:         NewAgentRegistry(RegistryConfig{Logger: logger}),
		TaskStore:        reg.Store(),
		TaskRegistry:     reg,
		ClassifierClient: trivialClassifierServer(t),
		Logger:           logger,
	})
	return d, reg.Store()
}

// trivialFastLaneConfigField returns the config field through reflection so
// the gate compiles before 02 lands it. ok=false means the field is absent.
func trivialFastLaneConfigField(t *testing.T) (value bool, ok bool) {
	t.Helper()
	field := reflect.ValueOf(config.OrchestratorConfig{}).FieldByName("TrivialFastLane")
	if !field.IsValid() || field.Kind() != reflect.Bool {
		return false, false
	}
	return field.Bool(), true
}

// TestTrivialFastLane pins the config-gated direct chat lane for trivial
// questions. RED today: quickplan + task row for "What is 2+2?".
func TestTrivialFastLane(t *testing.T) {
	t.Run("config_surface_exists_default_on", func(t *testing.T) {
		// Q2 ratified: orchestrator.trivial_fast_lane ships default-on.
		// The zero-value struct check pins the FIELD's existence; the
		// default-on check pins DefaultConfig's wiring.
		if _, ok := trivialFastLaneConfigField(t); !ok {
			t.Fatal("config.OrchestratorConfig has no TrivialFastLane bool field " +
				"(orchestrator.trivial_fast_lane, owned by 04-clutter/02 T1)")
		}
		cfg := config.DefaultConfig()
		field := reflect.ValueOf(cfg.Orchestrator).FieldByName("TrivialFastLane")
		if !field.IsValid() || !field.Bool() {
			t.Error("orchestrator.trivial_fast_lane must default ON (Q2 ratified at dispatch)")
		}
	})

	t.Run("trivial_question_routes_direct_chat_no_task", func(t *testing.T) {
		d, store := newTrivialLaneDispatcher(t)
		res, err := d.ClassifyAndRoute(context.Background(), trivialFixture, "sess-trivial-t5", nil, "", "")
		if err != nil {
			t.Fatalf("ClassifyAndRoute: %v", err)
		}
		if res.Intent == nil {
			t.Fatal("no intent returned")
		}
		// RED today: quickplan @0.8575 (the frozen over-classification).
		if res.Intent.Type != string(IntentChat) {
			t.Fatalf("F-ORCH-5: %q classified %q (conf %.4f), want the direct chat lane - "+
				"a trivial question must not take the task machinery",
				trivialFixture, res.Intent.Type, res.Intent.Confidence)
		}
		if res.Task != nil {
			t.Errorf("trivial question created task %s - the fast lane creates no task row", res.Task.ID)
		}
		if d.ShouldDispatchAsync(res) {
			t.Error("trivial question must dispatch synchronously (one brief reply, no orchestrator)")
		}
		tasks, err := store.GetTasksForSession("sess-trivial-t5")
		if err != nil {
			t.Fatalf("list session tasks: %v", err)
		}
		if len(tasks) != 0 {
			t.Errorf("trivial question left %d task rows for the session", len(tasks))
		}
	})

	t.Run("artifact_verbs_still_task_route", func(t *testing.T) {
		// Regression guard (GREEN today, must survive the fast lane): a
		// concrete artifact request keeps the task machinery. The fast
		// lane must not over-reach into real work.
		d, _ := newTrivialLaneDispatcher(t)
		const artifactInput = "create a file named notes.txt containing the meeting summary"
		res, err := d.ClassifyAndRoute(context.Background(), artifactInput, "sess-trivial-t5-artifact", nil, "", "")
		if err != nil {
			t.Fatalf("ClassifyAndRoute: %v", err)
		}
		if res.Intent == nil || res.Intent.Type == string(IntentChat) {
			t.Fatalf("artifact request deflected to chat: %+v - artifact verbs must task-route", res.Intent)
		}
		if !d.ShouldDispatchAsync(res) || res.Task == nil {
			t.Errorf("artifact request lost its async task dispatch: async=%v task=%v", d.ShouldDispatchAsync(res), res.Task)
		}
	})

	t.Run("flag_off_restores_legacy_classification", func(t *testing.T) {
		// Config off = legacy: the trivial fixture takes the quickplan
		// lane exactly as the frozen rig recorded. The seam is asserted
		// through reflection until 02 lands it (DispatcherConfig field
		// mirroring orchestrator.trivial_fast_lane, the SessionStateUpgrade
		// precedent); RED until the seam exists AND disables the lane.
		d, _ := newTrivialLaneDispatcher(t)
		cfgValue := reflect.ValueOf(d).Elem().FieldByName("trivialFastLane")
		if !cfgValue.IsValid() {
			// Also accept an exported setter seam.
			setter := reflect.ValueOf(d).MethodByName("SetTrivialFastLane")
			if !setter.IsValid() {
				t.Fatal("no trivial-fast-lane config seam on Dispatcher " +
					"(want a DispatcherConfig mirror of orchestrator.trivial_fast_lane or a setter; " +
					"flag off must restore the legacy quickplan classification)")
			}
			setter.Call([]reflect.Value{reflect.ValueOf(false)})
		} else {
			cfgValue.SetBool(false)
		}

		res, err := d.ClassifyAndRoute(context.Background(), trivialFixture, "sess-trivial-t5-off", nil, "", "")
		if err != nil {
			t.Fatalf("ClassifyAndRoute: %v", err)
		}
		if res.Intent == nil || res.Intent.Type != string(IntentQuickPlan) {
			got := ""
			if res.Intent != nil {
				got = res.Intent.Type
			}
			t.Errorf("flag off: classification = %q, want legacy quickplan (byte-identical legacy behavior)", got)
		}
	})

	t.Run("fast_lane_keeps_session_binding_and_sanitization", func(t *testing.T) {
		// The fast lane is a CLASSIFICATION shortcut, not a bypass: the
		// reply still runs through RouteToAgent's session-scoped loop
		// (working dir + security fence). Pin that a fast-laned turn
		// keeps the chat-agent destination whose loop resolution binds
		// the session's project dir - never an unscoped executor.
		d, _ := newTrivialLaneDispatcher(t)
		res, err := d.ClassifyAndRoute(context.Background(), trivialFixture, "sess-trivial-t5-bind", nil, "", "")
		if err != nil {
			t.Fatalf("ClassifyAndRoute: %v", err)
		}
		if res.Intent == nil {
			t.Fatal("no intent returned")
		}
		if res.Intent.Type != string(IntentChat) {
			t.Fatalf("fast lane not active: %q classified %q - the session-binding pin rides the chat lane",
				trivialFixture, res.Intent.Type)
		}
		if res.Intent.AgentType != config.AgentIDChat {
			t.Errorf("fast-laned intent routes to agent %q, want the chat agent (session-bound loop resolution)", res.Intent.AgentType)
		}
	})
}
