package rpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"log/slog"

	"github.com/caimlas/meept/internal/memory"
)

// dispatchReviewQueue calls memory.reviewQueue through the registration map
// (same CallMethod path as the expired/purge epistemic tests).
func dispatchReviewQueue(t *testing.T, h *EpistemicHandler, params []byte) (any, error) {
	t.Helper()
	srv := New(&Config{SocketPath: filepath.Join(t.TempDir(), "test.sock")}, nil, slog.Default())
	h.RegisterEpistemicHandlers(srv)
	return srv.CallMethod(context.Background(), "memory.reviewQueue", params)
}

// fakeRPCAuditor implements memory.VerdictAuditor for handler-level tests.
type fakeRPCAuditor struct {
	mu    sync.Mutex
	calls []struct{ claimID, verdict, reasonHash string }
}

func (f *fakeRPCAuditor) RecordVerdict(claimID, verdict, reasonHash string) error {
	f.mu.Lock()
	f.calls = append(f.calls, struct{ claimID, verdict, reasonHash string }{claimID, verdict, reasonHash})
	f.mu.Unlock()
	return nil
}

func TestReviewQueue_RPC_FiltersPaginationTotal(t *testing.T) {
	h := newPurgeTestHandler(t)
	mgr, err := h.managerOrErr()
	if err != nil {
		t.Fatalf("managerOrErr: %v", err)
	}
	ctx := context.Background()

	store := func(text string, conf float64) string {
		t.Helper()
		id, err := mgr.StoreClaim(ctx, memory.Claim{Text: text, Confidence: conf, Status: memory.ClaimStatusAuto})
		if err != nil {
			t.Fatalf("store %s: %v", text, err)
		}
		return id
	}
	store("high conf", 0.9)
	store("mid conf", 0.5)
	store("low conf", 0.2)
	store("fresh", 0.8)

	mk := func(f float64) *float64 { return &f }
	cases := []struct {
		name      string
		params    map[string]any
		wantItems int
		wantTotal int
	}{
		{"empty params", map[string]any{}, 4, 4},
		{"min_confidence", map[string]any{"min_confidence": 0.5}, 3, 3}, // 0.9, 0.8, 0.5
		{"max_confidence", map[string]any{"max_confidence": 0.5}, 2, 2}, // 0.5, 0.2
		{"band", map[string]any{"min_confidence": 0.3, "max_confidence": 0.7}, 1, 1},
		{"limit", map[string]any{"limit": 2}, 2, 4},
		{"offset", map[string]any{"offset": 2, "limit": 2}, 2, 4},
		{"offset past end", map[string]any{"offset": 99}, 0, 4},
		{"older_than 1h drops fresh", map[string]any{"older_than": "1h"}, 0, 0},
		{"since_hours legacy alias", map[string]any{"since_hours": 1}, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params, _ := json.Marshal(tc.params)
			res, err := dispatchReviewQueue(t, h, params)
			if err != nil {
				t.Fatalf("memory.reviewQueue: %v", err)
			}
			raw, err := json.Marshal(res)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var out struct {
				AutoClaims []json.RawMessage `json:"auto_claims"`
				Total      int               `json:"total"`
			}
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(out.AutoClaims) != tc.wantItems {
				t.Errorf("items = %d, want %d", len(out.AutoClaims), tc.wantItems)
			}
			if out.Total != tc.wantTotal {
				t.Errorf("total = %d, want %d", out.Total, tc.wantTotal)
			}
		})
	}

	// limit cap: asking for 1000 returns everything but must not error (cap 200).
	params, _ := json.Marshal(map[string]any{"limit": 1000})
	if _, err := dispatchReviewQueue(t, h, params); err != nil {
		t.Errorf("limit 1000 should be capped, not rejected: %v", err)
	}

	// invalid older_than errors.
	params, _ = json.Marshal(map[string]any{"older_than": "not-a-duration"})
	if _, err := dispatchReviewQueue(t, h, params); err == nil {
		t.Error("invalid older_than should error")
	}

	_ = mk
}

