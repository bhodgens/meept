//go:build e2e

// Suite github-intake: the hermetic github-intake-factory tree — the
// ticket -> goal -> write-back loop driven through the REAL scratch
// daemon against a fake GitHub HTTP server (httptest), so nothing ever
// dials live github.com. The [github] block's base_url override and the
// [intake] block's watch_repo make the loop hermetic; the token file
// pattern is copied from the github-pr-ci suite.
//
// GATE POSTURE (leaf 00T): these scenarios are SKELETONS + ASSERTIONS.
// The [intake] config block, the internal/intake package, and the daemon
// wiring do not exist yet — they land in leaves 01-05. Until then the
// config overlay keys (intake.*) are inert (unknown keys are ignored by
// the daemon) and the poller never exists, so the create scenario's
// goal-existence assertion fails RED. That is the pinned gate: leaf 05's
// wiring makes them pass without touching a single assertion here.
//
// Coverage map (manifest scenarios github-intake-01..05):
//
//	github-intake-01  intake-create-wait: daemon with [intake] enabled
//	                  pointed at the fake; a labeled issue exists; the
//	                  poll fires; the goal exists with source=trigger
//	                  and the fake saw the issues GET
//	                                                    — TestE2EIntakeCreateWait
//	github-intake-02  intake-dedup: a second poll cycle with an unchanged
//	                  issue leaves the goal count unchanged
//	                                                    — TestE2EIntakeDedup
//	github-intake-03  intake-redispatch: the issue body changes between
//	                  polls -> the goal mandate is updated, still exactly
//	                  one goal              — TestE2EIntakeRedispatch
//	github-intake-04  intake-writeback: a healthy goal -> the fake sees
//	                  a comments POST + a labels POST; a second cycle
//	                  adds no second comments POST
//	                                                    — TestE2EIntakeWriteBack
//	github-intake-05  intake-disabled: no [intake] block -> no poll
//	                  activity (fake counter stays at zero)
//	                                                    — TestE2EIntakeDisabled
//
// Run via `make e2e-fast-area AREA=github-intake`.
package githubintake

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"

	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)
)

// ---------------------------------------------------------------------------
// Fake GitHub: issues list + comments + labels, with request counters.
// ---------------------------------------------------------------------------

// intakeFakeGitHub counts every request the daemon's intake loop makes so
// the scenarios can assert both "the poll happened" and "no storm".
type intakeFakeGitHub struct {
	srv *httptest.Server

	mu            sync.Mutex
	issuesGets    int            // GET /repos/o/r/issues (poll cycles)
	issueBodies   map[int]string // issue number -> current body
	commentsPosts int            // POST .../issues/42/comments
	labelsPosts   int            // POST .../issues/42/labels
}

