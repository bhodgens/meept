package resources

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestManager_AddAndEnsure(t *testing.T) {
	store := newTestStore(t, 0)
	mgr := NewManager(store, nil)
	ctx := context.Background()

	// Add a file to CAS.
	src := writeFile(t, []byte("manager test"))
	hash, err := mgr.Add(ctx, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// hash should have the blake3: prefix.
	if len(hash) < 7 || hash[:7] != "blake3:" {
		t.Errorf("expected blake3: prefix, got %q", hash)
	}

	// Ensure the same ref — should hit locally.
	ref := ResourceRef{Raw: hash}
	path, err := mgr.Ensure(ctx, ref)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	// Verify path exists and has correct content.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "manager test" {
		t.Errorf("content mismatch: got %q", string(data))
	}

	// Ensure should have incremented refcount.
	_, body, _ := ParseRef(hash)
	if rc := store.Refcount(body); rc != 1 {
		t.Errorf("expected refcount 1 after Ensure, got %d", rc)
	}

	// Release decrements.
	mgr.Release(ref)
	if rc := store.Refcount(body); rc != 0 {
		t.Errorf("expected refcount 0 after Release, got %d", rc)
	}
}

func TestManager_RefPrefixRouting(t *testing.T) {
	store := newTestStore(t, 0)
	mgr := NewManager(store, nil)
	ctx := context.Background()

	t.Run("blake3_prefix", func(t *testing.T) {
		src := writeFile(t, []byte("blake3 routed"))
		hash, err := mgr.Add(ctx, src)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}

		path, err := mgr.Ensure(ctx, ResourceRef{Raw: hash})
		if err != nil {
			t.Fatalf("Ensure: %v", err)
		}
		if path == "" {
			t.Error("expected non-empty path")
		}
	})

	t.Run("sha256_prefix", func(t *testing.T) {
		// Add a file via sha256 store.
		dir := t.TempDir()
		cfg := CASConfig{StoreDir: dir, HashAlgorithm: AlgoSHA256}
		shaStore, err := NewCASStore(cfg, nil)
		if err != nil {
			t.Fatalf("NewCASStore: %v", err)
		}
		defer shaStore.Close()
		mgr2 := NewManager(shaStore, nil)

		src := writeFile(t, []byte("sha256 routed"))
		hash, err := mgr2.Add(ctx, src)
		if err != nil {
			t.Fatalf("Add: %v", err)
		}
		if len(hash) < 7 || hash[:7] != "sha256:" {
			t.Errorf("expected sha256: prefix, got %q", hash)
		}

		path, err := mgr2.Ensure(ctx, ResourceRef{Raw: hash})
		if err != nil {
			t.Fatalf("Ensure: %v", err)
		}
		if path == "" {
			t.Error("expected non-empty path")
		}
	})

	t.Run("gitcommit_prefix_returns_not_local", func(t *testing.T) {
		_, err := mgr.Ensure(ctx, ResourceRef{Raw: "gitcommit:abcdef123"})
		if !errors.Is(err, ErrNotLocal) {
			t.Errorf("expected ErrNotLocal, got %v", err)
		}
	})

	t.Run("workspace_prefix_returns_not_local", func(t *testing.T) {
		_, err := mgr.Ensure(ctx, ResourceRef{Raw: "workspace:myproject"})
		if !errors.Is(err, ErrNotLocal) {
			t.Errorf("expected ErrNotLocal, got %v", err)
		}
	})
}

func TestManager_EnsureMissNoFetcher(t *testing.T) {
	store := newTestStore(t, 0)
	mgr := NewManager(store, nil)
	ctx := context.Background()

	// Ensure a hash that's not in the store — no fetcher wired.
	_, err := mgr.Ensure(ctx, ResourceRef{Raw: "blake3:nonexistenthash"})
	if !errors.Is(err, ErrNotLocal) {
		t.Errorf("expected ErrNotLocal, got %v", err)
	}
}

func TestManager_EnsureMissWithFetcher(t *testing.T) {
	store := newTestStore(t, 0)
	mgr := NewManager(store, nil)
	ctx := context.Background()

	// Prepare blob data.
	data := []byte("fetched from peer")
	hash, _ := HashBytes(data, AlgoBlake3)

	// Wire a mock fetcher that stores the blob in the CAS.
	fetcher := &mockPeerFetcher{
		store: store,
		data:  data,
	}
	mgr.SetPeerFetcher(fetcher)

	path, err := mgr.Ensure(ctx, ResourceRef{Raw: HashPrefix(AlgoBlake3, hash)})
	if err != nil {
		t.Fatalf("Ensure with fetcher: %v", err)
	}

	// Verify data.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("data mismatch: got %q, want %q", string(got), string(data))
	}

	// Refcount should be incremented.
	if rc := store.Refcount(hash); rc != 1 {
		t.Errorf("expected refcount 1, got %d", rc)
	}
}

