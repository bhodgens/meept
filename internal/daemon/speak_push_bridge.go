package daemon

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/services"
)

// wireSpeakPushBridge subscribes employee.notify (harness-eval leaf 11,
// SpeakTopicNotify) and forwards each notification to the push service so
// Telegram/TUI/menubar push channels receive detached-run notifications.
// The WS bridge (internal/comm/http/server.go) independently forwards the
// same topic to browser clients; this bridge covers everything else.
//
// Notifications name one session: the standing employee chat session
// (payload.SessionID, set by SetSpeakContext) or the voice call's own
// session. An empty session id is not a broadcast.
func wireSpeakPushBridge(msgBus *bus.MessageBus, push *services.PushService, logger *slog.Logger) {
	if msgBus == nil || push == nil {
		return
	}
	sub := msgBus.Subscribe("speak-push-bridge", agent.SpeakTopicNotify)
	if sub == nil {
		return
	}
	go func() {
		for msg := range sub.Channel {
			if msg == nil || len(msg.Payload) == 0 {
				continue
			}
			var payload agent.NotifyPayload
			if err := json.Unmarshal(msg.Payload, &payload); err != nil {
				logger.Warn("speak push bridge: bad employee.notify payload", "error", err)
				continue
			}
			if payload.Text == "" {
				continue
			}
			if payload.SessionID == "" {
				logger.Warn("speak push bridge: notify has no session; not broadcasting")
				continue
			}
			_, err := push.Push(context.Background(), &services.PushRequest{
				SessionIDs: []string{payload.SessionID},
				Source:     "employee",
				Type:       services.PushTypeAlert,
				Content:    payload.Text,
				Priority:   services.PushPriorityNormal,
				Extra: map[string]any{
					"session_id":      payload.SessionID,
					"conversation_id": payload.ConversationID,
				},
			})
			if err != nil {
				logger.Warn("speak push bridge: push failed", "error", err)
			}
		}
	}()
	logger.Info("speak push bridge wired", "topic", agent.SpeakTopicNotify)
}
