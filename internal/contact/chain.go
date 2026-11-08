package contact

// Fallback chain runner (contract C2). One employee action resolves to
// an ordered channel walk: each registered ChannelAttempt is tried in
// policy order; err == nil (CONNECTED/DELIVERED) is terminal for the
// chain - a connected voice call proceeds on its own session and is NOT
// a chain failure; every named failure outcome (no_answer, busy,
// unavailable, failed) advances to the next channel. Every attempt of a
// run shares ONE chain execution id (pkg/id.Generate), written to the
// effects ledger + audit log so consumers can dedupe notifications and
// read honest per-attempt outcomes.

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/pkg/id"
)

// Named attempt outcomes (C2 vocabulary; the channel adapters return
// exactly these strings).
const (
	OutcomeAnswered    = "answered"
	OutcomeDelivered   = "delivered"
	OutcomeNoAnswer    = "no_answer"
	OutcomeUnavailable = "unavailable"
	OutcomeBusy        = "busy"
	OutcomeFailed      = "failed"
)

// isTerminalOutcome reports whether an attempt outcome ENDS the chain
// (the channel connected/delivered). Everything else falls through.
func isTerminalOutcome(outcome string) bool {
	return outcome == OutcomeAnswered || outcome == OutcomeDelivered
}

// ChannelAttempt is the seam every channel implements (C2). Attempt is
// synchronous for message channels and call-attempt-shaped for voice
// (returns at terminal/no-answer, not after a full conversation).
type ChannelAttempt interface {
	Channel() Channel
	Attempt(ctx context.Context, rec Record, message string) (outcome string, err error)
}

// AttemptRecord is one per-attempt row of the chain report (C2 shape:
// {channel, outcome, err, duration_ms} plus the shared chain id).
type AttemptRecord struct {
	Channel    Channel `json:"channel"`
	Outcome    string  `json:"outcome"`
	Err        string  `json:"err,omitempty"`
	DurationMS int64   `json:"duration_ms"`
	ChainID    string  `json:"chain_id"`
}

// ChainReport is one chain run's result: the shared execution id and the
// per-attempt rows in run order (skipped/unconfigured channels are
// absent, never padded).
type ChainReport struct {
	ChainID  string          `json:"chain_id"`
	Attempts []AttemptRecord `json:"attempts"`
}

// ChainSinks carries the ledger + audit seams a ChainRunner writes
// through (T4): ONE execution row before the first attempt and one
// attempt row after each attempt, every row keyed by ChainID. Nil
// sinks degrade to no rows (the report still carries the id).
type ChainSinks struct {
	Effects effects.Ledger
	Audit   *auditlog.Store
}

// ChainRunner walks the ordered channel policy. attempts maps each
// channel to its adapter; delays maps each channel to the wait honored
// BEFORE attempting that channel (the fallback hop delay - tests inject
// short clocks). nil maps are valid.
type ChainRunner struct {
	attempts map[Channel]ChannelAttempt
	delays   map[Channel]time.Duration
	sinks    ChainSinks
	logger   *slog.Logger
}

// NewChainRunner constructs the runner (C2 pinned signature). delays
// may be nil (no inter-channel delay). Use WithSinks to attach the
// effects/audit writers.
func NewChainRunner(attempts map[Channel]ChannelAttempt, delays map[Channel]time.Duration) *ChainRunner {
	return &ChainRunner{
		attempts: attempts,
		delays:   delays,
		logger:   slog.Default(),
	}
}

// WithSinks attaches the effects/audit seams and returns the runner for
// chaining. Nil sinks (the default) write nothing.
func (c *ChainRunner) WithSinks(sinks ChainSinks) *ChainRunner {
	c.sinks = sinks
	return c
}

// WithLogger overrides the default logger.
func (c *ChainRunner) WithLogger(logger *slog.Logger) *ChainRunner {
	if logger != nil {
		c.logger = logger
	}
	return c
}

// chainAttempt reads the attempts map nil-safely.
func (c *ChainRunner) chainAttempt(ch Channel) ChannelAttempt {
	if c == nil || c.attempts == nil {
		return nil
	}
	return c.attempts[ch]
}

