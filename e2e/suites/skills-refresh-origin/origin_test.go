//go:build e2e

// Origin/provenance e2e gate (branch 02, plans/skills-refresh-origin
// master.md contracts C2/C3):
//
//	skills-origin-01  origin stamped per write path (agent/evolver/user) - TestOriginStampedPerWritePath
//	skills-origin-02  memory_facts origin column + legacy 'unknown' read - TestMemoryFactOriginAndLegacyUnknown
//
// RED by design until the sibling branch-02 leaves land: 01-skill-origin-stamp
// adds lifecycle.Origin + Writer.WriteSkillWithOrigin and 02-memory-origin-column
// adds the memory_facts.origin column plus tool-path stamping. This package
// does not compile until then, which is the intended gate state.

package skillsrefreshorigin

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // pure-Go sqlite driver (same dep as the daemon)

	"github.com/caimlas/meept/e2e/harness"
	"github.com/caimlas/meept/internal/skills/lifecycle"
)

// originLegacyFactDDL is the v1 memory_facts schema - the fact-store DDL at
// internal/memory/fact_store.go BEFORE branch 02 adds the additive
// `origin TEXT NOT NULL DEFAULT 'unknown'` column. Seeding a row through this
// DDL builds the pre-migration fixture db that proves the legacy-read path:
// after the in-place schema upgrade the row must read back 'unknown' with its
// value intact.
const originLegacyFactDDL = `
CREATE TABLE IF NOT EXISTS memory_facts (
	id             TEXT PRIMARY KEY,
	owner_id       TEXT NOT NULL DEFAULT '',
	kind           TEXT NOT NULL,
	key            TEXT NOT NULL,
	value          TEXT NOT NULL,
	valid_from     DATETIME,
	valid_until    DATETIME,
	source_session TEXT NOT NULL DEFAULT '',
	updated_at     DATETIME NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memory_facts_owner_kind_key
	ON memory_facts (owner_id, kind, key);
CREATE INDEX IF NOT EXISTS idx_memory_facts_active
	ON memory_facts (owner_id, valid_until);
`

// originSkillContent is one minimal SKILL.md (frontmatter + body) with NO
// origin key - the shape every funnel's write must stamp into place.
func originSkillContent(name, description, body string) string {
	return "---\nname: " + name + "\ndescription: \"" + description + "\"\n---\n\n" + body + "\n"
}

// originFrontmatterOf returns the frontmatter block (text between the
// leading --- markers) of one SKILL.md on disk.
func originFrontmatterOf(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	trimmed := strings.TrimSpace(string(data))
	if !strings.HasPrefix(trimmed, "---\n") {
		t.Fatalf("%s: no frontmatter block:\n%s", path, data)
	}
	rest := strings.TrimPrefix(trimmed, "---\n")
	end := strings.Index(rest, "\n---")
	if end < 0 {
		t.Fatalf("%s: unterminated frontmatter block:\n%s", path, data)
	}
	return rest[:end]
}

// originSeedLegacyFact builds a pre-migration fact store at dbPath: the v1
// schema (no origin column) plus one deterministic legacy row.
func originSeedLegacyFact(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy fixture db %s: %v", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(originLegacyFactDDL); err != nil {
		_ = db.Close()
		t.Fatalf("create v1 memory_facts schema: %v", err)
	}
	_, err = db.Exec(`INSERT INTO memory_facts
		(id, owner_id, kind, key, value, updated_at)
		VALUES ('e2e-legacy-fact-0001', '', 'preference', 'preference',
			'prefers aisle seats', '2025-01-01 00:00:00')`)
	if err != nil {
		_ = db.Close()
		t.Fatalf("insert legacy fact row: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy fixture db: %v", err)
	}
}

// originQueryOne opens the fact store read-only (WAL + busy timeout, the
// harness tasksdb pattern so the live daemon's writer never blocks) and
// returns the single-column result. ok=false means no row - or, before the
// implementation lands, a query the current schema cannot answer yet.
func originQueryOne(t *testing.T, dbPath, query string) (string, bool) {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("open fact db %s: %v", dbPath, err)
	}
	defer db.Close() //nolint:errcheck // read-only close, best effort
	db.SetMaxOpenConns(1)
	var val string
	err = db.QueryRow(query).Scan(&val)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false
		}
		return "", false
	}
	return val, true
}

