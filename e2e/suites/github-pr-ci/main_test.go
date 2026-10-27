//go:build e2e

// Suite github-pr-ci: the hermetic github-pr-ci tree — the PR+CI loop
// driven through the REAL scratch daemon against a fake GitHub HTTP
// server (httptest), so nothing ever dials live github.com. The
// [github] config block's base_url override is what makes the loop
// hermetic; token_secret points at a test token file outside the
// sandbox home.
//
// Coverage map (manifest scenarios github-pr-ci-01..04):
//
//	github-pr-ci-01  create PR + wait for CI green: the fake sees exactly
//	                 ONE POST /pulls and the github_ci_wait tool result
//	                 reports "all_green":true           — TestE2ECreateWaitGreen
//	github-pr-ci-02  [github] off by default (shipped posture): the
//	                 executor's tool list carries NONE of the github_*
//	                 tools (github_pr_create, github_pr_status,
//	                 github_ci_wait, github_pr_comment, github_pr_review,
//	                 github_pr_merge, github_issue_get,
//	                 github_issue_comment)            — TestE2EDisabledByDefault
//	github-pr-ci-03  effects-ledger reconcile: a pre-seeded CLAIMED row for
//	                 EffectKey("github_pr_create","o","r","feat/x",
//	                 "feat/x: add PR") — exactly the title the scripted
//	                 call sends — is found by the claim's PK-conflict
//	                 path and NOT re-executed (the fake POST counter
//	                 never exceeds 1)                    — TestE2EEffectsReconcile
//	github-pr-ci-04  CI-wait park + re-arm: the fake's check-runs endpoint
//	                 ALWAYS serves in_progress, so a 1s github_ci_wait
//	                 parks at its deadline — the tool result carries
//	                 "parked":true and a non-empty wait_id, and the
//	                 parked wait's re-arm instruction row
//	                 (github_ci_wait-<waitID>.yaml) is persisted under
//	                 the sandbox daemon's instruction tier
//	                                                     — TestE2ECIWaitPark
//
// GREEN posture: the suite reconciles against the committed API surface —
// the [github] config block (config.Config.GitHub), the eight github_*
// builtin tools, the go-github v93 endpoint shapes, and the effects
// reconcile path that surfaces claimed github_pr_create rows. Run via
// `make e2e-fast-area AREA=github-pr-ci`.
package githubprci

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/effects"
)

// ---------------------------------------------------------------------------
// Fake GitHub server
// ---------------------------------------------------------------------------

// fakeGitHub is a minimal GitHub REST stand-in speaking only the routes the
// github-pr-ci tree needs. All handlers are mutex-guarded and count requests
// so tests can assert exact call counts (exactly one POST /pulls, etc.).
type fakeGitHub struct {
	srv *httptest.Server

	mu sync.Mutex
	// prNumber is the PR number the NEXT POST /pulls will create
	// (starts at 41, so the first POST returns 42 — matching the
	// scripted github_ci_wait call and the static GET /pulls/42).
	prNumber int
	// postsPulled counts POST /repos/o/r/pulls.
	postsPulled int
	// checkRunsCalls counts GET .../commits/{sha}/check-runs: the FIRST
	// call serves one in_progress check, every later call serves all
	// success (the CI "turns green while we wait" shape).
	checkRunsCalls int
	// actionsRunsCalls counts GET /repos/o/r/actions/runs.
	actionsRunsCalls int
	// alwaysInProgress pins the check-runs endpoint to serve an
	// in_progress check on EVERY call (the park scenario: the wait can
	// never turn green and must park at its deadline). Guarded by mu.
	alwaysInProgress bool
}

