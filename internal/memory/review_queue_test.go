package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeVerdictAuditor records RecordVerdict calls and optionally fails them.
// The call log pins call-before-return semantics: verdictClaim must invoke
// the auditor BEFORE the status transition writes, so a failure aborts the
// verdict with the status untouched.
type fakeVerdictAuditor struct {
	mu     sync.Mutex
	calls  []verdictCall
	err    error
	t      *testing.T
	mgr    *Manager
	status []string // status observed by the auditor at call time (per call)
}

type verdictCall struct {
	claimID    string
	verdict    string
	reasonHash string
}

func (f *fakeVerdictAuditor) RecordVerdict(claimID, verdict, reasonHash string) error {
	f.mu.Lock()
	f.calls = append(f.calls, verdictCall{claimID: claimID, verdict: verdict, reasonHash: reasonHash})
	f.mu.Unlock()
	if f.mgr != nil {
		mem, err := f.mgr.GetByID(context.Background(), claimID)
		if err == nil && mem != nil {
			f.mu.Lock()
			f.status = append(f.status, asString(mem.Metadata["status"]))
			f.mu.Unlock()
		}
	}
	return f.err
}

func TestVerdictAuditor_CallBeforeReturn(t *testing.T) {
	m := newTestManager(t)
	aud := &fakeVerdictAuditor{t: t, mgr: m}
	m.SetVerdictAuditor(aud)

	ctx := context.Background()
	id, err := m.StoreClaim(ctx, Claim{Text: "audited claim", Confidence: 0.7, Status: ClaimStatusAuto})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := m.PromoteClaim(ctx, id); err != nil {
		t.Fatalf("promote: %v", err)
	}

	aud.mu.Lock()
	defer aud.mu.Unlock()
	if len(aud.calls) != 1 {
		t.Fatalf("auditor calls = %d, want 1", len(aud.calls))
	}
	if len(aud.status) != 1 {
		t.Fatalf("status snapshots = %d, want 1", len(aud.status))
	}
	// Durable-then-ack: the auditor observed the claim still in its
	// pre-transition state (status=auto) at call time.
	if got := aud.status[0]; got != string(ClaimStatusAuto) {
		t.Errorf("status at audit time = %q, want %q (audit must precede transition)", got, ClaimStatusAuto)
	}
	// After the verdict the status transitioned.
	mem, err := m.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := asString(mem.Metadata["status"]); got != string(ClaimStatusPromoted) {
		t.Errorf("status after verdict = %q, want %q", got, ClaimStatusPromoted)
	}
}

func TestVerdictAuditor_FailureAbortsVerdict(t *testing.T) {
	m := newTestManager(t)
	aud := &fakeVerdictAuditor{err: errors.New("chain sealed")}
	m.SetVerdictAuditor(aud)

	ctx := context.Background()
	id, err := m.StoreClaim(ctx, Claim{Text: "unaudited claim", Status: ClaimStatusAuto})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := m.PromoteClaim(ctx, id); err == nil {
		t.Fatal("promote should fail when the auditor rejects the entry")
	}
	mem, err := m.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := asString(mem.Metadata["status"]); got != string(ClaimStatusAuto) {
		t.Errorf("status after failed audit = %q, want %q (verdict aborted)", got, ClaimStatusAuto)
	}
}

func TestVerdictAuditor_NilSafe(t *testing.T) {
	m := newTestManager(t)
	// Setting nil is a no-op (typed-nil guard convention), verdicts proceed.
	m.SetVerdictAuditor(nil)
	ctx := context.Background()
	id, err := m.StoreClaim(ctx, Claim{Text: "no auditor", Status: ClaimStatusAuto})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := m.RejectClaim(ctx, id); err != nil {
		t.Fatalf("reject without auditor: %v", err)
	}
}

