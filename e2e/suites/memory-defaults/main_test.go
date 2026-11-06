//go:build e2e

// Package memorydefaults is the RED e2e test gate for the memory
// second-brain tree (docs/plans/20261030-memory-second-brain, leaf 00T).
//
// It boots the scratch daemon with the DEFAULT config (no memory overlay)
// and pins the default-on world the implementation leaves must deliver:
//
//	defaults-on pipeline   F1/D3 — one completed turn records L0 lines,
//	                               enqueues a memory.l1_extract job, and the
//	                               knowledge tool rides the tool list (C4)
//	default knob report    C4/D1 — `meept config get` reports all five
//	                               memory knobs true
//	stable/volatile split  F4/C6 — stable item renders under
//	                               "Memory (standing)", volatile stays in
//	                               the <memory-context> fence
//	recall telemetry       F5/C5 — recall_outcomes.jsonl row carries the
//	                               verbatim C5 key set with cited=true
//	doctor visibility      F7    — `meept doctor` renders a
//	                               memory-pipeline check naming l0 status
//
// Every failure is a missing-implementation RED, never a config-parse or
// compile error: the config keys referenced all exist today defaulting
// false (internal/config/schema.go), the asserted file paths are the
// documented shapes (<memory.data_dir>/l0/, recall_outcomes.jsonl), and no
// not-yet-existing symbol is referenced.
package memorydefaults

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"

	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)
)

// Salient memory texts. Each carries a distinctive token ("convoy" as the
// shared FTS topic, plus a per-fact unique payload) so the recall lane ranks
// them deterministically.
const (
	stableFact   = "the convoy passphrase is amber Otter 4431"
	volatileFact = "the convoy standup rotates weekly per sprint"
	stableCite   = "the convoy passphrase is amber Otter 4431 — noted for the operation."
	recallQuery  = "convoy"
)

// defaultStack boots the hermetic stack with NO memory overlay: the point
// of this suite is the DEFAULT-config world (C4).
func defaultStack(t *testing.T) *harness.Stack {
	t.Helper()
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	return s
}

// pinChat pins the classifier to the chat lane and scripts the plain chat
// completion (the RouteToAgent path's reply).
func pinChat(s *harness.Stack, reply string) {
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	if reply != "" {
		s.Fake.SetChatText(reply)
	}
}

// l0DirFor returns the daemon's L0 directory (<memory.data_dir>/l0).
func l0DirFor(s *harness.Stack) string {
	return filepath.Join(s.StateDir, "memory", "l0")
}

// recallOutcomesPath returns <memory.data_dir>/recall_outcomes.jsonl (C5).
func recallOutcomesPath(s *harness.Stack) string {
	return filepath.Join(s.StateDir, "memory", "recall_outcomes.jsonl")
}

// queueDBPath returns the daemon's distill queue database (components.go:
// cfg.Queue.DBPath empty -> <daemon.data_dir>/queue.db).
func queueDBPath(s *harness.Stack) string {
	return filepath.Join(s.StateDir, "queue.db")
}

// dayFileHasExchange scans today's L0 day file for the session's user +
// assistant lines. A missing file is a plain false (nothing recorded yet).
func dayFileHasExchange(l0Dir, sessionID string) bool {
	path := filepath.Join(l0Dir, time.Now().Format("2006-01-02")+".jsonl")
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	hasUser, hasAssistant := false, false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec struct {
			SessionID string `json:"session_id"`
			Role      string `json:"role"`
		}
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec.SessionID != sessionID {
			continue
		}
		switch rec.Role {
		case "user":
			hasUser = true
		case "assistant":
			hasAssistant = true
		}
	}
	return hasUser && hasAssistant
}