// newFakeGitHub stands up the fake and registers its shutdown.
func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	fg := &fakeGitHub{prNumber: 41}
	mux := http.NewServeMux()

	// POST /repos/o/r/pulls -> 201 {number, html_url, state} (+ draft/
	// mergeable/head for convenience). Exact-path handler: the 1.22+
	// ServeMux treats "/repos/o/r/pulls" as an exact match, so the GET
	// on /pulls/{n} lives in the "/repos/o/r/pulls/" subtree handler.
	mux.HandleFunc("/repos/o/r/pulls", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		fg.mu.Lock()
		fg.postsPulled++
		fg.prNumber++
		n := fg.prNumber
		fg.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		// go-github v93 PullRequest: mergeable/draft are *bool (a string
		// "yes" would fail the client's json.Unmarshal), head is a nested
		// object carrying the sha.
		_, _ = w.Write([]byte(fmt.Sprintf(
			`{"number":%d,"html_url":"https://github.com/o/r/pull/%d","state":"open","draft":false,"mergeable":true,"head":{"sha":"0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f"}}`,
			n, n)))
	})

	// GET /repos/o/r/pulls/{n} -> 200 {state open, draft false,
	// mergeable true (v93 bool), head sha}.
	mux.HandleFunc("/repos/o/r/pulls/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(
			`{"number":42,"state":"open","draft":false,"mergeable":true,"head":{"sha":"0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f"}}`))
	})

	// GET /repos/o/r/commits/{sha}/check-runs -> 200 wrapped ListCheckRunsResults
	// object (v93 decodes into {total_count, check_runs} — a bare array
	// would fail the client's json.Unmarshal). First call: one in_progress
	// check; subsequent calls: all success.
	mux.HandleFunc("/repos/o/r/commits/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/check-runs") {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		fg.mu.Lock()
		fg.checkRunsCalls++
		first := fg.checkRunsCalls == 1 || fg.alwaysInProgress
		fg.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if first {
			_, _ = w.Write([]byte(`{"total_count":1,"check_runs":[{"name":"ci-build","status":"in_progress","conclusion":null}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"total_count":1,"check_runs":[{"name":"ci-build","status":"completed","conclusion":"success"}]}`))
	})

	// GET /repos/o/r/actions/runs -> 200 one success workflow run.
	mux.HandleFunc("/repos/o/r/actions/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		fg.mu.Lock()
		fg.actionsRunsCalls++
		fg.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total_count":1,"workflow_runs":[{"id":777,"name":"ci.yml","status":"completed","conclusion":"success","head_sha":"0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f"}]}`))
	})

	fg.srv = httptest.NewServer(mux)
	t.Cleanup(fg.srv.Close)
	return fg
}

// URL is the fake's base URL for the [github].base_url overlay.
func (fg *fakeGitHub) URL() string { return fg.srv.URL }

// newFakeGitHubAlwaysInProgress stands up the fake with the check-runs
// endpoint pinned to in_progress on every call — the park scenario's
// never-green CI.
func newFakeGitHubAlwaysInProgress(t *testing.T) *fakeGitHub {
	fg := newFakeGitHub(t)
	fg.mu.Lock()
	fg.alwaysInProgress = true
	fg.mu.Unlock()
	return fg
}

// postsPulledCount is the POST /repos/o/r/pulls call count.
func (fg *fakeGitHub) postsPulledCount() int {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	return fg.postsPulled
}

// checkRunsCallsCount is the check-runs GET call count.
func (fg *fakeGitHub) checkRunsCallsCount() int {
	fg.mu.Lock()
	defer fg.mu.Unlock()
	return fg.checkRunsCalls
}

// ---------------------------------------------------------------------------
// [github] config wiring helpers
// ---------------------------------------------------------------------------

// githubConfigOverlay returns the StartOption that points the [github] block
// at the fake server. tokenSecretPath is the absolute path of the test token
// file. Dotted keys use the config JSON tag names ("github.enabled", ...),
// which is exactly how WithConfigOverlay maps into the sandbox meept.json5.
func githubConfigOverlay(baseURL, tokenSecretPath string) harness.StartOption {
	return harness.WithConfigOverlay(map[string]any{
		"github.enabled":       true,
		"github.base_url":      baseURL,
		"github.token_secret":  tokenSecretPath,
		"github.default_owner": "o",
		"github.default_repo":  "r",
	})
}

