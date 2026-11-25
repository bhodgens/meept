package employee

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
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

// webhookMaxInFlight caps concurrently running detached webhook rounds
// (bughunt L11). An unauthenticated burst (RequireAuth=false) used to spawn
// one minutes-long round per POST with no ceiling, all serializing behind
// per-employee invoke mutexes. Past the cap the handler answers 503 +
// Retry-After — an explicit refusal, never a silent queue.
const webhookMaxInFlight = 8

// webhookRetryAfter is the Retry-After advertised on a saturated 503. Rounds
// are minutes-long, so a short retry is a hint, not a promise; the client
// that matters (an alerting webhook) sees the refusal and can back off.
const webhookRetryAfter = "5"

// webhookGate holds the per-manager concurrency cap and the in-flight round
// WaitGroup (bughunt L11). Manager fields live in manager.go (off-limits to
// this fix), so the gate lives in this file-local registry keyed by Manager
// pointer — the same shape cron_trigger.go's cronClaims registry uses:
// one Manager per process, confined to this file's helpers.
type webhookGate struct {
	sem    chan struct{}
	rounds sync.WaitGroup
}

var (
	webhookGatesMu sync.Mutex
	webhookGates   = make(map[*Manager]*webhookGate)
)

func webhookGateFor(m *Manager) *webhookGate {
	webhookGatesMu.Lock()
	defer webhookGatesMu.Unlock()
	gate, ok := webhookGates[m]
	if !ok {
		gate = &webhookGate{sem: make(chan struct{}, webhookMaxInFlight)}
		webhookGates[m] = gate
	}
	return gate
}

// drainWebhookRounds waits up to d for every detached webhook round to
// leave the gate. StopAll's shutdown cancellation makes the rounds fail
// fast, so a bounded drain proves nothing leaked. Returns false on timeout.
// Exported to same-package pins only (this file's seam).
func (m *Manager) drainWebhookRounds(d time.Duration) bool {
	gate := webhookGateFor(m)
	done := make(chan struct{})
	go func() {
		gate.rounds.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(d):
		return false
	}
}

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

	// L11 (1): the round is tied to the manager's shutdown context — the
	// same seam 7b7ab262 threads into fireCronTrigger — so StopAll past its
	// drain bound cancels it instead of letting it run to a dead backend,
	// and a POST landing after shutdown fails fast without spawning.
	roundCtx := m.ShutdownContext()
	if roundCtx.Err() != nil {
		logger := m.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("webhook trigger refused: manager shut down", "bot_id", def.ID)
		http.Error(w, `{"error":"shutting down"}`, http.StatusServiceUnavailable)
		return
	}

	// L11 (2): bounded in-flight cap. Past webhookMaxInFlight concurrent
	// rounds the request is REFUSED out loud (503 + Retry-After) rather than
	// queued silently behind minutes-long rounds.
	gate := webhookGateFor(m)
	select {
	case gate.sem <- struct{}{}:
	default:
		logger := m.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("webhook trigger refused: in-flight cap reached", "bot_id", def.ID, "cap", webhookMaxInFlight)
		w.Header().Set("Retry-After", webhookRetryAfter)
		http.Error(w, `{"error":"webhook rounds busy"}`, http.StatusServiceUnavailable)
		return
	}

	m.logWebhook(def.ID, len(body))

	// L11 (3): the round is tracked in the gate's WaitGroup so a drain
	// (drainWebhookRounds) can prove none leaked past shutdown.
	gate.rounds.Add(1)
	go func() {
		defer gate.rounds.Done()
		defer func() { <-gate.sem }()
		ctx, cancel := context.WithTimeout(roundCtx, webhookTriggerTimeout)
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