func TestManager_TelemetryCounters(t *testing.T) {
	store := newTestStore(t, 0)
	mgr := NewManager(store, nil)
	ctx := context.Background()

	mock := &mockMetrics{}
	store.SetMetricsEmitter(mock)

	// Add and Ensure — should register a hit.
	src := writeFile(t, []byte("telemetry"))
	hash, err := mgr.Add(ctx, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	_, _ = mgr.Ensure(ctx, ResourceRef{Raw: hash})
	if mock.hits != 1 {
		t.Errorf("expected 1 CAS hit, got %d", mock.hits)
	}

	// Ensure a missing ref — should register a miss.
	_, _ = mgr.Ensure(ctx, ResourceRef{Raw: "blake3:missinghash"})
	if mock.misses != 1 {
		t.Errorf("expected 1 CAS miss, got %d", mock.misses)
	}
}

func TestManager_SetPeerFetcher_NilSafe(t *testing.T) {
	store := newTestStore(t, 0)
	mgr := NewManager(store, nil)

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("SetPeerFetcher(nil) panicked: %v", r)
		}
	}()

	mgr.SetPeerFetcher(nil)

	// Ensure the fetcher is still nil (not set).
	_, err := mgr.Ensure(context.Background(), ResourceRef{Raw: "blake3:missing"})
	if !errors.Is(err, ErrNotLocal) {
		t.Errorf("expected ErrNotLocal with nil fetcher, got %v", err)
	}
}

func TestManager_Has(t *testing.T) {
	store := newTestStore(t, 0)
	mgr := NewManager(store, nil)
	ctx := context.Background()

	src := writeFile(t, []byte("has test"))
	hash, err := mgr.Add(ctx, src)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Has with prefixed ref.
	if !mgr.Has(hash) {
		t.Error("Has returned false for prefixed hash")
	}

	// Has with bare hash.
	_, body, _ := ParseRef(hash)
	if !mgr.Has(body) {
		t.Error("Has returned false for bare hash")
	}

	// Has with missing.
	if mgr.Has("blake3:nonexistent") {
		t.Error("Has returned true for missing hash")
	}
}

func TestManager_ReleaseIdempotent(t *testing.T) {
	store := newTestStore(t, 0)
	mgr := NewManager(store, nil)

	// Release on a non-tracked ref should not panic.
	mgr.Release(ResourceRef{Raw: "blake3:nonexistent"})

	// Release on non-CAS ref should not panic.
	mgr.Release(ResourceRef{Raw: "gitcommit:abc"})
}

func TestManager_ParseRef(t *testing.T) {
	cases := []struct {
		raw     string
		algo    string
		body    string
		isCAS   bool
	}{
		{"blake3:abcd", "blake3", "abcd", true},
		{"sha256:1234", "sha256", "1234", true},
		{"gitcommit:deadbeef", "gitcommit", "deadbeef", false},
		{"workspace:project1", "workspace", "project1", false},
		{"unknown:xyz", "", "", false},
		{"noprefix", "", "", false},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			algo, body, isCAS := ParseRef(tc.raw)
			if algo != tc.algo {
				t.Errorf("algo: got %q, want %q", algo, tc.algo)
			}
			if body != tc.body {
				t.Errorf("body: got %q, want %q", body, tc.body)
			}
			if isCAS != tc.isCAS {
				t.Errorf("isCAS: got %v, want %v", isCAS, tc.isCAS)
			}
		})
	}
}

func TestManager_StoreAccessor(t *testing.T) {
	store := newTestStore(t, 0)
	mgr := NewManager(store, nil)

	if mgr.Store() != store {
		t.Error("Store() did not return the underlying store")
	}
}

func TestManager_ErrorTypes(t *testing.T) {
	t.Run("ResourceUnavailable", func(t *testing.T) {
		ru := &ResourceUnavailable{Hash: "abc", SourceNode: "node-1"}
		if !errors.Is(ru, ErrResourceUnavailable) {
			t.Error("ResourceUnavailable should unwrap to ErrResourceUnavailable")
		}
		if ru.Error() == "" {
			t.Error("Error() should be non-empty")
		}
	})

	t.Run("ResourceCorrupt", func(t *testing.T) {
		rc := &ResourceCorrupt{Hash: "xyz", SourceNode: "node-2"}
		if !errors.Is(rc, ErrResourceCorrupt) {
			t.Error("ResourceCorrupt should unwrap to ErrResourceCorrupt")
		}
		if rc.Error() == "" {
			t.Error("Error() should be non-empty")
		}
	})
}

// --- mock peer fetcher ---

type mockPeerFetcher struct {
	store *CASStore
	data  []byte
}

func (m *mockPeerFetcher) Fetch(ctx context.Context, hashHex, algo string) (string, error) {
	if err := m.store.StoreBlob(ctx, hashHex, m.data, "peer-mock"); err != nil {
		return "", err
	}
	return m.store.GetPath(hashHex)
}
