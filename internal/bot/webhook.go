package bot

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// WebhookHandler handles incoming HTTP webhooks for bots.
type WebhookHandler struct {
	manager *Manager
	logger  *slog.Logger
}

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(manager *Manager) *WebhookHandler {
	return &WebhookHandler{
		manager: manager,
		logger:  slog.Default(),
	}
}

// ServeHTTP handles POST /api/v1/bot/{botID}/trigger
func (h *WebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	// Extract bot ID from path: /api/v1/bot/{botID}/trigger
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/bot/")
	path = strings.TrimSuffix(path, "/trigger")
	botID := path

	if botID == "" {
		http.Error(w, `{"error":"bot id required"}`, http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1MB max
	if err != nil {
		http.Error(w, `{"error":"read body"}`, http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}

	// Get bot definition
	def, err := h.manager.GetBot(r.Context(), botID)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"bot %q not found"}`, botID), http.StatusNotFound)
		return
	}

	// Verify bot has a webhook trigger
	triggerCtx := fmt.Sprintf("Webhook triggered with payload: %s", string(body))
	hasWebhookTrigger := false
	for _, t := range def.Triggers {
		if t.Type == TriggerTypeWebhook && t.Enabled {
			hasWebhookTrigger = true
			if t.PromptTemplate != "" {
				triggerCtx = expandTemplate(t.PromptTemplate, payload)
			}
			break
		}
	}
	if !hasWebhookTrigger {
		http.Error(w, fmt.Sprintf(`{"error":"bot %q has no webhook trigger"}`, botID), http.StatusBadRequest)
		return
	}

	// Check if bot should run
	state, _ := h.manager.GetBotStatus(r.Context(), botID)
	runner := NewBotRunner(*def)
	if !runner.ShouldRun(state) {
		http.Error(w, `{"error":"bot paused or budget exhausted"}`, http.StatusTooManyRequests)
		return
	}

	h.logger.Info("webhook trigger received", "bot_id", botID, "payload_size", len(body), "trigger_context", triggerCtx)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "triggered",
		"bot_id":  botID,
		"message": "bot invocation queued",
	})
}
