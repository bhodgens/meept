package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/tools/builtin"
)

// newChangesRPCFixture wires registerChangesRPCHandlers over a fresh RPC
// server, registry, resolve tool, and temp-dir journal.
func newChangesRPCFixture(t *testing.T) (*rpc.Server, *builtin.PendingChangesRegistry, *builtin.ResolveTool, *builtin.Journal) {
	t.Helper()
	registry := builtin.NewPendingChangesRegistry()
	resolveTool := builtin.NewResolveTool(registry)
	journal, err := builtin.NewJournal(builtin.JournalConfig{
		DBPath: filepath.Join(t.TempDir(), "changes.db"),
	}, nil)
	if err != nil {
		t.Fatalf("NewJournal: %v", err)
	}
	t.Cleanup(func() {
		if err := journal.Close(); err != nil {
			t.Errorf("journal close: %v", err)
		}
	})
	resolveTool.SetJournal(journal)

	srv := rpc.New(&rpc.Config{SocketPath: ""}, nil, nil)
	registerChangesRPCHandlers(srv, registry, resolveTool, journal)
	return srv, registry, resolveTool, journal
}

func stageRPCChange(t *testing.T, registry *builtin.PendingChangesRegistry, sessionID, original, modified string) *builtin.PendingChange {
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

func callChanges(t *testing.T, srv *rpc.Server, method, params string) (map[string]any, error) {
	t.Helper()
	result, err := srv.CallMethod(context.Background(), method, json.RawMessage(params))
	if err != nil {
		return nil, err
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("result type = %T, want map[string]any", result)
	}
	return m, nil
}

func TestChangesRPC_List(t *testing.T) {
	srv, registry, _, _ := newChangesRPCFixture(t)
	change := stageRPCChange(t, registry, "sess-rpc", "alpha\n", "alpha\nbeta\n")

	t.Run("lists staged changes with diff only", func(t *testing.T) {
		m, err := callChanges(t, srv, "changes.list", `{"session_id":"sess-rpc"}`)
		if err != nil {
			t.Fatalf("changes.list: %v", err)
		}
		items, _ := m["changes"].([]map[string]any)
		if len(items) != 1 {
			t.Fatalf("got %d changes, want 1", len(items))
		}
		item := items[0]
		if item["id"] != change.ID {
			t.Errorf("id = %v, want %s", item["id"], change.ID)
		}
		if !strings.Contains(item["diff"].(string), "+beta") {
			t.Errorf("diff = %q, want +beta", item["diff"])
		}
		if _, has := item["original"]; has {
			t.Error("response must not include original content")
		}
		if _, has := item["modified"]; has {
			t.Error("response must not include modified content")
		}
		if m["count"] != 1 {
			t.Errorf("count = %v, want 1", m["count"])
		}
	})

	t.Run("missing session_id is an error", func(t *testing.T) {
		if _, err := callChanges(t, srv, "changes.list", `{}`); err == nil {
			t.Error("expected error without session_id")
		}
	})
}

func TestChangesRPC_AcceptReject(t *testing.T) {
	tests := []struct {
		name       string
		action     string // changes.accept or changes.reject
		wantStatus string
		wantDisk   string
	}{
		{"accept applies the staged write", "changes.accept", "applied", "alpha\nbeta\n"},
		{"reject leaves the file untouched", "changes.reject", "rejected", "alpha\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, registry, _, journal := newChangesRPCFixture(t)
			change := stageRPCChange(t, registry, "sess-act", "alpha\n", "alpha\nbeta\n")

			m, err := callChanges(t, srv, tt.action, `{"id":"`+change.ID+`"}`)
			if err != nil {
				t.Fatalf("%s: %v", tt.action, err)
			}
			if m["status"] != tt.wantStatus {
				t.Errorf("status = %v, want %s", m["status"], tt.wantStatus)
			}
			if _, ok := registry.Get(change.ID); ok {
				t.Error("change still registered after resolution")
			}
			data, err := os.ReadFile(change.FilePath) //nolint:gosec // test temp dir
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != tt.wantDisk {
				t.Errorf("on-disk = %q, want %q", string(data), tt.wantDisk)
			}

			if tt.action == "changes.accept" {
				entries, err := journal.List("sess-act", 10)
				if err != nil || len(entries) != 1 {
					t.Errorf("journal entries = %d (err %v), want 1", len(entries), err)
				}
			}
		})
	}

	t.Run("accept drift surfaces sentinel error text", func(t *testing.T) {
		srv, registry, _, _ := newChangesRPCFixture(t)
		change := stageRPCChange(t, registry, "sess-drift", "alpha\n", "alpha\nbeta\n")
		if err := os.WriteFile(change.FilePath, []byte("TAMPERED\n"), 0o644); err != nil { //nolint:gosec // test temp dir
			t.Fatal(err)
		}
		_, err := callChanges(t, srv, "changes.accept", `{"id":"`+change.ID+`"}`)
		if err == nil {
			t.Fatal("expected drift error")
		}
		if !strings.Contains(err.Error(), "file changed since staging") {
			t.Errorf("error = %v, want drift sentinel text", err)
		}
		if _, ok := registry.Get(change.ID); !ok {
			t.Error("drift refusal must keep the change staged")
		}
	})

	t.Run("unknown id returns not-found error", func(t *testing.T) {
		srv, _, _, _ := newChangesRPCFixture(t)
		for _, method := range []string{"changes.accept", "changes.reject"} {
			if _, err := callChanges(t, srv, method, `{"id":"nope"}`); err == nil {
				t.Errorf("%s: expected error for unknown id", method)
			}
		}
	})
}