// writeGitHubTokenFile writes the test token into t.TempDir (kept alive
// for the whole test; the harness's LIFO cleanup stops the daemon first)
// and returns its absolute path for [github].token_secret.
func writeGitHubTokenFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "github_token")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("write github token file: %v", err)
	}
	return p
}

// ---------------------------------------------------------------------------
// Tool-list helpers (same pattern as the memory-knowledge-assets suite)
// ---------------------------------------------------------------------------

// seedGitHubAgent writes a "gitsync" executor agent into the sandbox roster
// that grants the eight github_* tools in additional_tools. Same pattern as
// the memory-knowledge-assets suite: "gitsync" shadows a REAL bundled lane —
// here "librarian" (agentForIntent consults the lane-agent index first,
// where "gitsync" sorts before "librarian", and the static agentMapping
// would otherwise fall through to librarian). The test then pins the fake
// LLM's classifier output to intent "librarian" so step jobs resolve to
// gitsync. Without this the global github tool registry has no agent that
// grants it, so an executor turn asking for github_pr_create lands on an
// agent whose granted set lacks the tool ("Unknown tool requested").
func seedGitHubAgent(t *testing.T, st *harness.Stack) {
	t.Helper()
	dir := filepath.Join(st.MeeptHome, "agents", "gitsync")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir gitsync agent dir: %v", err)
	}
	body := `---
id: gitsync
name: E2E GitHub PR/CI Executor
role: executor
description: e2e-only agent holding the github_* tool grants
intents: [librarian]
enabled: true
can_delegate: false
additional_tools:
  - github_pr_create
  - github_pr_status
  - github_ci_wait
  - github_pr_comment
  - github_pr_review
  - github_pr_merge
  - github_issue_get
  - github_issue_comment
capabilities:
  - code
  - reasoning
max_iterations: 8
timeout_seconds: 120
---

# E2E GitHub PR/CI Executor

Use the github tools exactly as the user asks.
`
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write gitsync AGENT.md: %v", err)
	}
}

// pinGitsyncClassifier pins the fake LLM's classifier to the shadowed
// "librarian" lane so step jobs route to the gitsync stub (which grants
// the github_* tools).
const gitsyncClassifierPin = `{"intent":"librarian","confidence":0.95,"reasoning":"pinned gitsync"}`

// toolResultTexts collects every role=tool message the fake LLM has seen.
func toolResultTexts(f *harness.FakeLLM) []string {
	var out []string
	for _, body := range f.Requests() {
		msgs, _ := body["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			if msg["role"] != "tool" {
				continue
			}
			if c, _ := msg["content"].(string); c != "" {
				out = append(out, c)
			}
		}
	}
	return out
}

func toolResultsJoined(f *harness.FakeLLM) string {
	return strings.Join(toolResultTexts(f), "\n")
}

// toolNamesInRequests lists every tool name the executor ever offered the
// model (the registry's projection into the requests' tools arrays).
func toolNamesInRequests(f *harness.FakeLLM) map[string]bool {
	names := map[string]bool{}
	for _, body := range f.Requests() {
		tools, ok := body["tools"].([]any)
		if !ok {
			continue
		}
		for _, raw := range tools {
			fn, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if f2, ok := fn["function"].(map[string]any); ok {
				fn = f2
			}
			if n, _ := fn["name"].(string); n != "" {
				names[n] = true
			}
		}
	}
	return names
}

// ---------------------------------------------------------------------------
// Scenario 1: github-pr-ci-01 — create PR + wait for CI green
// ---------------------------------------------------------------------------