func TestVerdictAuditor_HashCorrectness(t *testing.T) {
	m := newTestManager(t)
	aud := &fakeVerdictAuditor{}
	m.SetVerdictAuditor(aud)

	ctx := context.Background()
	id, err := m.StoreClaim(ctx, Claim{Text: "hash check", Status: ClaimStatusAuto})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	reason := "contradicted by the runbook"
	if err := m.RejectClaimWithReason(ctx, id, reason); err != nil {
		t.Fatalf("reject with reason: %v", err)
	}
	want := sha256.Sum256([]byte(reason))
	wantHash := hex.EncodeToString(want[:])[:16]
	aud.mu.Lock()
	defer aud.mu.Unlock()
	if len(aud.calls) != 1 {
		t.Fatalf("auditor calls = %d, want 1", len(aud.calls))
	}
	if got := aud.calls[0].reasonHash; got != wantHash {
		t.Errorf("reasonHash = %q, want %q", got, wantHash)
	}
	if got := aud.calls[0].verdict; got != "reject" {
		t.Errorf("verdict = %q, want reject", got)
	}
	// Raw reason must never reach the auditor — only the hash.
	if strings.Contains(aud.calls[0].reasonHash, reason) {
		t.Error("raw reason leaked into the audit hash")
	}
}

func TestReasonHash_EmptyReason(t *testing.T) {
	empty := sha256.Sum256([]byte(""))
	want := hex.EncodeToString(empty[:])[:16]
	if got := ReasonHash(""); got != want {
		t.Errorf("ReasonHash(\"\") = %q, want %q", got, want)
	}
	if len(ReasonHash("x")) != 16 {
		t.Errorf("ReasonHash length = %d, want 16", len(ReasonHash("x")))
	}
}

func TestVerdictReason_CapRejection(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()
	id, err := m.StoreClaim(ctx, Claim{Text: "cap check", Status: ClaimStatusAuto})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	tooLong := strings.Repeat("a", MaxVerdictReasonBytes+1)
	if err := m.PromoteClaimWithReason(ctx, id, tooLong); !errors.Is(err, ErrVerdictReasonTooLong) {
		t.Fatalf("err = %v, want ErrVerdictReasonTooLong", err)
	}
	// Boundary: exactly 512 bytes is accepted.
	mem, _ := m.GetByID(ctx, id)
	if got := asString(mem.Metadata["status"]); got != string(ClaimStatusAuto) {
		t.Fatalf("status = %q after cap rejection, want auto", got)
	}
	exact := strings.Repeat("b", MaxVerdictReasonBytes)
	if err := m.RejectClaimWithReason(ctx, id, exact); err != nil {
		t.Fatalf("reject with 512B reason: %v", err)
	}
	mem, err = m.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := asString(mem.Metadata["verdict_reason"]); got != exact {
		t.Errorf("verdict_reason = %d bytes, want the 512B reason stored", len(got))
	}
}

func TestVerdictReason_CalibrationCarriesReason(t *testing.T) {
	dir := t.TempDir()
	m := newTestManager(t)
	m.SetCalibrationLogger(NewCalibrationLogger(dir, nil))

	ctx := context.Background()
	id, err := m.StoreClaim(ctx, Claim{Text: "calibrated", Confidence: 0.6, Status: ClaimStatusAuto})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := m.PromoteClaimWithReason(ctx, id, "verified against prod"); err != nil {
		t.Fatalf("promote: %v", err)
	}
	assertCalibrationLine(t, dir, "promote", "calibrated", 0.6)
	// The reason field must appear in the calibration record.
	data, err := os.ReadFile(filepath.Join(dir, "claim_verdicts.jsonl"))
	if err != nil {
		t.Fatalf("read calibration: %v", err)
	}
	if !strings.Contains(string(data), "verified against prod") {
		t.Errorf("calibration record missing raw reason: %s", data)
	}
}

