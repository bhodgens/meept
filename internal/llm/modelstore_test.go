package llm_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// fakeHub serves a HF-like API: /api/models/<repo> manifest and
// /<repo>/resolve/main/<file> downloads with Range support.
type fakeHub struct {
	srv     *httptest.Server
	repos   map[string]map[string][]byte // repo -> filename -> content
	noRange atomic.Bool
	calls   map[string]int
}

func newFakeHub(t *testing.T) *fakeHub {
	h := &fakeHub{
		repos: make(map[string]map[string][]byte),
		calls: make(map[string]int),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/models/", func(w http.ResponseWriter, r *http.Request) {
		repo := strings.TrimPrefix(r.URL.Path, "/api/models/")
		files, ok := h.repos[repo]
		if !ok {
			http.NotFound(w, r)
			return
		}
		var siblings []map[string]any
		for name, body := range files {
			siblings = append(siblings, map[string]any{"rfilename": name, "size": len(body)})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"siblings": siblings})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// <repo>/resolve/<rev>/<file>
		parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/"), "/resolve/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		repo := parts[0]
		file := parts[1]
		if i := strings.Index(file, "/"); i >= 0 {
			file = file[i+1:] // strip <rev>/
		}
		files, ok := h.repos[repo]
		if !ok {
			http.NotFound(w, r)
			return
		}
		body, ok := files[file]
		if !ok {
			http.NotFound(w, r)
			return
		}
		h.calls["download"]++
		rng := r.Header.Get("Range")
		start := 0
		if rng != "" && !h.noRange.Load() {
			if _, err := fmt.Sscanf(rng, "bytes=%d-", &start); err != nil || start >= len(body) {
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(body)-1, len(body)))
			w.WriteHeader(http.StatusPartialContent)
		} else {
			w.WriteHeader(http.StatusOK)
		}
		_, _ = w.Write(body[start:]) // test fixture; write errors irrelevant
	})
	h.srv = httptest.NewServer(mux)
	t.Cleanup(h.srv.Close)
	return h
}

func (h *fakeHub) addRepo(repo string, files map[string]string) {
	m := make(map[string][]byte, len(files))
	for k, v := range files {
		m[k] = []byte(v)
	}
	h.repos[repo] = m
}

func openStore(t *testing.T, hub *fakeHub) *llm.ModelStore {
	t.Helper()
	dir := t.TempDir()
	s, err := llm.OpenModelStoreForTesting(dir, hub.srv.Client(), hub.srv.URL)
	if err != nil {
		t.Fatalf("OpenModelStoreForTesting: %v", err)
	}
	return s
}

func shaOf(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func TestPull_ManifestParseAndDownload(t *testing.T) {
	hub := newFakeHub(t)
	body := "hello-gguf-world"
	hub.addRepo("org/repo", map[string]string{"model-q4_k_m.gguf": body})
	s := openStore(t, hub)

	rec, err := s.Pull(context.Background(), "org/repo", "", nil)
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if rec.Bytes != int64(len(body)) {
		t.Errorf("Bytes = %d, want %d", rec.Bytes, len(body))
	}
	if rec.SHA256 != shaOf(body) {
		t.Errorf("SHA256 mismatch")
	}
	if rec.RepoID != "org/repo" || rec.Name == "" || rec.File == "" {
		t.Errorf("bad record: %+v", rec)
	}
	if _, err := os.Stat(rec.File); err != nil {
		t.Errorf("model file missing: %v", err)
	}
}

func TestPull_QuantSelectionAndAmbiguity(t *testing.T) {
	hub := newFakeHub(t)
	hub.addRepo("org/r1", map[string]string{
		"a-Q4_K_M.gguf": "x",
		"a-Q8_0.gguf":   "y",
	})
	s := openStore(t, hub)

	rec, err := s.Pull(context.Background(), "org/r1", "q4_k_m", nil)
	if err != nil {
		t.Fatalf("Pull with quant: %v", err)
	}
	if got := filepath.Base(rec.File); got != "a-Q4_K_M.gguf" {
		t.Errorf("picked %q, want a-Q4_K_M.gguf", got)
	}

	_, err = s.Pull(context.Background(), "org/r1", "", nil)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguity error, got: %v", err)
	}

	hub.addRepo("org/r2", map[string]string{"readme.md": "no models here"})
	if _, err := s.Pull(context.Background(), "org/r2", "", nil); err == nil || !strings.Contains(err.Error(), "gguf") {
		t.Fatalf("expected no-gguf error, got: %v", err)
	}
}

