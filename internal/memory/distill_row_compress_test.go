package memory

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/queue"
)

// Row-based L1 compress pins (H7/A3-1, owner ruling D3). Before the
// fix, the tierpolicy-produced payload (no L0Path) failed HERE with
// "distill: l1 payload missing l0_path" - non-vacuity: delete the
// MemoryIDs branch in handleL1Extract and every test in this file
// fails with exactly that error.

// storeCompressRow writes one episodic row and returns its ID.
func storeCompressRow(t *testing.T, mgr *Manager, content string) string {
	t.Helper()
	id, err := mgr.Episodic().Store(context.Background(), content, "conversation", nil)
	if err != nil {
		t.Fatalf("store row: %v", err)
	}
	return id
}

// claimOnly filters a search result slice to true claim rows (the FTS
// Type filter matches the claim text against ANY row type, so the
// source episodic rows can ride back with the claims).
func claimOnly(results []MemoryResult) []MemoryResult {
	out := make([]MemoryResult, 0, len(results))
	for _, r := range results {
		if r.Memory.Type == MemoryTypeClaim {
			out = append(out, r)
		}
	}
	return out
}

// newRowCompressWorker builds a DistillWorker over a real SQLite
// manager with a scripted extraction LLM.
func newRowCompressWorker(t *testing.T, mgr *Manager, llm *fakeDistillLLM) *DistillWorker {
	t.Helper()
	return NewDistillWorker(DistillWorkerConfig{
		Manager: mgr,
		LLM:     llm,
		DataDir: t.TempDir(),
		Logger:  slog.New(slog.DiscardHandler),
	})
}

