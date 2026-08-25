package http

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/tools/builtin"
)

// newChangesTestServer builds a bare Server with the changes API wired via
// WithChangesAPI. Routes are registered through setupRoutes + setupRESTRoutes
// (the same path Start() takes) so handlers run behind Go 1.22 method+path
// pattern matching.
func newChangesTestServer(t *testing.T, registry *builtin.PendingChangesRegistry, resolveTool *builtin.ResolveTool, journal *builtin.Journal) (*Server, *http.ServeMux) {
	t.Helper()
	s := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithChangesAPI(registry, resolveTool, journal))
	mux := http.NewServeMux()
	s.setupRoutes(mux)
	s.setupRESTRoutes(mux)
	t.Cleanup(func() {
		if journal != nil {
			if err := journal.Close(); err != nil {
				t.Errorf("journal close: %v", err)
			}
		}
	})
	return s, mux
}

func newTestJournal(t *testing.T) *builtin.Journal {
	t.Helper()
	j, err := builtin.NewJournal(builtin.JournalConfig{
		DBPath: filepath.Join(t.TempDir(), "changes.db"),
	}, nil)
	if err != nil {
		t.Fatalf("NewJournal: %v", err)
	}
	return j
}

// stageChange stages a file write through the registry against a real temp
// file (written with the original content) and returns the change.
func stageChange(t *testing.T, registry *builtin.PendingChangesRegistry, sessionID, original, modified string) *builtin.PendingChange {
	t.Helper()
	path := filepath.Join(t.TempDir(), "file.txt")
	if original != "" {
		if err := os.WriteFile(path, []byte(original), 0o644); err != nil { //nolint:gosec // test temp dir
			t.Fatalf("write staged file: %v", err)
		}
	}
	change, err := registry.StageWrite(sessionID, path, []byte(original), []byte(modified))
	if err != nil {
		t.Fatalf("StageWrite: %v", err)
	}
	return change
}

func doChangesRequest(t *testing.T, mux *http.ServeMux, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestHandleListPendingChanges(t *testing.T) {
	registry := builtin.NewPendingChangesRegistry()
	resolveTool := builtin.NewResolveTool(registry)
	_, mux := newChangesTestServer(t, registry, resolveTool, nil)

	t.Run("empty list is an empty array", func(t *testing.T) {
		w := doChangesRequest(t, mux, http.MethodGet, "/api/v1/sessions/sess-empty/pending-changes")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if got := strings.TrimSpace(w.Body.String()); got != "[]" {
			t.Errorf("body = %s, want []", got)
		}
	})

	t.Run("lists changes with contract fields and no content", func(t *testing.T) {
		change := stageChange(t, registry, "sess-list", "alpha\n", "alpha\nbeta\n")
		w := doChangesRequest(t, mux, http.MethodGet, "/api/v1/sessions/sess-list/pending-changes")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		var items []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("got %d items, want 1", len(items))
		}
		item := items[0]
		if item["id"] != change.ID {
			t.Errorf("id = %v, want %s", item["id"], change.ID)
		}
		if item["file_path"] != change.FilePath {
			t.Errorf("file_path = %v, want %s", item["file_path"], change.FilePath)
		}
		diff, _ := item["diff"].(string)
		if !strings.Contains(diff, "+beta") {
			t.Errorf("diff = %q, want it to contain +beta", diff)
		}
		if item["created_at"] == nil || item["created_at"] == "" {
			t.Error("created_at missing or empty")
		}
		if item["expires_at"] == nil || item["expires_at"] == "" {
			t.Error("expires_at missing or empty")
		}
		// Full file content must NOT leak through the list endpoint.
		if _, hasOriginal := item["original"]; hasOriginal {
			t.Error("response must not include original content")
		}
		if _, hasModified := item["modified"]; hasModified {
			t.Error("response must not include modified content")
		}
	})
}

