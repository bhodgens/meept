package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/internal/session"
)

// stampableQueue is a minimal queue.Queue fake that captures enqueued jobs.
type stampableQueue struct {
	jobs []*queue.Job
}

func (s *stampableQueue) Enqueue(_ context.Context, job *queue.Job) error {
	s.jobs = append(s.jobs, job)
	return nil
}
func (s *stampableQueue) Claim(_ context.Context, _ string, _ []string, _ string) (*queue.Job, error) {
	return nil, queue.ErrNoJobAvailable
}
func (s *stampableQueue) MarkProcessing(_ context.Context, _ string) error { return nil }
func (s *stampableQueue) Complete(_ context.Context, _ string, _ any) error {
	return nil
}
func (s *stampableQueue) Fail(_ context.Context, _ string, _ error) error { return nil }
func (s *stampableQueue) Retry(_ context.Context, _ string) error         { return nil }
func (s *stampableQueue) Get(_ context.Context, _ string) (*queue.Job, error) {
	return nil, errors.New("not implemented")
}
func (s *stampableQueue) ListByState(_ context.Context, _ queue.JobState, _ int) ([]*queue.Job, error) {
	return nil, nil
}
func (s *stampableQueue) ListByTaskID(_ context.Context, _ string) ([]*queue.Job, error) {
	return nil, nil
}
func (s *stampableQueue) Stats(_ context.Context) (*queue.QueueStats, error) {
	return &queue.QueueStats{}, nil
}
func (s *stampableQueue) RecoverFromDeadLetter(_ context.Context, _ string) (*queue.Job, error) {
	return nil, errors.New("not implemented")
}
func (s *stampableQueue) ListDeadLetter(_ context.Context, _ int) ([]*queue.Job, error) {
	return nil, nil
}
func (s *stampableQueue) DeadLetterStats(_ context.Context) (int, error) {
	return 0, nil
}
func (s *stampableQueue) Close() error { return nil }

// TestQueueService_Enqueue_StampInteractiveFromSession pins the leaf 02
// stamping contract: the only enqueue request that carries session_id today
// stamps Interactive from session.IsInteractive(origin, now, window).
func TestQueueService_Enqueue_StampInteractiveFromSession(t *testing.T) {
	base := time.Now().UTC()

	tests := []struct {
		name         string
		lastMessageA time.Time // zero = never messaged
		foreground   bool
		window       time.Duration
		wantFlag     bool
	}{
		{
			name:         "recent user message stamps interactive",
			lastMessageA: base,
			window:       5 * time.Minute,
			wantFlag:     true,
		},
		{
			name:         "message older than window stamps background",
			lastMessageA: base.Add(-10 * time.Minute),
			window:       5 * time.Minute,
			wantFlag:     false,
		},
		{
			name:       "foreground flag stamps interactive",
			foreground: true,
			window:     5 * time.Minute,
			wantFlag:   true,
		},
		{
			name:     "no signal stamps background",
			window:   5 * time.Minute,
			wantFlag: false,
		},
		{
			name:         "zero window means only foreground qualifies",
			lastMessageA: base,
			window:       0,
			wantFlag:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := base

			sessions := session.NewMemoryStore(nil)
			created, err := sessions.Create("origin")
			if err != nil {
				t.Fatalf("create session: %v", err)
			}
			originID := created.ID
			if !tt.lastMessageA.IsZero() {
				if err := sessions.SetLastUserMessage(originID, tt.lastMessageA); err != nil {
					t.Fatalf("set last user message: %v", err)
				}
			}
			if tt.foreground {
				if err := sessions.SetForeground(originID, true); err != nil {
					t.Fatalf("set foreground: %v", err)
				}
			}

			fake := &stampableQueue{}
			q := NewQueueService(fake)

			req := EnqueueRequest{
				Type:      "one_off",
				Prompt:    "test prompt",
				SessionID: originID,
			}

			job, err := q.enqueueWithStamp(context.Background(), req, sessions, now, tt.window)
			if err != nil {
				t.Fatalf("enqueueWithStamp: %v", err)
			}
			if job.Interactive != tt.wantFlag {
				t.Errorf("Interactive = %v, want %v", job.Interactive, tt.wantFlag)
			}
			if len(fake.jobs) != 1 {
				t.Errorf("expected job persisted via q.Enqueue, got %d", len(fake.jobs))
			}
		})
	}
}

// TestQueueService_Enqueue_NoSessionIDStampsFalse pins the R4 companion
// case: requests without a session stamp false by construction.
func TestQueueService_Enqueue_NoSessionIDStampsFalse(t *testing.T) {
	q := NewQueueService(&stampableQueue{})

	job, err := q.enqueueWithStamp(context.Background(),
		EnqueueRequest{Type: "one_off", Prompt: "no origin"}, nil, time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("enqueueWithStamp: %v", err)
	}
	if job.Interactive {
		t.Errorf("session-less request stamped Interactive=true, want false")
	}
}

// TestStampInteractive pins the exported helper directly (Contract 2): a nil
// origin session stamps false; the helper never panics on nil inputs.
func TestStampInteractive(t *testing.T) {
	job, err := queue.NewJob(queue.JobTypeOneOff, map[string]string{"prompt": "x"})
	if err != nil {
		t.Fatalf("NewJob: %v", err)
	}

	// nil session → false, no panic.
	StampInteractive(job, nil, time.Now(), 5*time.Minute)
	if job.Interactive {
		t.Errorf("nil session stamped Interactive=true, want false")
	}

	sessions := session.NewMemoryStore(nil)
	live, err := sessions.Create("live")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := sessions.SetLastUserMessage(live.ID, time.Now().UTC()); err != nil {
		t.Fatalf("set last user message: %v", err)
	}

	StampInteractive(job, sessions.Get(live.ID), time.Now(), 5*time.Minute)
	if !job.Interactive {
		t.Errorf("live session stamped Interactive=false, want true")
	}
}
