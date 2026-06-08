package memory

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// mustNewManager creates and initializes a Manager for testing, using the
// SQLite backend with a temporary directory.
func mustNewManager(t *testing.T) *Manager {
	t.Helper()
	tmpDir := t.TempDir()
	mgr := NewManager(ManagerConfig{
		Config: config.MemoryConfig{
			Backend: config.MemoryBackendSQLite,
			DataDir: tmpDir,
			Episodic: config.EpisodicConfig{
				Enabled: true,
			},
			Task: config.TaskMemoryConfig{
				Enabled: true,
				Domains: []string{DomainGeneral, DomainCode},
			},
			Personality: config.PersonalityConfig{
				Enabled: false,
			},
		},
		Logger: testLogger(),
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("manager.Initialize: %v", err)
	}
	return mgr
}

// testLogger returns the default slog.Logger for tests.
func testLogger() *slog.Logger {
	return slog.Default()
}

func TestScopedManager_StoreAndSearch(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	ctx := context.Background()
	botA := mgr.ScopedManager("bot-a")
	botB := mgr.ScopedManager("bot-b")

	// Store memories from bot-a
	_, err := botA.Store(ctx, Memory{
		Type:     MemoryTypeEpisodic,
		Content:  "alpha secret data",
		Category: "conversation",
	})
	if err != nil {
		t.Fatalf("botA.Store: %v", err)
	}

	// Store memories from bot-b
	_, err = botB.Store(ctx, Memory{
		Type:     MemoryTypeEpisodic,
		Content:  "bravo private data",
		Category: "conversation",
	})
	if err != nil {
		t.Fatalf("botB.Store: %v", err)
	}

	tests := []struct {
		name       string
		scopedMgr  *ScopedMemoryManager
		query      string
		wantCount  int
		wantSubstr string
	}{
		{
			name:       "bot-a sees only its own memory",
			scopedMgr:  botA,
			query:      "alpha",
			wantCount:  1,
			wantSubstr: "alpha secret data",
		},
		{
			name:       "bot-a does not see bot-b memory",
			scopedMgr:  botA,
			query:      "bravo",
			wantCount:  0,
			wantSubstr: "",
		},
		{
			name:       "bot-b sees only its own memory",
			scopedMgr:  botB,
			query:      "bravo",
			wantCount:  1,
			wantSubstr: "bravo private data",
		},
		{
			name:       "bot-b does not see bot-a memory",
			scopedMgr:  botB,
			query:      "alpha",
			wantCount:  0,
			wantSubstr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := tt.scopedMgr.Search(ctx, MemoryQuery{
				Query: tt.query,
				Limit: 10,
			})
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(results) != tt.wantCount {
				t.Fatalf("expected %d results, got %d (contents: %v)",
					tt.wantCount, len(results), resultContents(results))
			}
			if tt.wantSubstr != "" && len(results) > 0 {
				if results[0].Memory.Content != tt.wantSubstr {
					t.Errorf("expected content %q, got %q",
						tt.wantSubstr, results[0].Memory.Content)
				}
			}
		})
	}
}

func TestScopedManager_StoreTagsBotID(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	ctx := context.Background()
	scoped := mgr.ScopedManager("tag-test-bot")

	id, err := scoped.Store(ctx, Memory{
		Type:     MemoryTypeEpisodic,
		Content:  "check tag",
		Category: "test",
	})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}

	// Retrieve via the underlying manager (unscoped) and verify bot_id is in metadata.
	// Note: the SQLite backend does not populate the BotID struct field on read;
	// it stores bot_id only in metadata JSON. The scoped manager checks metadata
	// for ownership.
	mem, err := mgr.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if mem.Metadata["bot_id"] != "tag-test-bot" {
		t.Errorf("expected metadata bot_id %q, got %v",
			"tag-test-bot", mem.Metadata["bot_id"])
	}
}

func TestScopedManager_GetRecent(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	ctx := context.Background()
	botA := mgr.ScopedManager("recent-a")
	botB := mgr.ScopedManager("recent-b")

	// Interleave stores between bots.
	for i := 0; i < 3; i++ {
		if _, err := botA.Store(ctx, Memory{
			Type: MemoryTypeEpisodic, Content: time.Now().String(),
			Category: "conversation",
		}); err != nil {
			t.Fatalf("botA.Store %d: %v", i, err)
		}
		if _, err := botB.Store(ctx, Memory{
			Type: MemoryTypeEpisodic, Content: time.Now().String(),
			Category: "conversation",
		}); err != nil {
			t.Fatalf("botB.Store %d: %v", i, err)
		}
	}

	recentA, err := botA.GetRecent(ctx, 100)
	if err != nil {
		t.Fatalf("botA.GetRecent: %v", err)
	}
	if len(recentA) != 3 {
		t.Errorf("botA GetRecent: expected 3, got %d", len(recentA))
	}

	recentB, err := botB.GetRecent(ctx, 100)
	if err != nil {
		t.Fatalf("botB.GetRecent: %v", err)
	}
	if len(recentB) != 3 {
		t.Errorf("botB GetRecent: expected 3, got %d", len(recentB))
	}
}