func TestHandleAcceptPendingChange(t *testing.T) {
	t.Run("clean accept applies content and empties registry", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		journal := newTestJournal(t)
		resolveTool.SetJournal(journal)
		_, mux := newChangesTestServer(t, registry, resolveTool, journal)

		change := stageChange(t, registry, "sess-accept", "alpha\n", "alpha\nbeta\n")
		w := doChangesRequest(t, mux, http.MethodPost, "/api/v1/pending-changes/"+change.ID+"/accept")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["status"] != "applied" {
			t.Errorf("status = %q, want applied", body["status"])
		}

		data, err := os.ReadFile(change.FilePath) //nolint:gosec // test temp dir
		if err != nil {
			t.Fatalf("read applied file: %v", err)
		}
		if string(data) != "alpha\nbeta\n" {
			t.Errorf("on-disk = %q, want modified content", string(data))
		}
		if _, ok := registry.Get(change.ID); ok {
			t.Error("change still registered after accept")
		}

		// Acceptance must be journaled so the write can be reverted.
		entries, err := journal.List("sess-accept", 10)
		if err != nil {
			t.Fatalf("journal list: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("journal entries = %d, want 1", len(entries))
		}
	})

	t.Run("drifted file returns 409 and keeps the change staged", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		_, mux := newChangesTestServer(t, registry, resolveTool, nil)

		change := stageChange(t, registry, "sess-drift", "alpha\n", "alpha\nbeta\n")
		// External mutation between staging and accept.
		if err := os.WriteFile(change.FilePath, []byte("TAMPERED\n"), 0o644); err != nil { //nolint:gosec // test temp dir
			t.Fatal(err)
		}
		w := doChangesRequest(t, mux, http.MethodPost, "/api/v1/pending-changes/"+change.ID+"/accept")
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409", w.Code)
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !strings.Contains(body["error"], "file changed since staging") {
			t.Errorf("error = %q, want drift message", body["error"])
		}
		// Refused accept keeps the change recoverable in the registry.
		if _, ok := registry.Get(change.ID); !ok {
			t.Error("change removed from registry despite drift refusal")
		}
		// File untouched by the refused accept.
		data, err := os.ReadFile(change.FilePath) //nolint:gosec // test temp dir
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "TAMPERED\n" {
			t.Errorf("on-disk = %q, want TAMPERED", string(data))
		}
	})

	t.Run("unknown id returns 404", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		_, mux := newChangesTestServer(t, registry, resolveTool, nil)

		w := doChangesRequest(t, mux, http.MethodPost, "/api/v1/pending-changes/no-such-id/accept")
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})
}

func TestHandleRejectPendingChange(t *testing.T) {
	t.Run("reject removes change and leaves file untouched", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		_, mux := newChangesTestServer(t, registry, resolveTool, nil)

		change := stageChange(t, registry, "sess-reject", "alpha\n", "alpha\nbeta\n")
		w := doChangesRequest(t, mux, http.MethodPost, "/api/v1/pending-changes/"+change.ID+"/reject")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["status"] != "rejected" {
			t.Errorf("status = %q, want rejected", body["status"])
		}
		if _, ok := registry.Get(change.ID); ok {
			t.Error("change still registered after reject")
		}
		data, err := os.ReadFile(change.FilePath) //nolint:gosec // test temp dir
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "alpha\n" {
			t.Errorf("on-disk = %q, want original untouched", string(data))
		}
	})

	t.Run("unknown id returns 404", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		_, mux := newChangesTestServer(t, registry, resolveTool, nil)

		w := doChangesRequest(t, mux, http.MethodPost, "/api/v1/pending-changes/no-such-id/reject")
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})
}

