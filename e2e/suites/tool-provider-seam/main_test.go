//go:build e2e

// Package toolproviderseam is the hermetic live-gate suite for the
// tool-provider seam (docs/plans/20261031-tool-provider-seam/05-e2e-gate.md).
//
// A real meept-daemon boots over a scratch MEEPT_HOME (models-less rig:
// the fake LLM, classifier boot gate disarmed, remapped HTTP port) with
// the [tool_providers] config block and a live hermes gateway stub on a
// real loopback socket. The suite drives the LIVE surfaces:
//
//	platform_tools via a real chat turn  the live tool registry listing:
//	                                     a disabled provider's tools are
//	                                     absent, enabled provider tools
//	                                     are present (T3 + S1 listing)
//	scripted provider tool call          a provider-owned tool executes
//	                                     through the daemon end to end
//	tool_audit_chain.db                  the gate's G2 chain store, opened
//	                                     read-only from disk and queried
//	                                     for tool.execution records (S1)
//	meept doctor                         the provider report lines naming
//	                                     acp/github/hermes/scheduler with
//	                                     their enabled state (S3)
//
// The gate itself is armed at boot ("tool gate wired" INFO line asserted
// from the daemon log): components.go arms NewExecGate over the live
// security engine and calls ToolRegistry.SetGate BEFORE
// registerBuiltinTools.
//
// The e2e/suites/zz-cigate-sabotage/ directory is a sibling's CI-witness
// fixture: it is NEVER referenced, registered, swept, or deleted here.
package toolproviderseam

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	_ "modernc.org/sqlite"
)

const (
	hermesE2EKey = "toolprov-seam-e2e-key"
	hermesKeyEnv = "MEEPT_E2E_TOOLPROV_HERMES_KEY"
	// schedulerTools is the full scheduler-provider group (all eight
	// registry names the provider contributes).
	schedulerTools = "schedule_create schedule_list schedule_get schedule_delete schedule_pause schedule_resume schedule_run_now cron_create"
	// schedulerSpecialistTools is the subset the bundled scheduler
	// specialist's additional_tools grant (config/agents/scheduler).
	schedulerSpecialistTools = "schedule_create schedule_list schedule_delete"
)

// hermesGateway is a minimal hermes gateway stub speaking the wire shapes
// the internal/hermes client parses (same family as the hermes-client
// suite's live-wire stub, reduced to the health/capabilities/session/chat
// surface the scripted tool call needs). It records the bearer key and
// every chat input it received so the suite can prove the call really
// crossed the loopback socket.
type hermesGateway struct {
	srv      *httptest.Server
	minted   []string
	chatSay  []string
	bearerOK bool
}

