package employee

import (
	"context"
	"testing"
	"time"
)

// --- E1: ConversationTokenStore tests ---

// stubConvTokenStore is a test double for ConversationTokenStore.
type stubConvTokenStore struct {
	tokens map[string]int
	err    error
}

func (s *stubConvTokenStore) GetConversationTokens(conversationID string) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.tokens[conversationID], nil
}

func TestPreExecChecker_ConversationTokenBudget(t *testing.T) {
	c := &Constitution{
		Constraints: ConstitutionalConstraints{
			MaxConversationTokens: 1000,
		},
	}
	checker := NewPreExecChecker("emp-1", c)
	checker.SetConversationTokenStore(&stubConvTokenStore{
		tokens: map[string]int{"conv-1": 500},
	})

	t.Run("under budget allows", func(t *testing.T) {
		dec := checker.Check("file_read", "file_read", map[string]string{
			"conversation_id": "conv-1",
		})
		if !dec.Allowed {
			t.Errorf("expected allowed, got denied: %s", dec.Reason)
		}
	})

	t.Run("at budget denies", func(t *testing.T) {
		checker.SetConversationTokenStore(&stubConvTokenStore{
			tokens: map[string]int{"conv-1": 1000},
		})
		dec := checker.Check("file_read", "file_read", map[string]string{
			"conversation_id": "conv-1",
		})
		if dec.Allowed {
			t.Error("expected denied at budget limit")
		}
		if dec.Severity != string(SeverityCritical) {
			t.Errorf("expected critical severity, got %s", dec.Severity)
		}
	})

	t.Run("over budget denies", func(t *testing.T) {
		checker.SetConversationTokenStore(&stubConvTokenStore{
			tokens: map[string]int{"conv-1": 1500},
		})
		dec := checker.Check("file_read", "file_read", map[string]string{
			"conversation_id": "conv-1",
		})
		if dec.Allowed {
			t.Error("expected denied over budget")
		}
	})

	t.Run("no conversation_id skips check", func(t *testing.T) {
		checker.SetConversationTokenStore(&stubConvTokenStore{
			tokens: map[string]int{"conv-1": 10000},
		})
		dec := checker.Check("file_read", "file_read", map[string]string{})
		if !dec.Allowed {
			t.Errorf("expected allowed without conversation_id, got: %s", dec.Reason)
		}
	})

	t.Run("no token store skips check", func(t *testing.T) {
		c2 := &Constitution{
			Constraints: ConstitutionalConstraints{
				MaxConversationTokens: 100,
			},
		}
		checker2 := NewPreExecChecker("emp-2", c2)
		// No ConversationTokenStore wired (defaults to noop)
		dec := checker2.Check("file_read", "file_read", map[string]string{
			"conversation_id": "conv-1",
		})
		if !dec.Allowed {
			t.Errorf("expected allowed with noop store, got: %s", dec.Reason)
		}
	})

	t.Run("nil store is ignored by SetConversationTokenStore", func(t *testing.T) {
		checker3 := NewPreExecChecker("emp-3", c)
		checker3.SetConversationTokenStore(nil) // should be a no-op
		// The default noop store should still be in place
		dec := checker3.Check("file_read", "file_read", map[string]string{
			"conversation_id": "conv-1",
		})
		if !dec.Allowed {
			t.Errorf("expected allowed with nil store (noop), got: %s", dec.Reason)
		}
	})
}

// --- E2: TurnRecord fields test ---

