package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// BotTriggerHandler is the callback invoked when a bot's trigger fires.
type BotTriggerHandler interface {
	HandleBotTrigger(ctx context.Context, botID string, prompt string) error
}

// EventActionRouter subscribes to bus topics and routes events to bots.
type EventActionRouter struct {
	bus     *bus.MessageBus
	handler BotTriggerHandler
	logger  *slog.Logger

	mu        sync.RWMutex
	topicSubs map[string]map[string]BotTrigger // topic -> set of bot IDs
}

// NewEventActionRouter creates a new event-to-action router.
func NewEventActionRouter(msgBus *bus.MessageBus, handler BotTriggerHandler) *EventActionRouter {
	return &EventActionRouter{
		bus:       msgBus,
		handler:   handler,
		logger:    slog.Default(),
		topicSubs: make(map[string]map[string]BotTrigger),
	}
}

// Register adds a bot's bus_event triggers to the router.
func (r *EventActionRouter) Register(def BotDefinition) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, trigger := range def.Triggers {
		if trigger.Type != TriggerTypeBusEvent || !trigger.Enabled {
			continue
		}
		if r.topicSubs[trigger.Topic] == nil {
			r.topicSubs[trigger.Topic] = make(map[string]BotTrigger)
		}
		r.topicSubs[trigger.Topic][def.ID] = trigger
		r.logger.Info("registered bot for bus event", "bot_id", def.ID, "topic", trigger.Topic)
	}
}

// Unregister removes all bus_event subscriptions for a bot.
func (r *EventActionRouter) Unregister(botID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for topic, bots := range r.topicSubs {
		delete(bots, botID)
		if len(bots) == 0 {
			delete(r.topicSubs, topic)
		}
	}
	r.logger.Info("unregistered bot from bus events", "bot_id", botID)
}

// Start begins listening for bus events on all registered topics.
func (r *EventActionRouter) Start(ctx context.Context) {
	r.mu.RLock()
	topics := make([]string, 0, len(r.topicSubs))
	for topic := range r.topicSubs {
		topics = append(topics, topic)
	}
	r.mu.RUnlock()

	for _, topic := range topics {
		sub := r.bus.Subscribe("bot-router-"+topic, topic)
		go func(topic string, ch <-chan *models.BusMessage) {
			for {
				select {
				case <-ctx.Done():
					r.bus.Unsubscribe(sub)
					return
				case msg, ok := <-ch:
					if !ok {
						return
					}
					r.handleEvent(ctx, topic, msg)
				}
			}
		}(topic, sub.Channel)
	}
	r.logger.Info("event action router started", "topics", topics)
}

func (r *EventActionRouter) handleEvent(ctx context.Context, topic string, msg *models.BusMessage) {
	r.mu.RLock()
	bots := r.topicSubs[topic]
	r.mu.RUnlock()

	for botID, trigger := range bots {
		prompt := r.buildPrompt(trigger, msg)
		if r.handler != nil {
			if err := r.handler.HandleBotTrigger(ctx, botID, prompt); err != nil {
				r.logger.Error("bot trigger handler failed", "bot_id", botID, "topic", topic, "error", err)
			}
		}
	}
}

func (r *EventActionRouter) buildPrompt(trigger BotTrigger, msg *models.BusMessage) string {
	if trigger.PromptTemplate != "" {
		var payload map[string]any
		if err := json.Unmarshal(msg.Payload, &payload); err == nil {
			return expandTemplate(trigger.PromptTemplate, payload)
		}
	}
	return fmt.Sprintf("Event received on topic %s from %s", trigger.Topic, msg.Source)
}

// expandTemplate does simple {{.Key}} substitution.
func expandTemplate(tmpl string, data map[string]any) string {
	result := tmpl
	for k, v := range data {
		old := "{{." + k + "}}"
		result = strings.ReplaceAll(result, old, fmt.Sprintf("%v", v))
	}
	return result
}