func newHermesGateway(t *testing.T) *hermesGateway {
	t.Helper()
	g := &hermesGateway{}
	mux := http.NewServeMux()

	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer "+hermesE2EKey {
			g.bearerOK = false
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"error":{"message":"unauthorized"}}`)
			return false
		}
		g.bearerOK = true
		return true
	}

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok","platform":"hermes-agent"}`)
	})
	mux.HandleFunc("/v1/capabilities", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"object":"hermes.api_server.capabilities","platform":"hermes-agent","features":{"session_chat":true,"session_model_lock":true},"endpoints":{}}`)
	})
	mux.HandleFunc("/api/sessions", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		id, _ := body["id"].(string)
		if id == "" {
			id = fmt.Sprintf("api_%d", time.Now().UnixNano())
		}
		g.minted = append(g.minted, id)
		row := map[string]any{"id": id, "source": "api_server", "started_at": float64(time.Now().Unix())}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"object":"hermes.session","session":%s}`, jsonOf(row))
	})
	mux.HandleFunc("/api/sessions/", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
		if !strings.HasSuffix(rest, "/chat") {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"message":"not found"}}`)
			return
		}
		id := strings.TrimSuffix(rest, "/chat")
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		in, _ := body["input"].(string)
		g.chatSay = append(g.chatSay, in)
		reply := "SEAM: " + in
		fmt.Fprintf(w, `{"object":"hermes.session.chat.completion","session_id":%s,"message":{"role":"assistant","content":%s},"usage":{"input_tokens":1,"output_tokens":1}}`,
			jsonOf(id), jsonOf(reply))
	})

	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// bootOpts assembles the common scratch arming: models-less classifier
// gate disarmed, hermes interop enabled against the stub gateway, and
// the hermes key injected via the env-var indirection the config
// requires. extra carries per-scenario config overlays (later keys win).
func bootOpts(t *testing.T, gw *hermesGateway, extra map[string]any) []harness.StartOption {
	overlay := map[string]any{
		"orchestrator.classifier_boot_fail_fast": false,
		"hermes.enabled":                         true,
		"hermes.base_url":                        gw.srv.URL,
		"hermes.api_key_env":                     hermesKeyEnv,
		"hermes.timeout_seconds":                 10,
	}
	for k, v := range extra {
		overlay[k] = v
	}
	return []harness.StartOption{
		harness.WithConfigOverlay(overlay),
		harness.WithExtraEnv(map[string]string{hermesKeyEnv: hermesE2EKey}),
	}
}

// bootWithGate brings the stack up and asserts the boot log proves the
// exec gate armed over the live engine ("tool gate wired") - the wiring
// claim this suite exists to verify live.
func bootWithGate(t *testing.T, opts []harness.StartOption) *harness.Stack {
	t.Helper()
	s := harness.Start(t, opts...)
	waitForLogLine(t, s, "tool gate wired", "gate arming")
	return s
}

// waitForLogLine polls the daemon log for a line (bounded; no fixed sleep).
func waitForLogLine(t *testing.T, s *harness.Stack, substr, what string) {
	t.Helper()
	harness.WaitFor(t, 30*time.Second, what+" line "+substr, func() bool {
		data, err := os.ReadFile(filepath.Join(s.Work, "daemon.log"))
		if err != nil {
			return false
		}
		return strings.Contains(string(data), substr)
	})
}

// pinCodeIntent pins the fake classifier to intent=code so the turn
// routes to the seeded aastub specialist.
func pinCodeIntent(s *harness.Stack) {
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned code"}`)
}

// toolResultTexts collects the content of every role=tool message the
// daemon sent back to the fake LLM (the observable end state of tool calls).
func toolResultTexts(f *harness.FakeLLM) []string {
	var out []string
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if role, _ := msg["role"].(string); role == "tool" {
				if c, _ := msg["content"].(string); c != "" {
					out = append(out, c)
				}
			}
		}
	}
	return out
}

// toolResultsJoined is every tool result joined for substring assertions.
func toolResultsJoined(f *harness.FakeLLM) string {
	return strings.Join(toolResultTexts(f), "\n")
}

// auditChainRecords queries the gate's tool_audit_chain.db READ-ONLY from
// disk and returns (type, payload-json) pairs for tool.execution records.
// The chain store exists from boot (components.go opens it before arming
// the gate), so a missing FILE is a hard failure while an empty table is
// a legit empty result the caller decides on.
func auditChainRecords(t *testing.T, chainPath string) []auditRow {
	t.Helper()
	if _, err := os.Stat(chainPath); err != nil {
		t.Fatalf("tool audit chain missing at %s: %v (gate G2 store must exist from boot)", chainPath, err)
	}
	db, err := sql.Open("sqlite", "file:"+chainPath+"?mode=ro")
	if err != nil {
		t.Fatalf("open tool audit chain read-only: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT seq, type, payload FROM audit_log_chain WHERE type = 'tool.execution' ORDER BY seq`)
	if err != nil {
		t.Fatalf("query tool audit chain: %v", err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.Seq, &r.Type, &r.Payload); err != nil {
			t.Fatalf("scan audit chain row: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit chain rows: %v", err)
	}
	return out
}

type auditRow struct {
	Seq     int
	Type    string
	Payload string
}

// ToolAuditChainPath is the gate's chain store under the sandbox state dir.
func ToolAuditChainPath(s *harness.Stack) string {
	return filepath.Join(s.StateDir, "tool_audit_chain.db")
}