func TestTurnRecord_Fields(t *testing.T) {
	tr := TurnRecord{
		EmployeeID:     "emp-1",
		ConversationID: "conv-1",
		TurnID:         "turn-1",
		GoalID:         "goal-1",
		PlanID:         "plan-1",
		ToolCalls:      []ToolCallRecord{{ToolName: "file_read", Action: "read"}},
		FinalOutput:    "done",
		TokenUsage: TokenCounts{
			PromptTokens:     100,
			CompletionTokens: 50,
			TotalTokens:      150,
		},
		Duration:     2 * time.Second,
		Constitution: &Constitution{},
	}

	if tr.ConversationID != "conv-1" {
		t.Errorf("ConversationID = %q, want conv-1", tr.ConversationID)
	}
	if tr.TokenUsage.TotalTokens != 150 {
		t.Errorf("TokenUsage.TotalTokens = %d, want 150", tr.TokenUsage.TotalTokens)
	}
	if tr.Duration != 2*time.Second {
		t.Errorf("Duration = %v, want 2s", tr.Duration)
	}
}

// --- E5: DriftScore formula tests ---

func TestCalculateDriftScore(t *testing.T) {
	now := time.Now().UTC()

	t.Run("empty findings returns zero", func(t *testing.T) {
		score := CalculateDriftScore(nil, now, DriftHalfLife)
		if score != 0.0 {
			t.Errorf("expected 0.0, got %f", score)
		}
	})

	t.Run("single critical finding today returns 1.0", func(t *testing.T) {
		findings := []AuditFinding{
			{Severity: SeverityCritical, DetectedAt: now},
		}
		score := CalculateDriftScore(findings, now, DriftHalfLife)
		if score != 1.0 {
			t.Errorf("expected 1.0, got %f", score)
		}
	})

	t.Run("resolved findings excluded", func(t *testing.T) {
		resolved := now.Add(-1 * time.Hour)
		findings := []AuditFinding{
			{Severity: SeverityCritical, DetectedAt: now, ResolvedAt: &resolved},
		}
		score := CalculateDriftScore(findings, now, DriftHalfLife)
		if score != 0.0 {
			t.Errorf("expected 0.0 with resolved finding, got %f", score)
		}
	})

	t.Run("mixed severity findings today normalize to 1.0", func(t *testing.T) {
		findings := []AuditFinding{
			{Severity: SeverityCritical, DetectedAt: now},
			{Severity: SeverityWarning, DetectedAt: now},
			{Severity: SeverityInfo, DetectedAt: now},
		}
		// All time_decay=1.0, so sum_weights/max_score = (1.0+0.3+0.1)/(1.0+0.3+0.1) = 1.0
		score := CalculateDriftScore(findings, now, DriftHalfLife)
		if score != 1.0 {
			t.Errorf("expected 1.0, got %f", score)
		}
	})

	t.Run("old findings decay", func(t *testing.T) {
		// Finding from 7 days ago (one half-life) should have weight halved.
		old := now.Add(-7 * 24 * time.Hour)
		findings := []AuditFinding{
			{Severity: SeverityCritical, DetectedAt: old},
		}
		score := CalculateDriftScore(findings, now, DriftHalfLife)
		// time_decay = exp(-7/7) = exp(-1) ≈ 0.3679
		// score = (1.0 * 0.3679) / 1.0 ≈ 0.3679
		if score < 0.35 || score > 0.39 {
			t.Errorf("expected ~0.37, got %f", score)
		}
	})

	t.Run("mixed old and new findings", func(t *testing.T) {
		old := now.Add(-7 * 24 * time.Hour)
		findings := []AuditFinding{
			{Severity: SeverityCritical, DetectedAt: now},
			{Severity: SeverityWarning, DetectedAt: old},
		}
		score := CalculateDriftScore(findings, now, DriftHalfLife)
		// weight: crit=1.0*1.0, warn=0.3*0.3679
		// sum = 1.0 + 0.1104 = 1.1104
		// max = 1.0 + 0.3 = 1.3
		// score = 1.1104 / 1.3 ≈ 0.854
		if score < 0.84 || score > 0.87 {
			t.Errorf("expected ~0.85, got %f", score)
		}
	})

	t.Run("future-dated finding treated as just detected", func(t *testing.T) {
		findings := []AuditFinding{
			{Severity: SeverityCritical, DetectedAt: now.Add(1 * time.Hour)},
		}
		score := CalculateDriftScore(findings, now, DriftHalfLife)
		if score != 1.0 {
			t.Errorf("expected 1.0 for future-dated finding, got %f", score)
		}
	})

	t.Run("default half life when zero", func(t *testing.T) {
		findings := []AuditFinding{
			{Severity: SeverityCritical, DetectedAt: now},
		}
		score := CalculateDriftScore(findings, now, 0)
		if score != 1.0 {
			t.Errorf("expected 1.0 with default half-life, got %f", score)
		}
	})
}

