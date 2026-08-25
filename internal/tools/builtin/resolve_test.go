package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var errFenceDenied = errors.New("fence denied for test")

func TestResolveTool_AcceptDriftChecking(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	sessionID := "sess-drift"
	modified := "alpha\ngamma\n"

	tests := []struct {
		name          string
		preImage      string  // content staged as Original ("" = new file)
		interimWrite  *string // what the on-disk file contains at accept time; nil = untouched since staging
		wantAccepted  bool    // change ID appears in result.Accepted
		wantFailed    bool    // change ID appears in result.Failed
		wantDisk      string  // expected on-disk content after resolve
		diskMustExist bool
		wantErrSubstr string // substring required in Message when refused
	}{
		{
			name:          "clean accept proceeds",
			preImage:      "alpha\nbeta\n",
			wantAccepted:  true,
			wantDisk:      modified,
			diskMustExist: true,
		},
		{
			name:         "drifted file refuses with changed-since-staging message",
			preImage:     "alpha\nbeta\n",
			interimWrite: strPtr("alpha\nbeta\nTAMPERED\n"),
			wantFailed:   true,
			// File must remain the drifted content, NOT be overwritten.
			wantDisk:      "alpha\nbeta\nTAMPERED\n",
			diskMustExist: true,
			wantErrSubstr: "file changed since staging",
		},
		{
			name:          "already-applied idempotent no-op succeeds",
			preImage:      "alpha\nbeta\n",
			interimWrite:  strPtr(modified),
			wantAccepted:  true,
			wantDisk:      modified,
			diskMustExist: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(dir, strings.ReplaceAll(tt.name, " ", "_")+".txt")
			if tt.preImage != "" {
				if err := os.WriteFile(path, []byte(tt.preImage), 0o644); err != nil { //nolint:gosec // test temp dir
					t.Fatal(err)
				}
			}
			reg := NewPendingChangesRegistry()
			change2, err := reg.StageWrite(sessionID, path, []byte(tt.preImage), []byte(modified))
			if err != nil {
				t.Fatalf("StageWrite failed: %v", err)
			}

			if tt.interimWrite != nil {
				if err := os.WriteFile(path, []byte(*tt.interimWrite), 0o644); err != nil { //nolint:gosec // test temp dir
					t.Fatal(err)
				}
			}

			tool := NewResolveTool(reg)
			resultAny, err := tool.Execute(ctx, map[string]any{
				"change_ids": []any{change2.ID},
				"action":     "accept",
				"session_id": sessionID,
			})
			if err != nil {
				t.Fatalf("Execute returned error: %v", err)
			}
			result, ok := resultAny.(ResolveResult)
			if !ok {
				t.Fatalf("unexpected result type %T", resultAny)
			}

			idIn := func(list []string) bool {
				for _, v := range list {
					if v == change2.ID {
						return true
					}
				}
				return false
			}

			if tt.wantAccepted && !idIn(result.Accepted) {
				t.Errorf("change not accepted; Accepted=%v Failed=%v Message=%q", result.Accepted, result.Failed, result.Message)
			}
			if tt.wantFailed && !idIn(result.Failed) {
				t.Errorf("change not reported failed; Accepted=%v Failed=%v Message=%q", result.Accepted, result.Failed, result.Message)
			}
			if tt.wantErrSubstr != "" && !strings.Contains(result.Message, tt.wantErrSubstr) {
				t.Errorf("Message %q missing refusal text %q; full message: %q", result.Message, tt.wantErrSubstr, result.Message)
			}

			data, readErr := os.ReadFile(path)
			if tt.diskMustExist {
				if readErr != nil {
					t.Fatalf("expected on-disk file, got error: %v", readErr)
				}
				if string(data) != tt.wantDisk {
					t.Errorf("on-disk content = %q, want %q", string(data), tt.wantDisk)
				}
			} else if readErr == nil {
				t.Errorf("expected file absent/empty, got content %q", string(data))
			}

			// On acceptance the change is removed from the registry; on drift
			// refusal it remains registered (recoverable, mirrors the
			// fence-refusal behaviour of keeping the change for re-resolution).
			if _, stillThere := reg.Get(change2.ID); stillThere == tt.wantAccepted {
				t.Errorf("registry presence mismatch after resolve: wantAccepted=%v, stillRegistered=%v", tt.wantAccepted, stillThere)
			}
		})
	}
}

