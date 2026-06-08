package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func testWebhookHandler(t *testing.T) (*WebhookHandler, *Manager) {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	mgr := NewManager(store, nil)
	handler := NewWebhookHandler(mgr)
	return handler, mgr
}

func TestWebhookHandler_PostTriggersBot(t *testing.T) {
	handler, mgr := testWebhookHandler(t)
	ctx := context.Background()

	def := testBotDef("webhook-test")
	def.Triggers = []BotTrigger{
		{Type: TriggerTypeWebhook, Enabled: true},
	}
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	mgr.CreateBot(ctx, def)

	payload := map[string]any{"action": "deploy", "repo": "meept"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bot/webhook-test/trigger", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "triggered" {
		t.Errorf("status = %v, want %q", resp["status"], "triggered")
	}
	if resp["bot_id"] != "webhook-test" {
		t.Errorf("bot_id = %v, want %q", resp["bot_id"], "webhook-test")
	}
}

func TestWebhookHandler_GetMethodNotAllowed(t *testing.T) {
	handler, _ := testWebhookHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/bot/test/trigger", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestWebhookHandler_BotNotFound(t *testing.T) {
	handler, _ := testWebhookHandler(t)

	payload := map[string]any{"key": "value"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bot/nonexistent/trigger", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestWebhookHandler_NoWebhookTrigger(t *testing.T) {
	handler, mgr := testWebhookHandler(t)
	ctx := context.Background()

	def := testBotDef("cron-only-bot")
	def.Triggers = []BotTrigger{
		{Type: TriggerTypeCron, Schedule: "*/5 * * * *", Enabled: true},
	}
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	mgr.CreateBot(ctx, def)

	payload := map[string]any{"key": "value"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bot/cron-only-bot/trigger", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestWebhookHandler_InvalidJSON(t *testing.T) {
	handler, mgr := testWebhookHandler(t)
	ctx := context.Background()

	def := testBotDef("json-test")
	def.Triggers = []BotTrigger{
		{Type: TriggerTypeWebhook, Enabled: true},
	}
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	mgr.CreateBot(ctx, def)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bot/json-test/trigger", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestWebhookHandler_BudgetExhausted(t *testing.T) {
	handler, mgr := testWebhookHandler(t)
	ctx := context.Background()

	def := testBotDef("budget-bot")
	def.Triggers = []BotTrigger{
		{Type: TriggerTypeWebhook, Enabled: true},
	}
	def.Constraints = BotConstraints{DailyBudgetCents: 10}
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	mgr.CreateBot(ctx, def)

	// Set state to budget exhausted
	state := BotState{
		DefinitionID:   "budget-bot",
		Status:         BotStatusRunning,
		TodayCostCents: 10,
		TodayDate:      time.Now().Format("2006-01-02"),
	}
	mgr.store.UpdateState(ctx, state)

	payload := map[string]any{"key": "value"}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/bot/budget-bot/trigger", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", w.Code, http.StatusTooManyRequests)
	}
}
