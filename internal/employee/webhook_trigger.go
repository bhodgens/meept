package employee

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/caimlas/meept/internal/bot"
)

// WebhookTriggerPattern is the one HTTP route for an enabled bot webhook
// trigger. The path id is the bot id: BotTrigger has no separate webhook
// id, so the URL id is the lookup key.
const WebhookTriggerPattern = "POST /api/v1/webhooks/{id}"

const webhookMaxBody = 1 << 20 // 1 MiB

// webhookTriggerTimeout bounds the detached goal round. The route returns
// 202 immediately; the Trigger run (which serializes under invokeMu and can
// take minutes) runs in the background with this hard ceiling so an
// abandoned round cannot outlive the daemon's patience.
const webhookTriggerTimeout = 10 * time.Minute

// RegisterWebhookRoute mounts WebhookTriggerPattern on mux. Nil manager
// or mux is a no-op. The daemon calls this from HTTP setup.
func (m *Manager) RegisterWebhookRoute(mux *http.ServeMux) {
	if m == nil || mux == nil {
		return
	}
	// The path is a string literal so the connectivity-graph scanner can see it.
	// WebhookTriggerPattern must stay the same text.
	mux.HandleFunc("POST /api/v1/webhooks/{id}", m.handleWebhookTrigger)
}

// handleWebhookTrigger looks the bot up by the path id, requires an
// enabled webhook trigger, and returns 202 immediately; the Trigger run
// (source "webhook", raw request body as the prompt) proceeds in a
// detached goroutine bounded by webhookTriggerTimeout. Unknown id and a
// disabled (or absent) webhook trigger both return 404; a bot-store
// failure returns 500. The raw body is never logged; the accepted path
// logs the bot id and the byte length only.
func (m *Manager) handleWebhookTrigger(w http.ResponseWriter, r *http.Request) {
	if m == nil || m.botManager == nil {
		http.Error(w, `{"error":"not configured"}`, http.StatusServiceUnavailable)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, webhookMaxBody))
	if err != nil {
		var maxBytes *http.MaxBytesError
		if errors.As(err, &maxBytes) {
			// MaxBytesReader may have started the response already; the
			// explicit status here is what clients on a fresh response see.
			http.Error(w, `{"error":"request body too large"}`, http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}

	def, err := m.botManager.GetBot(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		// Store failure, not a missing bot: 500, and log the id only
		// (error text could echo internal state).
		logger := m.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Error("webhook trigger lookup failed", "bot_id", id)
		http.Error(w, `{"error":"store unavailable"}`, http.StatusInternalServerError)
		return
	}
	if def == nil || !enabledWebhookTrigger(def) {
		http.NotFound(w, r)
		return
	}

	m.logWebhook(def.ID, len(body))

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), webhookTriggerTimeout)
		defer cancel()
		if _, err := m.Trigger(ctx, def.ID, map[string]any{
			"source": "webhook",
			"prompt": string(body),
		}); err != nil {
			// Error text can echo the body. The accept log above already
			// recorded bot id and byte length; do not log err.
			logger := m.logger
			if logger == nil {
				logger = slog.Default()
			}
			logger.Error("webhook trigger failed", "bot_id", def.ID)
		}
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	if _, err := w.Write([]byte("{\"status\":\"accepted\"}\n")); err != nil {
		return
	}
}

func enabledWebhookTrigger(def *bot.BotDefinition) bool {
	if def == nil {
		return false
	}
	for _, trigger := range def.Triggers {
		if trigger.Type == bot.TriggerTypeWebhook && trigger.Enabled {
			return true
		}
	}
	return false
}

func (m *Manager) logWebhook(botID string, n int) {
	logger := m.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("webhook trigger", "bot_id", botID, "bytes", n)
}