func TestL1RowCompressDistillsExactIDs(t *testing.T) {
	mgr := newDistillTestManager(t)
	ctx := context.Background()
	idA := storeCompressRow(t, mgr, "alpha row content about project alpha")
	idB := storeCompressRow(t, mgr, "beta row content about project beta")
	idC := storeCompressRow(t, mgr, "charlie row NOT selected for compress")

	llm := &fakeDistillLLM{script: []fakeDistillEntry{{
		match:    "alpha row content",
		response: candidateJSON("user works on project alpha"),
	}}}
	w := newRowCompressWorker(t, mgr, llm)

	// Engine-produced payload shape (tierpolicy enqueueCompress):
	// SessionID is the tierpolicy pseudo-scope, MemoryIDs the exact
	// planCompress TargetIDs, L0Path EMPTY.
	payload := L1ExtractPayload{
		SessionID: "tierpolicy:episodic",
		MemoryIDs: []string{idA, idB},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := w.Process(ctx, &queue.Job{Type: MemoryJobL1Extract, Payload: raw})
	if err != nil {
		t.Fatalf("Process row-shape job (pre-fix this failed with 'missing l0_path'): %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("result type %T", res)
	}
	if m["status"] != "ok" {
		t.Fatalf("status = %v, want ok", m["status"])
	}
	if m["rows_distilled"] != 2 {
		t.Fatalf("rows_distilled = %v, want 2", m["rows_distilled"])
	}

	// The prompt saw EXACTLY the selected rows (D3: "the worker
	// compresses exactly those rows").
	llm.mu.Lock()
	prompt := llm.prompt
	llm.mu.Unlock()
	if !strings.Contains(prompt, "alpha row content") || !strings.Contains(prompt, "beta row content") {
		t.Fatalf("prompt missing selected rows: %q", prompt)
	}
	if strings.Contains(prompt, "charlie row") {
		t.Fatalf("prompt included an UNselected row: %q", prompt)
	}

	// The claim landed.
	claims, err := mgr.ListAutoClaims(ctx, time.Time{}, 10)
	if err != nil {
		t.Fatalf("list claims: %v", err)
	}
	if len(claims) == 0 {
		t.Fatal("expected a stored claim from the row distill")
	}

	// Source rows flagged; the unselected row untouched.
	for _, id := range []string{idA, idB} {
		r, err := mgr.Episodic().GetByID(ctx, id)
		if err != nil {
			t.Fatalf("get %s: %v", id, err)
		}
		if !metaStringFlag(r.Memory.Metadata, RowCompressedKey) {
			t.Fatalf("row %s not flagged %s after distill", id, RowCompressedKey)
		}
	}
	rc, err := mgr.Episodic().GetByID(ctx, idC)
	if err != nil {
		t.Fatalf("get C: %v", err)
	}
	if metaStringFlag(rc.Memory.Metadata, RowCompressedKey) {
		t.Fatal("unselected row must not be flagged")
	}
}

func TestL1RowCompressReplaySuppressesNoReEmit(t *testing.T) {
	mgr := newDistillTestManager(t)
	ctx := context.Background()
	idA := storeCompressRow(t, mgr, "alpha row content about project alpha")
	idB := storeCompressRow(t, mgr, "beta row content about project beta")

	llm := &fakeDistillLLM{script: []fakeDistillEntry{{
		match:    "alpha row content",
		response: candidateJSON("user works on project alpha"),
	}}}
	w := newRowCompressWorker(t, mgr, llm)

	raw, err := json.Marshal(L1ExtractPayload{SessionID: "tierpolicy:episodic", MemoryIDs: []string{idA, idB}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Process(ctx, &queue.Job{Type: MemoryJobL1Extract, Payload: raw}); err != nil {
		t.Fatalf("first Process: %v", err)
	}
	before := llm.calls
	// Queue retry / duplicate enqueue: flagged rows must NOT re-emit
	// claims (row-shape idempotence replaces the marker suppression).
	res, err := w.Process(ctx, &queue.Job{Type: MemoryJobL1Extract, Payload: raw})
	if err != nil {
		t.Fatalf("second Process: %v", err)
	}
	m := res.(map[string]any)
	if m["status"] != "suppressed" {
		t.Fatalf("status = %v, want suppressed", m["status"])
	}
	if llm.calls != before {
		t.Fatalf("replay made an LLM call (%d -> %d), want suppression", before, llm.calls)
	}
	claims, err := mgr.Search(ctx, MemoryQuery{Type: MemoryTypeClaim, Query: "project alpha", Limit: 10})
	if err != nil {
		t.Fatalf("search claims: %v", err)
	}
	claims = claimOnly(claims)
	if len(claims) != 1 {
		t.Fatalf("claims after replay = %d, want 1 (no re-emit)", len(claims))
	}
}

func TestL1RowCompressSkipsMissingRows(t *testing.T) {
	mgr := newDistillTestManager(t)
	ctx := context.Background()
	idA := storeCompressRow(t, mgr, "alpha row content about project alpha")
	deleted, err := mgr.Episodic().DeleteByIDs(ctx, []string{func() string {
		return storeCompressRow(t, mgr, "doomed row content")
	}()})
	if err != nil || deleted != 1 {
		t.Fatalf("setup delete: %d %v", deleted, err)
	}

	llm := &fakeDistillLLM{script: []fakeDistillEntry{{
		match:    "alpha row content",
		response: candidateJSON("user works on project alpha"),
	}}}
	w := newRowCompressWorker(t, mgr, llm)
	raw, _ := json.Marshal(L1ExtractPayload{SessionID: "tierpolicy:episodic", MemoryIDs: []string{idA, "nonexistent-id"}})
	res, err := w.Process(ctx, &queue.Job{Type: MemoryJobL1Extract, Payload: raw})
	if err != nil {
		t.Fatalf("missing row must skip, not fail: %v", err)
	}
	m := res.(map[string]any)
	if m["status"] != "ok" || m["rows_missing"] != 1 || m["rows_distilled"] != 1 {
		t.Fatalf("result = %+v, want ok/missing=1/distilled=1", m)
	}
}

// TestL1DayFileShapeByteIdentical: the daemon capture payload shape
// (memory_capture.go:316 - SessionID+L0Path+SinceMsgID, no MemoryIDs)
// keeps the marker contract EXACTLY: it marshals without the new key
// and processes through the day-file window path, advancing the
// per-session l0_progress marker. The row shape never touches markers.
func TestL1DayFileShapeByteIdentical(t *testing.T) {
	mgr := newDistillTestManager(t)
	ctx := context.Background()
	session := "sess-dayfile-pin"
	dataDir, l0Path, recs := writeL0Fixture(t, t.TempDir(), session, 4)

	// Frozen marshal shape: legacy payload JSON must not carry memory_ids.
	raw, err := json.Marshal(L1ExtractPayload{SessionID: session, L0Path: l0Path, SinceMsgID: recs[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "memory_ids") {
		t.Fatalf("legacy payload gained a key: %s", raw)
	}

	llm := &fakeDistillLLM{script: []fakeDistillEntry{{
		match:    "turn 2 about project alpha",
		response: candidateJSON("day file claim"),
	}}}
	w := NewDistillWorker(DistillWorkerConfig{Manager: mgr, LLM: llm, DataDir: dataDir, Logger: slog.New(slog.DiscardHandler)})
	res, err := w.Process(ctx, &queue.Job{Type: MemoryJobL1Extract, Payload: raw})
	if err != nil {
		t.Fatalf("day-file shape Process: %v", err)
	}
	m := res.(map[string]any)
	if m["status"] != "ok" {
		t.Fatalf("status = %v", m["status"])
	}
	if m["l0_progress"] != recs[3].ID {
		t.Fatalf("marker = %v, want %v (day-file shape must advance the marker)", m["l0_progress"], recs[3].ID)
	}
	// Window semantics intact: only lines strictly after SinceMsgID.
	llm.mu.Lock()
	prompt := llm.prompt
	llm.mu.Unlock()
	if strings.Contains(prompt, "turn 0") || strings.Contains(prompt, "turn 1") {
		t.Fatalf("day-file window read pre-since lines: %q", prompt)
	}

	// The row shape must NOT have written any marker for its
	// pseudo-session (suppression contract stays day-file-only).
	idA := storeCompressRow(t, mgr, "alpha row content about project alpha")
	raw2, _ := json.Marshal(L1ExtractPayload{SessionID: "tierpolicy:working", MemoryIDs: []string{idA}})
	w2 := NewDistillWorker(DistillWorkerConfig{Manager: mgr, LLM: &fakeDistillLLM{script: []fakeDistillEntry{{match: "alpha row", response: candidateJSON("row claim")}}}, DataDir: dataDir, Logger: slog.New(slog.DiscardHandler)})
	if _, err := w2.Process(ctx, &queue.Job{Type: MemoryJobL1Extract, Payload: raw2}); err != nil {
		t.Fatalf("row shape Process: %v", err)
	}
	if mk := readMarker(t, w2, "tierpolicy:working"); mk != "" {
		t.Fatalf("row shape wrote l0 marker %q - marker logic must be skipped for this shape", mk)
	}
	// Real session marker untouched by the row job.
	if mk := readMarker(t, w2, session); mk != recs[3].ID {
		t.Fatalf("row job clobbered the real session marker: %q", mk)
	}
}

// TestL1PayloadRejectsEmptyBothShapes: no L0Path AND no MemoryIDs is
// still the honest rejection (error text unchanged - capture-path
// telemetry and queue dead-letter matching depend on it).
func TestL1PayloadRejectsEmptyBothShapes(t *testing.T) {
	mgr := newDistillTestManager(t)
	w := NewDistillWorker(DistillWorkerConfig{Manager: mgr, DataDir: t.TempDir(), Logger: slog.New(slog.DiscardHandler)})
	raw, _ := json.Marshal(L1ExtractPayload{SessionID: "s"})
	_, err := w.Process(context.Background(), &queue.Job{Type: MemoryJobL1Extract, Payload: raw})
	if err == nil || !strings.Contains(err.Error(), "missing l0_path") {
		t.Fatalf("want 'missing l0_path' rejection, got %v", err)
	}
}
