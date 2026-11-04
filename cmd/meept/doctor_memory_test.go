package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// openMemoryDoctorTestDB opens a plain sqlite db for scratch-queue fixtures
// (test-only helper).
func openMemoryDoctorTestDB(path string) (*sql.DB, error) {
	return sql.Open("sqlite", "file:"+path)
}

// memoryDoctorScratchConfig builds a default config whose paths point at a
// scratch MEEPT_HOME-style dir (no file I/O outside it).
func memoryDoctorScratchConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	home := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Daemon.DataDir = filepath.Join(home, "daemon")
	cfg.Memory.DataDir = filepath.Join(home, "memory")
	cfg.Queue.DBPath = ""
	return cfg, home
}

// TestDoctorMemoryChecksWired runs the full block on an empty scratch home:
// all four contract check names + the memory-pipeline summary line render,
// nothing fails, nothing warns-as-fail (fresh home is a legal state), and
// the summary names l0.
func TestDoctorMemoryChecksWired(t *testing.T) {
	cfg, _ := memoryDoctorScratchConfig(t)
	oldHook := memoryDoctorConfigHook
	memoryDoctorConfigHook = func() *config.Config { return cfg }
	t.Cleanup(func() { memoryDoctorConfigHook = oldHook })

	checks := checkMemoryDoctor()

	names := map[string]bool{}
	for _, c := range checks {
		names[c.name] = true
		if !c.ok && !c.warn {
			t.Errorf("check %s: report-only semantics violated (ok=false, warn=false): %+v", c.name, c)
		}
	}
	for _, want := range []string{"memory-pipeline", "memory-capture", "memory-distill", "memory-recall", "memory-extras"} {
		if !names[want] {
			t.Errorf("missing check %q; got %v", want, names)
		}
	}
	var summary *doctorCheck
	for i := range checks {
		if checks[i].name == "memory-pipeline" {
			summary = &checks[i]
			break
		}
	}
	if summary == nil {
		t.Fatal("no memory-pipeline summary line")
	}
	if !strings.Contains(summary.detail, "l0") {
		t.Errorf("memory-pipeline summary does not name l0 status: %s", summary.detail)
	}
}