// skills-origin-01 (F3): each skill write path stamps the right frontmatter
// origin value - the skills_create tool path is 'agent' (daemon wiring),
// the evolver funnel is 'evolver', the CLI/manual funnel is 'user', and the
// legacy bare WriteSkill leaves frontmatter byte-stable (no origin key).
func TestOriginStampedPerWritePath(t *testing.T) {
	// Pin the C2 enum values (master.md): the gate must not silently accept
	// a renamed value set.
	wantOriginValues := map[lifecycle.Origin]string{
		lifecycle.OriginUnknown: "unknown",
		lifecycle.OriginUser:    "user",
		lifecycle.OriginAgent:   "agent",
		lifecycle.OriginEvolver: "evolver",
		lifecycle.OriginReview:  "review",
	}
	for o, want := range wantOriginValues {
		if string(o) != want {
			t.Fatalf("C2 enum drift: %v = %q, want %q", o, string(o), want)
		}
	}

	t.Run("agent funnel: skills_create tool writes origin agent", func(t *testing.T) {
		// Tool-mediated write through the daemon wiring
		// (internal/daemon/components.go skills_create + lifecycle.Writer):
		// the stamped value must come out the real funnel, not a hand-built
		// writer.
		s := harness.Start(t)
		s.RegisterProject(t, "e2e-origin-agent")
		sessionID := s.CreateSession(t, "origin-agent", s.ProjectDir)

		s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
		s.Fake.SetChatText("done")
		s.Fake.EnqueueToolCalls(
			harness.ToolCall{
				Name:      "skills_create",
				Arguments: `{"name":"e2e-origin-agent","description":"Agent-authored e2e origin fixture","body":"Step one: do the thing. Step two: verify the thing."}`,
			},
		)

		s.ChatTurn(t, sessionID, "create the e2e origin agent skill", 120*time.Second)

		written := filepath.Join(s.StateDir, "skills", "e2e-origin-agent", "SKILL.md")
		data, err := os.ReadFile(written)
		if err != nil {
			t.Fatalf("authored skill missing at %s: %v\ndaemon log tail:\n%s", written, err, s.Daemon.LogTail())
		}
		fm := originFrontmatterOf(t, written)
		if !strings.Contains(fm, "origin: agent") {
			t.Fatalf("agent funnel: frontmatter missing 'origin: agent':\n%s", fm)
		}
		// The stamp must not clobber the tool-assembled body.
		if !strings.Contains(string(data), "Step one: do the thing.") {
			t.Fatalf("agent funnel: skill body clobbered by the origin stamp:\n%s", data)
		}
	})

	t.Run("evolver funnel: evolver write stamped", func(t *testing.T) {
		// Direct lifecycle.Writer drive at the level the harness permits:
		// the same write the evolver applies (internal/skills/lifecycle/
		// evolver.go) funnels through, stamped OriginEvolver.
		dir := t.TempDir()
		w := lifecycle.NewWriter(dir, nil)
		content := originSkillContent("e2e-origin-evolver", "Evolver-authored e2e origin fixture", "evolver body line.")
		if err := w.WriteSkillWithOrigin("e2e-origin-evolver", content, lifecycle.OriginEvolver); err != nil {
			t.Fatalf("WriteSkillWithOrigin(evolver): %v", err)
		}
		fm := originFrontmatterOf(t, filepath.Join(dir, "e2e-origin-evolver", "SKILL.md"))
		if !strings.Contains(fm, "origin: evolver") {
			t.Fatalf("evolver funnel: frontmatter missing 'origin: evolver':\n%s", fm)
		}
	})

	t.Run("user funnel: manual/cli write stamped", func(t *testing.T) {
		// Same direct drive, stamped OriginUser: the CLI/manual funnel
		// (cmd/meept/skills.go) writes.
		dir := t.TempDir()
		w := lifecycle.NewWriter(dir, nil)
		content := originSkillContent("e2e-origin-user", "Manual e2e origin fixture", "manual body line.")
		if err := w.WriteSkillWithOrigin("e2e-origin-user", content, lifecycle.OriginUser); err != nil {
			t.Fatalf("WriteSkillWithOrigin(user): %v", err)
		}
		fm := originFrontmatterOf(t, filepath.Join(dir, "e2e-origin-user", "SKILL.md"))
		if !strings.Contains(fm, "origin: user") {
			t.Fatalf("user funnel: frontmatter missing 'origin: user':\n%s", fm)
		}
	})

	t.Run("legacy write stays byte-stable", func(t *testing.T) {
		// C2 backwards-compat: bare WriteSkill == WithOrigin(OriginUnknown),
		// and the omitempty origin key keeps existing skill content
		// byte-identical - no origin line may appear on the legacy path.
		dir := t.TempDir()
		w := lifecycle.NewWriter(dir, nil)
		content := originSkillContent("e2e-origin-legacy", "Legacy e2e origin fixture", "legacy body line.")
		if err := w.WriteSkill("e2e-origin-legacy", content); err != nil {
			t.Fatalf("WriteSkill: %v", err)
		}
		fm := originFrontmatterOf(t, filepath.Join(dir, "e2e-origin-legacy", "SKILL.md"))
		if strings.Contains(fm, "origin:") {
			t.Fatalf("legacy WriteSkill stamped an origin line; frontmatter must stay byte-stable:\n%s", fm)
		}
	})
}

