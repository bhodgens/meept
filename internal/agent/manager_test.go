package agent

import (
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/session"
	"log/slog"
)

// managerConfig returns a minimal, ready-to-use ManagerConfig for tests.
func managerConfig() ManagerConfig {
	return ManagerConfig{
		SessionStore: session.NewMemoryStore(slog.Default()),
	}
}

func TestManager_GetOrCreate_New(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	loop, err := mgr.GetOrCreate("sess-new", "/tmp/workdir")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
	if loop == nil {
		t.Fatal("expected non-nil loop")
	}

	// Subsequent GetOrCreate returns the same loop (identity)
	loop2, err := mgr.GetOrCreate("sess-new", "/tmp/workdir")
	if err != nil {
		t.Fatalf("second GetOrCreate failed: %v", err)
	}
	if loop2 != loop {
		t.Error("expected GetOrCreate to return the same loop for existing session")
	}
}

func TestManager_GetExisting(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	// Get returns false for non-existent session
	_, ok := mgr.Get("does-not-exist")
	if ok {
		t.Error("expected Get to return false for non-existent session")
	}

	// Create a session via GetOrCreate
	_, err := mgr.GetOrCreate("sess-getme", "/tmp/test")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	// Get is read-only: does not create
	loop, ok := mgr.Get("sess-getme")
	if !ok {
		t.Error("expected Get to return true for existing session")
	}
	if loop == nil {
		t.Fatal("expected non-nil loop from Get")
	}

	// Get never creates — verify by calling Get before a GetOrCreate
	_, ok = mgr.Get("sess-creates-no")
	if ok {
		t.Error("Get should not create sessions")
	}
	// Now create it via GetOrCreate (not Get — that's the whole point)
	_, err = mgr.GetOrCreate("sess-creates-no", "/tmp")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}
}

func TestManager_Remove(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	_, err := mgr.GetOrCreate("sess-remove", "/tmp/test")
	if err != nil {
		t.Fatalf("GetOrCreate failed: %v", err)
	}

	// Verify it exists
	if _, ok := mgr.Get("sess-remove"); !ok {
		t.Fatal("expected session to exist before Remove")
	}

	mgr.Remove("sess-remove")

	// Verify removal
	if _, ok := mgr.Get("sess-remove"); ok {
		t.Error("expected session to be removed")
	}

	// Remove is idempotent (no panic on missing key)
	mgr.Remove("sess-remove")

	// Can create new loop with same sessionID after remove
	loop, err := mgr.GetOrCreate("sess-remove", "/tmp/other")
	if err != nil {
		t.Fatalf("GetOrCreate after Remove failed: %v", err)
	}
	if loop == nil {
		t.Fatal("expected non-nil loop after re-Create")
	}
}

func TestManager_List(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	// Empty manager returns empty map
	loops := mgr.List()
	if len(loops) != 0 {
		t.Errorf("expected 0 loops, got %d", len(loops))
	}

	// After creating N loops, List returns N
	for i := 1; i <= 4; i++ {
		_, err := mgr.GetOrCreate("sess-list-"+string(rune('a'+i-1)), "/tmp/test")
		if err != nil {
			t.Fatalf("GetOrCreate [%d] failed: %v", i, err)
		}
	}

	loops = mgr.List()
	if len(loops) != 4 {
		t.Errorf("expected 4 loops, got %d", len(loops))
	}
}

func TestManager_Concurrent(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	var wg sync.WaitGroup
	const goroutines = 100
	var errs sync.Mutex
	var firstErr error

	// Goroutines target a small set of session IDs so some contention
	// occurs (10 unique IDs × 10 goroutines each).
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(bucket int) {
			defer wg.Done()
			sid := "sess-concurrent-" + string(rune('a'+bucket%10))
			_, err := mgr.GetOrCreate(sid, "/tmp/workdir")
			if err != nil {
				errs.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errs.Unlock()
			}
		}(g)
	}

	wg.Wait()

	if firstErr != nil {
		t.Fatalf("concurrent error: %v", firstErr)
	}

	// Verify consistency: List should reflect all 10 session IDs (not more,
	// since many goroutines targeted the same bucket).
	loops := mgr.List()
	if len(loops) != 10 {
		t.Errorf("expected 10 unique loops, got %d", len(loops))
	}
}

func TestManager_Validation(t *testing.T) {
	t.Parallel()

	mgr := NewManager(managerConfig())

	tests := []struct {
		name       string
		sessionID  string
		workingDir string
		wantErr    string
	}{
		{"empty sessionID", "", "/tmp/test", "sessionID required"},
		{"empty workingDir", "sess-1", "", "workingDir required"},
		{"both empty", "", "", "sessionID required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := mgr.GetOrCreate(tt.sessionID, tt.workingDir)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if err.Error() != tt.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}
