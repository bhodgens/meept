package task

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// AmendmentHandlerFunc handles an amendment request.
type AmendmentHandlerFunc func(context.Context, *AmendmentRequest) (*AmendmentReply, error)

// AmendmentManager manages amendment requests and routing.
type AmendmentManager struct {
	ctx       context.Context
	cancel    context.CancelFunc
	sub       *bus.Subscriber // bus subscription for cleanup
	mu        sync.RWMutex
	bus       *bus.MessageBus
	logger    *slog.Logger
	handlers  map[AmendmentType]AmendmentHandlerFunc
	pending   map[string]*AmendmentRequest // requestID -> request
	taskIndex map[string][]string          // taskID -> []requestID
}

// NewAmendmentManager creates a new amendment manager.
func NewAmendmentManager(msgBus *bus.MessageBus, logger *slog.Logger) *AmendmentManager {
	if logger == nil {
		logger = slog.Default()
	}
	mgr := &AmendmentManager{
		ctx:       context.Background(),
		cancel:    func() {},

		bus:       msgBus,
		logger:    logger,
		handlers:  make(map[AmendmentType]AmendmentHandlerFunc),
		pending:   make(map[string]*AmendmentRequest),
		taskIndex: make(map[string][]string),
	}

	// Start subscription goroutine
	mgr.subscribe()

	return mgr
}

// RegisterHandler registers a handler for an amendment type.
func (m *AmendmentManager) RegisterHandler(typ AmendmentType, handler AmendmentHandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[typ] = handler
	m.logger.Debug("Registered amendment handler", "type", typ)
}

// Submit submits an amendment request.
func (m *AmendmentManager) Submit(ctx context.Context, req *AmendmentRequest) error {
	m.mu.Lock()
	m.pending[req.ID] = req
	m.taskIndex[req.TaskID] = append(m.taskIndex[req.TaskID], req.ID)
	m.mu.Unlock()

	// Publish to bus
	payload, err := json.Marshal(req)
	if err != nil {
		m.logger.Error("Failed to marshal amendment request", "error", err)
		payload = []byte("{}")
	}
	msg := &models.BusMessage{
		ID:        req.ID,
		Type:      models.MessageTypeRequest,
		Topic:     "task.amend.request",
		Source:    "amendment-manager",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}

	m.bus.Publish("task.amend.request", msg)

	m.logger.Info("Amendment submitted",
		"request_id", req.ID,
		KeyTaskID, req.TaskID,
		"type", req.Type,
	)

	return nil
}

// GetPending returns a pending request by ID.
func (m *AmendmentManager) GetPending(requestID string) (*AmendmentRequest, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	req, ok := m.pending[requestID]
	return req, ok
}

// GetPendingForTask returns all pending requests for a task.
func (m *AmendmentManager) GetPendingForTask(taskID string) []*AmendmentRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	requestIDs := m.taskIndex[taskID]
	var requests []*AmendmentRequest
	for _, id := range requestIDs {
		if req, ok := m.pending[id]; ok && req.Status == AmendmentPending {
			requests = append(requests, req)
		}
	}
	return requests
}

// Process applies a handler to a pending request.
func (m *AmendmentManager) Process(ctx context.Context, requestID string) (*AmendmentReply, error) {
	m.mu.Lock()
	req, ok := m.pending[requestID]
	if !ok {
		m.mu.Unlock()
		return nil, fmt.Errorf("request not found: %s", requestID)
	}
	handler := m.handlers[req.Type]
	m.mu.Unlock()

	if handler == nil {
		reply := &AmendmentReply{
			RequestID: requestID,
			Success:   false,
			Message:   fmt.Sprintf("no handler for amendment type: %s", req.Type),
		}
		req.Status = AmendmentRejected
		return reply, nil
	}

	// Call handler
	reply, err := handler(ctx, req)
	if err != nil {
		req.Status = AmendmentRejected
		return nil, err
	}

	if reply.Success {
		req.Status = AmendmentApplied
		req.ProcessedAt = time.Now().UTC()
		m.publishEvent("task.amend.applied", req)
	} else {
		req.Status = AmendmentRejected
		m.publishEvent("task.amend.rejected", req)
	}

	return reply, nil
}

// CancelPendingForTask marks all pending amendments for a task as ignored.
func (m *AmendmentManager) CancelPendingForTask(taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	requestIDs := m.taskIndex[taskID]
	for _, id := range requestIDs {
		if req, ok := m.pending[id]; ok && req.Status == AmendmentPending {
			req.Status = AmendmentIgnored
			m.logger.Debug("Ignored pending amendment due to task cancellation",
				"request_id", id,
				KeyTaskID, taskID,
			)
		}
	}
}

func (m *AmendmentManager) subscribe() {
	m.ctx, m.cancel = context.WithCancel(context.Background())
	sub := m.bus.Subscribe("amendment-manager", "task.amend.request")
	m.sub = sub

	go func() {
		defer m.bus.Unsubscribe(sub)
		for {
			select {
			case <-m.ctx.Done():
				return
			case msg, ok := <-sub.Channel:
				if !ok {
					return
				}
				var req AmendmentRequest
				if err := json.Unmarshal(msg.Payload, &req); err != nil {
					m.logger.Error("Failed to parse amendment request", "error", err)
					continue
				}

				// Auto-process if handler registered
				m.logger.Debug("Received amendment request", "id", req.ID, "type", req.Type)
			}
		}
	}()
}

func (m *AmendmentManager) publishEvent(topic string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		m.logger.Error("Failed to marshal event data", "error", err)
		payload = []byte("{}")
	}
	msg := &models.BusMessage{
		ID:        id.Generate("amend-"),
		Type:      models.MessageTypeEvent,
		Topic:     topic,
		Source:    "amendment-manager",
		Timestamp: time.Now().UTC(),
		Payload:   payload,
	}
	m.bus.Publish(topic, msg)
}

// Close shuts down the amendment manager.
func (m *AmendmentManager) Close() error {
	// Snapshot cancel under the lock, release the lock before calling
	// cancel() to avoid holding the mutex across the context-cancellation
	// propagation (S6-11 — mu held across cancel()).
	m.mu.Lock()
	cancelFn := m.cancel
	pendingSnapshot := m.pending
	m.pending = nil // prevent further mutation by other callers
	m.mu.Unlock()

	// Mark all pending as ignored BEFORE cancelling so observers see the
	// final status rather than "pending-then-cancelled" tearing.
	m.mu.Lock()
	for _, req := range pendingSnapshot {
		if req.Status == AmendmentPending {
			req.Status = AmendmentIgnored
		}
	}
	m.pending = pendingSnapshot
	m.mu.Unlock()

	// Cancel the subscription context outside the lock.
	if cancelFn != nil {
		cancelFn()
	}
	return nil
}