// skills-origin-02 (F4): the memory_facts.origin column - a fact written
// through the tool path (the daemon's session-end extraction,
// internal/daemon/memory_fact_wiring.go) lands with origin 'agent', while a
// pre-migration legacy row reads back 'unknown' with its value intact
// (the additive migration must not lose data).
func TestMemoryFactOriginAndLegacyUnknown(t *testing.T) {
	s := harness.Start(t,
		harness.WithPreBootHook(func(st *harness.Stack) error {
			// Seed the v1 fact store BEFORE the daemon boots: the daemon's
			// OpenFactStore must upgrade it in place (additive origin
			// column) instead of clobbering the legacy row.
			originSeedLegacyFact(t, filepath.Join(st.StateDir, "memory_facts.db"))
			return nil
		}),
	)
	s.RegisterProject(t, "e2e-origin-fact")
	sessionID := s.CreateSession(t, "origin-fact", s.ProjectDir)

	// A turn whose user message matches the fact extractor's preference
	// pattern; the session-end hook extracts it and upserts the fact.
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	s.Fake.SetChatText("window seats noted")
	s.ChatTurn(t, sessionID, "I prefer window seats", 120*time.Second)

	dbPath := filepath.Join(s.StateDir, "memory_facts.db")

	// The tool-path fact: poll a short window. Extraction is synchronous at
	// turn end, but the read-only WAL reader needs one settle. Before the
	// implementation lands the query errors ("no such column") and the
	// deadline fires - the intended RED.
	//
	// Key the new row off source_session, NOT value. The extraction funnel
	// (C6) stamps the session id onto every fact it writes, while the
	// seeded legacy row leaves source_session empty. Both the user message
	// and the assistant's session-context preamble map to the same upsert
	// key (owner='' / kind=preference / key=preference), so last-write-wins
	// decides which value survives; the value is C6's concern, not C3's.
	// C3's contract is only that the funnel-written row carries
	// origin='agent'. (Asserting on value LIKE '%window seats%' coupled
	// this gate to C6's value behavior and is not what we are testing.)
	const newFactQuery = `SELECT COALESCE(origin,'') FROM memory_facts
		WHERE kind = 'preference' AND source_session <> ''`
	var newOrigin string
	deadline := time.Now().Add(15 * time.Second)
	for {
		if v, ok := originQueryOne(t, dbPath, newFactQuery); ok && v != "" {
			newOrigin = v
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no tool-path fact row with an origin in %s\ndaemon log tail:\n%s",
				dbPath, s.Daemon.LogTail())
		}
		time.Sleep(500 * time.Millisecond)
	}
	if newOrigin != "agent" {
		t.Fatalf("tool-path fact origin = %q, want \"agent\"", newOrigin)
	}

	// The pre-migration legacy row: the in-place upgrade must not lose it
	// and it must read 'unknown' (the NOT-NULL default).
	legacy, ok := originQueryOne(t, dbPath,
		`SELECT COALESCE(origin,'') || '|' || value FROM memory_facts WHERE id = 'e2e-legacy-fact-0001'`)
	if !ok {
		t.Fatalf("legacy fact row 'e2e-legacy-fact-0001' missing after the migration (data loss)\ndaemon log tail:\n%s",
			s.Daemon.LogTail())
	}
	originPart, valuePart, found := strings.Cut(legacy, "|")
	if !found {
		t.Fatalf("legacy row read malformed: %q", legacy)
	}
	if originPart != "unknown" {
		t.Fatalf("legacy row origin = %q, want \"unknown\" (additive default)", originPart)
	}
	if valuePart != "prefers aisle seats" {
		t.Fatalf("legacy row value = %q, want \"prefers aisle seats\" (no data loss)", valuePart)
	}
}