// TestDoctorMemoryCaptureStats: l0 dir with a yesterday file and a today
// file → latest=today, line count correct (leaf T1 Given/When/Then).
func TestDoctorMemoryCaptureStats(t *testing.T) {
	cfg, _ := memoryDoctorScratchConfig(t)
	cfg.Memory.L0Enabled = true
	l0Dir := filepath.Join(cfg.Memory.DataDir, "l0")
	if err := os.MkdirAll(l0Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	today := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	if err := os.WriteFile(filepath.Join(l0Dir, yesterday+".jsonl"),
		[]byte("{\"id\":\"a\"}\n{\"id\":\"b\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(l0Dir, today+".jsonl"),
		[]byte("{\"id\":\"c\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	latest, lines, files, err := l0CaptureStats(l0Dir)
	if err != nil {
		t.Fatalf("l0CaptureStats: %v", err)
	}
	if latest != today {
		t.Errorf("latest = %q, want today %q", latest, today)
	}
	if lines != 3 {
		t.Errorf("lines = %d, want 3", lines)
	}
	if files != 2 {
		t.Errorf("files = %d, want 2", files)
	}

	check := checkMemoryCaptureDoctor(cfg)
	if check.warn {
		t.Errorf("today-capture should not warn: %+v", check)
	}
	if !strings.Contains(check.detail, today) || !strings.Contains(check.detail, "3 line(s)") {
		t.Errorf("capture detail missing today/line count: %s", check.detail)
	}
}

// TestDoctorMemoryCaptureWarnStates: l0 armed with no files, and l0 armed
// with only an old file, both WARN (not fail); disabled l0 renders off with
// no warn (leaf: warn-not-fail semantics).
func TestDoctorMemoryCaptureWarnStates(t *testing.T) {
	cfg, _ := memoryDoctorScratchConfig(t)

	// armed, no files at all
	cfg.Memory.L0Enabled = true
	check := checkMemoryCaptureDoctor(cfg)
	if !check.warn || !check.ok {
		t.Errorf("armed-no-files: want ok+warn, got %+v", check)
	}

	// armed, only an old file
	l0Dir := filepath.Join(cfg.Memory.DataDir, "l0")
	if err := os.MkdirAll(l0Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -5).Format("2006-01-02")
	if err := os.WriteFile(filepath.Join(l0Dir, old+".jsonl"), []byte("{\"id\":\"a\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	check = checkMemoryCaptureDoctor(cfg)
	if !check.warn || !check.ok {
		t.Errorf("stale-capture: want ok+warn, got %+v", check)
	}
	if !strings.Contains(check.detail, old) {
		t.Errorf("stale-capture detail should name latest date: %s", check.detail)
	}

	// disabled: no warn
	cfg.Memory.L0Enabled = false
	check = checkMemoryCaptureDoctor(cfg)
	if check.warn {
		t.Errorf("disabled l0 should not warn: %+v", check)
	}
	if !strings.Contains(check.detail, "disabled") {
		t.Errorf("disabled l0 detail should say disabled: %s", check.detail)
	}
}

// TestDoctorMemoryDistillFromDB: a scratch queue.db with 2 pending jobs (one
// memory.l1_extract, one other) + 1 dead letter renders counts; the
// dead-letter leg warns (leaf T2 Given/When/Then).
func TestDoctorMemoryDistillFromDB(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "queue.db")
	db, err := openMemoryDoctorTestDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	stmts := []string{
		`CREATE TABLE jobs (id TEXT PRIMARY KEY, state TEXT, type TEXT)`,
		`CREATE TABLE dead_letter (id TEXT PRIMARY KEY)`,
		`INSERT INTO jobs (id, state, type) VALUES ('j1','pending','memory.l1_extract')`,
		`INSERT INTO jobs (id, state, type) VALUES ('j2','pending','one_off')`,
		`INSERT INTO dead_letter (id) VALUES ('d1')`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("exec %q: %v", s, err)
		}
	}

	counts, err := memoryQueueCountsFromDB(dbPath)
	if err != nil {
		t.Fatalf("memoryQueueCountsFromDB: %v", err)
	}
	if counts.pendingAll != 2 || counts.pendingL1 != 1 || counts.pendingL2 != 0 {
		t.Errorf("pending counts wrong: %+v", counts)
	}
	if counts.dead != 1 {
		t.Errorf("dead = %d, want 1", counts.dead)
	}

	// The doctor check over a config pointing at this db warns (dead letter)
	// but never fails.
	cfg, _ := memoryDoctorScratchConfig(t)
	cfg.Queue.DBPath = dbPath
	check := checkMemoryDistillDoctor(cfg)
	if !check.ok || !check.warn {
		t.Errorf("dead-letter leg: want ok+warn, got %+v", check)
	}
	if !strings.Contains(check.detail, "dead=1") {
		t.Errorf("distill detail missing dead count: %s", check.detail)
	}
}

// TestDoctorMemoryDistillCleanQueue: empty counts + unset memory_model →
// ok, no warn, models-less posture stays green (leaf self-check: works on
// models-less hosts).
func TestDoctorMemoryDistillCleanQueue(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "queue.db")
	db, err := openMemoryDoctorTestDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close() //nolint:errcheck // test cleanup
	for _, s := range []string{
		`CREATE TABLE jobs (id TEXT PRIMARY KEY, state TEXT, type TEXT)`,
		`CREATE TABLE dead_letter (id TEXT PRIMARY KEY)`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}

	cfg, _ := memoryDoctorScratchConfig(t)
	cfg.Queue.DBPath = dbPath
	check := checkMemoryDistillDoctor(cfg)
	// memory_model unset = documented fallback to general chat client: ok.
	if check.warn {
		t.Errorf("clean queue + unset memory_model should not warn: %+v", check)
	}
}

// TestDoctorMemoryRecallOutcomeAge: an outcomes file with a fresh row
// renders the row count and a last-recorded age (leaf T1 recall leg).
func TestDoctorMemoryRecallOutcomeAge(t *testing.T) {
	cfg, _ := memoryDoctorScratchConfig(t)
	if err := os.MkdirAll(cfg.Memory.DataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rows := "" +
		`{"ts":"` + time.Now().UTC().Add(-2*time.Hour).Format(time.RFC3339) + `","session_id":"s","memory_id":"m1","query":"q","injected":true,"cited":true,"tokens":10}` + "\n" +
		`{"ts":"` + time.Now().UTC().Format(time.RFC3339) + `","session_id":"s","memory_id":"m2","query":"q","injected":true,"cited":false,"tokens":10}` + "\n"
	path := filepath.Join(cfg.Memory.DataDir, "recall_outcomes.jsonl")
	if err := os.WriteFile(path, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}

	check := checkMemoryRecallDoctor(cfg)
	if check.warn {
		t.Errorf("fresh outcomes row should not warn: %+v", check)
	}
	if !strings.Contains(check.detail, "2 recall outcome row(s)") {
		t.Errorf("recall detail missing row count: %s", check.detail)
	}

	// Missing file: ok, armed phrasing.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	check = checkMemoryRecallDoctor(cfg)
	if check.warn {
		t.Errorf("missing outcomes file should not warn: %+v", check)
	}
	if !strings.Contains(check.detail, "no recall outcomes yet") {
		t.Errorf("missing-file phrasing wrong: %s", check.detail)
	}
}

// TestDoctorMemoryExtrasStates: knowledge on renders the page count
// (5 when the scratch db says 5), bridges state renders; disabled knowledge
// renders off; report-only verified via mtime-unchanged assertion
// (leaf T3 Given/When/Then: no mutation).
func TestDoctorMemoryExtrasStates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "daemon", "knowledge", "knowledge.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := openMemoryDoctorTestDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE knowledge_pages (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := db.Exec(`INSERT INTO knowledge_pages (id) VALUES (?)`, fmt.Sprintf("p%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	cfg, _ := memoryDoctorScratchConfig(t)
	cfg.Daemon.DataDir = filepath.Dir(filepath.Dir(dbPath)) // <data>/knowledge/knowledge.db
	cfg.Memory.Knowledge.Enabled = true
	cfg.Memory.Bridges.Enabled = true

	// mtime before/after: the check must not touch the store.
	before, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	check := checkMemoryExtrasDoctor(cfg)
	after, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Errorf("knowledge db mtime changed — memory-extras mutated state")
	}

	if check.warn {
		t.Errorf("healthy extras should not warn: %+v", check)
	}
	if !strings.Contains(check.detail, "pages: 5") {
		t.Errorf("extras detail missing page count: %s", check.detail)
	}
	if !strings.Contains(check.detail, "bridges on") {
		t.Errorf("extras detail missing bridges state: %s", check.detail)
	}

	// disabled knowledge: rendered off, no warn.
	cfg.Memory.Knowledge.Enabled = false
	check = checkMemoryExtrasDoctor(cfg)
	if check.warn {
		t.Errorf("disabled knowledge should not warn: %+v", check)
	}
	if !strings.Contains(check.detail, "knowledge store disabled") || !strings.Contains(check.detail, "bridges on") {
		t.Errorf("disabled-knowledge detail wrong: %s", check.detail)
	}
}

// TestDoctorMemorySummaryPhrases pins the F7-visible phrasing: the summary
// line carries "memory-pipeline", the l0 status phrase, and every arm state.
func TestDoctorMemorySummaryPhrases(t *testing.T) {
	cfg, _ := memoryDoctorScratchConfig(t)
	cfg.Memory.L0Enabled = true
	cfg.Memory.Knowledge.Enabled = true
	cfg.Memory.Bridges.Enabled = true
	oldHook := memoryDoctorConfigHook
	memoryDoctorConfigHook = func() *config.Config { return cfg }
	t.Cleanup(func() { memoryDoctorConfigHook = oldHook })

	checks := checkMemoryDoctor()
	var summary string
	for _, c := range checks {
		if c.name == "memory-pipeline" {
			summary = c.detail
		}
	}
	if summary == "" {
		t.Fatal("no memory-pipeline summary")
	}
	for _, want := range []string{"l0 on", "knowledge on", "bridges on"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary missing %q: %s", want, summary)
		}
	}
}

// TestDoctorMemorySummaryWiredIntoRunDoctor: runDoctor's output carries the
// memory-pipeline line (the seam the F7 e2e gate asserts through the real
// binary; this pins the wiring in-process).
func TestDoctorMemorySummaryWiredIntoRunDoctor(t *testing.T) {
	oldState := stateDir
	t.Cleanup(func() { stateDir = oldState })
	stateDir = t.TempDir()

	out := captureStdout(t, func() {
		if err := runDoctor(false, false); err != nil {
			t.Errorf("runDoctor(false, false): %v", err)
		}
	})
	if !strings.Contains(out, "memory-pipeline") || !strings.Contains(out, "l0") {
		t.Errorf("runDoctor output has no memory-pipeline check naming l0:\n%s", out)
	}
	if !strings.Contains(out, "memory-capture") || !strings.Contains(out, "memory-distill") ||
		!strings.Contains(out, "memory-recall") || !strings.Contains(out, "memory-extras") {
		t.Errorf("runDoctor output missing one or more memory checks:\n%s", out)
	}
}