func TestHandleListJournal(t *testing.T) {
	t.Run("lists entries without pre_image bytes", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		journal := newTestJournal(t)
		resolveTool.SetJournal(journal)
		_, mux := newChangesTestServer(t, registry, resolveTool, journal)

		change := stageChange(t, registry, "sess-journal", "alpha\n", "alpha\nbeta\n")
		if _, err := resolveTool.AcceptChange(change.ID); err != nil {
			t.Fatalf("AcceptChange: %v", err)
		}

		w := doChangesRequest(t, mux, http.MethodGet, "/api/v1/changes/journal?session=sess-journal&limit=10")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		var items []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("got %d entries, want 1", len(items))
		}
		entry := items[0]
		if entry["session_id"] != "sess-journal" {
			t.Errorf("session_id = %v", entry["session_id"])
		}
		if entry["file_path"] == nil || entry["file_path"] == "" {
			t.Error("file_path missing")
		}
		if entry["post_sha"] == nil || entry["post_sha"] == "" {
			t.Error("post_sha missing")
		}
		if entry["applied_at"] == nil || entry["applied_at"] == "" {
			t.Error("applied_at missing")
		}
		ids, _ := entry["change_ids"].([]any)
		if len(ids) != 1 || ids[0] != change.ID {
			t.Errorf("change_ids = %v, want [%s]", entry["change_ids"], change.ID)
		}
		if size, ok := entry["pre_image_size"].(float64); !ok || size != 6 {
			t.Errorf("pre_image_size = %v, want 6", entry["pre_image_size"])
		}
		if _, hasPreImage := entry["pre_image"]; hasPreImage {
			t.Error("response must not include pre_image bytes")
		}
	})

	t.Run("limit parameter is honoured", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		journal := newTestJournal(t)
		resolveTool.SetJournal(journal)
		_, mux := newChangesTestServer(t, registry, resolveTool, journal)

		for i := range 3 {
			change := stageChange(t, registry, "sess-limit", "v"+strconv.Itoa(i)+"\n", "v"+strconv.Itoa(i)+"b\n")
			if _, err := resolveTool.AcceptChange(change.ID); err != nil {
				t.Fatalf("AcceptChange %d: %v", i, err)
			}
		}

		w := doChangesRequest(t, mux, http.MethodGet, "/api/v1/changes/journal?session=sess-limit&limit=2")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		var items []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(items) != 2 {
			t.Errorf("got %d entries, want 2", len(items))
		}
	})

	t.Run("unknown session returns empty array", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		journal := newTestJournal(t)
		_, mux := newChangesTestServer(t, registry, resolveTool, journal)

		w := doChangesRequest(t, mux, http.MethodGet, "/api/v1/changes/journal?session=nope")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		if got := strings.TrimSpace(w.Body.String()); got != "[]" {
			t.Errorf("body = %s, want []", got)
		}
	})

	t.Run("malformed limit falls back to default", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		journal := newTestJournal(t)
		_, mux := newChangesTestServer(t, registry, resolveTool, journal)

		w := doChangesRequest(t, mux, http.MethodGet, "/api/v1/changes/journal?limit=abc")
		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want 200", w.Code)
		}
	})
}