// trackAttempt is the chain's own per-attempt bookkeeping wrapper: it
// times one Attempt call and captures the outcome/error into an
// AttemptRecord.
type trackAttempt struct {
	inner ChannelAttempt
}

func (t *trackAttempt) Channel() Channel { return t.inner.Channel() }

// Run executes the chain for one record (C2 pinned signature): override
// replaces the record policy for THIS run only (nil/empty = record
// policy). One chain id spans every attempt of the run. Fail-soft: a
// policy channel with no registered attempt is skipped, never a
// run-abort.
func (c *ChainRunner) Run(ctx context.Context, rec Record, message string, override []Channel) ChainReport {
	chainID := id.Generate("chain-")
	policy := override
	if len(policy) == 0 {
		policy = rec.Policy
	}

	// The execution row lands BEFORE the first attempt (T4): one row per
	// run, keyed by the shared chain id.
	c.writeExecutionRow(ctx, chainID, rec, message, policy)

	report := ChainReport{ChainID: chainID}
	for _, ch := range policy {
		if ctx.Err() != nil {
			c.logger.Info("contact chain: context cancelled mid-run",
				"chain_id", chainID, "contact", rec.ID, "channel", string(ch))
			break
		}
		attempt := c.chainAttempt(ch)
		if attempt == nil {
			// Unconfigured channel: skipped (fail-soft), reported nowhere.
			c.logger.Debug("contact chain: channel not registered, skipped",
				"chain_id", chainID, "channel", string(ch))
			continue
		}
		// Per-channel hop delay BEFORE the attempt (the fallback pacing;
		// the first channel with a configured delay also waits - the map
		// is the policy, not an inter-attempt-only rule). The attempt's
		// duration row INCLUDES the wait (duration_ms = delay + attempt,
		// the observable the gate pins).
		started := time.Now()
		if d := c.delays[ch]; d > 0 {
			if err := sleepCtx(ctx, d); err != nil {
				c.logger.Info("contact chain: delay cancelled",
					"chain_id", chainID, "channel", string(ch))
				break
			}
		}
		record := c.runAttempt(ctx, chainID, attempt, ch, rec, message, started)
		report.Attempts = append(report.Attempts, record)
		c.writeAttemptRow(ctx, chainID, rec, message, record)
		if isTerminalOutcome(record.Outcome) {
			// CONNECTED/DELIVERED: the chain stops. A connected voice
			// call proceeds on its own session; the chain does not wait.
			break
		}
	}
	c.logger.Info("contact chain: run complete",
		"chain_id", chainID, "contact", rec.ID, "attempts", len(report.Attempts))
	return report
}

// runAttempt times one channel attempt and shapes its AttemptRecord.
// err != nil means NOT connected/delivered (C2); the outcome string is
// still honored (adapters return the named failure outcome with the
// error). A nil-outcome failure records "failed".
func (c *ChainRunner) runAttempt(ctx context.Context, chainID string, attempt ChannelAttempt, ch Channel, rec Record, message string, started time.Time) AttemptRecord {
	outcome, err := attempt.Attempt(ctx, rec, message)
	elapsed := time.Since(started)
	record := AttemptRecord{
		Channel:    ch,
		Outcome:    outcome,
		DurationMS: elapsed.Milliseconds(),
		ChainID:    chainID,
	}
	if err != nil {
		record.Err = err.Error()
		if record.Outcome == "" {
			record.Outcome = OutcomeFailed
		}
		c.logger.Info("contact chain: attempt failed",
			"chain_id", chainID, "channel", string(ch), "outcome", record.Outcome, "err", err)
	} else {
		c.logger.Info("contact chain: attempt connected",
			"chain_id", chainID, "channel", string(ch), "outcome", record.Outcome)
	}
	return record
}

// sleepCtx waits d, aborting early on ctx cancellation (the injected
// delay seam; tests use short durations, production uses
// [contact].hop_delay_ms resolved by the wiring leaf).
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// ---- effects/audit rows (T4) ----

// contactChainToolName is the effects `tool` stamp both row kinds carry.
const contactChainToolName = "contact.chain"

