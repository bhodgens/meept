package employee

import (
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/caimlas/meept/internal/bot"
)

// WebhookTriggerPattern is the one HTTP route for an enabled bot webhook
// trigger. The path id is the bot id: BotTrigger has no separate webhook
// id, so the URL id is the lookup key.
const WebhookTriggerPattern = "POST /api/v1/webhooks/{id}"

const webhookMaxBody = 1 << 20 // 1 MiB

// RegisterWebhookRoute mounts WebhookTriggerPattern on mux. Nil manager
// or mux is a no-op. The daemon calls this from HTTP setup.
func (m *Manager) RegisterWebhookRoute(mux *http.ServeMux) {
	if m == nil || mux == nil {
		return
	}
	mux.HandleFunc(WebhookTriggerPattern, m.handleWebhookTrigger)
}

// handleWebhookTrigger looks the bot up by the path id, requires an
// enabled webhook trigger, and calls Trigger with source "webhook" and
// the raw request body as the prompt. Unknown id and a disabled (or
// absent) webhook trigger both return 404. The raw body is never logged;
// the accepted path logs the bot id and the byte length only.
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
			// MaxBytesReader already wrote 413.
			return
		}
		http.Error(w, `{"error":"read body"}`, http.StatusBadRequest)
		return
	}

	def, err := m.botManager.GetBot(r.Context(), id)
	if err != nil || def == nil || !enabledWebhookTrigger(def) {
		http.NotFound(w, r)
		return
	}

	m.logWebhook(def.ID, len(body))
	if _, err := m.Trigger(r.Context(), def.ID, map[string]any{
		"source": "webhook",
		"prompt": string(body),
	}); err != nil {
		// Error text can echo the body. The accept log above already
		// recorded bot id and byte length; do not log err.
		http.Error(w, `{"error":"trigger failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("{\"status\":\"triggered\"}\n")); err != nil {
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