func TestReviewQueue_RPC_RejectedClaimExcluded(t *testing.T) {
	h := newPurgeTestHandler(t)
	mgr, err := h.managerOrErr()
	if err != nil {
		t.Fatalf("managerOrErr: %v", err)
	}
	ctx := context.Background()
	id, err := mgr.StoreClaim(ctx, memory.Claim{Text: "doomed claim", Status: memory.ClaimStatusAuto})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	params, _ := json.Marshal(map[string]any{})
	res, err := dispatchReviewQueue(t, h, params)
	if err != nil {
		t.Fatalf("reviewQueue before verdict: %v", err)
	}
	raw, _ := json.Marshal(res)
	var out struct {
		Total int `json:"total"`
	}
	_ = json.Unmarshal(raw, &out)
	if out.Total != 1 {
		t.Fatalf("total before verdict = %d, want 1", out.Total)
	}

	if err := mgr.RejectClaim(ctx, id); err != nil {
		t.Fatalf("reject: %v", err)
	}
	res, err = dispatchReviewQueue(t, h, params)
	if err != nil {
		t.Fatalf("reviewQueue after verdict: %v", err)
	}
	raw, _ = json.Marshal(res)
	_ = json.Unmarshal(raw, &out)
	if out.Total != 0 {
		t.Errorf("total after verdict = %d, want 0 (rejected claim excluded from all queries)", out.Total)
	}
}

func TestVerdict_RPC_ReasonFlowsToMetadataAndAudit(t *testing.T) {
	h := newPurgeTestHandler(t)
	mgr, err := h.managerOrErr()
	if err != nil {
		t.Fatalf("managerOrErr: %v", err)
	}
	aud := &fakeRPCAuditor{}
	mgr.SetVerdictAuditor(aud)

	ctx := context.Background()
	id, err := mgr.StoreClaim(ctx, memory.Claim{Text: "reason claim", Status: memory.ClaimStatusAuto})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	srv := New(&Config{SocketPath: filepath.Join(t.TempDir(), "test.sock")}, nil, slog.Default())
	h.RegisterEpistemicHandlers(srv)

	params, _ := json.Marshal(map[string]any{"id": id, "reason": "contradicts the docs"})
	if _, err := srv.CallMethod(ctx, "memory.promoteClaim", params); err != nil {
		t.Fatalf("promoteClaim: %v", err)
	}

	mem, err := mgr.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := mem.Metadata["verdict_reason"]; got != "contradicts the docs" {
		t.Errorf("verdict_reason = %v, want stored reason", got)
	}
	if got := mem.Metadata["status"]; got != string(memory.ClaimStatusPromoted) {
		t.Errorf("status = %v, want promoted", got)
	}

	aud.mu.Lock()
	defer aud.mu.Unlock()
	if len(aud.calls) != 1 {
		t.Fatalf("auditor calls = %d, want 1", len(aud.calls))
	}
	sum := sha256.Sum256([]byte("contradicts the docs"))
	wantHash := hex.EncodeToString(sum[:])[:16]
	if aud.calls[0].reasonHash != wantHash {
		t.Errorf("reasonHash = %q, want %q", aud.calls[0].reasonHash, wantHash)
	}
	if aud.calls[0].verdict != "promote" {
		t.Errorf("verdict = %q, want promote", aud.calls[0].verdict)
	}
}

func TestVerdict_RPC_ReasonOverCapRejected(t *testing.T) {
	h := newPurgeTestHandler(t)
	mgr, err := h.managerOrErr()
	if err != nil {
		t.Fatalf("managerOrErr: %v", err)
	}
	ctx := context.Background()
	id, err := mgr.StoreClaim(ctx, memory.Claim{Text: "cap claim", Status: memory.ClaimStatusAuto})
	if err != nil {
		t.Fatalf("store: %v", err)
	}

	srv := New(&Config{SocketPath: filepath.Join(t.TempDir(), "test.sock")}, nil, slog.Default())
	h.RegisterEpistemicHandlers(srv)

	tooLong := strings.Repeat("x", memory.MaxVerdictReasonBytes+1)
	params, _ := json.Marshal(map[string]any{"id": id, "reason": tooLong})
	if _, err := srv.CallMethod(ctx, "memory.rejectClaim", params); err == nil {
		t.Fatal("over-cap reason should be rejected with a clear error")
	} else if !strings.Contains(err.Error(), "512") {
		t.Errorf("error should name the 512B cap, got: %v", err)
	}
	// The claim must be untouched after the rejection.
	mem, err := mgr.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := mem.Metadata["status"]; got != string(memory.ClaimStatusAuto) {
		t.Errorf("status = %v, want auto (verdict must not have landed)", got)
	}
}