// countL1Jobs opens the queue DB read-only and counts memory.l1_extract
// rows. A missing/unmigrated DB counts as zero.
func countL1Jobs(dbPath string) int {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		return 0
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return 0
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM jobs WHERE type = 'memory.l1_extract'`).Scan(&n); err != nil {
		return 0
	}
	return n
}

// promptTextsFrom joins the system+user message contents of the completion
// requests captured from index `since` onward (the surfaces recall
// injection renders into).
func promptTextsFrom(f *harness.FakeLLM, since int) string {
	var b strings.Builder
	reqs := f.Requests()
	for i := since; i < len(reqs); i++ {
		msgs, _ := reqs[i]["messages"].([]any)
		for _, m := range msgs {
			msg, ok := m.(map[string]any)
			if !ok {
				continue
			}
			role, _ := msg["role"].(string)
			if role != "system" && role != "user" {
				continue
			}
			c, _ := msg["content"].(string)
			b.WriteString(c)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// toolNamesSeen lists every tool name the executor ever offered the model.
func toolNamesSeen(f *harness.FakeLLM) map[string]bool {
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

// sectionAround returns the prompt slice from the first occurrence of title
// to the next "\n# " heading (the rendered custom-section body).
func sectionAround(prompt, title string) string {
	idx := strings.Index(prompt, title)
	if idx < 0 {
		return ""
	}
	rest := prompt[idx:]
	body := rest[len(title):]
	if end := strings.Index(body, "\n# "); end >= 0 {
		return rest[:len(title)+end]
	}
	return rest
}

// fenceSpan returns the complete <memory-context>...</memory-context> span
// (empty when no complete fence is present).
func fenceSpan(prompt string) string {
	start := strings.Index(prompt, "<memory-context>")
	if start < 0 {
		return ""
	}
	tail := prompt[start:]
	end := strings.Index(tail, "</memory-context>")
	if end < 0 {
		return ""
	}
	return tail[:end+len("</memory-context>")]
}

// ---------------------------------------------------------------------------
// memory-defaults-01 (T1/F1/D3): DEFAULT config boots the full pipeline
// ---------------------------------------------------------------------------

// TestDefaultsOnPipelineRecordsEnqueuesExposes boots the scratch daemon
// with the DEFAULT config (no memory overlay) and, after ONE completed
// chat turn, requires the default-on pipeline end to end:
//
//	a. the L0 day file gained the session's user+assistant lines
//	b. a memory.l1_extract distill job was enqueued on the persistent queue
//	c. the knowledge tool rides the executor tool list
//
// RED today (all three): memory.l0_enabled / memory.knowledge.enabled
// default false (internal/config/schema.go DefaultConfig), so the capture
// subscriber is never wired and the knowledge_* tools are never registered.
func TestDefaultsOnPipelineRecordsEnqueuesExposes(t *testing.T) {
	s := defaultStack(t)
	sessionID := s.CreateSession(t, "def01", s.ProjectDir)
	pinChat(s, "def01 default-config reply: noted.")

	s.ChatTurn(t, sessionID, "def01 default-config probe message", 120*time.Second)

	// (a) L0 capture recorded the exchange.
	l0Deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(l0Deadline) {
		if dayFileHasExchange(l0DirFor(s), sessionID) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if !dayFileHasExchange(l0DirFor(s), sessionID) {
		t.Fatalf("F1 RED: default-config turn recorded NO L0 lines in %s — "+
			"memory.l0_enabled defaults false so the turn.terminal capture "+
			"subscriber is never wired (default-on flip not implemented)",
			l0DirFor(s))
	}

	// (b) the chained distill job was enqueued.
	qDeadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(qDeadline) {
		if countL1Jobs(queueDBPath(s)) >= 1 {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if countL1Jobs(queueDBPath(s)) < 1 {
		t.Fatalf("F1 RED: no memory.l1_extract job in %s — the default-on "+
			"capture path never enqueues distill jobs (l0 wiring not armed)",
			queueDBPath(s))
	}

	// (c) the knowledge tool is exposed to the model.
	if !toolNamesSeen(s.Fake)["knowledge_search"] {
		t.Fatalf("F1 RED: knowledge_search absent from the executor tool list — " +
			"memory.knowledge.enabled defaults false so knowledge_* tools are " +
			"never registered (default-on flip not implemented)")
	}
}

// ---------------------------------------------------------------------------
// memory-defaults-02 (C4/D1): the five memory knobs report true
// ---------------------------------------------------------------------------

// TestDefaultsOnConfigGetReportsAllMemoryKnobs pins C4's single source of
// truth through the live config surface: all five memory knobs report true
// on a default boot.
//
// RED today: every one of them reports false (DefaultConfig + the
// epistemic/usefulness zero values).
func TestDefaultsOnConfigGetReportsAllMemoryKnobs(t *testing.T) {
	s := harness.Start(t)
	if !s.HealthOK() {
		t.Fatal("daemon did not come up healthy on default config")
	}
	for _, key := range []string{
		"memory.l0_enabled",
		"memory.usefulness.enabled",
		"memory.epistemic.ambient_extraction.enabled",
		"memory.knowledge.enabled",
		"memory.bridges.enabled",
	} {
		stdout, stderr := s.RunCLI(t, 30*time.Second, true, "config", "get", key)
		got := stdout + stderr
		if !strings.Contains(got, "true") {
			t.Fatalf("C4 RED: `meept config get %s` does not report true on a "+
				"default boot (got %q) — the default-on flip is not implemented",
				key, strings.TrimSpace(got))
		}
	}
}

// waitStoreExecuted polls the daemon log until a memory_store execution for
// marker lands (the reliable store-landed observable: the terminating tool's
// JSON envelope is reply-guard-rewritten before it becomes the reply, so the
// reply never carries memory_id).
func waitStoreExecuted(t *testing.T, s *harness.Stack, marker string) {
	t.Helper()
	harness.WaitFor(t, 30*time.Second, "the memory_store execution for "+marker, func() bool {
		return strings.Contains(s.Daemon.LogTail(), "tool=memory_store") &&
			strings.Contains(s.Daemon.LogTail(), marker)
	})
	if !strings.Contains(s.Daemon.LogTail(), "tool=memory_store") {
		t.Fatalf("memory_store never executed (no tool=memory_store line in the daemon log)")
	}
}

// ---------------------------------------------------------------------------
// memory-defaults-03 (T2/F4/C6): stable/volatile recall split
// ---------------------------------------------------------------------------

// TestStableVolatileRecallSplit stores one stable and one volatile memory,
// runs a recall turn, and requires the C6 assembly split in the rendered
// prompt: the stable item under the "Memory (standing)" section, the
// volatile item inside the <memory-context> fence.
//
// RED today: no stable-section render exists (memory injection rides the
// dispatcher's single volatile context block; AddSectionWithStability is
// never called with a memory section), so the title lookup fails first.
func TestStableVolatileRecallSplit(t *testing.T) {
	s := defaultStack(t)
	sessionID := s.CreateSession(t, "def03", s.ProjectDir)
	// Pin the chat lane for the WHOLE test: the store turns must ride the
	// chat loop so the terminating memory_store result becomes the reply
	// (an unpinned imperative "store in memory..." classifies code and the
	// store lands in a dispatched task lane instead).
	pinChat(s, "stored.")

	// Store the stable item. The stable flag rides the tool args — post-impl
	// the store stamps metadata["stable"]; today the unknown arg is ignored.
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "memory_store",
		Arguments: `{"content":"` + stableFact + `","type":"task",` +
			`"category":"general","stable":true}`,
	})
	s.ChatTurn(t, sessionID,
		"Please remember that "+stableFact, 120*time.Second)
	waitStoreExecuted(t, s, "amber Otter 4431")

	// Store the volatile item.
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "memory_store",
		Arguments: `{"content":"` + volatileFact + `","type":"task",` +
			`"category":"general","stable":false}`,
	})
	s.ChatTurn(t, sessionID,
		"Please remember that "+volatileFact, 120*time.Second)
	waitStoreExecuted(t, s, "standup rotates weekly")

	// Recall turn: the reply cites the stable item's salient text (F5's
	// "cited" precondition also depends on this shape).
	before := s.Fake.RequestCount()
	pinChat(s, stableCite)
	s.ChatTurn(t, sessionID, "what is the "+recallQuery+" passphrase?", 120*time.Second)
	prompt := promptTextsFrom(s.Fake, before)

	// F4a: the stable section renders under the C6 title.
	if !strings.Contains(prompt, "Memory (standing)") {
		t.Fatalf("F4 RED: no \"Memory (standing)\" section in the recall-turn prompt — "+
			"the stable/volatile assembly split (builder.AddSectionWithStability "+
			"with a memory section) is not implemented.\nprompt:\\n%s", prompt)
	}
	section := sectionAround(prompt, "Memory (standing)")
	if !strings.Contains(section, "amber Otter 4431") {
		t.Fatalf("F4 RED: the \"Memory (standing)\" section does not carry the stable item.\nsection:\n%s", section)
	}
	if strings.Contains(section, "standup rotates weekly") {
		t.Fatalf("F4 RED: the volatile item leaked into the \"Memory (standing)\" section.\nsection:\n%s", section)
	}

	// F4b: the volatile item stays inside the <memory-context> fence.
	fence := fenceSpan(prompt)
	if fence == "" {
		t.Fatalf("F4 RED: no <memory-context> fence in the recall-turn prompt — "+
			"the volatile recall lane lost its fence in the split.\nprompt:\n%s", prompt)
	}
	if !strings.Contains(fence, "standup rotates weekly") {
		t.Fatalf("F4 RED: the volatile item is not inside the <memory-context> fence.\nfence:\n%s", fence)
	}
}

// ---------------------------------------------------------------------------
// memory-defaults-04 (T2/F5/C5): recall telemetry row
// ---------------------------------------------------------------------------

// TestRecallOutcomeRowWritten stores one memory, runs a recall turn whose
// reply cites the injected item, and requires a recall_outcomes.jsonl row
// carrying the verbatim C5 key set with injected=true and cited=true.
//
// RED today: the telemetry writer does not exist, so the file never
// appears.
func TestRecallOutcomeRowWritten(t *testing.T) {
	s := defaultStack(t)
	sessionID := s.CreateSession(t, "def04", s.ProjectDir)
	// Chat lane pinned for the whole test so the store + recall turns ride
	// the chat loop (the unpinned imperative phrasing dispatches a task).
	pinChat(s, "stored.")

	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "memory_store",
		Arguments: `{"content":"` + stableFact + `","type":"task",` +
			`"category":"general","stable":true}`,
	})
	s.ChatTurn(t, sessionID,
		"Please remember that "+stableFact, 120*time.Second)
	waitStoreExecuted(t, s, "amber Otter 4431")

	// Recall turn whose reply cites the injected memory.
	pinChat(s, stableCite)
	s.ChatTurn(t, sessionID, "what is the "+recallQuery+" passphrase?", 120*time.Second)

	// C5 row: {"ts","session_id","memory_id","query","injected","cited","tokens"}.
	path := recallOutcomesPath(s)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, statErr := os.Stat(path); statErr == nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("F5 RED: %s was not written after a citing recall turn — "+
			"the recall telemetry writer (leaf 04, contract C5) is not implemented: %v",
			path, readErr)
	}
	var lastErr string
	rowFound := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			lastErr = line
			continue
		}
		rowFound = true
		for _, key := range []string{"ts", "session_id", "memory_id", "query", "injected", "cited", "tokens"} {
			if _, ok := row[key]; !ok {
				t.Fatalf("F5 RED: recall_outcomes.jsonl row is missing the C5 key %q.\nrow: %s", key, line)
			}
		}
		if row["injected"] != true {
			t.Fatalf("F5 RED: recall_outcomes.jsonl row injected = %v, want true.\nrow: %s", row["injected"], line)
		}
		if row["cited"] != true {
			t.Fatalf("F5 RED: recall_outcomes.jsonl row cited = %v, want true (the reply cites the injected text).\nrow: %s", row["cited"], line)
		}
		break
	}
	if !rowFound {
		t.Fatalf("F5 RED: no parsable row in %s (last unparsable line: %q) — "+
			"the C5 row shape is not implemented", path, lastErr)
	}
}

// ---------------------------------------------------------------------------
// memory-defaults-05 (T5/F7): doctor visibility
// ---------------------------------------------------------------------------

// TestDoctorSurfacesMemoryPipeline runs `meept doctor` against the scratch
// home and requires a memory-pipeline check line naming the l0 status.
//
// RED today: doctor renders no memory check at all (cmd/meept/doctor.go
// has no memory-pipeline entry), so no line carries the marker pair.
func TestDoctorSurfacesMemoryPipeline(t *testing.T) {
	s := defaultStack(t)

	stdout, _ := s.RunCLI(t, 60*time.Second, false, "doctor")
	for _, line := range strings.Split(stdout, "\n") {
		if strings.Contains(line, "memory-pipeline") && strings.Contains(line, "l0") {
			return // found: F7 satisfied
		}
	}
	t.Fatalf("F7 RED: `meept doctor` renders no memory-pipeline check naming "+
		"l0 status (the doctor memory check, leaf 06, is not implemented).\noutput:\n%s", stdout)
}
