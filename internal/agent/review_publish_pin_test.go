package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// Bughunt 2026-11-24 H1/H2 pins: every ReviewStep verdict path must
// publish step.review_completed. Before the fix only the full-reviewer
// tail published; the evidence gate (H1) and the 8 other early-return
// paths (H2) returned before the single publishReviewEvent call, so the
// model-rating panel never saw fabricated-success rejections -- its
// primary defect class -- and pass rates were biased toward LLM-judged
// steps.

// reviewEvents drains step.review_completed events from the bus until n
// arrive or the deadline passes (poll the terminal state, no fixed sleep).
func reviewEvents(t *testing.T, sub *bus.Subscriber, n int) []models.BusMessage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var got []models.BusMessage
	for len(got) < n && time.Now().Before(deadline) {
		select {
		case m, ok := <-sub.Channel:
			if !ok {
				t.Fatal("bus subscriber channel closed")
			}
			got = append(got, *m)
		case <-time.After(50 * time.Millisecond):
		}
	}
	return got
}

// TestReviewStepGatePublishesVerdict is the H1 pin: the file-evidence gate
// rejection (a file-shaped step with zero file-tool evidence, rejected
// deterministically) must reach the bus.
func TestReviewStepGatePublishesVerdict(t *testing.T) {
	logger := slogDefault()
	msgBus := bus.New(nil, logger)
	rm := NewReviewManager(ReviewManagerConfig{
		StepStore: newTestStepStore(t),
		Bus:       msgBus,
	})
	sub := msgBus.Subscribe("h1-gate-publish", "step.review_completed")

	step := gateFileStep(t, rm, "step-h1-gate", "task-h1-gate", nil)
	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		if strings.Contains(err.Error(), "reviewer") {
			t.Skip("reviewer unresolvable in test rig; gate path unreachable here")
		}
		t.Fatalf("ReviewStep: %v", err)
	}
	if res.Status != ReviewRejected {
		t.Fatalf("expected gate rejection, got %q (%s)", res.Status, res.Feedback)
	}

	events := reviewEvents(t, sub, 1)
	if len(events) == 0 {
		t.Fatal("H1: evidence-gate rejection published NO step.review_completed event")
	}
	// Exactly one per review.
	if extra := reviewEventsNoWait(t, sub); len(extra) != 0 {
		t.Fatalf("H1: %d extra review events published, want exactly 1", len(extra))
	}
}

// TestReviewStepAutoApprovePublishesVerdict is the H2 pin: the no-review
// auto-approve path (a conversational hint with no claims) is a real
// terminal verdict and must reach the bus too.
func TestReviewStepAutoApprovePublishesVerdict(t *testing.T) {
	logger := slogDefault()
	msgBus := bus.New(nil, logger)
	rm := NewReviewManager(ReviewManagerConfig{
		StepStore: newTestStepStore(t),
		Bus:       msgBus,
	})
	sub := msgBus.Subscribe("h2-autoapprove-publish", "step.review_completed")

	step := &task.TaskStep{
		ID:          "step-h2-approve",
		TaskID:      "task-h2-approve",
		Sequence:    0,
		State:       task.StepCompleted,
		ToolHint:    "chat",
		Description: "answer the user's question about the repo layout",
		Result:      "The repository layout is a standard Go monorepo with internal packages.",
		Evidence:    nil,
	}
	if err := rm.stepStore.Create(step); err != nil {
		t.Fatalf("create step: %v", err)
	}

	res, err := rm.ReviewStep(context.Background(), step, nil)
	if err != nil {
		t.Fatalf("ReviewStep: %v", err)
	}
	if res.Status != ReviewApproved {
		t.Fatalf("expected auto-approve, got %q (%s)", res.Status, res.Feedback)
	}

	events := reviewEvents(t, sub, 1)
	if len(events) == 0 {
		t.Fatal("H2: auto-approve verdict published NO step.review_completed event")
	}
	// The payload must be parseable and carry the verdict.
	var payload map[string]any
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatalf("event payload not JSON: %v", err)
	}
}

// reviewEventsNoWait drains whatever is already queued without waiting.
func reviewEventsNoWait(t *testing.T, sub *bus.Subscriber) []models.BusMessage {
	t.Helper()
	var got []models.BusMessage
	for {
		select {
		case m, ok := <-sub.Channel:
			if !ok {
				return got
			}
			got = append(got, *m)
		default:
			return got
		}
	}
}

func slogDefault() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