// writeExecutionRow writes the ONE chain-execution ledger row before the
// first attempt, keyed by ChainID, plus the audit mirror. Best-effort:
// a sink failure is logged, never fails the run (the channel attempts
// are the user-facing action; telemetry degrades alone).
func (c *ChainRunner) writeExecutionRow(ctx context.Context, chainID string, rec Record, message string, policy []Channel) {
	if c.sinks.Effects == nil && c.sinks.Audit == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"chain_id":   chainID,
		"contact_id": rec.ID,
		"phone":      rec.Phone,
		"policy":     channelsToStrings(policy),
		"message":    message,
	})
	if err != nil {
		c.logger.Warn("contact chain: encode execution payload", "chain_id", chainID, "err", err)
		return
	}
	if c.sinks.Effects != nil {
		key := effects.EffectKey(contactChainToolName, "execution", chainID)
		granted, _, err := c.sinks.Effects.Claim(ctx, key, effects.EffectMeta{
			Tool:      contactChainToolName,
			SessionID: rec.ID,
			Payload:   payload,
		})
		if err != nil {
			c.logger.Warn("contact chain: execution claim", "chain_id", chainID, "err", err)
		} else if !granted {
			// The same chain id claimed twice cannot happen (fresh
			// pkg/id value per run); a non-grant here means a hash
			// collision at best - log loudly and continue.
			c.logger.Warn("contact chain: execution claim not granted", "chain_id", chainID)
		}
	}
	c.writeAudit(ctx, "contact.chain.execution", chainID, rec, payload)
}

// writeAttemptRow writes one attempt ledger row after the attempt,
// keyed by the shared ChainID (the per-attempt effect key hashes the
// channel too so rows never collide), plus the audit mirror.
func (c *ChainRunner) writeAttemptRow(ctx context.Context, chainID string, rec Record, message string, record AttemptRecord) {
	if c.sinks.Effects == nil && c.sinks.Audit == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"chain_id":    chainID,
		"contact_id":  rec.ID,
		"phone":       rec.Phone,
		"channel":     string(record.Channel),
		"outcome":     record.Outcome,
		"err":         record.Err,
		"duration_ms": record.DurationMS,
		"message":     message,
	})
	if err != nil {
		c.logger.Warn("contact chain: encode attempt payload", "chain_id", chainID, "err", err)
		return
	}
	if c.sinks.Effects != nil {
		key := effects.EffectKey(contactChainToolName, "attempt", chainID, string(record.Channel))
		if _, _, err := c.sinks.Effects.Claim(ctx, key, effects.EffectMeta{
			Tool:      contactChainToolName,
			SessionID: rec.ID,
			Payload:   payload,
		}); err != nil {
			c.logger.Warn("contact chain: attempt claim", "chain_id", chainID, "err", err)
		}
	}
	c.writeAudit(ctx, "contact.chain.attempt", chainID, rec, payload)
}

// writeAudit mirrors one row into the audit log (best-effort, same
// posture as the dial audit adapter).
func (c *ChainRunner) writeAudit(ctx context.Context, rowType, chainID string, rec Record, payload []byte) {
	if c.sinks.Audit == nil {
		return
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		c.logger.Warn("contact chain: decode audit payload", "chain_id", chainID, "err", err)
		return
	}
	if _, err := c.sinks.Audit.Append(ctx, auditlog.Record{
		Type:       rowType,
		EmployeeID: rec.ID,
		Payload:    decoded,
	}); err != nil {
		c.logger.Warn("contact chain: audit append", "chain_id", chainID, "type", rowType, "err", err)
	}
}

// channelsToStrings renders a policy for JSON payloads.
func channelsToStrings(policy []Channel) []string {
	out := make([]string, 0, len(policy))
	for _, ch := range policy {
		out = append(out, string(ch))
	}
	return out
}

// ---- test seam ----

// trackAttempt defer: the unexported wrapper above is declared before
// Run uses it; this compile-time line pins the interface.
var _ ChannelAttempt = (*trackAttempt)(nil)

// Attempt implements ChannelAttempt by delegation (timing lives in the
// runner, which calls Attempt directly on registered adapters; the
// wrapper exists for tests that need a labeled pass-through).
func (t *trackAttempt) Attempt(ctx context.Context, rec Record, message string) (string, error) {
	return t.inner.Attempt(ctx, rec, message)
}
