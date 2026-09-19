package builtin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/models"
)

// Evidence emission (plan 20260917-routing-repair): tool.execution.complete
// events carry the tool-issued Evidence verbatim (internal/agent/executor.go
// publishToolComplete), so transcript_fetch must attach turn-to-tool identity
// evidence — the requested URL and a content hash of the fetched text — so a
// downstream checker can verify THIS turn fetched THIS URL. Shapes follow the
// peer tools: shell.go (EvidenceProcessExit + EvidenceShellOutput with a
// sha256 hex value, subject = requested target, source = tool name) and
// file_write (EvidenceFileExists with "size=N" value).

func okRunner(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
	stdout := "{\"text\": \"hello\", \"start\": 0.0}\n{\"text\": \"world\", \"start\": 65.0}\n"
	return []byte(stdout), nil, nil
}

func TestTranscriptFetch_EvidencePresentOnSuccess(t *testing.T) {
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(okRunner)

	res, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://youtu.be/DWoJZs6TuVs",
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok {
		t.Fatalf("expected *tools.ToolResult, got %T", res)
	}
	if !tr.Success {
		t.Fatalf("expected success, got error: %s", tr.Error)
	}
	if len(tr.Evidence) == 0 {
		t.Fatalf("no evidence on success; got %+v", tr.Evidence)
	}

	var transcriptHash, fetched *models.Evidence
	for i := range tr.Evidence {
		switch tr.Evidence[i].Type {
		case models.EvidenceTranscriptHash:
			transcriptHash = &tr.Evidence[i]
		case models.EvidenceTranscriptFetched:
			fetched = &tr.Evidence[i]
		}
	}
	if fetched == nil {
		t.Fatalf("no transcript_fetched evidence; got %v", tr.Evidence)
	}
	if transcriptHash == nil {
		t.Fatalf("no transcript_hash evidence; got %v", tr.Evidence)
	}

	// Subject of BOTH items is the requested URL as the caller typed it,
	// so a checker can join evidence to the turn's tool-call arguments.
	if fetched.Subject != "https://youtu.be/DWoJZs6TuVs" {
		t.Errorf("transcript_fetched subject = %q, want requested URL", fetched.Subject)
	}
	if transcriptHash.Subject != "https://youtu.be/DWoJZs6TuVs" {
		t.Errorf("transcript_hash subject = %q, want requested URL", transcriptHash.Subject)
	}
	// Source is the tool name (peer convention: shell.go, file_write).
	if fetched.Source != "transcript_fetch" {
		t.Errorf("transcript_fetched source = %q, want transcript_fetch", fetched.Source)
	}
	if transcriptHash.Source != "transcript_fetch" {
		t.Errorf("transcript_hash source = %q, want transcript_fetch", transcriptHash.Source)
	}
	if fetched.Timestamp.IsZero() {
		t.Error("transcript_fetched timestamp is zero")
	}
	if transcriptHash.Timestamp.IsZero() {
		t.Error("transcript_hash timestamp is zero")
	}
	// fetched value carries the fetched byte count (peer convention:
	// file_exists value is "size=N").
	if fetched.Value != "chars=11" {
		t.Errorf("transcript_fetched value = %q, want chars=11 (\"hello\\nworld\")", fetched.Value)
	}
	// hash value is the sha256 hex of the fetched (pre-pagination) text,
	// mirroring shell.go's EvidenceShellOutput.
	sum := sha256.Sum256([]byte("hello\nworld"))
	if transcriptHash.Value != hex.EncodeToString(sum[:]) {
		t.Errorf("transcript_hash value = %q, want sha256 hex of %q", transcriptHash.Value, "hello\nworld")
	}
}

func TestTranscriptFetch_EvidenceHashesFullTextNotPage(t *testing.T) {
	// Paginated call: the hash must cover the FULL fetched text, not the
	// returned page, so the hash is stable across pagination windows.
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(okRunner)

	res, err := tool.Execute(context.Background(), map[string]any{
		"url":       "DWoJZs6TuVs",
		"offset":    0,
		"max_chars": 5,
	})
	if err != nil {
		t.Fatalf("Execute unexpected error: %v", err)
	}
	tr, ok := res.(*tools.ToolResult)
	if !ok {
		t.Fatalf("expected *tools.ToolResult, got %T", res)
	}
	var transcriptHash *models.Evidence
	for i := range tr.Evidence {
		if tr.Evidence[i].Type == models.EvidenceTranscriptHash {
			transcriptHash = &tr.Evidence[i]
		}
	}
	if transcriptHash == nil {
		t.Fatalf("no transcript_hash evidence; got %v", tr.Evidence)
	}
	sum := sha256.Sum256([]byte("hello\nworld"))
	if transcriptHash.Value != hex.EncodeToString(sum[:]) {
		t.Errorf("transcript_hash = %q, want hash of FULL text (not the 5-char page)", transcriptHash.Value)
	}
	// pagination evidence subject still joins to the requested URL.
	for _, ev := range tr.Evidence {
		if ev.Subject != "DWoJZs6TuVs" {
			t.Errorf("evidence subject = %q, want requested URL %q", ev.Subject, "DWoJZs6TuVs")
		}
	}
}

func TestTranscriptFetch_EvidenceEmptyOnFailure(t *testing.T) {
	tool := NewTranscriptFetchTool(TranscriptConfig{}, nil)
	tool.SetTranscriptRunner(func(ctx context.Context, name string, args []string) ([]byte, []byte, error) {
		return nil, []byte("No module named youtube_transcript_api"), errors.New("exit status 1")
	})

	res, err := tool.Execute(context.Background(), map[string]any{
		"url": "https://youtu.be/DWoJZs6TuVs",
	})
	// Failure-path convention in this tool: hard failures return
	// (nil, err) — no result envelope at all — so a checker can never
	// see evidence on a fetch that did not happen. Assert both: the
	// error surfaces AND no ToolResult/evidence exists.
	if err == nil {
		t.Fatal("expected error on failed fetch, got nil")
	}
	if res != nil {
		if tr, ok := res.(*tools.ToolResult); ok && len(tr.Evidence) > 0 {
			t.Errorf("evidence on failure = %v, want empty (never claim a fetch that did not happen)", tr.Evidence)
		}
	}
}
