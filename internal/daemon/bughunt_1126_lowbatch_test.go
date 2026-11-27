package daemon

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/employee"
)

// TestActivityFindings_TotalUsesStoreCount pins the A1-13 fix: the
// activity.findings handler must report total from the store's exact COUNT,
// not len(findings). ListAuditFindings is capped at the store's default
// 100 rows, so before the fix an employee with >100 findings reported total
// of at most 100 (under-reporting) and offset>=100 returned an empty page.
// After the fix, total reflects the true store count while the returned
// list is the store's (100-row-capped) page.
func TestActivityFindings_TotalUsesStoreCount(t *testing.T) {
	f := newActivityRPCFixture(t)
	ctx := context.Background()

	// The fixture already seeded find-1/find-2/find-3 for emp-1 (3 findings).
	// Seed 101 more so the store total for emp-1 exceeds the 100-row cap.
	ts := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 101; i++ {
		finding := employee.AuditFinding{
			ID:           fmt.Sprintf("bulk-%d", i),
			EmployeeID:   "emp-1",
			Severity:     employee.AuditSeverity("info"),
			Checkpoint:   employee.CheckpointPostTurn,
			ViolatedRule: "rule-1",
			DetectedAt:   ts.Add(time.Duration(i) * time.Second),
		}
		if err := f.audit.Create(ctx, finding); err != nil {
			t.Fatalf("audit.Create(bulk-%d): %v", i, err)
		}
	}

	m, err := callActivity(t, f.srv, "activity.findings", `{"employee_id":"emp-1"}`)
	if err != nil {
		t.Fatalf("activity.findings: %v", err)
	}
	// Store count = 3 (seeded) + 101 (bulk) = 104, NOT capped at 100.
	if got := numAsFloat(t, m["total"]); got != 104 {
		t.Fatalf("total = %v, want 104 (store count, not the 100-row cap)", m["total"])
	}
	// The returned list is the store's 100-row-capped page.
	findings, _ := m["findings"].([]any)
	if len(findings) != 100 {
		t.Fatalf("findings = %d, want 100 (store list cap)", len(findings))
	}
}

// TestActivityFindings_ResolvedTotalUsesPostFilteredList pins the A1-13
// interaction: when a resolved filter is applied (which the store count
// cannot express), total must reflect the post-filtered list, not the raw
// store count. The fixture seeds 3 findings for emp-1 (find-1 warning open,
// find-2 critical open, find-3 warning resolved). resolved=true must report
// total=1 (the single resolved finding), not 3.
func TestActivityFindings_ResolvedTotalUsesPostFilteredList(t *testing.T) {
	f := newActivityRPCFixture(t)

	m, err := callActivity(t, f.srv, "activity.findings", `{"employee_id":"emp-1","resolved":true}`)
	if err != nil {
		t.Fatalf("activity.findings: %v", err)
	}
	if got := numAsFloat(t, m["total"]); got != 1 {
		t.Fatalf("total = %v, want 1 (resolved filter is post-filtered, not store count)", m["total"])
	}
	findings, _ := m["findings"].([]any)
	if len(findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(findings))
	}
}

// TestActivityFindings_OffsetBeyondWindowNoPanic pins the paging guard:
// total is now the unbounded store count while the returned window is the
// store's 100-row-capped list, so slice bounds must come from len(findings),
// never total. Before the orchestrator's bounds fix, offset=101 against a
// 104-finding employee sliced findings[101:104] on a 100-row window and
// panicked (start > len). After: empty page, exact total, no panic.
func TestActivityFindings_OffsetBeyondWindowNoPanic(t *testing.T) {
	f := newActivityRPCFixture(t)
	ctx := context.Background()
	ts := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 101; i++ {
		finding := employee.AuditFinding{
			ID:           fmt.Sprintf("bulk-%d", i),
			EmployeeID:   "emp-1",
			Severity:     employee.AuditSeverity("info"),
			Checkpoint:   employee.CheckpointPostTurn,
			ViolatedRule: "rule-1",
			DetectedAt:   ts.Add(time.Duration(i) * time.Second),
		}
		if err := f.audit.Create(ctx, finding); err != nil {
			t.Fatalf("audit.Create(bulk-%d): %v", i, err)
		}
	}

	m, err := callActivity(t, f.srv, "activity.findings", `{"employee_id":"emp-1","offset":101}`)
	if err != nil {
		t.Fatalf("activity.findings offset=101: %v (panic surfaced as error?)", err)
	}
	if got := numAsFloat(t, m["total"]); got != 104 {
		t.Fatalf("total = %v, want 104", m["total"])
	}
	findings, _ := m["findings"].([]any)
	if len(findings) != 0 {
		t.Fatalf("findings = %d, want 0 (offset beyond the store's window)", len(findings))
	}
}