func newIntakeFakeGitHub(t *testing.T) *intakeFakeGitHub {
	f := &intakeFakeGitHub{
		issueBodies: map[int]string{
			// One labeled issue the factory should pick up.
			42: "build the warp drive",
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/octo/forge/issues", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.issuesGets++
		type ghLabel struct {
			Name string `json:"name"`
		}
		type ghIssue struct {
			Number  int       `json:"number"`
			Title   string    `json:"title"`
			Body    string    `json:"body"`
			Labels  []ghLabel `json:"labels"`
			HTMLURL string    `json:"html_url"`
			User    struct {
				Login string `json:"login"`
			} `json:"user"`
			UpdatedAt time.Time `json:"updated_at"`
		}
		now := time.Now().UTC()
		out := []ghIssue{{
			Number:  42,
			Title:   "warp drive ticket",
			Body:    f.issueBodies[42],
			Labels:  []ghLabel{{Name: "factory"}},
			HTMLURL: "https://github.com/octo/forge/issues/42",
			User: struct {
				Login string `json:"login"`
			}{Login: "octo"},
			// UpdatedAt deliberately drifts forward on every GET so a
			// watermark keyed on it re-surfaces the issue for re-dispatch
			// scenarios (they set f.issueBodies to detect the new body).
			UpdatedAt: now,
		}}
		payload, _ := json.Marshal(out)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	})
	mux.HandleFunc("/repos/octo/forge/issues/42/comments", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.commentsPosts++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":9001,"html_url":"https://github.com/octo/forge/issues/42#issuecomment-9001"}`))
	})
	mux.HandleFunc("/repos/octo/forge/issues/42/labels", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.labelsPosts++
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"name":"factory:done"}]`))
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *intakeFakeGitHub) URL() string { return f.srv.URL }

func (f *intakeFakeGitHub) issuesGETs() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.issuesGets
}

func (f *intakeFakeGitHub) counts() (comments, labels int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.commentsPosts, f.labelsPosts
}

// setIssueBody swaps the served body (the re-dispatch trigger).
func (f *intakeFakeGitHub) setIssueBody(body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issueBodies[42] = body
}

// ---------------------------------------------------------------------------
// Harness helpers (same shapes as the github-pr-ci suite).
// ---------------------------------------------------------------------------

// writeIntakeTokenFile writes the test token into its own dir (NOT
// t.TempDir() itself: the harness sandbox outlives individual TempDirs is
// not true — t.TempDir is fine here since the daemon reads it during the
// test) and returns its absolute path for [github].token_secret.
func writeIntakeTokenFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "github_token")
	if err := os.WriteFile(p, []byte("ghp_intake_test_token"), 0o600); err != nil {
		t.Fatalf("write github token file: %v", err)
	}
	return p
}

// intakeConfigOverlay points [github] at the fake and turns [intake] on
// with a short poll interval (well inside the suite budget).
func intakeConfigOverlay(baseURL, tokenPath string) harness.StartOption {
	return harness.WithConfigOverlay(map[string]any{
		"github.enabled":          true,
		"github.base_url":         baseURL,
		"github.token_secret":     tokenPath,
		"intake.enabled":          true,
		"intake.default_employee": "intake-emp",
		"intake.poll_interval":    "2s",
		"intake.watch_repo":       "octo/forge",
		"intake.required_labels":  []any{"factory"},
		"intake.done_label":       "factory:done",
	})
}

// rpcCall performs one JSON-RPC request over the scratch daemon's Unix
// socket (length-prefixed framing — same wire shape as the
// goal-completion suite's helper).
func rpcCall(t *testing.T, s *harness.Stack, method string, params any) (json.RawMessage, error) {
	t.Helper()
	conn, err := net.DialTimeout("unix", s.SocketPath, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	req := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		req["params"] = params
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
		return nil, err
	}
	if _, err := fmt.Fprintf(conn, "%d\n", len(payload)); err != nil {
		return nil, err
	}
	if _, err := conn.Write(payload); err != nil {
		return nil, err
	}

	buf := make([]byte, 4096)
	// Frame: "<length>\n<payload>".
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	head := string(buf[:n])
	lineEnd := strings.IndexByte(head, '\n')
	if lineEnd < 0 {
		return nil, fmt.Errorf("malformed rpc frame: %q", head)
	}
	length, err := strconv.Atoi(strings.TrimSpace(head[:lineEnd]))
	if err != nil {
		return nil, fmt.Errorf("bad rpc frame length: %w", err)
	}
	acc := []byte(head[lineEnd+1:])
	for len(acc) < length {
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		acc = append(acc, buf[:n]...)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(acc[:length], &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc %s: %s", method, resp.Error.Message)
	}
	return resp.Result, nil
}

// listIntakeGoals fetches agents.goals.list for the intake employee and
// returns the goals array.
func listIntakeGoals(t *testing.T, s *harness.Stack, employeeID string) []map[string]any {
	t.Helper()
	raw, err := rpcCall(t, s, "agents.goals.list", map[string]any{"employee_id": employeeID})
	if err != nil {
		t.Fatalf("agents.goals.list: %v", err)
	}
	var resp struct {
		Goals []map[string]any `json:"goals"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("decode goals.list: %v (%s)", err, string(raw))
	}
	return resp.Goals
}

// waitFor polls cond until it holds or the budget lapses.
func waitFor(t *testing.T, budget time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", budget, what)
}

// ---------------------------------------------------------------------------
// github-intake-01: create-wait
// ---------------------------------------------------------------------------

