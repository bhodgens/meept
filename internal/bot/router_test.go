package bot

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

type mockHandler struct {
	mu          sync.Mutex
	invocations []routerInvocation
}

type routerInvocation struct {
	BotID  string
	Prompt string
}

func (m *mockHandler) HandleBotTrigger(ctx context.Context, botID string, prompt string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.invocations = append(m.invocations, routerInvocation{BotID: botID, Prompt: prompt})
	return nil
}

func (m *mockHandler) getInvocations() []routerInvocation {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]routerInvocation{}, m.invocations...)
}

func TestEventActionRouter_BusEvent(t *testing.T) {
	msgBus := bus.New(nil, nil)
	handler := &mockHandler{}
	router := NewEventActionRouter(msgBus, handler)

	def := BotDefinition{
		ID:   "calendar-bot",
		Name: "Calendar Bot",
		Triggers: []BotTrigger{
			{
				Type:          TriggerTypeBusEvent,
				Topic:         "calendar.reminder",
				PromptTemplate: "Calendar event: {{.summary}} starts in {{.starts_in}}",
				Enabled:       true,
			},
		},
	}

	router.Register(def)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	router.Start(ctx)

	// Give subscriptions time to register
	time.Sleep(100 * time.Millisecond)

	payload := map[string]any{
		"event_id":  "evt-1",
		"summary":   "Team standup",
		"starts_in": "5 minutes",
	}
	msg, _ := models.NewBusMessage(models.MessageTypeEvent, "test", payload)
	msgBus.Publish("calendar.reminder", msg)

	time.Sleep(200 * time.Millisecond)

	invocations := handler.getInvocations()
	if len(invocations) != 1 {
		t.Fatalf("expected 1 invocation, got %d", len(invocations))
	}
	if invocations[0].BotID != "calendar-bot" {
		t.Errorf("BotID = %q, want %q", invocations[0].BotID, "calendar-bot")
	}
}

func TestEventActionRouter_Unregister(t *testing.T) {
	msgBus := bus.New(nil, nil)
	handler := &mockHandler{}
	router := NewEventActionRouter(msgBus, handler)

	def := BotDefinition{
		ID:   "test-bot",
		Name: "Test",
		Triggers: []BotTrigger{
			{Type: TriggerTypeBusEvent, Topic: "test.topic", Enabled: true},
		},
	}

	router.Register(def)
	router.Unregister("test-bot")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	router.Start(ctx)

	time.Sleep(100 * time.Millisecond)

	payload := map[string]any{"key": "value"}
	msg, _ := models.NewBusMessage(models.MessageTypeEvent, "test", payload)
	msgBus.Publish("test.topic", msg)

	time.Sleep(200 * time.Millisecond)

	invocations := handler.getInvocations()
	if len(invocations) != 0 {
		t.Errorf("expected 0 invocations after unregister, got %d", len(invocations))
	}
}

func TestExpandTemplate(t *testing.T) {
	result := expandTemplate("Hello {{.name}}, you have {{.count}} items", map[string]any{
		"name":  "World",
		"count": 42,
	})
	if result != "Hello World, you have 42 items" {
		t.Errorf("expandTemplate = %q, want %q", result, "Hello World, you have 42 items")
	}
}