func TestChangesRPC_Journal(t *testing.T) {
	srv, registry, resolveTool, _ := newChangesRPCFixture(t)
	change := stageRPCChange(t, registry, "sess-j", "alpha\n", "alpha\nbeta\n")
	if _, err := resolveTool.AcceptChange(change.ID); err != nil {
		t.Fatalf("AcceptChange: %v", err)
	}

	t.Run("lists entries without pre_image bytes", func(t *testing.T) {
		m, err := callChanges(t, srv, "changes.journal", `{"session_id":"sess-j","limit":10}`)
		if err != nil {
			t.Fatalf("changes.journal: %v", err)
		}
		items, _ := m["entries"].([]map[string]any)
		if len(items) != 1 {
			t.Fatalf("got %d entries, want 1", len(items))
		}
		entry := items[0]
		for _, key := range []string{"id", "session_id", "file_path", "post_sha", "applied_at", "change_ids", "pre_image_size"} {
			if _, has := entry[key]; !has {
				t.Errorf("entry missing %q", key)
			}
		}
		if _, has := entry["pre_image"]; has {
			t.Error("entry must not include pre_image bytes")
		}
		if entry["pre_image_size"].(int64) != 6 {
			t.Errorf("pre_image_size = %v, want 6", entry["pre_image_size"])
		}
		ids, _ := entry["change_ids"].([]string)
		if len(ids) != 1 || ids[0] != change.ID {
			t.Errorf("change_ids = %v, want [%s]", entry["change_ids"], change.ID)
		}
	})

	t.Run("revert restores the pre-image", func(t *testing.T) {
		m, err := callChanges(t, srv, "changes.journal", `{"session_id":"sess-j"}`)
		if err != nil {
			t.Fatalf("changes.journal: %v", err)
		}
		entries, _ := m["entries"].([]map[string]any)
		if len(entries) == 0 {
			t.Fatal("no journal entries listed")
		}
		id := entries[0]["id"].(string)

		res, err := callChanges(t, srv, "changes.revert", `{"id":"`+id+`"}`)
		if err != nil {
			t.Fatalf("changes.revert: %v", err)
		}
		if res["restored_path"] != change.FilePath {
			t.Errorf("restored_path = %v, want %s", res["restored_path"], change.FilePath)
		}
		data, err := os.ReadFile(change.FilePath) //nolint:gosec // test temp dir
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "alpha\n" {
			t.Errorf("on-disk = %q, want pre-image", string(data))
		}
	})
}

func TestChangesRPC_RevertDrift(t *testing.T) {
	srv, registry, resolveTool, _ := newChangesRPCFixture(t)
	change := stageRPCChange(t, registry, "sess-rd", "alpha\n", "alpha\nbeta\n")
	if _, err := resolveTool.AcceptChange(change.ID); err != nil {
		t.Fatalf("AcceptChange: %v", err)
	}
	if err := os.WriteFile(change.FilePath, []byte("edited since apply\n"), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}

	m, err := callChanges(t, srv, "changes.journal", `{"session_id":"sess-rd"}`)
	if err != nil {
		t.Fatalf("changes.journal: %v", err)
	}
	entries, _ := m["entries"].([]map[string]any)
	if len(entries) == 0 {
		t.Fatal("no journal entries listed")
	}
	id := entries[0]["id"].(string)

	if _, err := callChanges(t, srv, "changes.revert", `{"id":"`+id+`"}`); err == nil {
		t.Fatal("expected drift refusal")
	} else if !strings.Contains(err.Error(), "changed since apply") {
		t.Errorf("error = %v, want drift text", err)
	}

	data, err := os.ReadFile(change.FilePath) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "edited since apply\n" {
		t.Errorf("refused revert modified the file: %q", string(data))
	}
}

func TestChangesRPC_NilGuards(t *testing.T) {
	// nil registry: nothing registered, calls hit "method not found".
	srv := rpc.New(&rpc.Config{SocketPath: ""}, nil, nil)
	registerChangesRPCHandlers(srv, nil, nil, nil)
	if _, err := srv.CallMethod(context.Background(), "changes.list", json.RawMessage(`{}`)); err == nil {
		t.Error("expected method-not-found when registry is nil")
	}

	// nil journal: list/accept/reject work, journal methods absent.
	registry := builtin.NewPendingChangesRegistry()
	resolveTool := builtin.NewResolveTool(registry)
	srv2 := rpc.New(&rpc.Config{SocketPath: ""}, nil, nil)
	registerChangesRPCHandlers(srv2, registry, resolveTool, nil)
	if _, err := srv2.CallMethod(context.Background(), "changes.journal", json.RawMessage(`{}`)); err == nil {
		t.Error("expected method-not-found when journal is nil")
	}
	if _, err := srv2.CallMethod(context.Background(), "changes.list", json.RawMessage(`{"session_id":"s"}`)); err != nil {
		t.Errorf("changes.list with nil journal: %v", err)
	}
}
