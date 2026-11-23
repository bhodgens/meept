package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
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
func (s *stampableQueue) MarkProcessing(_ context.Context, _ string, _ string) error { return nil }
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

// L4 pin: at the HTTP service layer an ambiguous multi-thread queue match
// must carry the resolver's actionable guidance with an ErrInvalidInput
// marker (HTTP 400), NOT ErrQueueNotFound (HTTP 404). A true miss stays 404.
func TestChatService_AmbiguousQueue_IsActionable(t *testing.T) {
	msgBus := bus.New(nil, nil)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{MessageBus: msgBus})
	defer reg.Close()

	reg.RegisterActiveQueue("conv-amb-thread-work-aa01", agent.NewMessageQueue(), nil)
	reg.RegisterActiveQueue("conv-amb-thread-work-bb02", agent.NewMessageQueue(), nil)
	svc := NewChatService(msgBus, reg, nil)

	checks := []struct {
		name string
		call func() error
	}{
		{"Steer", func() error {
			return svc.Steer(context.Background(), SteerRequest{Message: "change", ConversationID: "conv-amb"})
		}},
		{"FollowUp", func() error {
			return svc.FollowUp(context.Background(), FollowUpRequest{Message: "more", ConversationID: "conv-amb"})
		}},
		{"GetQueueStatus", func() error {
			_, err := svc.GetQueueStatus(context.Background(), QueueStatusRequest{ConversationID: "conv-amb"})
			return err
		}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			err := check.call()
			if err == nil {
				t.Fatal("expected an actionable error for an ambiguous queue")
			}
			if !errors.Is(err, agent.ErrAmbiguousQueue) {
				t.Errorf("error = %v, want it to wrap ErrAmbiguousQueue", err)
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("error = %v, want ErrInvalidInput (HTTP 400)", err)
			}
			if errors.Is(err, agent.ErrQueueNotFound) {
				t.Errorf("error = %v, ambiguity must NOT be reported as ErrQueueNotFound (HTTP 404)", err)
			}
			if !strings.Contains(err.Error(), "thread conversation id") {
				t.Errorf("error = %q, want the resolver's actionable thread-id guidance", err.Error())
			}
		})
	}
}

func TestChatService_NotFound_StaysQueueNotFound(t *testing.T) {
	msgBus := bus.New(nil, nil)
	reg := agent.NewAgentRegistry(agent.RegistryConfig{MessageBus: msgBus})
	defer reg.Close()
	svc := NewChatService(msgBus, reg, nil)

	err := svc.Steer(context.Background(), SteerRequest{Message: "x", ConversationID: "conv-nope"})
	if !errors.Is(err, agent.ErrQueueNotFound) {
		t.Errorf("error = %v, want ErrQueueNotFound for a true miss", err)
	}
	if errors.Is(err, ErrInvalidInput) {
		t.Errorf("error = %v, a true miss must not be ErrInvalidInput (HTTP 400)", err)
	}
}