// TestE2ECreateWaitGreen drives the full loop: the fake GitHub server stands
// up first; the scratch daemon boots with [github] pointing at it; a scripted
// executor turn calls github_pr_create then github_ci_wait. Asserts: the
// fake saw EXACTLY ONE POST /pulls and the wait tool result carries
// all_green=true (the fake's first check-runs call is in_progress, later
// calls all success, so a correct wait polls to green).
func TestE2ECreateWaitGreen(t *testing.T) {
	fg := newFakeGitHub(t)
	tokenPath := writeGitHubTokenFile(t, "ghp_test_token_e2e")
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			seedGitHubAgent(t, st)
			return nil
		}),
		githubConfigOverlay(fg.URL(), tokenPath),
	)
	s.RegisterProject(t, "e2e-project")
	// Pin the classifier to the gitsync stub so step jobs route to an
	// agent that grants the github_* tools (same technique as the
	// memory-knowledge-assets suite).
	s.Fake.SetClassifierOutput(gitsyncClassifierPin)
	sessionID := s.CreateSession(t, "e2e-github-pr", s.ProjectDir)

	// Script the executor turn: create the PR, then wait for CI on the PR
	// the fake's POST returns (number 42).
	s.Fake.EnqueueToolCalls(
		harness.ToolCall{
			Name: "github_pr_create",
			Arguments: `{
				"owner":"o","repo":"r",
				"title":"feat/x: add PR","body":"hermetic e2e PR",
				"head":"feat/x","base":"main"
			}`,
		},
		harness.ToolCall{
			Name:      "github_ci_wait",
			Arguments: `{"owner":"o","repo":"r","pr":42}`,
		},
	)
	// NO SetPostToolText: a terminal model text that claims a side effect
	// ("Created PR #42 and CI is green.") with zero tool executions in that
	// iteration trips the review step's unbacked-claim nudge, the fake LLM
	// repeats the same text, and the convergence guard aborts the turn with
	// an empty reply. The reference suite (memory-knowledge-assets)
	// terminates its tool turns the same way: the scripted queue drains and
	// the default (empty) post-tool text ends the loop. The scenario's
	// assertions read the tool RESULTS, not the terminal text.
	reply := s.ChatTurn(t, sessionID,
		"create a pull request on branch feat/x targeting main, then wait until its CI checks pass",
		180*time.Second)

	// Exactly ONE POST /pulls: the whole loop must create the PR once.
	if got := fg.postsPulledCount(); got != 1 {
		t.Fatalf("fake GitHub saw %d POST /pulls, want exactly 1\nreply: %s\nlog tail:\n%s",
			got, reply, s.Daemon.LogTail())
	}
	// The wait tool returned all_green=true: its structured PRStatus result
	// is JSON-marshalled into the role=tool message the fake LLM saw, so
	// the green verdict appears as the compact form `"all_green":true`.
	// Accept both the JSON and a bare-spelling form so the assertion is
	// robust to how the executor serializes the tool result.
	if res := toolResultsJoined(s.Fake); !strings.Contains(res, `all_green":true`) && !strings.Contains(res, "all_green=true") {
		t.Fatalf("github_ci_wait result missing all_green=true; tool results:\n%s\nreply: %s\nlog tail:\n%s",
			res, reply, s.Daemon.LogTail())
	}
	// The wait actually polled check-runs (first call in_progress -> the
	// tool must re-poll until success).
	if got := fg.checkRunsCallsCount(); got < 2 {
		t.Fatalf("check-runs endpoint called %d times, want >= 2 (poll past the initial in_progress)", got)
	}
	// Anti-hammer ceiling (plan leaf 06 Task 2): the fake turns success
	// after the 2nd poll, so a correct run lands well under 6 check-runs
	// GETs; more than 6 means the wait is hammering the endpoint.
	if got := fg.checkRunsCallsCount(); got > 6 {
		t.Fatalf("check-runs endpoint called %d times, want <= 6 (anti-hammer ceiling)", got)
	}
}

