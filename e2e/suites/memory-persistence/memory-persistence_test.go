//go:build e2e

// Suite memory-persistence: episodic/task memory over real sandbox
// sqlite that survives a store reopen (the process-restart analogue),
// and the DualStore mirror writing exactly one local row per memory.
package memorypersistence

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory"
)

// TestMemoryPersistence_SqliteSurvivesReopen covers memory-persistence-01:
// episodic and task memory writes land in real sqlite files and are
// re-read verbatim after the store is closed and REOPENED (the restart).
func TestMemoryPersistence_SqliteSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	// --- Episodic: store in generation 1, reopen in generation 2. ---
	episodicDir := root + "/episodic"
	e1, err := memory.NewEpisodicMemory(memory.EpisodicConfig{DataDir: episodicDir, Logger: nil})
	if err != nil {
		t.Fatalf("episodic gen1: %v", err)
	}
	if err := e1.Initialize(ctx); err != nil {
		t.Fatalf("episodic init: %v", err)
	}
	const content = "the deploy gate for repo-widget pins tag v2.3.1"
	const category = "conversation"
	id, err := e1.Store(ctx, content, category, map[string]any{"session": "sess-e2e"})
	if err != nil {
		t.Fatalf("episodic store: %v", err)
	}
	if id == "" {
		t.Fatal("episodic store returned an empty id")
	}
	// The DB file really exists on disk under the sandbox dir.
	if _, err := rootDBFile(episodicDir); err != nil {
		t.Fatalf("episodic sqlite file: %v", err)
	}

	// Generation 2: a FRESH store over the same directory (restart).
	e2, err := memory.NewEpisodicMemory(memory.EpisodicConfig{DataDir: episodicDir})
	if err != nil {
		t.Fatalf("episodic gen2: %v", err)
	}
	if err := e2.Initialize(ctx); err != nil {
		t.Fatalf("episodic gen2 init: %v", err)
	}
	got, err := e2.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("episodic get after reopen: %v", err)
	}
	if got == nil {
		t.Fatal("episodic memory lost across store reopen")
	}
	if got.Memory.Content != content || got.Memory.Category != category {
		t.Fatalf("reopened content mismatch: %q / %q", got.Memory.Content, got.Memory.Category)
	}
	// And it is searchable by content.
	results, err := e2.Search(ctx, "deploy gate", 5)
	if err != nil {
		t.Fatalf("episodic search after reopen: %v", err)
	}
	found := false
	for _, r := range results {
		if strings.Contains(r.Memory.Content, "v2.3.1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("reopened episodic search missed the stored memory: %+v", results)
	}

	// --- Task memory: same reopen discipline. ---
	taskDir := root + "/task"
	t1, err := memory.NewTaskMemory(memory.TaskMemoryConfig{DataDir: taskDir, Domains: []string{"code"}})
	if err != nil {
		t.Fatalf("task gen1: %v", err)
	}
	if err := t1.Initialize(ctx); err != nil {
		t.Fatalf("task init: %v", err)
	}
	const taskContent = "meept's queue backoff doubles per retry and caps at 8s"
	taskID, err := t1.Store(ctx, taskContent, "code", nil)
	if err != nil {
		t.Fatalf("task store: %v", err)
	}

	t2, err := memory.NewTaskMemory(memory.TaskMemoryConfig{DataDir: taskDir, Domains: []string{"code"}})
	if err != nil {
		t.Fatalf("task gen2: %v", err)
	}
	if err := t2.Initialize(ctx); err != nil {
		t.Fatalf("task gen2 init: %v", err)
	}
	taskResults, err := t2.Search(ctx, "queue backoff", "code", 5)
	if err != nil {
		t.Fatalf("task search after reopen: %v", err)
	}
	taskFound := false
	for _, r := range taskResults {
		if r.Memory.ID == taskID && strings.Contains(r.Memory.Content, "caps at 8s") {
			taskFound = true
		}
	}
	if !taskFound {
		t.Fatalf("reopened task search missed the stored memory: %+v", taskResults)
	}
}

// rootDBFile finds the sqlite file NewEpisodicMemory created in dir.
func rootDBFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".db") {
			return filepath.Join(dir, e.Name()), nil
		}
	}
	return "", errNoDB
}

var errNoDB = &noDBError{}

type noDBError struct{}

func (*noDBError) Error() string { return "no .db file found in memory data dir" }

// TestMemoryPersistence_DualStoreMirrorNoDuplicate covers
// memory-persistence-02: a DualStore mirror write stores the memory
// exactly once — re-mirroring the same memory (the Manager mirrors every
// store call) never duplicates the local row.
func TestMemoryPersistence_DualStoreMirrorNoDuplicate(t *testing.T) {
	ctx := context.Background()
	ds, err := memory.NewDualStore(t.TempDir(), "node-e2e", nil)
	if err != nil {
		t.Fatalf("dual store: %v", err)
	}
	defer ds.Close()

	m := &memory.Memory{
		Content:   "dual-store mirror row for the e2e wave",
		Type:      memory.MemoryTypeEpisodic,
		Category:  "conversation",
		CreatedAt: time.Now().UTC(),
	}

	// Mirror the SAME memory twice (as Manager.maybeMirrorToDualStore
	// would across two store calls of one memory id).
	for i := 0; i < 2; i++ {
		if err := ds.StoreMemory(ctx, m); err != nil {
			t.Fatalf("mirror %d: %v", i, err)
		}
	}

	// Exactly one local row exists for the id.
	var count int
	if err := ds.LocalDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memories WHERE id = ?`, m.ID).Scan(&count); err != nil {
		t.Fatalf("count local rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("local rows = %d, want 1 (the mirror duplicated the row)", count)
	}

	// And the row round-trips through the query API.
	got, err := ds.GetMemories(ctx, &memory.MemoryQuery{Limit: 10})
	if err != nil {
		t.Fatalf("GetMemories: %v", err)
	}
	found := false
	for _, r := range got {
		if r.Memory.ID == m.ID && r.Memory.Content == m.Content {
			found = true
		}
	}
	if !found {
		t.Fatalf("mirrored memory not queryable: %+v", got)
	}

	// A remote (peer-sourced) memory lands in the gossip DB, and the
	// echo-loop guard keeps it out of a re-mirror of local data.
	remote := &memory.Memory{
		Content:   "peer memory from node-west",
		Type:      memory.MemoryTypeEpisodic,
		Category:  "conversation",
		CreatedAt: time.Now().UTC(),
	}
	if err := ds.StoreRemoteMemory(ctx, remote, "node-west"); err != nil {
		t.Fatalf("StoreRemoteMemory: %v", err)
	}
	var gossipCount int
	if err := ds.GossipDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM memories WHERE id = ?`, remote.ID).Scan(&gossipCount); err != nil {
		t.Fatalf("count gossip rows: %v", err)
	}
	if gossipCount != 1 {
		t.Fatalf("gossip rows = %d, want 1", gossipCount)
	}
}