// TestE2EIntakeCreateWait: the daemon boots with [intake] enabled pointed
// at the fake; the labeled issue 42 exists; within the budget the poll
// fires (the fake's issues GET counter moves) and agents.goals.list shows
// a goal with source=trigger whose trigger_ref is
// `intake:octo/forge#42` (F2/F7; manifest github-intake-01).
//
// RED posture: the [intake] block is inert until leaf 04/05, so the goal
// never appears — the goal assertion failing IS the pinned gate.
func TestE2EIntakeCreateWait(t *testing.T) {
	fg := newIntakeFakeGitHub(t)
	tokenPath := writeIntakeTokenFile(t)
	s := harness.Start(t, intakeConfigOverlay(fg.URL(), tokenPath))
	s.RegisterProject(t, "intake-project")

	waitFor(t, 30*time.Second, "the intake poll to reach the fake", func() bool {
		return fg.issuesGETs() > 0
	})

	var goal map[string]any
	waitFor(t, 30*time.Second, "the intake goal to be created", func() bool {
		goals := listIntakeGoals(t, s, "intake-emp")
		for _, g := range goals {
			if ref, _ := g["trigger_ref"].(string); ref == "intake:octo/forge#42" {
				goal = g
				return true
			}
		}
		return false
	})

	if src, _ := goal["source"].(string); !strings.Contains(src, "trigger") {
		t.Errorf("intake goal source = %v, want trigger", goal["source"])
	}
}

// ---------------------------------------------------------------------------
// github-intake-02: dedup
// ---------------------------------------------------------------------------

// TestE2EIntakeDedup: after the create, a second poll cycle with an
// unchanged issue leaves the goal count unchanged and makes no second
// goal (F3; manifest github-intake-02).
func TestE2EIntakeDedup(t *testing.T) {
	fg := newIntakeFakeGitHub(t)
	tokenPath := writeIntakeTokenFile(t)
	s := harness.Start(t, intakeConfigOverlay(fg.URL(), tokenPath))
	s.RegisterProject(t, "intake-project")

	waitFor(t, 30*time.Second, "the intake goal to be created", func() bool {
		return len(intakeGoalsByRef(t, s, "intake:octo/forge#42")) == 1
	})

	// Let at least one more poll cycle run against the unchanged issue.
	first := fg.issuesGETs()
	waitFor(t, 30*time.Second, "a second poll cycle", func() bool {
		return fg.issuesGETs() > first
	})
	time.Sleep(500 * time.Millisecond)

	goals := intakeGoalsByRef(t, s, "intake:octo/forge#42")
	if len(goals) != 1 {
		t.Fatalf("goal count for intake:octo/forge#42 = %d after re-poll, want exactly 1 (dedup)", len(goals))
	}
}