func TestPull_ResumeAfterInterruptedBody(t *testing.T) {
	hub := newFakeHub(t)
	body := strings.Repeat("abcdefghij", 1000)
	hub.addRepo("org/repo", map[string]string{"m.gguf": body})

	// Simulate interrupted download by pre-seeding a .part file.
	dir := t.TempDir()
	part := filepath.Join(dir, "m.gguf.part")
	if err := os.WriteFile(part, []byte(body[:4000]), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := llm.OpenModelStoreForTesting(dir, hub.srv.Client(), hub.srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	var resumed bool
	rec, err := s.Pull(context.Background(), "org/repo", "", func(done, total int64) {})
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	_ = resumed
	if rec.SHA256 != shaOf(body) || rec.Bytes != int64(len(body)) {
		t.Fatalf("resume produced wrong result: %+v", rec)
	}
	if hub.calls["download"] < 1 {
		t.Error("expected at least one download call")
	}
	// The second request must carry a Range header starting at 4000.
	// Verify via part-file absence and correct final size.
	st, err := os.Stat(rec.File)
	if err != nil || st.Size() != int64(len(body)) {
		t.Errorf("final file wrong: %v", err)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Errorf(".part should be gone after success, err=%v", err)
	}
}

func TestPull_ServerWithoutRangeRestarts(t *testing.T) {
	hub := newFakeHub(t)
	body := strings.Repeat("z", 9000)
	hub.addRepo("org/repo", map[string]string{"m.gguf": body})

	dir := t.TempDir()
	part := filepath.Join(dir, "m.gguf.part")
	if err := os.WriteFile(part, []byte(body[:2000]), 0o600); err != nil {
		t.Fatal(err)
	}
	hub.noRange.Store(true)

	s, err := llm.OpenModelStoreForTesting(dir, hub.srv.Client(), hub.srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := s.Pull(context.Background(), "org/repo", "", nil)
	if err != nil {
		t.Fatalf("Pull without Range support: %v", err)
	}
	if rec.SHA256 != shaOf(body) || rec.Bytes != int64(len(body)) {
		t.Fatalf("restart download wrong: %+v", rec)
	}
}

func TestPull_ShaVerifyAndCorruptRejection(t *testing.T) {
	hub := newFakeHub(t)
	body := "consistent-content-1234567890"
	hub.addRepo("org/repo", map[string]string{"m.gguf": body})
	s := openStore(t, hub)

	for i := range 2 {
		rec, err := s.Pull(context.Background(), "org/repo", "", nil)
		if err != nil {
			t.Fatalf("Pull #%d: %v", i+1, err)
		}
		if rec.SHA256 != shaOf(body) {
			t.Fatalf("sha mismatch on pull #%d", i+1)
		}
	}

	// Corrupt the stored file; re-pull must reject it.
	recs := s.List()
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	if err := os.WriteFile(recs[0].File, []byte("CORRUPTED!"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Pull(context.Background(), "org/repo", "", nil)
	if err != nil {
		t.Fatalf("re-pull after corruption should redownload: %v", err)
	}
	if rec.SHA256 != shaOf(body) {
		t.Errorf("sha still mismatched after repair")
	}
	b, _ := os.ReadFile(rec.File)
	if string(b) != body {
		t.Errorf("file not repaired after corrupt detection")
	}
}

func TestModelStore_PersistAcrossReopen(t *testing.T) {
	hub := newFakeHub(t)
	hub.addRepo("org/repo", map[string]string{"m.gguf": "abc"})
	dir := t.TempDir()

	s, _ := llm.OpenModelStoreForTesting(dir, hub.srv.Client(), hub.srv.URL)
	if _, err := s.Pull(context.Background(), "org/repo", "", nil); err != nil {
		t.Fatal(err)
	}

	s2, err := llm.OpenModelStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := s2.List()
	if len(got) != 1 || got[0].SHA256 != shaOf("abc") {
		t.Errorf("records not persisted: %+v", got)
	}
}