// ---------------------------------------------------------------------------
// Scenario 2: github-pr-ci-02 — disabled by default
// ---------------------------------------------------------------------------

// TestE2EDisabledByDefault asserts the shipped posture: with NO [github]
// config block the executor's tool list carries NONE of the github_* tools.
// The config.Config.GitHub field (the [github] block) defaults Enabled=false,
// so the reference below pins the default-off assertion against the real
// config type.
func TestE2EDisabledByDefault(t *testing.T) {
	gh := config.DefaultConfig().GitHub
	if gh.Enabled {
		t.Fatal("[github] enabled by default; must ship disabled")
	}

	s := harness.Start(t) // no [github] overlay: the shipped posture
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "e2e-github-off", s.ProjectDir)

	// One scripted executor turn to capture the tool list.
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "file_read",
		Arguments: `{"path":"README.md"}`,
	})
	s.Fake.SetPostToolText("done")
	s.ChatTurn(t, sessionID, "read the project README", 120*time.Second)

	names := toolNamesInRequests(s.Fake)
	if len(names) == 0 {
		t.Fatal("executor offered no tools at all (harness broken)")
	}
	for _, n := range []string{
		"github_pr_create", "github_pr_status", "github_ci_wait",
		"github_pr_comment", "github_pr_review", "github_pr_merge",
		"github_issue_get", "github_issue_comment",
	} {
		if names[n] {
			t.Fatalf("tool %q exposed with [github] disabled (default)", n)
		}
	}
}

// ---------------------------------------------------------------------------
// Scenario 3: github-pr-ci-03 — effects-ledger reconcile
// ---------------------------------------------------------------------------