func intakeGoalsByRef(t *testing.T, s *harness.Stack, ref string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, g := range listIntakeGoals(t, s, "intake-emp") {
		if r, _ := g["trigger_ref"].(string); r == ref {
			out = append(out, g)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// github-intake-03: re-dispatch
// ---------------------------------------------------------------------------

// TestE2EIntakeRedispatch: the issue body changes between polls -> the
// goal's mandate is updated and there is still exactly ONE goal (F4;
// manifest github-intake-03).
func TestE2EIntakeRedispatch(t *testing.T) {
	fg := newIntakeFakeGitHub(t)
	tokenPath := writeIntakeTokenFile(t)
	s := harness.Start(t, intakeConfigOverlay(fg.URL(), tokenPath))
	s.RegisterProject(t, "intake-project")

	waitFor(t, 30*time.Second, "the intake goal to be created", func() bool {
		return len(intakeGoalsByRef(t, s, "intake:octo/forge#42")) == 1
	})

	fg.setIssueBody("REWRITTEN: build the warp drive v2, with plutonium")

	var mandate string
	waitFor(t, 45*time.Second, "the goal mandate to be rewritten", func() bool {
		goals := intakeGoalsByRef(t, s, "intake:octo/forge#42")
		if len(goals) != 1 {
			return false
		}
		mandate, _ = goals[0]["mandate"].(string)
		return strings.Contains(mandate, "v2")
	})

	if total := len(intakeGoalsByRef(t, s, "intake:octo/forge#42")); total != 1 {
		t.Fatalf("goal count after re-dispatch = %d, want 1 (in-place update)", total)
	}
}

// ---------------------------------------------------------------------------
// github-intake-04: write-back
// ---------------------------------------------------------------------------

// TestE2EIntakeWriteBack: a goal reaching healthy -> the fake sees a
// comments POST + a labels POST; a second cycle adds NO second comments
// POST (F5; manifest github-intake-04). The goal is driven healthy via
// the goal-health assess RPC the write-back subscribes to; until leaf 05
// lands the loop, the fake counters stay at zero and this REDs.
func TestE2EIntakeWriteBack(t *testing.T) {
	fg := newIntakeFakeGitHub(t)
	tokenPath := writeIntakeTokenFile(t)
	s := harness.Start(t, intakeConfigOverlay(fg.URL(), tokenPath))
	s.RegisterProject(t, "intake-project")

	waitFor(t, 30*time.Second, "the intake goal to be created", func() bool {
		return len(intakeGoalsByRef(t, s, "intake:octo/forge#42")) == 1
	})

	// Drive the goal healthy: the write-back path subscribes to
	// GoalHealth.GoalHealthy set by goal-loop reflect. Drive it through
	// the real assess surface via a steering trigger; the scenario only
	// asserts the EXTERNAL effect (comment + label + no storm).
	goal := intakeGoalsByRef(t, s, "intake:octo/forge#42")[0]
	gid, _ := goal["id"].(string)
	if gid == "" {
		t.Fatalf("intake goal has no id: %v", goal)
	}
	if _, err := rpcCall(t, s, "agents.goals.assess", map[string]any{
		"goal_id": gid,
		"health":  "healthy",
	}); err != nil {
		// The assess RPC surface is leaf-05-owned; if absent, the write-back
		// loop cannot be driven — RED for the right reason.
		t.Fatalf("agents.goals.assess unavailable (wiring leaf pending?): %v", err)
	}

	waitFor(t, 30*time.Second, "the outcome comment POST", func() bool {
		comments, _ := fg.counts()
		return comments >= 1
	})
	comments, labels := fg.counts()
	if comments != 1 {
		t.Fatalf("comments POSTs = %d, want exactly 1", comments)
	}
	if labels != 1 {
		t.Fatalf("labels POSTs = %d, want exactly 1 (factory:done on healthy)", labels)
	}

	// A second healthy cycle must not storm: the effects key
	// (goalID, state) collides and no second comment lands.
	if _, err := rpcCall(t, s, "agents.goals.assess", map[string]any{
		"goal_id": gid,
		"health":  "healthy",
	}); err != nil {
		t.Fatalf("second assess: %v", err)
	}
	time.Sleep(2 * time.Second)
	if comments, _ := fg.counts(); comments != 1 {
		t.Fatalf("comments POSTs after repeat healthy = %d, want 1 (no storm)", comments)
	}
}

// ---------------------------------------------------------------------------
// github-intake-05: disabled by default
// ---------------------------------------------------------------------------

// TestE2EIntakeDisabled: with NO [intake] block, no poll activity ever
// reaches the fake — the issues GET counter stays at zero (F7; manifest
// github-intake-05). Ships-green today: the block is inert by default.
func TestE2EIntakeDisabled(t *testing.T) {
	fg := newIntakeFakeGitHub(t)
	tokenPath := writeIntakeTokenFile(t)
	// [github] enabled (tools registered) but NO [intake] block at all.
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"github.enabled":      true,
		"github.base_url":     fg.URL(),
		"github.token_secret": tokenPath,
	}))
	s.RegisterProject(t, "intake-project")

	// Give any (wrongly) scheduled poll ample chances to fire.
	time.Sleep(3 * time.Second)
	if got := fg.issuesGETs(); got != 0 {
		t.Fatalf("fake saw %d issues GETs with [intake] absent, want 0 (disabled default)", got)
	}
}

// keepImports pins helpers used only by the write-back scenario once
// leaf 05 lands (compile-time references, no runtime effect).
var _ = context.Background
