package builtin

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

func newTestJournal(t *testing.T, maxEntryBytes int64) (*Journal, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "changes.db")
	cfg := JournalConfig{DBPath: dbPath}
	if maxEntryBytes > 0 {
		cfg.MaxEntryBytes = maxEntryBytes
	}
	j, err := NewJournal(cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewJournal: %v", err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return j, dbPath
}

func TestJournalRecordListRoundtrip(t *testing.T) {
	j, _ := newTestJournal(t, 0)

	now := time.Now().UTC().Truncate(time.Second)
	for i := range 3 {
		pre := []byte(fmt.Sprintf("original %d\n", i))
		post := []byte(fmt.Sprintf("modified %d\n", i))
		err := j.Record(&JournalEntry{
			SessionID: "sess-1",
			FilePath:  fmt.Sprintf("/tmp/f%d.txt", i),
			PreImage:  pre,
			PostSHA:   sha256Hex(string(post)),
			AppliedAt: now.Add(time.Duration(i) * time.Second),
			ChangeIDs: []string{id.Generate("stage-")},
		})
		if err != nil {
			t.Fatalf("Record(%d): %v", i, err)
		}
	}

	entries, err := j.List("sess-1", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	// Newest first.
	if !entries[0].AppliedAt.After(entries[2].AppliedAt) {
		t.Errorf("ordering not newest-first: %v then %v", entries[0].AppliedAt, entries[2].AppliedAt)
	}
	// List must NOT return PreImage bytes.
	for _, e := range entries {
		if e.PreImage != nil {
			t.Errorf("List returned PreImage bytes for %s (len %d)", e.ID, len(e.PreImage))
		}
		if e.PostSHA == "" || e.FilePath == "" {
			t.Errorf("entry %s missing PostSHA/FilePath", e.ID)
		}
		if len(e.ChangeIDs) == 0 {
			t.Errorf("entry %s missing ChangeIDs", e.ID)
		}
	}

	// Session filter excludes other sessions.
	other, err := j.List("sess-other", 10)
	if err != nil {
		t.Fatalf("List(other): %v", err)
	}
	if len(other) != 0 {
		t.Errorf("expected no entries for other session, got %d", len(other))
	}

	// Limit is respected (limit=1 -> single newest).
	capped, err := j.List("sess-1", 1)
	if err != nil {
		t.Fatalf("List(limit=1): %v", err)
	}
	if len(capped) != 1 {
		t.Fatalf("limit=1 returned %d entries", len(capped))
	}
	if capped[0].ID != entries[0].ID {
		t.Errorf("limit=1 got ID %s, want newest %s", capped[0].ID, entries[0].ID)
	}
}

func TestJournalRecordOversizeSkipsPreImage(t *testing.T) {
	j, _ := newTestJournal(t, 32)

	huge := strings.Repeat("x", 64)
	err := j.Record(&JournalEntry{
		SessionID: "sess-big",
		FilePath:  "/tmp/big.txt",
		PreImage:  []byte(huge),
		PostSHA:   sha256Hex("applied"),
		AppliedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Record(oversize): %v", err)
	}

	// The row exists but PreImage was dropped at write time.
	var stored sql.NullInt64
	if err := j.db.QueryRow(`SELECT LENGTH(pre_image) FROM change_journal WHERE session_id = ?`, "sess-big").Scan(&stored); err != nil {
		t.Fatalf("query pre_image: %v", err)
	}
	if stored.Valid && stored.Int64 > 0 {
		t.Errorf("oversize PreImage stored (%d bytes), want NULL/empty", stored.Int64)
	}
}

func TestJournalRevertHappyPath(t *testing.T) {
	j, _ := newTestJournal(t, 0)

	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	preImage := "line one\nline two\n"
	applied := "line ONE\nline TWO\n"

	if err := os.WriteFile(path, []byte(applied), 0o644); err != nil { //nolint:gosec // test file
		t.Fatal(err)
	}

	entry := &JournalEntry{
		ID:        id.Generate("change-"),
		SessionID: "sess-r",
		FilePath:  path,
		PreImage:  []byte(preImage),
		PostSHA:   sha256Hex(applied),
		AppliedAt: time.Now(),
	}
	if err := j.Record(entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	gotPath, err := j.Revert(entry.ID, nil)
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if gotPath != path {
		t.Errorf("Revert returned path %q, want %q", gotPath, path)
	}

	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != preImage {
		t.Errorf("revert wrote %q, want pre-image %q", data, preImage)
	}
}

func TestJournalRevertNoPreImageErrors(t *testing.T) {
	j, _ := newTestJournal(t, 16)

	entry := &JournalEntry{
		ID:        id.Generate("change-"),
		SessionID: "sess-cap",
		FilePath:  "/tmp/capped.txt",
		PreImage:  nil, // size cap skipped it
		PostSHA:   sha256Hex("whatever"),
		AppliedAt: time.Now(),
	}
	if err := j.Record(entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	_, err := j.Revert(entry.ID, nil)
	if err == nil {
		t.Fatal("expected error reverting entry without pre-image")
	}
	if !strings.Contains(err.Error(), "pre-image not journaled") || !strings.Contains(err.Error(), "size cap") {
		t.Errorf("error %q missing size-cap explanation", err)
	}
}

func TestJournalRevertDriftRefusal(t *testing.T) {
	j, _ := newTestJournal(t, 0)

	dir := t.TempDir()
	path := filepath.Join(dir, "drifted.txt")
	preImage := "before\n"
	applied := "after\n"

	if err := os.WriteFile(path, []byte("someone else edited this\n"), 0o644); err != nil { //nolint:gosec // test file
		t.Fatal(err)
	}

	entry := &JournalEntry{
		ID:        id.Generate("change-"),
		SessionID: "sess-d",
		FilePath:  path,
		PreImage:  []byte(preImage),
		PostSHA:   sha256Hex(applied),
		AppliedAt: time.Now(),
	}
	if err := j.Record(entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	_, err := j.Revert(entry.ID, nil)
	if err == nil {
		t.Fatal("expected drift refusal error")
	}
	if !strings.Contains(err.Error(), "changed since apply") {
		t.Errorf("drift error %q does not contain 'changed since apply'", err)
	}

	// File untouched by refused revert.
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "someone else edited this\n" {
		t.Errorf("refused revert modified the file: %q", data)
	}
}

func TestJournalRevertIdempotent(t *testing.T) {
	j, _ := newTestJournal(t, 0)

	dir := t.TempDir()
	path := filepath.Join(dir, "idem.txt")
	preImage := "original content\n"

	if err := os.WriteFile(path, []byte(preImage), 0o644); err != nil { //nolint:gosec // test file
		t.Fatal(err)
	}

	entry := &JournalEntry{
		ID:        id.Generate("change-"),
		SessionID: "sess-i",
		FilePath:  path,
		PreImage:  []byte(preImage),
		PostSHA:   sha256Hex("modified content\n"),
		AppliedAt: time.Now(),
	}
	if err := j.Record(entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	// First revert: current == sha256(Modified)? No — current == sha(PreImage),
	// so this is already-reverted state; both reverts must succeed without
	// rewriting and return the restored path.
	path1, err := j.Revert(entry.ID, nil)
	if err != nil {
		t.Fatalf("first Revert: %v", err)
	}
	if path1 != path {
		t.Errorf("first revert path %q, want %q", path1, path)
	}
	data, _ := os.ReadFile(path)

	path2, err := j.Revert(entry.ID, nil)
	if err != nil {
		t.Fatalf("second Revert: %v", err)
	}
	if path2 != path {
		t.Errorf("second revert path %q, want %q", path2, path)
	}
	data2, _ := os.ReadFile(path)
	if string(data) != string(data2) || string(data2) != preImage {
		t.Errorf("idempotent rewrite changed content: %q -> %q", data, data2)
	}
}

func TestJournalRevertFenceRefused(t *testing.T) {
	j, _ := newTestJournal(t, 0)

	dir := t.TempDir()
	path := filepath.Join(dir, "fenced.txt")
	preImage := "keep me\n"

	if err := os.WriteFile(path, []byte("applied content\n"), 0o644); err != nil { //nolint:gosec // test file
		t.Fatal(err)
	}

	entry := &JournalEntry{
		ID:        id.Generate("change-"),
		SessionID: "sess-f",
		FilePath:  path,
		PreImage:  []byte(preImage),
		PostSHA:   sha256Hex("applied content\n"),
		AppliedAt: time.Now(),
	}
	if err := j.Record(entry); err != nil {
		t.Fatalf("Record: %v", err)
	}

	fc := &denyingFence{}
	_, err := j.Revert(entry.ID, fc)
	if err == nil {
		t.Fatal("expected fence refusal error")
	}
	if !errors.Is(err, errFenceDenied) && !strings.Contains(err.Error(), "fence denied for test") {
		t.Errorf("fence error not propagated: %v", err)
	}

	// File must be untouched after fence refusal.
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "applied content\n" {
		t.Errorf("fence-refused revert modified file: %q", data)
	}
}

type denyingFence struct{}

func (d *denyingFence) CheckPath(path string, op string) error { return errFenceDenied }
func (d *denyingFence) CheckCommand(cmd string, workDir string) error {
	return errFenceDenied
}

func TestJournalRevertUnknownID(t *testing.T) {
	j, _ := newTestJournal(t, 0)
	_, err := j.Revert(id.Generate("change-"), nil)
	if err == nil {
		t.Fatal("expected error for unknown journal ID")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("unknown-ID error %q should mention 'not found'", err)
	}
}