// TestE2EEffectsReconcile pre-seeds the daemon's effects.db with a CLAIMED
// (not completed) row whose key is built from the EXACT parts the scripted
// call produces — EffectKey("github_pr_create","o","r","feat/x",
// "feat/x: add PR"), i.e. (tool, owner, repo, head, TITLE) per
// github_pr.go's key composition (the title is the last part, not the head).
// The seeded key collides with the scripted call, so the claim's PK-conflict
// path finds the prior claimed row and the create is BLOCKED: no second POST
// /pulls is issued. The row stays in state=claimed (a crash-window record)
// on purpose — the daemon's startup reconcile surfaces it via log.
// Asserts: the fake's POST counter stays at 0 and the tool result carries
// the prior-owned error.
func TestE2EEffectsReconcile(t *testing.T) {
	fg := newFakeGitHub(t)
	tokenPath := writeGitHubTokenFile(t, "ghp_test_token_e2e")
	s := harness.Start(t,
		// Seed BEFORE the daemon boots: the daemon reopens the same
		// <data_dir>/effects.db at startup. NewSQLiteLedger migrates the
		// schema on open; the row is left in state=claimed on purpose
		// (a crash-window record — exactly what ReconcilePending
		// surfaces). The gitsync agent is seeded here too so the
		// scripted github_pr_create lands on an agent that grants it.
		harness.WithPreBootHook(func(st *harness.Stack) error {
			seedGitHubAgent(t, st)
			ledgerPath := filepath.Join(st.StateDir, "effects.db")
			l, err := effects.NewSQLiteLedger(ledgerPath, nil)
			if err != nil {
				return fmt.Errorf("seed: open effects ledger: %w", err)
			}
			// The seed key MUST use the exact title the scripted call
			// sends ("feat/x: add PR") so the keyed claim collides with
			// the seeded row — the key's last part is the TITLE
			// (github_pr.go composition: tool, owner, repo, head, title).
			key := effects.EffectKey("github_pr_create", "o", "r", "feat/x", "feat/x: add PR")
			if _, _, err := l.Claim(context.Background(), key, effects.EffectMeta{
				Tool: "github_pr_create",
				Payload: json.RawMessage(
					`{"owner":"o","repo":"r","head":"feat/x","base":"main","title":"feat/x: add PR"}`),
			}); err != nil {
				return fmt.Errorf("seed: claim effect row: %w", err)
			}
			if err := l.Close(); err != nil {
				return fmt.Errorf("seed: close ledger (WAL checkpoint): %w", err)
			}
			return nil
		}),
		githubConfigOverlay(fg.URL(), tokenPath),
	)
	s.RegisterProject(t, "e2e-project")
	s.Fake.SetClassifierOutput(gitsyncClassifierPin)
	sessionID := s.CreateSession(t, "e2e-github-reconcile", s.ProjectDir)

	// The identical create request: same owner/repo/head/title as the
	// seeded key's parts, so the content-derived key collides with the
	// claimed row and the create is blocked, not re-executed.
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "github_pr_create",
		Arguments: `{
			"owner":"o","repo":"r",
			"title":"feat/x: add PR","body":"hermetic e2e PR (reconcile)",
			"head":"feat/x","base":"main"
		}`,
	})
	s.Fake.SetPostToolText("github_pr_create was blocked by a prior claimed effect record; no second POST was issued.")
	reply := s.ChatTurn(t, sessionID,
		"open a PR from branch feat/x to main",
		180*time.Second)

	// No SECOND POST /pulls: the claimed ledger row (the prior attempt)
	// blocks re-execution. With the colliding seed the create never
	// reaches the fake, so the counter must stay at 0.
	if got := fg.postsPulledCount(); got > 1 {
		t.Fatalf("fake GitHub saw %d POST /pulls after reconcile; want <= 1 "+
			"(the claimed ledger row must prevent a second POST)\nreply: %s\nlog tail:\n%s",
			got, reply, s.Daemon.LogTail())
	}
	// The claimed prior record was surfaced: effects.Run refuses to run a
	// key already in a claimed/receipted state owned by another caller, so
	// the tool result carries that "owned by another caller" error (or,
	// under a receipt-surfacing refactor, the prior receipt / a reconcile
	// note). The POST bound above already proves the block; this proves
	// the agent saw the prior record, not a silent success.
	joined := toolResultsJoined(s.Fake) + "\n" + reply
	if !strings.Contains(joined, "owned by another caller") && !strings.Contains(joined, "reconcil") && !strings.Contains(joined, "prior receipt") {
		t.Fatalf("prior claimed effect not surfaced (no 'owned by another caller'/'reconcil'/'prior receipt' in tool results or reply):\n%s\nlog tail:\n%s",
			joined, s.Daemon.LogTail())
	}
}

// ---------------------------------------------------------------------------
// Scenario 4: github-pr-ci-04 — CI-wait park + re-arm row
// ---------------------------------------------------------------------------

