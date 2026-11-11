package scheduler

// Regression-review R2 wave pin tests (2026-11-11).
//
//   R2-1: recoverClaimedTicksWithoutReceipt must SKIP receipt-less job
//         kinds. A legacy fire-and-forget agent job (TargetSessionID == "")
//         writes NO receipts and mints a fresh turn id per delivery, so
//         hasReceiptForTick is permanently false — the old sweep re-delivered
//         every claim in the 7-day window on EVERY daemon Start with a new,
//         dedupe-proof turn id. The at-least-once guarantee honestly applies
//         only to receipt-writing (session-pinned) jobs.

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
)

// TestRecoverySkipsLegacyFireAndForgetClaims (R2-1a): a claim row for a
// legacy no-receipt agent job (TargetSessionID == "") is NOT re-delivered by
// the recovery sweep — no chat.request publish fires for it.
func TestRecoverySkipsLegacyFireAndForgetClaims(t *testing.T) {
	msgBus := bus.New(nil, nil)
	tmpDir := t.TempDir()
	s, err := NewScheduler(config.SchedulerConfig{Enabled: true, Timezone: "UTC"}, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	defer func() { _ = s.Stop(context.Background()) }()

	// Legacy fire-and-forget job: no TargetSessionID => Execute writes no
	// receipts and would mint a fresh turn id per delivery.
	if _, err := s.ScheduleConfig(JobConfig{
		ID:       "r21-legacy",
		Name:     "r21-legacy",
		Type:     JobTypeAgent,
		Schedule: "@yearly",
		Enabled:  true,
		AgentConfig: &AgentJobConfig{
			Prompt: "ping",
			// TargetSessionID deliberately empty.
		},
	}); err != nil {
		t.Fatalf("ScheduleConfig: %v", err)
	}

	tick := time.Now().UTC().Add(-time.Minute)
	if claimed, err := s.ClaimTick("r21-legacy", tick); err != nil || !claimed {
		t.Fatalf("ClaimTick: claimed=%v err=%v", claimed, err)
	}

	sub := msgBus.Subscribe("r21-count", "chat.request")
	defer msgBus.Unsubscribe(sub)

	var mu sync.Mutex
	deliveries := 0
	go func() {
		for msg := range sub.Channel {
			if strings.Contains(string(msg.Payload), "r21-legacy") {
				mu.Lock()
				deliveries++
				mu.Unlock()
			}
		}
	}()

	if err := s.recoverClaimedTicksWithoutReceipt(); err != nil {
		t.Fatalf("recoverClaimedTicksWithoutReceipt: %v", err)
	}
	// dispatchMissed (if it wrongly fired) runs async; give it a window.
	time.Sleep(1500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if deliveries != 0 {
		t.Errorf("legacy fire-and-forget claim delivered %d times on recovery, want 0 (receipt-less job kinds must be skipped)", deliveries)
	}
}

// TestRecoveryRedeliversSessionPinnedNoReceiptClaim (R2-1b): a session-pinned
// agent job's claim with NO receipt (the crash window) IS re-delivered by the
// sweep — the at-least-once guarantee holds for receipt-writing jobs.
func TestRecoveryRedeliversSessionPinnedNoReceiptClaim(t *testing.T) {
	msgBus := bus.New(nil, nil)
	tmpDir := t.TempDir()
	s, err := NewScheduler(config.SchedulerConfig{Enabled: true, Timezone: "UTC"}, msgBus, WithDataDir(tmpDir))
	if err != nil {
		t.Fatalf("NewScheduler: %v", err)
	}
	defer func() { _ = s.Stop(context.Background()) }()

	if _, err := s.ScheduleConfig(JobConfig{
		ID:       "r21-pinned",
		Name:     "r21-pinned",
		Type:     JobTypeAgent,
		Schedule: "@yearly",
		Enabled:  true,
		AgentConfig: &AgentJobConfig{
			Prompt:          "ping",
			TargetSessionID: "sess-r21",
		},
	}); err != nil {
		t.Fatalf("ScheduleConfig: %v", err)
	}

	tick := time.Now().UTC().Add(-time.Minute)
	if claimed, err := s.ClaimTick("r21-pinned", tick); err != nil || !claimed {
		t.Fatalf("ClaimTick: claimed=%v err=%v", claimed, err)
	}

	sub := msgBus.Subscribe("r21p-count", "chat.request")
	defer msgBus.Unsubscribe(sub)

	var mu sync.Mutex
	deliveries := 0
	go func() {
		for msg := range sub.Channel {
			if strings.Contains(string(msg.Payload), "r21-pinned") {
				mu.Lock()
				deliveries++
				mu.Unlock()
			}
		}
	}()

	if err := s.recoverClaimedTicksWithoutReceipt(); err != nil {
		t.Fatalf("recoverClaimedTicksWithoutReceipt: %v", err)
	}
	// dispatchMissed runs async; give Execute a moment, then drain.
	time.Sleep(1500 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if deliveries != 1 {
		t.Errorf("session-pinned no-receipt claim delivered %d times in one pass, want exactly 1", deliveries)
	}
}

// TestJobKindWritesReceipts pins the predicate: only a session-pinned agent
// config counts as receipt-writing.
func TestJobKindWritesReceipts(t *testing.T) {
	if !jobKindWritesReceipts(JobConfig{AgentConfig: &AgentJobConfig{TargetSessionID: "sess"}}) {
		t.Error("session-pinned agent job must classify as receipt-writing")
	}
	for name, cfg := range map[string]JobConfig{
		"legacy agent":     {AgentConfig: &AgentJobConfig{}},
		"nil agent config": {},
		"shell job":        {ShellConfig: &ShellJobConfig{Command: "true"}},
		"reminder job":     {ReminderConfig: &ReminderJobConfig{Message: "m"}},
		"optimization job": {OptimizationConfig: &OptimizationJobConfig{Target: "memory_graph"}},
		"security job":     {SecurityConfig: &SecurityJobConfig{}},
		"learning job":     {LearningConfig: &LearningJobConfig{}},
	} {
		if jobKindWritesReceipts(cfg) {
			t.Errorf("%s must classify as receipt-less", name)
		}
	}
}
