package employee

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

// webhookSourceReflector records the assess prompt so a test can see the
// trigger source and the prompt payload. Same mark the cron gate uses.
type webhookSourceReflector struct {
	mu       sync.Mutex
	contents []string
}

func (r *webhookSourceReflector) Chat(_ context.Context, msgs []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, m := range msgs {
		r.contents = append(r.contents, m.Content)
	}
	return &llm.Response{Content: `{"candidates":[]}`}, nil
}

func (r *webhookSourceReflector) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, _ llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return r.Chat(ctx, msgs, opts...)
}

func (r *webhookSourceReflector) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "webhook-source-stub"}
}

func (r *webhookSourceReflector) joined() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.contents, "\n")
}

func (r *webhookSourceReflector) source() string {
	const mark = "**source:** "
	joined := r.joined()
	i := strings.Index(joined, mark)
	if i < 0 {
		return ""
	}
	rest := joined[i+len(mark):]
	line, _, _ := strings.Cut(rest, "\n")
	return strings.TrimSpace(line)
}

type webhookLogCapture struct {
	mu   sync.Mutex
	text strings.Builder
}

func (h *webhookLogCapture) Enabled(context.Context, slog.Level) bool { return true }

func (h *webhookLogCapture) Handle(_ context.Context, rec slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.text.WriteString(rec.Message)
	rec.Attrs(func(a slog.Attr) bool {
		h.text.WriteByte(' ')
		h.text.WriteString(a.Key)
		h.text.WriteByte('=')
		h.text.WriteString(a.Value.String())
		return true
	})
	h.text.WriteByte('\n')
	return nil
}

func (h *webhookLogCapture) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *webhookLogCapture) WithGroup(string) slog.Handler      { return h }

func (h *webhookLogCapture) String() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.text.String()
}

func newWebhookFixture(t *testing.T, id string, triggers []bot.BotTrigger) (*Manager, *webhookSourceReflector, *http.ServeMux) {
	t.Helper()
	store, err := bot.NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("store close: %v", err)
		}
	})
	botMgr := bot.NewManager(store, nil)
	rec := &webhookSourceReflector{}
	loop := NewGoalLoop(id, &Constitution{Purpose: "webhook sweep"}, nil, slog.New(slog.DiscardHandler))
	loop.WithReflector(rec)
	emp := NewManager(botMgr)
	emp.RegisterGoalLoop(id, loop)
	if err := botMgr.CreateBot(context.Background(), bot.BotDefinition{
		ID:       id,
		Name:     id,
		Prompt:   "handle the hook",
		Enabled:  true,
		Triggers: triggers,
	}); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}
	mux := http.NewServeMux()
	emp.RegisterWebhookRoute(mux)
	return emp, rec, mux
}

// TestWebhookTriggerCallsManager pins the C6 webhook route. The cron gate
// does not cover HTTP.
func TestWebhookTriggerCallsManager(t *testing.T) {
	const (
		botID  = "hook-1"
		secret = "WEBHOOK-SECRET-BODY-do-not-log"
	)
	emp, rec, mux := newWebhookFixture(t, botID, []bot.BotTrigger{{
		Type:           bot.TriggerTypeWebhook,
		Enabled:        true,
		PromptTemplate: "ignored",
	}})
	logs := &webhookLogCapture{}
	emp.logger = slog.New(logs)

	body := `{"secret":"` + secret + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/"+botID, strings.NewReader(body))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := rec.source(); got != "webhook" {
		t.Fatalf("Trigger source = %q, want %q", got, "webhook")
	}
	if !strings.Contains(rec.joined(), secret) {
		t.Fatal("assess prompt did not receive the request body as the prompt payload")
	}
	logged := logs.String()
	if strings.Contains(logged, secret) || strings.Contains(logged, body) {
		t.Fatalf("log contains the raw body:\n%s", logged)
	}
	if !strings.Contains(logged, "bot_id="+botID) {
		t.Fatalf("log missing bot id:\n%s", logged)
	}
	if !strings.Contains(logged, "bytes=") {
		t.Fatalf("log missing byte length:\n%s", logged)
	}

	t.Run("unknown id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/missing", strings.NewReader(secret))
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", w.Code)
		}
		if strings.Contains(logs.String(), secret) {
			t.Fatal("404 path logged the raw body")
		}
	})

	t.Run("disabled trigger", func(t *testing.T) {
		_, _, disabledMux := newWebhookFixture(t, "hook-off", []bot.BotTrigger{{
			Type:    bot.TriggerTypeWebhook,
			Enabled: false,
		}})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/hook-off", strings.NewReader(secret))
		w := httptest.NewRecorder()
		disabledMux.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", w.Code)
		}
	})
}

func TestCronRegistrationDuplicateIsNoop(t *testing.T) {
	store, err := bot.NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	emp := NewManager(bot.NewManager(store, nil))
	def := bot.BotDefinition{
		ID: "nightly",
		Triggers: []bot.BotTrigger{{
			Type:           bot.TriggerTypeCron,
			Schedule:       "0 4 * * *",
			PromptTemplate: "run the nightly sweep",
			Enabled:        true,
		}},
	}
	emp.registerEnabledCronTriggers(context.Background(), def)
	emp.registerEnabledCronTriggers(context.Background(), def)
	if n := len(emp.Cron.Entries()); n != 1 {
		t.Fatalf("cron entries = %d, want 1", n)
	}

	disabled := def
	disabled.ID = "paused-cron"
	disabled.Triggers = []bot.BotTrigger{{
		Type:     bot.TriggerTypeCron,
		Schedule: "0 5 * * *",
		Enabled:  false,
	}}
	before := len(emp.Cron.Entries())
	emp.registerEnabledCronTriggers(context.Background(), disabled)
	if n := len(emp.Cron.Entries()); n != before {
		t.Fatalf("disabled cron registered: entries %d, want %d", n, before)
	}
}