func TestScopedManager_GetByID_Ownership(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	ctx := context.Background()
	botA := mgr.ScopedManager("own-a")
	botB := mgr.ScopedManager("own-b")

	id, err := botA.Store(ctx, Memory{
		Type: MemoryTypeEpisodic, Content: "owned by a", Category: "test",
	})
	if err != nil {
		t.Fatalf("botA.Store: %v", err)
	}

	// BotA can retrieve its own memory.
	mem, err := botA.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("botA.GetByID: %v", err)
	}
	if mem.Content != "owned by a" {
		t.Errorf("unexpected content: %s", mem.Content)
	}

	// BotB gets ErrNotFound for botA's memory.
	_, err = botB.GetByID(ctx, id)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestScopedManager_Delete_Ownership(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	ctx := context.Background()
	botA := mgr.ScopedManager("del-a")
	botB := mgr.ScopedManager("del-b")

	id, err := botA.Store(ctx, Memory{
		Type: MemoryTypeEpisodic, Content: "deletable by a", Category: "test",
	})
	if err != nil {
		t.Fatalf("botA.Store: %v", err)
	}

	// BotB cannot delete botA's memory.
	err = botB.Delete(ctx, id)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}

	// BotA can delete its own memory.
	err = botA.Delete(ctx, id)
	if err != nil {
		t.Fatalf("botA.Delete: %v", err)
	}

	// Verify it's gone.
	_, err = mgr.GetByID(ctx, id)
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}
}

func TestScopedManager_GetRelevantContext(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	ctx := context.Background()
	botA := mgr.ScopedManager("ctx-a")
	botB := mgr.ScopedManager("ctx-b")

	if _, err := botA.Store(ctx, Memory{
		Type: MemoryTypeEpisodic, Content: "rust programming tips", Category: "code",
	}); err != nil {
		t.Fatalf("botA.Store: %v", err)
	}
	if _, err := botB.Store(ctx, Memory{
		Type: MemoryTypeEpisodic, Content: "go programming tips", Category: "code",
	}); err != nil {
		t.Fatalf("botB.Store: %v", err)
	}

	resultsA, err := botA.GetRelevantContext(ctx, "programming", 10)
	if err != nil {
		t.Fatalf("botA.GetRelevantContext: %v", err)
	}
	for _, r := range resultsA {
		if r.Memory.Content == "go programming tips" {
			t.Error("botA should not see botB memory in GetRelevantContext")
		}
	}

	resultsB, err := botB.GetRelevantContext(ctx, "programming", 10)
	if err != nil {
		t.Fatalf("botB.GetRelevantContext: %v", err)
	}
	for _, r := range resultsB {
		if r.Memory.Content == "rust programming tips" {
			t.Error("botB should not see botA memory in GetRelevantContext")
		}
	}
}

func TestScopedManager_BotID(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	scoped := mgr.ScopedManager("my-bot")
	if scoped.BotID() != "my-bot" {
		t.Errorf("expected BotID %q, got %q", "my-bot", scoped.BotID())
	}
}

func TestScopedManager_Manager(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	scoped := mgr.ScopedManager("bot-x")
	if scoped.Manager() != mgr {
		t.Error("expected Manager() to return the same underlying manager")
	}
}

func TestScopedManager_IsInitialized(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	scoped := mgr.ScopedManager("bot-y")
	if !scoped.IsInitialized() {
		t.Error("expected IsInitialized true for an initialized manager")
	}
}

func TestScopedManager_EmptyBotID(t *testing.T) {
	mgr := mustNewManager(t)
	defer mgr.Close()

	ctx := context.Background()
	scoped := mgr.ScopedManager("")

	// Store should still work with empty botID.
	_, err := scoped.Store(ctx, Memory{
		Type: MemoryTypeEpisodic, Content: "unscoped", Category: "test",
	})
	if err != nil {
		t.Fatalf("Store with empty botID: %v", err)
	}

	// Search should return the unscoped memory.
	results, err := scoped.Search(ctx, MemoryQuery{Query: "unscoped", Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected to find unscoped memory")
	}
}

// resultContents extracts the content strings from a slice of MemoryResult
// for test diagnostics.
func resultContents(results []MemoryResult) []string {
	contents := make([]string, len(results))
	for i, r := range results {
		contents[i] = r.Memory.Content
	}
	return contents
}