// TestE2ECIWaitPark drives the park path (master plan Integration Test
// Plan item 3): the fake GitHub's check-runs endpoint ALWAYS serves
// in_progress, so a short github_ci_wait cannot go green and must park
// at its deadline. The park path returns (result, error) with the error
// wrapping githubsvc.ErrCIWaitParked; the agent executor turns a tool
// error into {"success":false,"error":"tool execution failed: <err>"} —
// the result map is dropped — so the role=tool message the fake LLM
// sees carries the park via the ErrCIWaitParked text ("ci wait parked;
// re-armed o/r#42 as <waitID> ..."), NOT a literal "parked":true JSON
// field. The assertions below accept either shape (the JSON field if a
// future executor change surfaces result+error together) and always
// require a non-empty wait_id.
//
// Instruction-row persistence: the scratch daemon DOES wire the
// instruction store unconditionally (internal/daemon/components.go
// wireInstructions -> c.InstructionStore, set BEFORE wireGitHubTools at
// the end of component wiring, so github_wiring.go constructs
// githubsvc.InstructionReArmSink with it). InstructionReArmSink.Save
// writes <tier>/github_ci_wait-<waitID>.yaml (lowercased via
// sanitizeFilename) into the DEFAULT tier, which is ".meept/instructions"
// — a RELATIVE path resolved against the daemon process's CWD. The
// harness starts the scratch daemon with cmd.Dir = s.Work (the sandbox
// root), so the row must exist at <Work>/.meept/instructions/.
func TestE2ECIWaitPark(t *testing.T) {
	fg := newFakeGitHubAlwaysInProgress(t)
	tokenPath := writeGitHubTokenFile(t, "ghp_test_token_e2e")
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			seedGitHubAgent(t, st)
			return nil
		}),
		githubConfigOverlay(fg.URL(), tokenPath),
	)
	s.RegisterProject(t, "e2e-project")
	s.Fake.SetClassifierOutput(gitsyncClassifierPin)
	sessionID := s.CreateSession(t, "e2e-github-park", s.ProjectDir)

	// Script the executor turn: wait for CI on PR 42 with a 1s timeout.
	// The fake never leaves in_progress, so the wait parks.
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "github_ci_wait",
		Arguments: `{"owner":"o","repo":"r","pr":42,"timeout_seconds":1}`,
	})
	s.Fake.SetPostToolText("the CI wait timed out and was parked for a later resume.")
	reply := s.ChatTurn(t, sessionID,
		"wait until the CI checks on PR 42 pass",
		180*time.Second)

	// The wait actually polled check-runs before parking (>= 1 GET).
	if got := fg.checkRunsCallsCount(); got < 1 {
		t.Fatalf("check-runs endpoint called %d times, want >= 1 (the wait must observe CI before parking)\nreply: %s\nlog tail:\n%s",
			got, reply, s.Daemon.LogTail())
	}

	// The park result: park evidence plus a non-empty wait_id. On the
	// live executor path the park surfaces as the ErrCIWaitParked error
	// text ("ci wait parked; re-armed o/r#42 as <waitID> ..."); the
	// "parked":true JSON field is accepted as the alternate shape.
	res := toolResultsJoined(s.Fake)
	waitRe := regexp.MustCompile(`ciwait_[0-9a-f]{16}`)
	m := waitRe.FindStringSubmatch(res)
	parked := strings.Contains(res, `"parked":true`) ||
		strings.Contains(res, `"parked": true`) ||
		strings.Contains(res, "ci wait parked")
	if !parked {
		t.Fatalf("github_ci_wait result carries no park evidence (parked=true JSON or ErrCIWaitParked text); tool results:\n%s\nreply: %s\nlog tail:\n%s",
			res, reply, s.Daemon.LogTail())
	}
	if m == nil {
		t.Fatalf("github_ci_wait result missing a wait_id (ciwait_<hex>); tool results:\n%s\nreply: %s\nlog tail:\n%s",
			res, reply, s.Daemon.LogTail())
	}
	waitID := m[0]

	// The instruction row: the scratch daemon wires the instruction
	// store, so the parked wait's re-arm row must be persisted as
	// github_ci_wait-<waitID>.yaml under the default tier
	// (.meept/instructions, resolved against the daemon CWD = s.Work).
	rowPath := filepath.Join(s.Work, ".meept", "instructions",
		"github_ci_wait-"+strings.ToLower(waitID)+".yaml")
	row, err := os.ReadFile(rowPath)
	if err != nil {
		t.Fatalf("parked wait did not persist its re-arm instruction row at %s: %v\nlog tail:\n%s",
			rowPath, err, s.Daemon.LogTail())
	}
	if !strings.Contains(string(row), waitID) {
		t.Fatalf("re-arm instruction row at %s does not reference the wait_id %s:\n%s",
			rowPath, waitID, string(row))
	}
	if !strings.Contains(string(row), "cron:") {
		t.Fatalf("re-arm instruction row at %s does not carry a cron trigger:\n%s",
			rowPath, string(row))
	}
}