// --- E8: Reservoir sampling tests ---

func TestReservoirSample(t *testing.T) {
	t.Run("returns all when under sample size", func(t *testing.T) {
		items := make([]TurnRecord, 10)
		result := reservoirSample(items, 50)
		if len(result) != 10 {
			t.Errorf("expected 10, got %d", len(result))
		}
	})

	t.Run("returns exactly k when over sample size", func(t *testing.T) {
		items := make([]TurnRecord, 100)
		for i := range items {
			items[i] = TurnRecord{TurnID: string(rune('a' + i%26))}
		}
		result := reservoirSample(items, 10)
		if len(result) != 10 {
			t.Errorf("expected 10, got %d", len(result))
		}
	})

	t.Run("k <= 0 returns original", func(t *testing.T) {
		items := make([]TurnRecord, 5)
		result := reservoirSample(items, 0)
		if len(result) != 5 {
			t.Errorf("expected 5, got %d", len(result))
		}
	})
}

func TestPeriodicAuditor_SetSampleSize(t *testing.T) {
	auditor := NewPeriodicAuditor(nil, nil, 0.3)
	if auditor.sampleSize != DefaultPeriodicAuditSampleSize {
		t.Errorf("expected default %d, got %d", DefaultPeriodicAuditSampleSize, auditor.sampleSize)
	}

	auditor.SetSampleSize(25)
	if auditor.sampleSize != 25 {
		t.Errorf("expected 25, got %d", auditor.sampleSize)
	}

	auditor.SetSampleSize(0)
	if auditor.sampleSize != DefaultPeriodicAuditSampleSize {
		t.Errorf("expected reset to default %d, got %d", DefaultPeriodicAuditSampleSize, auditor.sampleSize)
	}
}

// --- E9: Composite indexes test ---

func TestAuditStore_CompositeIndexes(t *testing.T) {
	if !containsStr(auditSchemaSQL, "idx_audit_severity_resolved") {
		t.Error("missing idx_audit_severity_resolved in schema SQL")
	}
	if !containsStr(auditSchemaSQL, "idx_audit_checkpoint_detected") {
		t.Error("missing idx_audit_checkpoint_detected in schema SQL")
	}
}

// --- E10: Plan ID validator test ---

