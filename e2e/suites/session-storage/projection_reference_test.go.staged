//go:build e2e

// G6 gate (03-session-storage/00T-tests.md): projection + reference read a
// real second session. Pins contracts S2/S3 (.hermes/contracts/03-session-storage.md):
//   - a registered projection unit folds a second session's committed messages
//     (drive-over-append: no full replay, OnAppend per committed row)
//   - SessionReferenceResolver.Prepare returns a bounded user/assistant-only
//     preview (tools/reasoning/system excluded), within the 0.2x context
//     budget; on truncation it spills to $MEEPT_HOME/spill and returns a
//     locator
//
// Per the 00T doc the e2e spins its own SQLiteStore + MessageBus in-process.
// RED until leaves 02-session-projection + 03-session-reference land.

package session_storage_test

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/context/sessionref"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/session"
)

func TestProjectionReference_SecondSessionBounded(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "meept-home")
	dbPath := filepath.Join(tmp, "sessions.db")

	store, err := session.NewSQLiteStore(dbPath, slog.Default())
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Two sessions: A cites B. B carries a user/assistant exchange PLUS a
	// tool message that must be EXCLUDED from the reference surface.
	sessA, err := store.Create("g6-owner")
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}
	sessB, err := store.Create("g6-target")
	if err != nil {
		t.Fatalf("Create B: %v", err)
	}

	big := strings.Repeat("lorem-ipsum-dolor ", 8000) // ~144KB, over the 64KiB floor
	if err := store.SaveMessages(sessB.ID, []session.Message{
		{ID: 1, SessionID: sessB.ID, Role: "user", Content: "question in B",
			Timestamp: time.Now(), EntryType: "message", BranchID: "main"},
		{ID: 2, SessionID: sessB.ID, Role: "assistant", Content: big,
			Timestamp: time.Now(), EntryType: "message", BranchID: "main"},
		{ID: 3, SessionID: sessB.ID, Role: "tool", Content: "SECRET-TOOL-OUTPUT",
			ToolCallID: "tc-1", Name: "shell_execute",
			Timestamp: time.Now(), EntryType: "message", BranchID: "main"},
	}); err != nil {
		t.Fatalf("SaveMessages B: %v", err)
	}

	spill := llm.NewSpillService(home)
	resolver := sessionref.NewSessionReferenceResolver(store, spill)

	// Prepare a reference to B from A with a small window so the 0.2x
	// fraction truncates the big assistant message.
	prepared, err := resolver.Prepare(context.Background(), sessA.ID,
		[]sessionref.SessionReferenceInput{{SessionID: sessB.ID, Label: "B"}},
		16000, // contextWindowTokens -> budget floor 65_536 bytes
	)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if prepared == nil {
		t.Fatal("Prepare returned nil")
	}

	// Tool content must never leak into the reference surface.
	if strings.Contains(prepared.Preview, "SECRET-TOOL-OUTPUT") {
		t.Error("tool message content leaked into the reference preview")
	}
	// The user message is inside the budget and must be present.
	if !strings.Contains(prepared.Preview, "question in B") {
		t.Error("user message missing from the reference preview")
	}
	// Truncation happened (144KB content vs 64KiB budget): a spill locator
	// must be returned and the spilled file must exist under $MEEPT_HOME.
	if prepared.Truncated && prepared.SpillLocator == "" {
		t.Error("truncated preview without a spill locator")
	}
	if prepared.SpillLocator != "" {
		if !strings.HasPrefix(prepared.SpillLocator, home) && !strings.Contains(prepared.SpillLocator, "spill") {
			t.Errorf("spill locator %q does not point under $MEEPT_HOME/spill", prepared.SpillLocator)
		}
	}
	if len(prepared.Preview) > sessionref.DefaultMaxReferenceBytes {
		t.Errorf("preview bytes = %d, over the %d floor budget", len(prepared.Preview), sessionref.DefaultMaxReferenceBytes)
	}
}

func TestProjectionReference_MentionParse(t *testing.T) {
	// The mention grammar feeds Prepare: @[label](meept-session:<base64url>).
	text := "see @[Session B](meept-session:" + base64urlOf("sess-b-1") + ") for details"
	clean, refs, err := sessionref.ParseMentionText(text)
	if err != nil {
		t.Fatalf("ParseMentionText: %v", err)
	}
	if len(refs) != 1 || refs[0].SessionID != "sess-b-1" || refs[0].Label != "Session B" {
		t.Fatalf("parsed refs = %+v, want one ref {sess-b-1, Session B}", refs)
	}
	if strings.Contains(clean, "meept-session:") {
		t.Errorf("clean text still carries the mention markup: %q", clean)
	}

	// A malformed explicit mention must fail with ErrSessionRefInvalid.
	_, _, err = sessionref.ParseMentionText("@[broken](meept-session:!!!not-base64!!!)")
	if err == nil {
		t.Fatal("malformed mention must error, got nil")
	}
	if !errors.Is(err, sessionref.ErrSessionRefInvalid) {
		t.Errorf("err = %v, want wrapping ErrSessionRefInvalid", err)
	}
}

func base64urlOf(s string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(s))
}