func TestResolveTool_LegacyEmptyHashWarnsAndProceeds(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	path := filepath.Join(dir, "legacy.txt")
	original := "legacy original\n"
	modified := "legacy modified\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}

	reg := NewPendingChangesRegistry()
	now := time.Now()
	expires := now.Add(30 * time.Minute)
	change := &PendingChange{
		ID:        "edit-legacy001",
		SessionID: "sess-legacy",
		FilePath:  path,
		Original:  original,
		Modified:  modified,
		Diff:      generateUnifiedDiff(path, original, modified),
		CreatedAt: now,
		ExpiresAt: &expires,
	}
	reg.Add(change)

	tool := NewResolveTool(reg)
	resultAny, err := tool.Execute(ctx, map[string]any{
		"change_ids": []any{change.ID},
		"action":     "accept",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	result := resultAny.(ResolveResult)

	found := false
	for _, id := range result.Accepted {
		if id == change.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("legacy change not accepted; Accepted=%v Failed=%v", result.Accepted, result.Failed)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != modified {
		t.Errorf("on-disk content = %q, want %q", string(data), modified)
	}
}

func TestResolveTool_DriftRefusalCarriesShortHashes(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	path := filepath.Join(dir, "hashes.txt")
	original := "before\n"
	modified := "after\n"
	drifted := "someone else wrote this\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}

	reg := NewPendingChangesRegistry()
	change, err := reg.StageWrite("sess-h", path, []byte(original), []byte(modified))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(drifted), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}

	tool := NewResolveTool(reg)
	resultAny, err := tool.Execute(ctx, map[string]any{
		"change_ids": []any{change.ID},
		"action":     "accept",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	result := resultAny.(ResolveResult)

	msg := result.Message + " " + strings.Join(result.Failed, " ")
	h := sha256.Sum256([]byte(original))
	stagedShort := hex.EncodeToString(h[:])[:12]
	h2 := sha256.Sum256([]byte(drifted))
	currentShort := hex.EncodeToString(h2[:])[:12]
	if !strings.Contains(msg, stagedShort) {
		t.Errorf("message %q missing staged pre-image short hash %q", msg, stagedShort)
	}
	if !strings.Contains(msg, currentShort) {
		t.Errorf("message %q missing current-file short hash %q", msg, currentShort)
	}
}

func TestResolveTool_RejectUnaffectedByHash(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	path := filepath.Join(dir, "rejectme.txt")
	if err := os.WriteFile(path, []byte("orig"), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}
	reg := NewPendingChangesRegistry()
	change, err := reg.StageWrite("sess-r", path, []byte("orig"), []byte("new"))
	if err != nil {
		t.Fatal(err)
	}

	tool := NewResolveTool(reg)
	resultAny, err := tool.Execute(ctx, map[string]any{
		"change_ids": []any{change.ID},
		"action":     "reject",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	result := resultAny.(ResolveResult)
	if len(result.Rejected) != 1 || result.Rejected[0] != change.ID {
		t.Errorf("Rejected=%v, want [%s]", result.Rejected, change.ID)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "orig" {
		t.Errorf("reject must leave file untouched, got %q", string(data))
	}
}

func TestResolveTool_FenceRevalidationStillRuns(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	path := filepath.Join(dir, "fenced.txt")
	if err := os.WriteFile(path, []byte("orig"), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}
	reg := NewPendingChangesRegistry()
	change, err := reg.StageWrite("sess-f", path, []byte("orig"), []byte("new"))
	if err != nil {
		t.Fatal(err)
	}

	tool := NewResolveTool(reg)
	tool.SetFenceChecker(&refusingFenceChecker{})
	resultAny, err := tool.Execute(ctx, map[string]any{
		"change_ids": []any{change.ID},
		"action":     "accept",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	result := resultAny.(ResolveResult)
	if len(result.Failed) != 1 {
		t.Errorf("expected fence refusal to fail the change; got %+v", result)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "orig" {
		t.Errorf("fence refusal must leave file untouched, got %q", string(data))
	}
}

func strPtr(s string) *string { return &s }

func TestResolveTool_ExistingCleanAcceptStillWorks(t *testing.T) {
	// Sanity: a plain accept through StageWrite-produced change writes Modified
	// and emits no evidence regression (accept path never emitted evidence).
	dir := t.TempDir()
	ctx := context.Background()
	path := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}

	reg := NewPendingChangesRegistry()
	change, err := reg.StageWrite("sess-p", path, []byte("a\nb\n"), []byte("a\nc\n"))
	if err != nil {
		t.Fatal(err)
	}

	tool := NewResolveTool(reg)
	res, err := tool.Execute(ctx, map[string]any{"change_ids": []any{change.ID}, "action": "accept"})
	if err != nil {
		t.Fatal(err)
	}
	_ = res.(ResolveResult)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "a\nc\n" {
		t.Errorf("content = %q", string(data))
	}
}

// refusingFenceChecker always denies, used to verify fence re-validation runs
// before any write in the accept branch.
type refusingFenceChecker struct{}

func (r *refusingFenceChecker) CheckPath(string, string) error { return errFenceDenied }
func (r *refusingFenceChecker) CheckCommand(string, string) error {
	return nil
}

var _ FenceChecker = (*refusingFenceChecker)(nil)

func newJournalForResolveTest(t *testing.T) *Journal {
	t.Helper()
	j, err := NewJournal(JournalConfig{DBPath: filepath.Join(t.TempDir(), "changes.db")}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewJournal: %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return j
}

// TestResolveTool_AcceptRecordsJournal verifies the accept branch journals the
// applied change with the pre-image it already holds (Original) and the
// staged ChangeIDs, so `meept changes revert <id>` can restore it later.
func TestResolveTool_AcceptRecordsJournal(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	path := filepath.Join(dir, "journaled.txt")
	original := "original bytes\n"
	modified := "modified bytes\n"

	if err := os.WriteFile(path, []byte(original), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}
	reg := NewPendingChangesRegistry()
	change, err := reg.StageWrite("sess-j", path, []byte(original), []byte(modified))
	if err != nil {
		t.Fatal(err)
	}

	journal := newJournalForResolveTest(t)
	tool := NewResolveTool(reg)
	tool.SetJournal(journal)

	resultAny, err := tool.Execute(ctx, map[string]any{
		"change_ids": []any{change.ID},
		"action":     "accept",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	result := resultAny.(ResolveResult)
	if len(result.Accepted) != 1 {
		t.Fatalf("accept failed: %+v", result)
	}

	entries, err := journal.List("sess-j", 10)
	if err != nil {
		t.Fatalf("journal.List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly one journal entry after accept, got %d", len(entries))
	}
	entry := entries[0]
	if entry.FilePath != path {
		t.Errorf("entry FilePath=%q, want %q", entry.FilePath, path)
	}
	if entry.PostSHA != sha256Hex(modified) {
		t.Errorf("entry PostSHA=%q, want sha256(modified) %q", entry.PostSHA, sha256Hex(modified))
	}
	if len(entry.ChangeIDs) != 1 || entry.ChangeIDs[0] != change.ID {
		t.Errorf("entry ChangeIDs=%v, want [%s]", entry.ChangeIDs, change.ID)
	}
	if entry.SessionID != "sess-j" {
		t.Errorf("entry SessionID=%q, want sess-j", entry.SessionID)

	}

	// The stored pre-image equals Original — verify via direct DB read.
	var storedPre []byte
	if err := journal.db.QueryRow(`SELECT pre_image FROM change_journal WHERE id = ?`, entry.ID).Scan(&storedPre); err != nil {
		t.Fatalf("read stored pre_image: %v", err)
	}
	if string(storedPre) != original {
		t.Errorf("stored PreImage=%q, want original %q", storedPre, original)
	}
}

// TestResolveTool_AcceptLegacyEmptyHashStillRecords covers changes staged
// before integrity tracking (empty PreImageSHA256): accept proceeds and must
// still record a journal row with PostSHA computed from the written bytes.
func TestResolveTool_AcceptLegacyEmptyHashStillRecords(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	path := filepath.Join(dir, "legacy.txt")

	reg := NewPendingChangesRegistry()
	change, err := reg.StageWrite("sess-legacy", path, nil, []byte("brand new\n"))
	if err != nil {
		t.Fatal(err)
	}
	change.PreImageSHA256 = "" // simulate legacy staged change w/o hash

	journal := newJournalForResolveTest(t)
	tool := NewResolveTool(reg)
	tool.SetJournal(journal)

	resultAny, err := tool.Execute(ctx, map[string]any{
		"change_ids": []any{change.ID},
		"action":     "accept",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	result := resultAny.(ResolveResult)
	if len(result.Accepted) != 1 {
		t.Fatalf("legacy accept should succeed; got %+v", result)
	}

	entries, err := journal.List("sess-legacy", 10)
	if err != nil {
		t.Fatalf("journal.List: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one journal entry for legacy accept, got %d", len(entries))
	}
	if entries[0].PostSHA != sha256Hex("brand new\n") {
		t.Errorf("legacy entry PostSHA=%q, want sha256 of written bytes", entries[0].PostSHA)
	}
}

// TestResolveTool_NilJournalStillAccepts ensures the typed-nil guard keeps
// resolve usable without a journal (e.g. tests, fallback registries).
func TestResolveTool_NilJournalStillAccepts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nojournal.txt")
	if err := os.WriteFile(path, []byte("a"), 0o644); err != nil { //nolint:gosec // test temp dir
		t.Fatal(err)
	}
	reg := NewPendingChangesRegistry()
	change, err := reg.StageWrite("sess-nj", path, []byte("a"), []byte("b"))
	if err != nil {
		t.Fatal(err)
	}

	tool := NewResolveTool(reg)
	tool.SetJournal(nil) // typed-nil guard must swallow this

	resultAny, err := tool.Execute(context.Background(), map[string]any{
		"change_ids": []any{change.ID},
		"action":     "accept",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	result := resultAny.(ResolveResult)
	if len(result.Accepted) != 1 {
		t.Fatalf("accept without journal should succeed; got %+v", result)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "b" {
		t.Errorf("file content=%q, want modified \"b\"", data)
	}
}
