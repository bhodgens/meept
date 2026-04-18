package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// Orchestrator coordinates the strategic and tactical layers via bus subscriptions.
type Orchestrator struct {
	strategic *StrategicPlanner
	tactical  *TacticalScheduler
	bus       *bus.MessageBus
	logger    *slog.Logger

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// OrchestratorDeps holds dependencies for the orchestrator.
type OrchestratorDeps struct {
	Strategic *StrategicPlanner
	Tactical  *TacticalScheduler
	Bus       *bus.MessageBus
	Logger    *slog.Logger
}

// NewOrchestrator creates a new orchestrator.
func NewOrchestrator(deps OrchestratorDeps) *Orchestrator {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}

	return &Orchestrator{
		strategic: deps.Strategic,
		tactical:  deps.Tactical,
		bus:       deps.Bus,
		logger:    deps.Logger,
	}
}

// Start subscribes to orchestrator bus topics and begins processing.
func (o *Orchestrator) Start(ctx context.Context) error {
	ctx, o.cancel = context.WithCancel(ctx)

	topics := map[string]func(context.Context, *models.BusMessage){
		"orchestrator.plan":     o.handlePlanRequest,
		"orchestrator.schedule": o.handleScheduleRequest,
		"queue.job.completed":   o.handleJobCompleted,
		"queue.job.failed":      o.handleJobFailed,
	}

	for topic, handler := range topics {
		sub := o.bus.Subscribe("orchestrator-"+topic, topic)
		o.wg.Add(1)
		go o.runSubscription(ctx, sub, handler)
	}

	o.logger.Info("Orchestrator started",
		"subscriptions", len(topics),
	)
	return nil
}

// Stop gracefully stops the orchestrator.
func (o *Orchestrator) Stop(ctx context.Context) error {
	if o.cancel != nil {
		o.cancel()
	}

	done := make(chan struct{})
	go func() {
		o.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		o.logger.Info("Orchestrator stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Name returns the component name.
func (o *Orchestrator) Name() string {
	return "orchestrator"
}

func (o *Orchestrator) runSubscription(ctx context.Context, sub *bus.Subscriber, handler func(context.Context, *models.BusMessage)) {
	defer o.wg.Done()
	for {
		select {
		case <-ctx.Done():
			o.bus.Unsubscribe(sub)
			return
		case msg, ok := <-sub.Channel:
			if !ok {
				return
			}
			handler(ctx, msg)
		}
	}
}

func (o *Orchestrator) handlePlanRequest(ctx context.Context, msg *models.BusMessage) {
	var req PlanRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		o.logger.Error("Failed to parse plan request", "error", err)
		return
	}

	o.logger.Info("Received plan request",
		"task_id", req.TaskID,
		"session_id", req.SessionID,
	)

	if err := o.strategic.Plan(ctx, req); err != nil {
		o.logger.Error("Strategic planning failed",
			"task_id", req.TaskID,
			"error", err,
		)
	}
}

func (o *Orchestrator) handleScheduleRequest(ctx context.Context, msg *models.BusMessage) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		o.logger.Error("Failed to parse schedule request", "error", err)
		return
	}

	o.logger.Info("Received schedule request", "task_id", req.TaskID)

	if err := o.tactical.ScheduleReadySteps(ctx, req.TaskID); err != nil {
		o.logger.Error("Tactical scheduling failed",
			"task_id", req.TaskID,
			"error", err,
		)
	}
}

func (o *Orchestrator) handleJobCompleted(ctx context.Context, msg *models.BusMessage) {
	var event struct {
		JobID  string          `json:"job_id"`
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse job completed event", "error", err)
		return
	}

	o.logger.Info("DONE job completed event received", "job_id", event.JobID)

	if err := o.tactical.OnJobCompleted(ctx, event.JobID, event.Result); err != nil {
		o.logger.Error("Failed to handle job completion",
			"job_id", event.JobID,
			"error", err,
		)
	}
}

func (o *Orchestrator) handleJobFailed(ctx context.Context, msg *models.BusMessage) {
	var event struct {
		JobID string `json:"job_id"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(msg.Payload, &event); err != nil {
		o.logger.Error("Failed to parse job failed event", "error", err)
		return
	}

	o.logger.Info("FAIL job failed event received",
		"job_id", event.JobID,
		"error", event.Error,
	)

	if err := o.tactical.OnJobFailed(ctx, event.JobID, event.Error); err != nil {
		o.logger.Error("Failed to handle job failure",
			"job_id", event.JobID,
			"error", err,
		)
	}
}