func TestHandleRevertJournalEntry(t *testing.T) {
	t.Run("clean revert restores pre-image", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		journal := newTestJournal(t)
		resolveTool.SetJournal(journal)
		_, mux := newChangesTestServer(t, registry, resolveTool, journal)

		change := stageChange(t, registry, "sess-revert", "alpha\n", "alpha\nbeta\n")
		if _, err := resolveTool.AcceptChange(change.ID); err != nil {
			t.Fatalf("AcceptChange: %v", err)
		}
		entries, err := journal.List("sess-revert", 10)
		if err != nil || len(entries) != 1 {
			t.Fatalf("journal list: entries=%d err=%v", len(entries), err)
		}

		w := doChangesRequest(t, mux, http.MethodPost, "/api/v1/changes/journal/"+entries[0].ID+"/revert")
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if body["restored_path"] != change.FilePath {
			t.Errorf("restored_path = %q, want %q", body["restored_path"], change.FilePath)
		}
		data, err := os.ReadFile(change.FilePath) //nolint:gosec // test temp dir
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "alpha\n" {
			t.Errorf("on-disk = %q, want pre-image alpha", string(data))
		}
	})

	t.Run("drifted file returns 409", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		journal := newTestJournal(t)
		resolveTool.SetJournal(journal)
		_, mux := newChangesTestServer(t, registry, resolveTool, journal)

		change := stageChange(t, registry, "sess-revert-drift", "alpha\n", "alpha\nbeta\n")
		if _, err := resolveTool.AcceptChange(change.ID); err != nil {
			t.Fatalf("AcceptChange: %v", err)
		}
		entries, err := journal.List("sess-revert-drift", 10)
		if err != nil || len(entries) != 1 {
			t.Fatalf("journal list: entries=%d err=%v", len(entries), err)
		}
		// External mutation after apply.
		if err := os.WriteFile(change.FilePath, []byte("edited since apply\n"), 0o644); err != nil { //nolint:gosec // test temp dir
			t.Fatal(err)
		}

		w := doChangesRequest(t, mux, http.MethodPost, "/api/v1/changes/journal/"+entries[0].ID+"/revert")
		if w.Code != http.StatusConflict {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !strings.Contains(body["error"], "changed since apply") {
			t.Errorf("error = %q, want drift message", body["error"])
		}
		data, err := os.ReadFile(change.FilePath) //nolint:gosec // test temp dir
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "edited since apply\n" {
			t.Errorf("refused revert modified the file: %q", string(data))
		}
	})

	t.Run("size-capped entry returns 400", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		journal := newTestJournal(t)
		_, mux := newChangesTestServer(t, registry, resolveTool, journal)

		// A pre-image larger than the journal cap is stored without bytes.
		big := bytes.Repeat([]byte("x"), 2<<20) // 2 MiB > 1 MiB default cap
		path := filepath.Join(t.TempDir(), "big.txt")
		if err := os.WriteFile(path, big, 0o644); err != nil { //nolint:gosec // test temp dir
			t.Fatal(err)
		}
		entry := &builtin.JournalEntry{
			SessionID: "sess-cap",
			FilePath:  path,
			PreImage:  big,
			PostSHA:   "unused",
		}
		if err := journal.Record(entry); err != nil {
			t.Fatalf("Record: %v", err)
		}

		w := doChangesRequest(t, mux, http.MethodPost, "/api/v1/changes/journal/"+entry.ID+"/revert")
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !strings.Contains(body["error"], "pre-image") {
			t.Errorf("error = %q, want size-cap explanation", body["error"])
		}
	})

	t.Run("unknown id returns 404", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		journal := newTestJournal(t)
		_, mux := newChangesTestServer(t, registry, resolveTool, journal)

		w := doChangesRequest(t, mux, http.MethodPost, "/api/v1/changes/journal/no-such-id/revert")
		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})

	t.Run("missing journal returns 503", func(t *testing.T) {
		registry := builtin.NewPendingChangesRegistry()
		resolveTool := builtin.NewResolveTool(registry)
		_, mux := newChangesTestServer(t, registry, resolveTool, nil)

		w := doChangesRequest(t, mux, http.MethodPost, "/api/v1/changes/journal/any-id/revert")
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("status = %d, want 503", w.Code)
		}
	})
}

func TestChangesRoutesGatedByREST(t *testing.T) {
	registry := builtin.NewPendingChangesRegistry()
	resolveTool := builtin.NewResolveTool(registry)
	s := NewServer(ServerConfig{}, nil, nil, nil, nil, nil, WithChangesAPI(registry, resolveTool, nil))
	s.config.RESTEnabled = false // same default the daemon reads from transport.http.rest

	mux := http.NewServeMux()
	s.setupRoutes(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions/s/pending-changes", http.NoBody)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when REST disabled", w.Code)
	}
}