func TestAuditStore_PlanIDValidator(t *testing.T) {
	store, err := NewAuditStore(":memory:")
	if err != nil {
		t.Fatalf("NewAuditStore: %v", err)
	}
	defer store.Close()

	var validatedCalls []string
	store.SetPlanIDValidator(func(ctx context.Context, planID string) bool {
		validatedCalls = append(validatedCalls, planID)
		return planID == "valid-plan-id"
	})

	t.Run("valid plan_id kept", func(t *testing.T) {
		validatedCalls = nil
		finding := AuditFinding{
			EmployeeID: "emp-1",
			PlanID:     "valid-plan-id",
			Severity:   SeverityInfo,
			Checkpoint: CheckpointPreExec,
			DetectedAt: time.Now().UTC(),
		}
		if err := store.Create(context.Background(), finding); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if len(validatedCalls) != 1 || validatedCalls[0] != "valid-plan-id" {
			t.Errorf("expected validator called with valid-plan-id, got %v", validatedCalls)
		}
		findings, err := store.List(context.Background(), AuditListFilter{EmployeeID: "emp-1"})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(findings))
		}
		if findings[0].PlanID != "valid-plan-id" {
			t.Errorf("expected plan_id valid-plan-id, got %s", findings[0].PlanID)
		}
	})

	t.Run("invalid plan_id cleared", func(t *testing.T) {
		validatedCalls = nil
		finding := AuditFinding{
			EmployeeID: "emp-2",
			PlanID:     "invalid-plan-id",
			Severity:   SeverityInfo,
			Checkpoint: CheckpointPreExec,
			DetectedAt: time.Now().UTC(),
		}
		if err := store.Create(context.Background(), finding); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if len(validatedCalls) != 1 || validatedCalls[0] != "invalid-plan-id" {
			t.Errorf("expected validator called with invalid-plan-id, got %v", validatedCalls)
		}
		findings, err := store.List(context.Background(), AuditListFilter{EmployeeID: "emp-2"})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(findings) != 1 {
			t.Fatalf("expected 1 finding, got %d", len(findings))
		}
		if findings[0].PlanID != "" {
			t.Errorf("expected empty plan_id, got %s", findings[0].PlanID)
		}
	})

	t.Run("empty plan_id skips validation", func(t *testing.T) {
		validatedCalls = nil
		finding := AuditFinding{
			EmployeeID: "emp-3",
			PlanID:     "",
			Severity:   SeverityInfo,
			Checkpoint: CheckpointPreExec,
			DetectedAt: time.Now().UTC(),
		}
		if err := store.Create(context.Background(), finding); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if len(validatedCalls) != 0 {
			t.Errorf("expected no validator calls for empty plan_id, got %d", len(validatedCalls))
		}
	})
}

// --- E10: FK comment test ---

func TestAuditSchema_HasPlanIDComment(t *testing.T) {
	if !containsStr(auditSchemaSQL, "plan_id references plans.id") {
		t.Error("missing E10 FK comment for plan_id in schema SQL")
	}
}

// --- E7: Severity rubric test ---

func TestSeverityRubric_InPrompt(t *testing.T) {
	if !containsStr(postTurnSystemPrompt, "critical: Never[] violation") {
		t.Error("severity rubric for critical not in postTurnSystemPrompt")
	}
	if !containsStr(postTurnSystemPrompt, "warning: Charter commitment violation") {
		t.Error("severity rubric for warning not in postTurnSystemPrompt")
	}
	if !containsStr(postTurnSystemPrompt, "info: Minor style drift") {
		t.Error("severity rubric for info not in postTurnSystemPrompt")
	}
}

// --- E4: CriticalFindingEvent test ---

func TestCriticalFindingEvent_Fields(t *testing.T) {
	evt := CriticalFindingEvent{
		EmployeeID:   "emp-1",
		FindingID:    "finding-1",
		ViolatedRule: "never[0]",
		Evidence:     "merged to main",
	}
	if evt.EmployeeID != "emp-1" {
		t.Errorf("EmployeeID = %q, want emp-1", evt.EmployeeID)
	}
	if evt.FindingID != "finding-1" {
		t.Errorf("FindingID = %q, want finding-1", evt.FindingID)
	}
	if evt.ViolatedRule != "never[0]" {
		t.Errorf("ViolatedRule = %q, want never[0]", evt.ViolatedRule)
	}
}

func TestBusPublisher_PublishCriticalFinding(t *testing.T) {
	pub := &mockBusPublisher{}
	pub.PublishCriticalFinding("emp-1", "finding-1", "never[0]", "merged to main")

	if len(pub.criticalEvts) != 1 {
		t.Fatalf("expected 1 critical event, got %d", len(pub.criticalEvts))
	}
	evt := pub.criticalEvts[0]
	if evt.EmployeeID != "emp-1" || evt.FindingID != "finding-1" {
		t.Errorf("unexpected event: %+v", evt)
	}
}

// --- Helpers ---
// containsStr is defined in phase2_gaps_test.go. Reused here.
