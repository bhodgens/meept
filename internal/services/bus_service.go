package services

import (
	"context"
	"encoding/json"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// BusService handles bus subscription operations.
type BusService struct {
	bus *bus.MessageBus
}

// NewBusService creates a bus service.
func NewBusService(b *bus.MessageBus) *BusService {
	return &BusService{bus: b}
}

// PublishRequest contains publish parameters.
type PublishRequest struct {
	Topic   string         `json:"topic"`
	Type    string         `json:"type"`
	Source  string         `json:"source,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`
}

// Publish publishes a message to the bus.
func (s *BusService) Publish(ctx context.Context, req PublishRequest) error {
	if req.Topic == "" {
		return wrapError("bus", "Publish", ErrInvalidInput)
	}
	if s.bus == nil {
		return wrapError("bus", "Publish", ErrUnavailable)
	}

	msgType := models.MessageTypeEvent
	switch req.Type {
	case "command":
		msgType = models.MessageTypeRequest
	case "request":
		msgType = models.MessageTypeRequest
	case "response":
		msgType = models.MessageTypeResponse
	case "status_update":
		msgType = models.MessageTypeStatusUpdate
	case "error":
		msgType = models.MessageTypeError
	}

	// Convert payload to bytes
	payload := []byte{}
	if req.Payload != nil {
		var err error
		payload, err = json.Marshal(req.Payload)
		if err != nil {
			return wrapError("bus", "Publish", err)
		}
	}

	msg := &models.BusMessage{
		Type:    msgType,
		Topic:   req.Topic,
		Source:  req.Source,
		Payload: payload,
	}

	s.bus.Publish(req.Topic, msg)
	return nil
}

// BusStatsResponse contains bus statistics.
type BusStatsResponse struct {
	Subscribers    int   `json:"subscribers"`
	MessagesSent   int64 `json:"messages_sent"`
	QueuedMessages int   `json:"queued_messages"`
}

// Stats returns bus statistics.
func (s *BusService) Stats(ctx context.Context) (*BusStatsResponse, error) {
	if s.bus == nil {
		return &BusStatsResponse{}, nil
	}

	raw := s.bus.Stats()
	resp := &BusStatsResponse{}
	if v, ok := raw["_total"]; ok {
		resp.Subscribers = v
	}
	if v, ok := raw["_messages_sent"]; ok {
		resp.MessagesSent = int64(v)
	}
	if v, ok := raw["_queued"]; ok {
		resp.QueuedMessages = v
	}
	return resp, nil
}

// Subscribe creates a bus subscription. Returns the subscriber and an unsubscribe function.
// Returns nil if the bus is not available.
func (s *BusService) Subscribe(id, topic string) (sub *bus.Subscriber, cleanup func()) {
	if s.bus == nil {
		return nil, func() {}
	}
	sub = s.bus.Subscribe(id, topic)
	cleanup = func() {
		s.bus.Unsubscribe(sub)
	}
	return sub, cleanup
}