func TestListAutoClaimsFiltered(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

	store := func(text string, conf float64, age time.Duration) {
		t.Helper()
		id, err := m.StoreClaim(ctx, Claim{Text: text, Confidence: conf, Status: ClaimStatusAuto})
		if err != nil {
			t.Fatalf("store %s: %v", text, err)
		}
		if age > 0 {
			// Backdate by rewriting created_at through a versioned store
			// is invasive; instead store a claim "observed" earlier — for
			// age filtering we rely on OlderThan checks against CreatedAt,
			// so seed only fresh claims here and exercise OlderThan via
			// the tiny-duration cases below.
			_ = id
		}
	}

	store("high confidence", 0.9, 0)
	store("mid confidence", 0.5, 0)
	store("low confidence", 0.2, 0)
	store("no confidence", 0, 0) // stored with 0.0 confidence (parseable)

	mk := func(f float64) *float64 { return &f }
	mkDur := func(d time.Duration) *time.Duration { return &d }

	cases := []struct {
		name      string
		opts      ListAutoClaimsOptions
		wantCount int
		wantTotal int
	}{
		{name: "no filters", opts: ListAutoClaimsOptions{}, wantCount: 4, wantTotal: 4},
		{
			name: "min_confidence inclusive",
			opts: ListAutoClaimsOptions{MinConfidence: mk(0.5)},
			// 0.9 and 0.5 (inclusive); 0.0 is parseable but below bound.
			wantCount: 2, wantTotal: 2,
		},
		{
			name:      "max_confidence inclusive",
			opts:      ListAutoClaimsOptions{MaxConfidence: mk(0.5)},
			wantCount: 3, wantTotal: 3, // 0.5, 0.2, 0.0
		},
		{
			name:      "confidence band",
			opts:      ListAutoClaimsOptions{MinConfidence: mk(0.3), MaxConfidence: mk(0.7)},
			wantCount: 1, wantTotal: 1, // only 0.5
		},
		{
			name:      "older_than excludes fresh claims",
			opts:      ListAutoClaimsOptions{OlderThan: mkDur(time.Hour)},
			wantCount: 0, wantTotal: 0,
		},
		{
			name:      "older_than zero keeps everything",
			opts:      ListAutoClaimsOptions{OlderThan: mkDur(0)},
			wantCount: 4, wantTotal: 4,
		},
		{
			name:      "limit paginates",
			opts:      ListAutoClaimsOptions{Limit: 2},
			wantCount: 2, wantTotal: 4,
		},
		{
			name:      "limit and offset",
			opts:      ListAutoClaimsOptions{Limit: 2, Offset: 2},
			wantCount: 2, wantTotal: 4,
		},
		{
			name:      "offset past end",
			opts:      ListAutoClaimsOptions{Limit: 2, Offset: 10},
			wantCount: 0, wantTotal: 4,
		},
		{
			name:      "negative offset treated as zero",
			opts:      ListAutoClaimsOptions{Offset: -5},
			wantCount: 4, wantTotal: 4,
		},
		{
			name:      "limit zero defaults",
			opts:      ListAutoClaimsOptions{Limit: 0},
			wantCount: 4, wantTotal: 4,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, total, err := m.ListAutoClaimsFiltered(ctx, tc.opts)
			if err != nil {
				t.Fatalf("ListAutoClaimsFiltered: %v", err)
			}
			if len(items) != tc.wantCount {
				t.Errorf("items = %d, want %d", len(items), tc.wantCount)
			}
			if total != tc.wantTotal {
				t.Errorf("total = %d, want %d", total, tc.wantTotal)
			}
		})
	}

	// Sanity: ListAutoClaims (legacy signature) still delegates and returns
	// the same universe.
	legacy, err := m.ListAutoClaims(ctx, time.Time{}, 20)
	if err != nil {
		t.Fatalf("ListAutoClaims: %v", err)
	}
	if len(legacy) != 4 {
		t.Errorf("legacy ListAutoClaims = %d items, want 4", len(legacy))
	}
}

func TestListAutoClaimsFiltered_Uninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, _, err := m.ListAutoClaimsFiltered(context.Background(), ListAutoClaimsOptions{}); err == nil {
		t.Fatal("expected error on uninitialized manager")
	}
}

func TestListAutoClaimsFiltered_PaginationWindowIsStable(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := m.StoreClaim(ctx, Claim{Text: "pag claim", Status: ClaimStatusAuto}); err != nil {
			t.Fatalf("store %d: %v", i, err)
		}
	}
	page1, total, err := m.ListAutoClaimsFiltered(ctx, ListAutoClaimsOptions{Limit: 2})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	page2, total2, err := m.ListAutoClaimsFiltered(ctx, ListAutoClaimsOptions{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if total != 5 || total2 != 5 {
		t.Fatalf("totals = %d/%d, want 5/5", total, total2)
	}
	seen := map[string]bool{}
	for _, it := range append(append([]MemoryResult{}, page1...), page2...) {
		seen[it.Memory.ID] = true
	}
	if len(seen) != 4 {
		t.Errorf("distinct items across pages = %d, want 4 (no overlap)", len(seen))
	}
}
